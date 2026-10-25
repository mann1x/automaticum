package detect

import (
	"strings"
	"testing"
)

func TestSpansShortUnit(t *testing.T) {
	text := "intro " + strings.Repeat("abc ", 10) + "end"
	hits := Block(text, Options{})
	var h *Hit
	for i := range hits {
		if hits[i].Detector == ShortUnitRepeat {
			h = &hits[i]
		}
	}
	if h == nil {
		t.Fatalf("no short unit hit in %v", hits)
	}
	got := Spans(text, *h)
	// The leftmost unit is " abc", from the space after "intro".
	want := []Span{{len("intro abc"), len("intro") + 40}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("spans %v, want %v", got, want)
	}
}

func TestSpansSentenceLaterOnly(t *testing.T) {
	s := "This sentence is long enough to count as one for the detector here."
	text := s + "\n" + s + "\n" + s + "\n"
	got := later(text, s)
	n := len([]rune(s))
	if len(got) != 2 || got[0] != (Span{n + 1, 2*n + 1}) || got[1] != (Span{2*n + 2, 3*n + 2}) {
		t.Fatalf("spans %v", got)
	}
}

func TestCycleStart(t *testing.T) {
	block := "first line\n  second line\nthird\n"
	text := "lead in\n\n" + block + block + block + "partial"
	r := []rune(text)
	at, ok := CycleStart(r, 3, 3)
	if !ok || at != len("lead in\n\n")+len(block) {
		t.Fatalf("start %d %v", at, ok)
	}
	if _, ok := CycleStart(r, 3, 4); ok {
		t.Fatal("four cycles found where there are three")
	}
	if _, ok := CycleStart([]rune("a\nb\na\nc\n"), 2, 2); ok {
		t.Fatal("a broken cycle was taken for one")
	}
}
