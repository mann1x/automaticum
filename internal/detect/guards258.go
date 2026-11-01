package detect

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The SDK's reasoning-loop guard changed in release 4.100.258, and is the
// same code in every build from there to 4.100.269 (the class behind
// `gt`/`me` of @cerebriline/agents): its line cycles are looked for over
// the last 126 lines instead of the window of 60; a cycle of a block of 7
// to 400 lines repeating 3 times is a new signal ("block-cycle"); and each
// signal is passed over when the repetition is already in what the model
// was given (the request's messages other than its own: the task, the
// tool results), so a model quoting a repetitive file back is not looping.
// RulesFor says which set holds for a build; Rules258 is this one.
const (
	Rules250 = "4.100.250" // releases 4.100.250 to 4.100.257
	Rules258 = "4.100.258" // releases 4.100.258 on

	loopCycleWindow    = 126 // lines looked at for a line cycle (4.100.258 on)
	loopBlockMinPeriod = 7   // lines in a block: "block-cycle"
	loopBlockMaxPeriod = 400 //
	loopBlockMinCycles = 3   //
	loopBlockMinChars  = 200 // characters of one block
	BlockCycle         = "block-cycle"
)

// RulesFor is the reasoning-loop rule set of an SDK release: Rules258 for
// 4.100.258 and later, Rules250 for an earlier or an unknown release.
func RulesFor(release string) string {
	parts := strings.Split(release, ".")
	if len(parts) != 3 {
		return Rules250
	}
	n := make([]int, 3)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return Rules250
		}
		n[i] = v
	}
	switch {
	case n[0] > 4 || (n[0] == 4 && n[1] > 100):
		return Rules258
	case n[0] == 4 && n[1] == 100 && n[2] >= 258:
		return Rules258
	}
	return Rules250
}

var lineNumber = regexp.MustCompile(`^\d+\s*\|\s?`)

// sourceLine is a given line as the guard compares it: trimmed, without a
// leading line number ("12 | ").
func sourceLine(s string) string {
	return strings.TrimSpace(lineNumber.ReplaceAllString(strings.TrimSpace(s), ""))
}

// given is what the model was given, as the guard reads it.
type given struct {
	text  string
	lines []string
	memo  map[string]int
}

func newGiven(text string) *given {
	if text == "" {
		return nil
	}
	g := &given{text: text, memo: map[string]int{}}
	for _, l := range strings.Split(text, "\n") {
		if l = sourceLine(l); l != "" {
			g.lines = append(g.lines, l)
		}
	}
	return g
}

// phraseRun is how many times in a row the phrase stands in the given text,
// at most.
func (g *given) phraseRun(p string) int {
	if g == nil || p == "" {
		return 0
	}
	key := "p\n" + p
	if v, ok := g.memo[key]; ok {
		return v
	}
	best := 0
	for i := strings.Index(g.text, p); i != -1; {
		run, o := 1, i+len(p)
		for strings.HasPrefix(g.text[o:], p) {
			run++
			o += len(p)
		}
		best = max(best, run)
		j := strings.Index(g.text[o:], p)
		if j == -1 {
			break
		}
		i = o + j
	}
	g.memo[key] = best
	return best
}

// blockRun is how many times in a row the block of lines stands in the
// given lines, at most.
func (g *given) blockRun(block []string) int {
	if g == nil {
		return 0
	}
	n := make([]string, len(block))
	for i, l := range block {
		n[i] = sourceLine(l)
	}
	key := "b\n" + strings.Join(n, "\n")
	if v, ok := g.memo[key]; ok {
		return v
	}
	lines, r, best := g.lines, len(n), 0
	for s := 0; s+r <= len(lines); s++ {
		a, u := 0, s
		for u+r <= len(lines) {
			same := true
			for d := 0; d < r; d++ {
				if lines[u+d] != n[d] {
					same = false
					break
				}
			}
			if !same {
				break
			}
			a++
			u += r
		}
		best = max(best, a)
		if a > 1 {
			s = u - r
		}
	}
	g.memo[key] = best
	return best
}

// stretch is the longest run of given lines that are all among set.
func (g *given) stretch(set map[string]bool) int {
	if g == nil {
		return 0
	}
	n := map[string]bool{}
	for l := range set {
		n[sourceLine(l)] = true
	}
	keys := make([]string, 0, len(n))
	for l := range n {
		keys = append(keys, l)
	}
	sort.Strings(keys)
	key := "s\n" + strings.Join(keys, "\n")
	if v, ok := g.memo[key]; ok {
		return v
	}
	best, run := 0, 0
	for _, l := range g.lines {
		if n[l] {
			run++
		} else {
			run = 0
		}
		best = max(best, run)
	}
	g.memo[key] = best
	return best
}

