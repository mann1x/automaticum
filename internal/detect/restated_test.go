package detect

import (
	"fmt"
	"strings"
	"testing"
)

// TestRestatedText is the SDK's measure, ported: 300 long lines or more,
// 30% or more of them a third copy or later, beyond what the source holds.
func TestRestatedText(t *testing.T) {
	line := func(i int) string { return fmt.Sprintf("the step number %d of the plan reads the file\n", i) }
	var distinct, plan strings.Builder
	for i := range 400 {
		distinct.WriteString(line(i))
		plan.WriteString(line(i % 50)) // each line 8 times
	}
	if r := RestatedText(distinct.String(), ""); r != nil {
		t.Errorf("distinct lines: %+v", r)
	}
	r := RestatedText(plan.String(), "")
	first300 := len(strings.Join(strings.SplitAfter(plan.String(), "\n")[:restatedMinLines], ""))
	// 300 lines in, the first 100 are first and second copies: 200 of 300
	// are third copies or later.
	if r == nil || r.Lines != restatedMinLines || r.Distinct != 50 || r.Most != 6 || r.Offset != first300 {
		t.Fatalf("restated: %+v", r)
	}
	// Short lines are not counted, and a line not yet ended is not either.
	if RestatedText(strings.Repeat("}\n", 1000), "") != nil || RestatedText(strings.TrimSuffix(plan.String()[:first300], "\n"), "") != nil {
		t.Error("short or unended lines counted")
	}
	// A source that holds the lines 8 times excuses them; line numbers are
	// read through ("12 | ").
	var src strings.Builder
	for i := range 400 {
		fmt.Fprintf(&src, "%d | %s", i+1, line(i%50))
	}
	if r := RestatedText(plan.String(), src.String()); r != nil {
		t.Errorf("excused by the source: %+v", r)
	}
}
