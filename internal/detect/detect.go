// Package detect is the detector library (spec 070 §3): degeneration and
// looping in what a model wrote. It is the one implementation: reports,
// ingest and other projects call it, none re-declares a detector.
//
// A detector looks at one block: the reasoning or the visible text of one
// turn. Each detector's result is kept apart; their OR is derived by whoever
// needs it and is never what is stored.
package detect

import (
	"strings"
	"unicode"
)

// Version is the version of the library's definitions. It changes when any
// detector's answer on some input changes, and findings are then recomputed.
const Version = "2"

// The detectors, by name.
const (
	ShortUnitRepeat  = "short_unit_repeat"
	CharRun          = "char_run"
	SentenceRepeat   = "sentence_repeat"
	Runaway          = "runaway"
	RepeatedToolCall = "repeated_tool_call"
	// The SDK's own reasoning guards, read after the run (guards.go).
	ReasoningRepetition = "reasoning_repetition"
	ReasoningLoop       = "reasoning_loop"
)

// Text lists the detectors that read a block of text, in the order they run.
var Text = []string{ShortUnitRepeat, CharRun, SentenceRepeat, ReasoningRepetition, ReasoningLoop, Runaway}

// The thresholds of the definitions. They are part of the definition: a
// change is a new Version.
const (
	unitMin, unitMax = 2, 40 // short unit, in characters
	unitRepeats      = 8     // adjacent repeats of a short unit
	unitLetters      = 3     // letters the unit itself must hold
	runLength        = 41    // one letter, this many times in a row
	sentenceChars    = 60    // a sentence qualifies from this length
	sentenceLetters  = 3
	sentenceCount    = 3 // fires from this many repeats, with the share
	sentenceShare    = 0.30
	sentenceAlways   = 8 // fires from this many repeats, whatever the share
)

// Options are the settings a caller may declare.
type Options struct {
	// RunawayChars is the size from which a block is a runaway. There is
	// no default: length alone is not a signal (healthy reasoning runs to
	// hundreds of thousands of characters a turn), so the detector reports
	// only where a size is declared (D75).
	RunawayChars int
}

// Hit is one detector firing on one block.
type Hit struct {
	Detector string `json:"detector"`
	// Count is what the detector counted: repeats of the unit, the length
	// of the run, repeats of the sentence, or the block's length.
	Count int `json:"count"`
	// Share is the repeated sentence's share of the qualifying sentences.
	Share float64 `json:"share,omitempty"`
	// Kind and Period are a reasoning loop's: which of its three signals,
	// and the cycle's length in lines or characters.
	Kind   string `json:"kind,omitempty"`
	Period int    `json:"period,omitempty"`
	// Unit is what repeated, cut to a readable length.
	Unit string `json:"unit,omitempty"`
	// Offset is where it starts, in characters from the start of the block.
	Offset int `json:"offset"`
}

// Block runs every text detector on one block and returns the hits, at most
// one per detector: the first place it fires.
func Block(text string, o Options) []Hit {
	r := []rune(text)
	var hits []Hit
	for _, f := range []func([]rune, string, Options) *Hit{shortUnit, charRun, sentence, reasoningRepetition, reasoningLoop, runaway} {
		if h := f(r, text, o); h != nil {
			hits = append(hits, *h)
		}
	}
	return hits
}

// shortUnit: a unit of 2 to 40 characters, repeated 8 or more times one
// after the other, where the unit itself holds 3 or more letters. Without
// the letters rule every firing on code is indentation or a number table.
//
// The unit is the shortest one that repeats at the leftmost place, as the
// pattern (.{2,40}?)\1{7,} finds it: in "abababab..." the unit is "ab", which
// has two letters, and the repeat is passed over whole. A longer unit made
// of the same repeat is not looked for inside it.
func shortUnit(r []rune, _ string, _ Options) *Hit {
	for i := 0; i+unitMin*unitRepeats <= len(r); {
		next := i + 1
		for n := unitMin; n <= unitMax && i+n*unitRepeats <= len(r); n++ {
			reps := 1
			for j := i + n; j+n <= len(r) && equal(r[i:i+n], r[j:j+n]); j += n {
				reps++
			}
			if reps < unitRepeats {
				continue
			}
			if letters(r[i:i+n]) >= unitLetters {
				return &Hit{Detector: ShortUnitRepeat, Count: reps, Unit: string(r[i : i+n]), Offset: i}
			}
			next = i + n*reps
			break
		}
		i = next
	}
	return nil
}

