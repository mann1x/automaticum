package detect

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Every number a detector or a measure decides with is a Threshold (D148,
// the owner, 2026-11-12: "the thresholds comes from the SDK but as
// defaults. automaticum must be flexible so it needs to have the
// thresholds customizable"). Its default is the SDK's own value where the
// SDK has the rule, else the owner's, and says which; an installation may
// override it in automaticum.toml ([thresholds]) and one `detect` command
// with --threshold. Whatever reads with thresholds records the effective
// values and where each came from, so results read at different values
// are never merged unseen.

// Threshold is one number, its default and where the default comes from.
type Threshold struct {
	Name    string  `json:"name"`
	Default float64 `json:"default"`
	// Integer says the value is a count: an override must be a whole
	// number.
	Integer bool `json:"integer,omitempty"`
	// Min is the smallest value an override may take (a count the
	// detector divides by must be 1 or more); zero by default.
	Min float64 `json:"min,omitempty"`
	// Origin is "sdk" (the SDK's own value for the same rule) or "owner"
	// (the SDK has no such rule; the owner set it, or a document or
	// decision of the owner's did).
	Origin string `json:"origin"`
	// Source says exactly where the default comes from.
	Source string `json:"source"`
	Doc    string `json:"doc"`
}

// The reasoning-block repeat measure (D148): the most repeats of one unit
// in a block. The SDK has no rule that counts a block's repeats this way
// (its reasoning-loop guard counts cycles of consecutive lines, its
// argument guard third copies), so every number of it is the owner's, as
// the client's loopscan used them on 2026-11-11.
const (
	BlockMaxRepeat   = "reasoning_block.max_repeat"
	BlockUnitChars   = "reasoning_block.unit_min_chars"
	BlockKeyChars    = "reasoning_block.key_chars"
	BlockKeyMinChars = "reasoning_block.key_min_chars"
)

const ownerLoopscan = "owner 2026-11-12 (D148): the client's loopscan (bm-889, 2026-11-11); the SDK has no rule that counts one unit's repeats in a block"

// The sources of the defaults, by group: the SDK's code for its own guards
// (D75), the owner's documents and decisions for the rest.
const (
	srcRequirements = "owner: the requirements' reference pattern (omnimergekit evaluation requirements 0.1, 070 §3); the SDK has no such detector"
	srcCallsSDK     = "SDK packages/core/src/runtime/safety/loop-detection.ts, tags v4.100.250 and v4.100.251: identical calls in a row (D75)"
	srcGuards250    = "SDK reasoning guards of @cerebriline/agents, tags v4.100.250 and v4.100.251, confirmed equal by the SDK's session (D75); the same numbers through 4.100.269"
	srcGuards258    = "SDK reasoning-loop guard of @cerebriline/agents 4.100.258 to 4.100.269 (class behind gt/me): the line-cycle window and the block cycle"
	srcRestated     = "SDK tool-argument loop guard RestatedLines, 4.100.258 on (set by the SDK against 3,890 stored calls of 2,000 characters or more)"
	srcCallGuard    = "SDK loop guard on tool calls, @cerebriline/core 4.100.269 (class Bu of runtime/safety/loop-detection, D129)"
	srcStruggle     = "SDK struggle detector, @cerebriline/core class Pa and observer Tl, the same numbers 4.100.250 to 4.100.269 (D126)"
)

// Names of the detectors' thresholds (D148).
const (
	ThUnitMinChars       = "short_unit_repeat.min_chars"
	ThUnitMaxChars       = "short_unit_repeat.max_chars"
	ThUnitRepeats        = "short_unit_repeat.repeats"
	ThUnitLetters        = "short_unit_repeat.letters"
	ThRunLength          = "char_run.length"
	ThSentenceChars      = "sentence_repeat.min_chars"
	ThSentenceLetters    = "sentence_repeat.letters"
	ThSentenceCount      = "sentence_repeat.count"
	ThSentenceShare      = "sentence_repeat.share"
	ThSentenceAlways     = "sentence_repeat.always"
	ThCallsSoft          = "repeated_tool_call.soft"
	ThCallsHard          = "repeated_tool_call.hard"
	ThRepMinChars        = "reasoning_repetition.min_chars"
	ThRepParagraph       = "reasoning_repetition.paragraph_chars"
	ThRepStopwords       = "reasoning_repetition.stopwords"
	ThRepMaxRepeat       = "reasoning_repetition.max_repeat"
	ThRepDuplicateShare  = "reasoning_repetition.duplicate_share"
	ThLoopMinChars       = "reasoning_loop.min_chars"
	ThLoopWindow         = "reasoning_loop.window"
	ThLoopMaxUnique      = "reasoning_loop.max_unique"
	ThLoopMaxPeriod      = "reasoning_loop.max_period"
	ThLoopMinCycles      = "reasoning_loop.min_cycles"
	ThLoopLineChars      = "reasoning_loop.phrase_line_chars"
	ThLoopPhrasePeriod   = "reasoning_loop.phrase_max_period"
	ThLoopPhraseCycles   = "reasoning_loop.phrase_min_cycles"
	ThLoopCycleWindow    = "reasoning_loop.cycle_window"
	ThLoopBlockMinPeriod = "reasoning_loop.block_min_period"
	ThLoopBlockMaxPeriod = "reasoning_loop.block_max_period"
	ThLoopBlockCycles    = "reasoning_loop.block_min_cycles"
	ThLoopBlockChars     = "reasoning_loop.block_min_chars"
	ThRestatedLineChars  = "restated_lines.min_line_chars"
	ThRestatedMinLines   = "restated_lines.min_lines"
	ThRestatedShare      = "restated_lines.share"
	ThRestatedRecheck    = "restated_lines.recheck_lines"
	ThRefusedHard        = "call_guard.refused_hard"
	ThFailedHard         = "call_guard.failed_hard"
	ThSameResultSoft     = "call_guard.same_result_soft"
	ThSameResultHard     = "call_guard.same_result_hard"
	ThStruggleWindow     = "struggle.window"
	ThStruggleFailed     = "struggle.failed_calls"
	ThStruggleDistress   = "struggle.distress_hits"
	ThStruggleMinTurn    = "struggle.min_iteration"
	ThStruggleMaxPerTask = "struggle.max_per_task"
	ThStruggleEdits      = "struggle.edit_streak"
	ThStruggleTxns       = "struggle.failed_transactions"
	ThDuplicateMax       = "duplicate_definitions.max"
	ThCapTolerance       = "capped.tolerance_tokens"
	ThCanaryRequests     = "canary.requests"
	ThReloadSeconds      = "model_reloaded.min_load_seconds"
)

var thresholds = []Threshold{
	{Name: ThUnitMinChars, Default: unitMin, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "a short unit is at least this many characters"},
	{Name: ThUnitMaxChars, Default: unitMax, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "a short unit is at most this many characters"},
	{Name: ThUnitRepeats, Default: unitRepeats, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "adjacent repeats of a short unit that fire"},
	{Name: ThUnitLetters, Default: unitLetters, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "letters a short unit must hold"},
	{Name: ThRunLength, Default: runLength, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "one letter this many times in a row fires char_run"},
	{Name: ThSentenceChars, Default: sentenceChars, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "a sentence counts from this many characters"},
	{Name: ThSentenceLetters, Default: sentenceLetters, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "letters a counted sentence must hold"},
	{Name: ThSentenceCount, Default: sentenceCount, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "repeats of one sentence that fire with the share"},
	{Name: ThSentenceShare, Default: sentenceShare, Origin: "owner", Source: srcRequirements, Doc: "share of sentences that repeat, with the count"},
	{Name: ThSentenceAlways, Default: sentenceAlways, Integer: true, Origin: "owner", Source: srcRequirements, Doc: "repeats of one sentence that fire whatever the share"},
	{Name: ThCallsSoft, Default: RepeatedCallsSoft, Integer: true, Origin: "sdk", Source: srcCallsSDK, Doc: "identical calls in a row from which the SDK warns (a sequence is reported)"},
	{Name: ThCallsHard, Default: RepeatedCallsHard, Integer: true, Origin: "sdk", Source: srcCallsSDK, Doc: "identical calls in a row at which the SDK escalates"},
	{Name: ThRepMinChars, Default: repetitionMinChars, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "reasoning repetition judges a block from this many characters"},
	{Name: ThRepParagraph, Default: repetitionParagraph, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "a paragraph counts from this many characters"},
	{Name: ThRepStopwords, Default: repetitionStopwords, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "common words a paragraph needs to be a sentence, not a bare quote"},
	{Name: ThRepMaxRepeat, Default: repetitionMaxRepeat, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "occurrences of the most repeated paragraph that fire"},
	{Name: ThRepDuplicateShare, Default: repetitionDuplicateFrac, Origin: "sdk", Source: srcGuards250, Doc: "share of duplicates among the counted paragraphs that fire"},
	{Name: ThLoopMinChars, Default: loopMinChars, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "the reasoning-loop guard judges nothing before this much of the block"},
	{Name: ThLoopWindow, Default: loopWindow, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "lines looked at for low uniqueness"},
	{Name: ThLoopMaxUnique, Default: loopMaxUnique, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "distinct lines in a full window at or under which low-uniqueness fires"},
	{Name: ThLoopMaxPeriod, Default: loopMaxPeriod, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "lines in a line cycle, at most"},
	{Name: ThLoopMinCycles, Default: loopMinCycles, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "repeats of a line cycle that fire"},
	{Name: ThLoopLineChars, Default: loopMinLineChars, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "an unbroken line from this length is read for a phrase cycle"},
	{Name: ThLoopPhrasePeriod, Default: loopPhraseMaxPeriod, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "characters in a phrase cycle, at most"},
	{Name: ThLoopPhraseCycles, Default: loopPhraseMinCycles, Integer: true, Origin: "sdk", Source: srcGuards250, Doc: "repeats of a phrase cycle that fire"},
	{Name: ThLoopCycleWindow, Default: loopCycleWindow, Integer: true, Origin: "sdk", Source: srcGuards258, Doc: "lines looked at for a line cycle (4.100.258 on)"},
	{Name: ThLoopBlockMinPeriod, Default: loopBlockMinPeriod, Integer: true, Origin: "sdk", Source: srcGuards258, Doc: "lines in a cycling block, at least"},
	{Name: ThLoopBlockMaxPeriod, Default: loopBlockMaxPeriod, Integer: true, Origin: "sdk", Source: srcGuards258, Doc: "lines in a cycling block, at most"},
	{Name: ThLoopBlockCycles, Default: loopBlockMinCycles, Integer: true, Origin: "sdk", Source: srcGuards258, Doc: "repeats of a block of lines that fire"},
	{Name: ThLoopBlockChars, Default: loopBlockMinChars, Integer: true, Origin: "sdk", Source: srcGuards258, Doc: "characters of one cycling block, at least"},
	{Name: ThRestatedLineChars, Default: restatedMinLineChars, Integer: true, Origin: "sdk", Source: srcRestated, Doc: "a long line is at least this many characters"},
	{Name: ThRestatedMinLines, Default: restatedMinLines, Integer: true, Origin: "sdk", Source: srcRestated, Doc: "long lines a text must have before it is judged"},
	{Name: ThRestatedShare, Default: restatedFraction, Origin: "sdk", Source: srcRestated, Doc: "share of long lines that are third copies or more, that fires"},
	{Name: ThRestatedRecheck, Default: restatedRecheckLines, Integer: true, Origin: "sdk", Source: srcRestated, Doc: "more long lines before a text the source excused is asked again"},
	{Name: ThRefusedHard, Default: RefusedHard, Integer: true, Origin: "sdk", Source: srcCallGuard, Doc: "repeats of a refused call at which it is escalated"},
	{Name: ThFailedHard, Default: FailedHard, Integer: true, Origin: "sdk", Source: srcCallGuard, Doc: "failures of a call before it is escalated"},
	{Name: ThSameResultSoft, Default: SameResultSoft, Integer: true, Origin: "sdk", Source: srcCallGuard, Doc: "same results in a row, the next call included, that are warned on"},
	{Name: ThSameResultHard, Default: SameResultHard, Integer: true, Origin: "sdk", Source: srcCallGuard, Doc: "the same, for a writing tool, that escalate"},
	{Name: ThStruggleWindow, Default: StruggleWindow, Integer: true, Origin: "sdk", Source: srcStruggle, Doc: "turns the struggle detector looks back over"},
	{Name: ThStruggleFailed, Default: StruggleFailedCalls, Integer: true, Origin: "sdk", Source: srcStruggle, Doc: "failed or refused calls in the window that signal"},
	{Name: ThStruggleDistress, Default: StruggleDistressHits, Integer: true, Origin: "sdk", Source: srcStruggle, Doc: "times the reasoning said it was stuck, in the window, that signal"},
	{Name: ThStruggleMinTurn, Default: StruggleMinIteration, Integer: true, Origin: "sdk", Source: srcStruggle, Doc: "the turn from which the detector may suggest an escalation"},
	{Name: ThStruggleMaxPerTask, Default: StruggleMaxPerTask, Integer: true, Origin: "sdk", Source: srcStruggle, Doc: "escalations suggested per task, at most"},
	{Name: ThStruggleEdits, Default: StruggleEditStreak, Integer: true, Min: 1, Origin: "sdk", Source: srcStruggle, Doc: "file-changing calls failed in a row on which it nudges"},
	{Name: ThStruggleTxns, Default: StruggleFailedTransactions, Integer: true, Min: 1, Origin: "sdk", Source: srcStruggle, Doc: "attempts thrown away on which it nudges"},
	{Name: ThCapTolerance, Default: 16, Integer: true, Origin: "owner", Source: "owner: omnimergekit's number (070 §5): a server may stop a few tokens early", Doc: "tokens short of a declared limit at which a completion still counts as capped"},
	{Name: ThCanaryRequests, Default: 5, Integer: true, Min: 1, Origin: "owner", Source: "owner D84 (050 §3.3)", Doc: "finished requests of a batch the first-generations canary judges"},
	{Name: ThReloadSeconds, Default: 1, Origin: "owner", Source: "owner 2026-11-12 (D148): \"keep 1 second, unless it's set manually with a proper value in edge cases, there's no safe default value which is better than 1 second\" (a load takes from under a second to minutes with the model's size; in memory it answers in milliseconds)", Doc: "load time in seconds from which a request counts as a model reload"},
	{Name: ThDuplicateMax, Default: DuplicateDefinitionsMax, Integer: true, Origin: "owner", Source: "owner D134 (2026-11-03, \"build it with your recommendation\"): measured over 371 base-model and stub runs; the SDK has no such rule", Doc: "definitions of one name in its class, in one string argument, above which it fires"},
	{Name: BlockMaxRepeat, Default: 20, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a reasoning block whose most repeated unit is written this many times or more is a loop block"},
	{Name: BlockUnitChars, Default: 40, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a unit (a line, or a sentence ended by . ! or ?) counts from this many characters, spaces at its ends left out"},
	{Name: BlockKeyChars, Default: 80, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a unit is compared by its first this many letters and digits, lower case, so a line re-quoted with small edits stays one unit"},
	{Name: BlockKeyMinChars, Default: 20, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a unit with fewer letters and digits than this is not counted"},
}

// Thresholds lists every threshold, by name.
func Thresholds() []Threshold {
	out := slices.Clone(thresholds)
	slices.SortFunc(out, func(a, b Threshold) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func lookup(name string) (Threshold, bool) {
	for _, t := range thresholds {
		if t.Name == name {
			return t, true
		}
	}
	return Threshold{}, false
}

// Values are overrides of thresholds, by name; a name not in it has its
// default. The zero value is every default.
type Values map[string]float64

// Get is a threshold's value. An unknown name is a programming error.
func (v Values) Get(name string) float64 {
	if x, ok := v[name]; ok {
		return x
	}
	t, ok := lookup(name)
	if !ok {
		panic("detect: no threshold " + name)
	}
	return t.Default
}

// Int is a count threshold's value.
func (v Values) Int(name string) int { return int(v.Get(name)) }

// Check says what is wrong with overrides: a name no threshold has, a
// count that is not whole, a value below zero.
func (v Values) Check() error {
	var bad []string
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range v {
			if !yield(k) {
				return
			}
		}
	}) {
		t, ok := lookup(name)
		x := v[name]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("there is no threshold %q", name))
		case x < 0 || x < t.Min:
			bad = append(bad, fmt.Sprintf("threshold %s must be %v or more, not %v", name, t.Min, x))
		case t.Integer && x != float64(int(x)):
			bad = append(bad, fmt.Sprintf("threshold %s is a count, not %v", name, x))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%s (the thresholds are %s)", strings.Join(bad, "; "), strings.Join(Names(), ", "))
	}
	return nil
}

// Names lists the thresholds' names.
func Names() []string {
	var out []string
	for _, t := range Thresholds() {
		out = append(out, t.Name)
	}
	return out
}

// ParseValues reads overrides written name=value.
func ParseValues(items []string) (Values, error) {
	v := Values{}
	for _, it := range items {
		name, val, ok := strings.Cut(it, "=")
		name = strings.TrimSpace(name)
		x, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if !ok || name == "" || err != nil {
			return nil, fmt.Errorf("threshold %q: write it name=number", it)
		}
		v[name] = x
	}
	return v, v.Check()
}

// Effective is a threshold's value as read, and where the value came
// from: "sdk default", "owner default", "config" (the installation's
// automaticum.toml) or "cli" (the command's --threshold).
type Effective struct {
	Name    string  `json:"name"`
	Value   float64 `json:"value"`
	From    string  `json:"from"`
	Default float64 `json:"default"`
	Source  string  `json:"default_source"`
}

// Resolve lays the command's overrides over the installation's and says
// for every threshold what it is and why. Both must have passed Check.
func Resolve(config, cli Values) (Values, []Effective) {
	merged := Values{}
	var out []Effective
	for _, t := range Thresholds() {
		e := Effective{Name: t.Name, Value: t.Default, From: t.Origin + " default", Default: t.Default, Source: t.Source}
		if x, ok := config[t.Name]; ok {
			e.Value, e.From = x, "config"
			merged[t.Name] = x
		}
		if x, ok := cli[t.Name]; ok {
			e.Value, e.From = x, "cli"
			merged[t.Name] = x
		}
		out = append(out, e)
	}
	return merged, out
}

// BlockRepeat is a reasoning block read for its most repeated unit.
type BlockRepeat struct {
	Units int `json:"units"`
	// Max is how often the most repeated unit was written; Top its key.
	Max int    `json:"max_repeat"`
	Top string `json:"top,omitempty"`
}

// BlockRepeats counts a block's units, as the client's loopscan did: the
// text is cut into lines, and each line after a ".", "!" or "?" followed by
// a space; a unit of BlockUnitChars characters or more is kept, compared
// by its first BlockKeyChars letters and digits in lower case, and left out
// when that key has fewer than BlockKeyMinChars.
func BlockRepeats(text string, v Values) BlockRepeat {
	unitMin, keyLen, keyMin := v.Int(BlockUnitChars), v.Int(BlockKeyChars), v.Int(BlockKeyMinChars)
	counts := map[string]int{}
	var order []string
	n := 0
	add := func(u string) {
		u = strings.TrimSpace(u)
		if len([]rune(u)) < unitMin {
			return
		}
		var k []rune
		for _, c := range strings.ToLower(u) {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				k = append(k, c)
				if len(k) == keyLen {
					break
				}
			}
		}
		if len(k) < keyMin {
			return
		}
		key := string(k)
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
		n++
	}
	for _, line := range strings.Split(text, "\n") {
		r := []rune(line)
		start := 0
		for i := 0; i < len(r); i++ {
			if (r[i] == '.' || r[i] == '!' || r[i] == '?') && i+1 < len(r) && unicode.IsSpace(r[i+1]) {
				add(string(r[start : i+1]))
				start = i + 1
			}
		}
		add(string(r[start:]))
	}
	out := BlockRepeat{Units: n}
	// The first key to reach the most, as Counter.most_common gives it.
	for _, k := range order {
		if counts[k] > out.Max {
			out.Max, out.Top = counts[k], k
		}
	}
	return out
}
