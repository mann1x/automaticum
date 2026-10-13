package detect

import (
	"fmt"
	"strings"
	"testing"
)

func TestSelfTest(t *testing.T) {
	results, ok := SelfTest()
	for _, r := range results {
		if !r.OK {
			t.Errorf("control %q (%s): fired %v, want %v", r.Control, r.Domain, r.Got, r.Want)
		}
	}
	if !ok && !t.Failed() {
		t.Error("the self-test failed without a failing control")
	}
	// Every detector has a control it fires on and each domain a healthy one.
	fired, healthy := map[string]bool{}, map[string]bool{}
	for _, r := range results {
		for _, d := range r.Want {
			fired[d] = true
		}
		if len(r.Want) == 0 {
			healthy[r.Domain] = true
		}
	}
	for _, d := range append(append([]string{}, Text...), RepeatedToolCall) {
		if !fired[d] {
			t.Errorf("no positive control for %s", d)
		}
	}
	for _, d := range []string{"prose", "code", "tool output", "tool calls"} {
		if !healthy[d] {
			t.Errorf("no negative control for %s", d)
		}
	}
}

func hit(t *testing.T, text, detector string) *Hit {
	t.Helper()
	for _, h := range Block(text, Options{}) {
		if h.Detector == detector {
			return &h
		}
	}
	return nil
}

func TestShortUnit(t *testing.T) {
	rep := strings.Repeat
	// The edges of the definition.
	if h := hit(t, "x "+rep("abc", 8), ShortUnitRepeat); h == nil || h.Count != 8 || h.Unit != "abc" || h.Offset != 2 {
		t.Errorf("8 repeats of a 3-letter unit: %+v", h)
	}
	if h := hit(t, rep("abc", 7)+"ab", ShortUnitRepeat); h != nil {
		t.Errorf("7 repeats fired: %+v", h)
	}
	// The unit is the shortest that repeats: "ab" has two letters and "a1 "
	// one, so neither fires, however long the repeat.
	if h := hit(t, rep("ab", 30)+" "+rep("a1 ", 40)+rep("a", 39), ShortUnitRepeat); h != nil {
		t.Errorf("a unit with fewer than three letters fired: %+v", h)
	}
	if h := hit(t, rep("ab", 30)+" "+rep("abc ", 8), ShortUnitRepeat); h == nil || h.Unit != " abc" || h.Offset != 60 {
		t.Errorf("a loop after a repeat that is passed over: %+v", h)
	}
	if h := hit(t, rep("12 ", 100)+rep("  ", 100)+rep("=-", 100)+rep("0x00, ", 40), ShortUnitRepeat); h != nil {
		t.Errorf("numbers, indentation and separators fired: %+v", h)
	}
	unit40 := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN"
	if h := hit(t, rep(unit40, 8), ShortUnitRepeat); h == nil || len(h.Unit) != 40 {
		t.Errorf("a 40-character unit: %+v", h)
	}
	if h := hit(t, rep(unit40+"O", 8), ShortUnitRepeat); h != nil {
		t.Errorf("a 41-character unit fired: %+v", h)
	}
	// Characters, not bytes.
	if h := hit(t, "öx"+rep("längé", 8), ShortUnitRepeat); h == nil || h.Offset != 2 || h.Unit != "längé" {
		t.Errorf("characters outside ASCII: %+v", h)
	}
}

// The unit and place chosen must be those of the pattern the definition
// came from, (.{2,40}?)\1{7,} with the letters rule on the unit; these
// answers were produced by that pattern.
func TestShortUnitAgainstPattern(t *testing.T) {
	rep := strings.Repeat
	for _, c := range []struct {
		text, unit   string
		offset, reps int
	}{
		{"x " + rep("abc", 8), "abc", 2, 8},
		{rep("abc", 7) + "ab", "", 0, 0},
		{rep("ab", 30) + rep("abc ", 8), "", 0, 0},
		{"The answer is " + rep("very ", 12) + "clear.", " very", 13, 12},
		{"Let me check. " + rep("I will fix the jump. ", 9), ". I will fix the jump", 12, 9},
	} {
		h := hit(t, c.text, ShortUnitRepeat)
		if c.unit == "" {
			if h != nil {
				t.Errorf("%.30q: fired %+v, the pattern does not", c.text, h)
			}
			continue
		}
		if h == nil || h.Unit != c.unit || h.Offset != c.offset || h.Count != c.reps {
			t.Errorf("%.30q: %+v, the pattern gives %q at %d, %d times", c.text, h, c.unit, c.offset, c.reps)
		}
	}
}

