package detect

import (
	"fmt"
	"strings"
	"testing"
)

// filler is varied prose, so that no detector fires on it alone.
func filler(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "Step %d: looked at the function and its callers, then at test number %d.\n", i, i*7)
	}
	return b.String()
}

// block is a block of seven distinct lines, over 200 characters.
var block = func() string {
	var b strings.Builder
	for i := 0; i < 7; i++ {
		fmt.Fprintf(&b, "I will edit line %d of the file with the editor and check it afterwards.\n", 280+i)
	}
	return b.String()
}()

// TestReasoningLoop258 pins the guard of 4.100.258 on: a block of lines
// repeating three times fires (block-cycle), not under the older rules;
// the same repetition already in what the model was given does not.
func TestReasoningLoop258(t *testing.T) {
	text := filler(4200) + strings.Repeat(block, 3)
	new := Options{Rules: Rules258}
	h := reasoningLoop([]rune(text), text, new)
	if h == nil || h.Kind != BlockCycle || h.Period != 7 || h.Count != 3 {
		t.Fatalf("block cycle: %+v", h)
	}
	if h := reasoningLoop([]rune(text), text, Options{}); h != nil {
		t.Errorf("the older rules have no block cycle: %+v", h)
	}
	given := new
	given.Given = "the file reads:\n" + strings.Repeat(block, 3)
	if h := reasoningLoop([]rune(text), text, given); h != nil {
		t.Errorf("a block quoted from what was given: %+v", h)
	}
	// Two repeats in the given text excuse two, not three.
	given.Given = strings.Repeat(block, 2)
	if h := reasoningLoop([]rune(text), text, given); h == nil || h.Kind != BlockCycle {
		t.Errorf("more repeats than given: %+v", h)
	}
	// A line cycle is looked for over 126 lines: a period of 5 reaches 20
	// cycles there, never inside the older window of 60. The lines are
	// short, so no block of them reaches 200 characters first, and five
	// distinct lines are not low uniqueness.
	cyc := filler(4200) + strings.Repeat("a1\nb2\nc3\nd4\ne5\n", 22)
	if h := reasoningLoop([]rune(cyc), cyc, new); h == nil || h.Kind != LineCycle || h.Period != 5 {
		t.Errorf("a cycle of five lines: %+v", h)
	}
	if h := reasoningLoop([]rune(cyc), cyc, Options{}); h != nil {
		t.Errorf("the older window of 60 holds no 20 cycles of five: %+v", h)
	}
	// A phrase repeated in an unbroken line, excused when given as often.
	phrase := filler(4200) + strings.Repeat("abc, W", 400)
	if h := reasoningLoop([]rune(phrase), phrase, new); h == nil || h.Kind != PhraseCycle {
		t.Errorf("phrase cycle: %+v", h)
	}
	if h := reasoningLoop([]rune(phrase), phrase, Options{Rules: Rules258, Given: strings.Repeat("abc, W", 500)}); h != nil {
		t.Errorf("a phrase given as often: %+v", h)
	}
}

func TestRulesFor(t *testing.T) {
	for in, want := range map[string]string{"4.100.251": Rules250, "4.100.257": Rules250, "4.100.258": Rules258, "4.100.269": Rules258,
		"4.101.0": Rules258, "5.0.0": Rules258, "": Rules250, "dev": Rules250} {
		if got := RulesFor(in); got != want {
			t.Errorf("RulesFor(%q) = %s, want %s", in, got, want)
		}
	}
}
