package detect

import "strings"

// RestatedLines is the SDK's measure of a text that writes its own long
// lines again and again with small changes between the copies, so that no
// block cycle or repeated paragraph is exact: long lines written a third
// time or more. It is ported from the SDK's tool-argument loop guard
// (`RestatedLines`, 4.100.258 on), where it was set against 3,890 stored
// calls of 2,000 characters or more: once a call has 300 long lines, no
// healthy one has more than 6% of them as third copies, and the loops it
// was made for pass 30% within their first 720. A third copy, not a second:
// a call that rewrites a file it read holds each line once more than the
// source. Lines the source already repeats are allowed that many again.
const (
	restatedMinLineChars = 24
	restatedMinLines     = 300
	restatedFraction     = 0.3
	// restatedRecheckLines is how many more lines, after the source excused
	// a text, before it is asked again.
	restatedRecheckLines = 50
)

// Restated is where a text met the measure: how far it had written, in
// characters, its long lines and how many distinct, and the line written
// most often.
type Restated struct {
	Offset   int    `json:"offset"`
	Lines    int    `json:"lines"`
	Distinct int    `json:"distinct"`
	Most     int    `json:"most"`
	Sample   string `json:"sample"`
}

// RestatedText reads a text as the SDK's guard reads a call's arguments
// while they stream, and says where it would first have tripped; nil if
// never. given is what the model was given, the source a restated line is
// excused by.
func RestatedText(text, given string) *Restated {
	counts := map[string]int{}
	var source map[string]int
	lines, excess, next, offset := 0, 0, restatedMinLines, 0
	for _, raw := range strings.SplitAfter(text, "\n") {
		offset += len([]rune(raw))
		if !strings.HasSuffix(raw, "\n") {
			break // a line is counted when it ends
		}
		line := sourceLine(raw)
		if len([]rune(line)) < restatedMinLineChars {
			continue
		}
		counts[line]++
		lines++
		if counts[line] > 2 {
			excess++
		}
		if lines < next || float64(excess) < restatedFraction*float64(lines) {
			continue
		}
		if source == nil {
			source = map[string]int{}
			for _, g := range strings.Split(given, "\n") {
				if l := sourceLine(g); len([]rune(l)) >= restatedMinLineChars {
					source[l]++
				}
			}
		}
		beyond := 0
		for l, n := range counts {
			beyond += max(0, n-max(2, source[l]+1))
		}
		if float64(beyond) < restatedFraction*float64(lines) {
			next = lines + restatedRecheckLines
			continue
		}
		r := &Restated{Offset: offset, Lines: lines, Distinct: len(counts)}
		for l, n := range counts {
			if n > r.Most || n == r.Most && l < r.Sample {
				r.Most, r.Sample = n, l
			}
		}
		r.Sample = cut(r.Sample)
		return r
	}
	return nil
}
