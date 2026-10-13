package detect

import (
	"fmt"
	"strings"
	"sync"
)

// Control is a text with a known answer: the detectors that must fire on it.
// A positive control proves a detector can say yes; the negative ones are
// healthy long text of each domain, on which every detector must stay
// silent. A zero from a detector with no proven non-zero floor is not a
// measurement.
type Control struct {
	Name   string
	Domain string // prose, code, tool output
	Text   string
	Fires  []string // the detectors that must fire; no other may
	Opt    Options
}

// ControlResult is one control's outcome.
type ControlResult struct {
	Control string   `json:"control"`
	Domain  string   `json:"domain"`
	Want    []string `json:"want"`
	Got     []string `json:"got"`
	OK      bool     `json:"ok"`
}

// Controls returns the library's own controls.
func Controls() []Control {
	rep := strings.Repeat
	line := "The function returns the value it was given and nothing else happens after that point. "
	// Healthy prose: distinct sentences, each long enough to qualify.
	var prose strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&prose, "In step %d the loader reads section %d of the level table and checks it against the %s list before moving on. ",
			i, i*7+3, []string{"platform", "conveyor", "guardian", "portal", "key"}[i%5])
	}
	// Healthy code: indentation, separators, repeated short tokens.
	var code strings.Builder
	code.WriteString("// " + rep("=", 76) + "\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&code, "%sif (level[%d].tiles[%d] === TILE_%d) {\n%sreturn %d;\n%s}\n", rep("    ", 1+i%4), i, i*3, i%9, rep("    ", 2+i%4), i, rep("    ", 1+i%4))
	}
	code.WriteString(rep("0, ", 64) + "\n" + rep("-", 80) + "\n" + rep(" ", 120) + "\n")
	// Healthy tool output: a table and a test report.
	var out strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&out, "%4d | %08x | %-12s | ok\n", i, i*2654435761, fmt.Sprintf("trap_%02d", i))
	}
	out.WriteString(rep("........", 12) + "\n" + rep("PASS\n", 4))
	// Iterative drafting: one sentence a few times, far apart, among much
	// new text. Not a loop.
	var draft strings.Builder
	for i := 0; i < 5; i++ {
		draft.WriteString(line)
		for j := 0; j < 8; j++ {
			fmt.Fprintf(&draft, "Attempt %d, variant %d changes the collision test on row %d and leaves every other row of the table as it was. ", i, j, i*8+j)
		}
	}
	return []Control{
		{Name: "word loop", Domain: "prose", Text: "The answer is " + rep("very ", 12) + "clear.", Fires: []string{ShortUnitRepeat}},
		{Name: "phrase loop", Domain: "prose", Text: "Let me check. " + rep("I will fix the jump. ", 9), Fires: []string{ShortUnitRepeat}},
		{Name: "letter run", Domain: "prose", Text: "Wait" + rep("a", 60) + " no.", Fires: []string{CharRun}},
		{Name: "sentence loop", Domain: "prose", Text: rep(line+"\n", 4) + "So it must be something else in the way the frame counter is advanced by the loop.", Fires: []string{SentenceRepeat}},
		{Name: "sentence loop among new text", Domain: "prose", Text: rep(line+"Then a short note. ", 8) + prose.String(), Fires: []string{SentenceRepeat}},
		{Name: "long block", Domain: "prose", Text: prose.String() + prose.String() + prose.String(), Fires: []string{Runaway}},
		{Name: "declared size", Domain: "prose", Text: rep("x1 ", 40), Fires: []string{Runaway}, Opt: Options{RunawayChars: 100}},
		{Name: "healthy prose", Domain: "prose", Text: prose.String()},
		{Name: "healthy code", Domain: "code", Text: code.String()},
		{Name: "healthy tool output", Domain: "tool output", Text: out.String()},
		{Name: "iterative drafting", Domain: "prose", Text: draft.String()},
		{Name: "separators and numbers", Domain: "code", Text: rep("=", 200) + "\n" + rep("12 ", 50) + "\n" + rep("0x00, ", 30) + "\n" + rep("\t", 60)},
		{Name: "empty", Domain: "prose", Text: ""},
	}
}

// SelfTest runs every control and reports each outcome; ok is false when any
// control did not give its known answer.
func SelfTest() (results []ControlResult, ok bool) {
	ok = true
	for _, c := range Controls() {
		got := []string{}
		for _, h := range Block(c.Text, c.Opt) {
			got = append(got, h.Detector)
		}
		want := c.Fires
		if want == nil {
			want = []string{}
		}
		res := ControlResult{Control: c.Name, Domain: c.Domain, Want: want, Got: got, OK: strings.Join(got, ",") == strings.Join(want, ",")}
		ok = ok && res.OK
		results = append(results, res)
	}
	// The repeated-call detector's controls.
	same := []Call{{1, "read_files", `{"path":"a"}`}, {2, "read_files", `{"path":"a"}`}, {3, "read_files", `{"path":"a"}`}}
	varied := []Call{{1, "read_files", `{"path":"a"}`}, {2, "read_files", `{"path":"b"}`}, {3, "editor", `{"path":"a"}`}, {4, "read_files", `{"path":"a"}`}}
	for _, c := range []struct {
		name  string
		calls []Call
		fires bool
	}{{"same call three times", same, true}, {"different calls", varied, false}} {
		got, want := []string{}, []string{}
		if len(RepeatedCalls(c.calls, 0)) > 0 {
			got = append(got, RepeatedToolCall)
		}
		if c.fires {
			want = append(want, RepeatedToolCall)
		}
		res := ControlResult{Control: c.name, Domain: "tool calls", Want: want, Got: got, OK: len(got) == len(want)}
		ok = ok && res.OK
		results = append(results, res)
	}
	return results, ok
}

var available = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, d := range append(append([]string{}, Text...), RepeatedToolCall) {
		out[d] = true
	}
	results, _ := SelfTest()
	for _, r := range results {
		if r.OK {
			continue
		}
		// A control that went wrong takes down every detector it names:
		// those that should have fired and those that did.
		for _, d := range append(append([]string{}, r.Want...), r.Got...) {
			out[d] = false
		}
	}
	return out
})

// Available reports whether a detector gave its known answer on every one
// of its controls in this build. A detector that did not is unavailable: it
// reports nothing, which is not the same as reporting zero (050 §3.2).
func Available(detector string) bool { return available()[detector] }