// charRun: one character 41 or more times in a row, only when it is a
// letter. A run of "=" or "-" is a separator.
func charRun(r []rune, _ string, _ Options) *Hit {
	for i := 0; i < len(r); {
		j := i + 1
		for j < len(r) && r[j] == r[i] {
			j++
		}
		if j-i >= runLength && unicode.IsLetter(r[i]) {
			return &Hit{Detector: CharRun, Count: j - i, Unit: string(r[i]), Offset: i}
		}
		i = j
	}
	return nil
}

// sentence: the same sentence, exactly, several times within one block
// (070 §3.1).
func sentence(_ []rune, text string, _ Options) *Hit {
	units := Sentences(text)
	if len(units) < sentenceCount {
		return nil
	}
	count := map[string]int{}
	best := ""
	for _, u := range units {
		count[u]++
		// The first unit to reach the highest count wins a tie.
		if count[u] > count[best] {
			best = u
		}
	}
	n := count[best]
	share := float64(n) / float64(len(units))
	if n >= sentenceCount && (share >= sentenceShare || n >= sentenceAlways) {
		off := strings.Index(text, best)
		return &Hit{Detector: SentenceRepeat, Count: n, Share: share, Unit: cut(best), Offset: len([]rune(text[:max(off, 0)]))}
	}
	return nil
}

// Sentences splits a block into the units the sentence detector compares:
// split on whitespace after ".", "!" or "?" and on newlines, stripped, kept
// when 60 or more characters long with 3 or more letters.
func Sentences(text string) []string {
	var out []string
	keep := func(s string) {
		s = strings.TrimSpace(s)
		if r := []rune(s); len(r) >= sentenceChars && letters(r) >= sentenceLetters {
			out = append(out, s)
		}
	}
	for _, line := range strings.FieldsFunc(text, func(c rune) bool { return c == '\n' || c == '\r' }) {
		r := []rune(line)
		start := 0
		for i := 1; i < len(r); i++ {
			if unicode.IsSpace(r[i]) && (r[i-1] == '.' || r[i-1] == '!' || r[i-1] == '?') {
				keep(string(r[start:i]))
				start = i
			}
		}
		keep(string(r[start:]))
	}
	return out
}

// runaway: a block at or above the declared size; nothing when none is
// declared.
func runaway(r []rune, _ string, o Options) *Hit {
	limit := o.RunawayChars
	if limit > 0 && len(r) >= limit {
		return &Hit{Detector: Runaway, Count: len(r)}
	}
	return nil
}

// Call is one tool call, as the repeated-call detector sees it.
type Call struct {
	Turn int
	Tool string
	// Input is the call's arguments, as the model wrote them.
	Input string
}

// The SDK's own thresholds for identical calls in a row
// (packages/core/src/runtime/safety/loop-detection.ts, as of commit
// daa9edb883): from RepeatedCallsSoft it warns the model, at
// RepeatedCallsHard it escalates (D75). A sequence is reported from the
// first, and is a warning from the second.
const (
	RepeatedCallsSoft = 2
	RepeatedCallsHard = 5
)

// RepeatedCall is the longest sequence of identical calls.
type RepeatedCall struct {
	Tool  string `json:"tool"`
	Input string `json:"input"`
	Count int    `json:"count"`
	Turn  int    `json:"turn"`  // of the first call of the sequence
	Index int    `json:"index"` // of that call among the run's calls
}

// RepeatedCalls finds every sequence of n or more calls in a row with the
// same name and the same arguments; n of 0 means RepeatedCallsSoft.
func RepeatedCalls(calls []Call, n int) []RepeatedCall {
	if n <= 0 {
		n = RepeatedCallsSoft
	}
	var out []RepeatedCall
	for i := 0; i < len(calls); {
		j := i + 1
		for j < len(calls) && calls[j].Tool == calls[i].Tool && calls[j].Input == calls[i].Input {
			j++
		}
		if j-i >= n {
			out = append(out, RepeatedCall{Tool: calls[i].Tool, Input: cut(calls[i].Input), Count: j - i, Turn: calls[i].Turn, Index: i})
		}
		i = j
	}
	return out
}

func equal(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func letters(r []rune) int {
	n := 0
	for _, c := range r {
		if unicode.IsLetter(c) {
			n++
		}
	}
	return n
}

// cut shortens a quoted unit so that evidence stays readable.
func cut(s string) string {
	const keep = 200
	if r := []rune(s); len(r) > keep {
		return string(r[:keep]) + "…"
	}
	return s
}