func TestCharRun(t *testing.T) {
	rep := strings.Repeat
	if h := hit(t, "ab"+rep("z", 41), CharRun); h == nil || h.Count != 41 || h.Offset != 2 {
		t.Errorf("41 letters: %+v", h)
	}
	if h := hit(t, rep("z", 40), CharRun); h != nil {
		t.Errorf("40 letters fired: %+v", h)
	}
	if h := hit(t, rep("=", 500)+rep("-", 500)+rep(" ", 500)+rep("7", 500), CharRun); h != nil {
		t.Errorf("a run of something that is not a letter fired: %+v", h)
	}
}

func TestSentence(t *testing.T) {
	s := "This sentence is long enough to qualify because it has more than sixty characters."
	other := func(i int) string {
		return "A different sentence number " + strings.Repeat("i", i+1) + " that also qualifies by being long enough to count."
	}
	if got := Sentences(s + " Short one. " + s + "\n" + other(1) + " 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 12"); len(got) != 3 {
		t.Errorf("qualifying units: %q", got)
	}
	// Three of three: fires.
	if h := hit(t, s+" "+s+"\n"+s, SentenceRepeat); h == nil || h.Count != 3 || h.Share != 1 {
		t.Errorf("three of three: %+v", h)
	}
	// Twice is not enough.
	if h := hit(t, s+" "+s, SentenceRepeat); h != nil {
		t.Errorf("two repeats fired: %+v", h)
	}
	// Three among eleven: under the share, does not fire. Three among ten:
	// exactly the share, fires.
	var b strings.Builder
	for i := 0; i < 8; i++ {
		b.WriteString(other(i) + " ")
	}
	if h := hit(t, b.String()+s+" "+s+" "+s, SentenceRepeat); h != nil {
		t.Errorf("3 of 11 fired: %+v", h)
	}
	b.Reset()
	for i := 0; i < 7; i++ {
		b.WriteString(other(i) + " ")
	}
	if h := hit(t, b.String()+s+" "+s+" "+s, SentenceRepeat); h == nil || h.Share != 0.3 {
		t.Errorf("3 of 10 did not fire: %+v", h)
	}
	// Eight repeats fire whatever the share.
	b.Reset()
	for i := 0; i < 60; i++ {
		b.WriteString(other(i) + " ")
		if i%8 == 0 {
			b.WriteString(s + " ")
		}
	}
	if h := hit(t, b.String(), SentenceRepeat); h == nil || h.Count != 8 || h.Share >= 0.30 {
		t.Errorf("eight repeats: %+v", h)
	}
	// Identity is exact: another case is another sentence.
	if h := hit(t, s+" "+strings.ToUpper(s)+" "+s+" "+strings.ToLower(s), SentenceRepeat); h != nil {
		t.Errorf("sentences that differ in case were taken as one: %+v", h)
	}
}

func TestRunaway(t *testing.T) {
	// No size declared: length alone is never reported (D75).
	if h := hit(t, strings.Repeat("a b ", 100000), Runaway); h != nil {
		t.Errorf("a long block fired with no size declared: %+v", h)
	}
	if got := Block("1234", Options{RunawayChars: 5}); len(got) != 0 {
		t.Errorf("one under the declared size: %+v", got)
	}
	if got := Block("12345", Options{RunawayChars: 5}); len(got) != 1 || got[0].Detector != Runaway {
		t.Errorf("a declared size: %+v", got)
	}
}

