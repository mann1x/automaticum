package detect

import (
	"sort"
	"strings"
)

// RepetitionGuard is the harness's repetition guard (D135): a model that
// writes the same thing again and again in its visible text, its reasoning
// or the arguments of a call, read as it was written so that the first
// point where the guard would have tripped is known. It observes by
// default: it records that point and changes nothing in the run. Stopping
// a run on it is an arm's choice.
//
// Its rules are the SDK's own or measured, never chosen, and each output
// gets only those that stayed silent there on the runs of healthy models
// (2026-11-03, 202 runs on a client's lane host):
//
//   - visible text: the paragraph rule (reasoning_repetition, the SDK's
//     measureReasoningRepetition, which the SDK applies to reasoning only),
//     restated lines (RestatedText, the SDK's measure of a call's
//     arguments) or the reasoning-loop rule (reasoning_loop, the SDK's
//     reasoningLoopDetection with the run's rule set and what the model was
//     given; a few short lines written a thousand times, which the other
//     two never see). On 399 runs it tripped on two outputs, both written
//     to the output limit (a gate reply of three closing tags 1,002 times,
//     at character 5,960 of 31,535; a base-model answer of one progress
//     line, at 5,346 of 20,466), and on nothing else;
//   - reasoning: the paragraph rule; restated lines also fire on healthy
//     reasoning that quotes code while it works (a run that passed);
//   - arguments: duplicate definitions (DuplicateDefinitionsMax) or
//     restated lines.
const RepetitionGuard = "repetition_guard"

// The rules of the repetition guard.
const (
	RuleParagraph   = "paragraph"
	RuleRestated    = "restated_lines"
	RuleDefinitions = "duplicate_definitions"
	RuleLoop        = "reasoning_loop"
)

// GuardFire is where the repetition guard would first have tripped on one
// output: the rule, how far the output had gone, in characters, of how
// many, and what repeated.
type GuardFire struct {
	Rule   string `json:"rule"`
	Offset int    `json:"offset"`
	Chars  int    `json:"chars"`
	// Count is how often the unit was written by then; Share, for the
	// paragraph rule, the share of paragraphs that repeat an earlier one.
	Count int     `json:"count"`
	Share float64 `json:"share,omitempty"`
	Unit  string  `json:"unit"`
}

// paragraphAt is the shortest prefix, on a grid, on which the paragraph
// rule holds; nil if it never does on the whole text.
func paragraphAt(text string, v Values) *GuardFire {
	repetitionMinChars := v.Int(ThRepMinChars)
	r := []rune(text)
	if reasoningRepetition(r, text, Options{T: v}) == nil {
		return nil
	}
	step := max(2000, len(r)/300)
	for p := min(step, len(r)); ; p = min(p+step, len(r)) {
		if p >= repetitionMinChars || p == len(r) {
			s := string(r[:p])
			if h := reasoningRepetition([]rune(s), s, Options{T: v}); h != nil {
				return &GuardFire{Rule: RuleParagraph, Offset: p, Chars: len(r), Count: h.Count, Share: h.Share, Unit: h.Unit}
			}
		}
		if p == len(r) {
			return nil
		}
	}
}

func restatedAt(text, given string, v Values) *GuardFire {
	x := RestatedTextWith(text, given, v)
	if x == nil {
		return nil
	}
	return &GuardFire{Rule: RuleRestated, Offset: x.Offset, Chars: len([]rune(text)), Count: x.Most, Unit: x.Sample}
}

// loopAt is where the reasoning-loop rule first holds: the rule reads the
// text line by line and stops at the first place it fires.
func loopAt(text string, o Options) *GuardFire {
	h := reasoningLoop([]rune(text), text, o)
	if h == nil {
		return nil
	}
	return &GuardFire{Rule: RuleLoop, Offset: h.Offset, Chars: len([]rune(text)), Count: h.Count, Unit: h.Unit}
}

// first is the fire that came first.
func first(fs ...*GuardFire) *GuardFire {
	var out *GuardFire
	for _, f := range fs {
		if f != nil && (out == nil || f.Offset < out.Offset) {
			out = f
		}
	}
	return out
}

// GuardText is the guard on a block of visible text: given is what the
// model was given before it, o the block's detector options (the run's
// reasoning-loop rules).
func GuardText(text, given string, o Options) *GuardFire {
	return first(paragraphAt(text, o.T), restatedAt(text, given, o.T), loopAt(text, o))
}

// GuardReasoning is the guard on a block of reasoning.
func GuardReasoning(text string, v Values) *GuardFire { return paragraphAt(text, v) }

// GuardArgument is the guard on one string argument of a call.
func GuardArgument(text, given string, v Values) *GuardFire {
	return first(definitionsAt(text, v), restatedAt(text, given, v))
}

// definitionsAt is where a string first held more than
// DuplicateDefinitionsMax definitions of names already defined in their
// scope.
func definitionsAt(text string, v Values) *GuardFire {
	DuplicateDefinitionsMax := v.Int(ThDuplicateMax)
	if total, names := definitionCount(Definitions(text)); total-names <= DuplicateDefinitionsMax {
		return nil
	}
	// The count only grows with the text: the first line where it passes.
	lines := strings.SplitAfter(text, "\n")
	lo, hi := 1, len(lines)
	for lo < hi {
		mid := (lo + hi) / 2
		if total, names := definitionCount(Definitions(strings.Join(lines[:mid], ""))); total-names > DuplicateDefinitionsMax {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	prefix := strings.Join(lines[:lo], "")
	defs := Definitions(prefix)
	total, names := definitionCount(defs)
	top := make([]string, 0, len(defs))
	for n, c := range defs {
		if c > 1 {
			top = append(top, n)
		}
	}
	sort.Slice(top, func(a, b int) bool {
		return defs[top[a]] > defs[top[b]] || defs[top[a]] == defs[top[b]] && top[a] < top[b]
	})
	f := &GuardFire{Rule: RuleDefinitions, Offset: len([]rune(prefix)), Chars: len([]rune(text)), Count: total - names}
	if len(top) > 0 {
		f.Unit = top[0]
	}
	return f
}

func definitionCount(defs map[string]int) (total, names int) {
	for _, c := range defs {
		total += c
	}
	return total, len(defs)
}
