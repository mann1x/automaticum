package detect

import (
	"strings"
	"unicode"
)

// Span is a stretch of a block that repeats what came before it, from the
// second occurrence of the repeated unit to the end of the last, in
// characters (Unicode code points) from the start of the block, the end
// not included. A trainer is handed these as loop spans (D102, D106): the
// first occurrence is ordinary text, what follows it is the loop.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Spans says where a hit's repeats are in the block it was found in. A
// runaway has none: length is not a repeat.
func Spans(text string, h Hit) []Span {
	r := []rune(text)
	switch h.Detector {
	case ShortUnitRepeat:
		n := len([]rune(h.Unit))
		return clip(len(r), Span{h.Offset + n, h.Offset + h.Count*n})
	case CharRun:
		return clip(len(r), Span{h.Offset + 1, h.Offset + h.Count})
	case SentenceRepeat:
		best, _, _ := repeatedSentence(text)
		return later(text, best)
	case ReasoningRepetition:
		sample, _, _ := repeatedParagraph(text)
		return later(text, sample)
	case ReasoningLoop:
		switch h.Kind {
		case PhraseCycle:
			return clip(len(r), Span{h.Offset + h.Period, h.Offset + h.Count*h.Period})
		case LineCycle:
			// The hit is at the start of the line after the cycle.
			if s, ok := CycleStart(r[:min(h.Offset, len(r))], h.Period, h.Count); ok {
				return clip(len(r), Span{s, h.Offset})
			}
		case LowUniqueness:
			if s, ok := WindowRepeatStart(r[:min(h.Offset, len(r))]); ok {
				return clip(len(r), Span{s, h.Offset})
			}
		}
	}
	return nil
}

// line is one line as the SDK's loop guard counts it: trimmed, and where
// its first character that is not a space stands.
type line struct {
	text string
	at   int
}

// guardLines reads a text as the SDK's loop guard does: lines end at "\n"
// and are trimmed, a blank one is not a line, and a last line without its
// "\n" is not yet one.
func guardLines(r []rune) []line {
	var lines []line
	start := 0
	for i, c := range r {
		if c != '\n' {
			continue
		}
		seg := r[start:i]
		if t := strings.TrimSpace(string(seg)); t != "" {
			lead := 0
			for unicode.IsSpace(seg[lead]) {
				lead++
			}
			lines = append(lines, line{t, start + lead})
		}
		start = i + 1
	}
	return lines
}

// CycleStart is where the second of the last cycles repeats of a block of
// period lines begins, the lines read as the SDK's loop guard reads them
// (its "line-cycle" and "block-cycle"). ok is false when the last
// period×cycles lines are not that many repeats of one block.
func CycleStart(r []rune, period, cycles int) (int, bool) {
	lines := guardLines(r)
	n := period * cycles
	if period < 1 || cycles < 2 || len(lines) < n {
		return 0, false
	}
	tail := lines[len(lines)-n:]
	for k := period; k < n; k++ {
		if tail[k].text != tail[k-period].text {
			return 0, false
		}
	}
	return tail[period].at, true
}

// WindowRepeatStart is where, in the last window of lines the loop guard
// looks at, the first line that repeats an earlier one of the window
// begins (its "low-uniqueness").
func WindowRepeatStart(r []rune) (int, bool) {
	lines := guardLines(r)
	if len(lines) < loopWindow {
		return 0, false
	}
	seen := map[string]bool{}
	for _, l := range lines[len(lines)-loopWindow:] {
		if seen[l.text] {
			return l.at, true
		}
		seen[l.text] = true
	}
	return 0, false
}

// later is every occurrence of unit in text but the first.
func later(text, unit string) []Span {
	if unit == "" {
		return nil
	}
	var out []Span
	n := len([]rune(unit))
	from, runes := 0, 0
	for i := 0; ; i++ {
		k := strings.Index(text[from:], unit)
		if k < 0 {
			return out
		}
		runes += len([]rune(text[from : from+k]))
		if i > 0 {
			out = append(out, Span{runes, runes + n})
		}
		runes += n
		from += k + len(unit)
	}
}

// clip keeps a span inside a block of n characters; one left empty is none.
func clip(n int, s Span) []Span {
	s.Start, s.End = max(0, s.Start), min(n, s.End)
	if s.Start >= s.End {
		return nil
	}
	return []Span{s}
}