func TestRepeatedCalls(t *testing.T) {
	c := func(turn int, tool, in string) Call { return Call{turn, tool, in} }
	calls := []Call{c(1, "a", "1"), c(2, "a", "1"), c(3, "b", "1"), c(4, "b", "1"), c(5, "b", "1"), c(6, "b", "1"), c(7, "b", "2"), c(8, "b", "1")}
	// From the second identical call in a row, as the SDK warns.
	got := RepeatedCalls(calls, 0)
	if len(got) != 2 || got[0].Count != 2 || got[1] != (RepeatedCall{Tool: "b", Input: "1", Count: 4, Turn: 3, Index: 2}) {
		t.Errorf("%+v", got)
	}
	if got := RepeatedCalls(calls, RepeatedCallsHard); len(got) != 0 {
		t.Errorf("four in a row is under where the SDK escalates: %+v", got)
	}
	if got := RepeatedCalls(nil, 0); got != nil {
		t.Errorf("no calls: %+v", got)
	}
}

// The SDK's own guards, on the cases its source names (D75).
func TestReasoningGuards(t *testing.T) {
	para := "Actually, I think I found it! These should all be fine. Let me look more carefully at the test.\n\n"
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "Step %d: the loader reads section %d and it is compared with the list for the level.\n\n", i, i*3)
	}
	clean := b.String()
	// Three repeats: under the count. Four with much new text: under the share.
	if h := hit(t, clean+strings.Repeat(para, 3), ReasoningRepetition); h != nil {
		t.Errorf("three repeats fired: %+v", h)
	}
	if h := hit(t, clean+strings.Repeat(para, 4), ReasoningRepetition); h != nil {
		t.Errorf("four repeats among thirty new paragraphs fired: %+v", h)
	}
	h := hit(t, clean+strings.Repeat(para, 16), ReasoningRepetition)
	if h == nil || h.Count != 16 || h.Share < 0.25 {
		t.Errorf("sixteen repeats: %+v", h)
	}
	// Under 4,000 characters nothing is judged.
	if h := hit(t, strings.Repeat(para, 20), ReasoningRepetition); h != nil {
		t.Errorf("a short block fired: %+v", h)
	}
	// A phrase 12 times in a line is one short of the 13 the guard asks for.
	var nums strings.Builder
	for i := 1000; nums.Len() < 2000; i++ {
		fmt.Fprintf(&nums, "%d ", i*i)
	}
	lead := clean + clean + nums.String()
	if h := hit(t, lead+strings.Repeat("and I'll do it, ", 12), ReasoningLoop); h != nil {
		t.Errorf("twelve times fired: %+v", h)
	}
	h = hit(t, lead+strings.Repeat("and I'll do it, ", 13)+"and then", ReasoningLoop)
	if h == nil || h.Kind != PhraseCycle || h.Period != 16 || h.Count != 13 {
		t.Errorf("thirteen times: %+v", h)
	}
	// 21 identical long lines: an exact cycle before the window is full.
	h = hit(t, strings.Repeat(strings.Repeat("the same line once more ", 9)+"\n", 21), ReasoningLoop)
	if h == nil || h.Kind != LineCycle || h.Period != 1 || h.Count != 21 {
		t.Errorf("one line 21 times: %+v", h)
	}
}

// Every match is the reference: a block that opens with layout and then
// loops is flagged. The older first-match way misses it (D76).
func TestMatching(t *testing.T) {
	for _, text := range []string{strings.Repeat("    ", 12) + strings.Repeat("hello wor ", 10), strings.Repeat("=", 41) + strings.Repeat("a", 41)} {
		if got := Block(text, Options{}); len(got) != 1 {
			t.Errorf("every match on %.20q: %+v", text, got)
		}
		if got := Block(text, Options{FirstMatch: true}); len(got) != 0 {
			t.Errorf("first match on %.20q: %+v", text, got)
		}
	}
	if (Options{}).Matching() != EveryMatch || (Options{FirstMatch: true}).Matching() != FirstMatch {
		t.Error("the options do not name their way of matching")
	}
}