// reasoningLoop258 is the guard of 4.100.258 on, read after the run like
// reasoningLoop: after every line, and inside a line after every character.
// o.Given is what the model was given before the block.
func reasoningLoop258(r []rune, o Options) *Hit {
	if len(r) < loopMinChars {
		return nil
	}
	src := newGiven(o.Given)
	var window, cycle, history, recent []string
	streaks := make([]int, loopBlockMaxPeriod+1)
	for start := 0; start <= len(r); {
		end := start
		for end < len(r) && r[end] != '\n' {
			end++
		}
		seg := r[start:end]
		if h := phraseCycleGiven(seg, start, src); h != nil {
			return h
		}
		if end == len(r) {
			break // a line without its end is not yet a line
		}
		var block *Hit
		if line := strings.TrimSpace(string(seg)); line != "" {
			window = append(window, line)
			if len(window) > loopWindow {
				window = window[1:]
			}
			cycle = append(cycle, line)
			if len(cycle) > loopCycleWindow {
				cycle = cycle[1:]
			}
			history = append(history, line)
			// The block cycle is tracked on every line, from the first.
			recent = append(recent, line)
			if len(recent) > loopBlockMaxPeriod+1 {
				recent = recent[1:]
			}
			i := len(recent)
			for p := loopBlockMinPeriod; p <= loopBlockMaxPeriod && p < i; p++ {
				o := 0
				if recent[i-1] == recent[i-1-p] {
					o = streaks[p] + 1
				}
				streaks[p] = o
				if block == nil && o >= (loopBlockMinCycles-1)*p && runeLen(recent[i-p:]) >= loopBlockMinChars {
					block = &Hit{Detector: ReasoningLoop, Kind: BlockCycle, Period: p, Count: o/p + 1}
				}
			}
		}
		start = end + 1
		if start < loopMinChars {
			continue
		}
		// Low uniqueness, over the window of 60.
		if len(window) == loopWindow {
			distinct, list := map[string]bool{}, []string{}
			for _, l := range window {
				if !distinct[l] {
					distinct[l] = true
					list = append(list, l)
				}
			}
			if len(list) <= loopMaxUnique {
				fires := true
				if src != nil {
					t := 0
					for k := len(history) - 1; k >= 0 && distinct[history[k]]; k-- {
						t++
					}
					fires = t-src.stretch(distinct) >= loopWindow
				}
				if fires {
					return &Hit{Detector: ReasoningLoop, Kind: LowUniqueness, Count: len(list), Unit: cut(strings.Join(list, " / ")), Offset: start}
				}
			}
		}
		// Line cycles, over the last 126 lines.
		if n := len(cycle); n >= loopMaxPeriod*2 {
			for p := 1; p <= loopMaxPeriod; p++ {
				cycles := 0
			back:
				for i := n - p; i-p >= 0; i -= p {
					for k := 0; k < p; k++ {
						if cycle[i+k] != cycle[i-p+k] {
							break back
						}
					}
					cycles++
				}
				if cycles < loopMinCycles {
					continue
				}
				if src != nil {
					h, all := history, 1
				hist:
					for s := len(h) - p; s-p >= 0; s -= p {
						for u := 0; u < p; u++ {
							if h[s+u] != h[s-p+u] {
								break hist
							}
						}
						all++
					}
					if all <= src.blockRun(cycle[n-p:]) {
						continue
					}
				}
				return &Hit{Detector: ReasoningLoop, Kind: LineCycle, Period: p, Count: cycles + 1, Unit: cut(strings.Join(cycle[n-p:], " / ")), Offset: start}
			}
		}
		// A block of lines cycling.
		if block != nil {
			lines := recent[len(recent)-block.Period:]
			if block.Count > src.blockRun(lines) {
				block.Unit, block.Offset = cut(strings.Join(lines, " / ")), start
				return block
			}
		}
	}
	return nil
}

func runeLen(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len([]rune(l))
	}
	return n
}

// phraseCycleGiven is phraseCycle with the given text: a phrase that
// stands in it as many times in a row is passed over, and the next period
// looked at.
func phraseCycleGiven(seg []rune, at int, src *given) *Hit {
	if len(seg) < loopMinLineChars {
		return nil
	}
	var same [loopPhraseMaxPeriod + 1]int
	for e := 0; e < len(seg); e++ {
		for p := 4; p <= loopPhraseMaxPeriod && p <= e; p++ {
			if seg[e] == seg[e-p] {
				same[p]++
			} else {
				same[p] = 0
			}
		}
		if e+1 < loopMinLineChars || at+e+1 < loopMinChars {
			continue
		}
		for p := 4; p <= loopPhraseMaxPeriod; p++ {
			cycles := same[p] / p
			if cycles < loopPhraseMinCycles {
				continue
			}
			unit := string(seg[e+1-p : e+1])
			if cycles+1 <= src.phraseRun(unit) {
				continue
			}
			return &Hit{Detector: ReasoningLoop, Kind: PhraseCycle, Period: p, Count: cycles + 1, Unit: cut(strings.Join(strings.Fields(unit), " ")), Offset: at + e + 1 - (cycles+1)*p}
		}
	}
	return nil
}
