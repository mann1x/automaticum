package detect

import (
	"encoding/json"
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
	// A long block of distinct sentences: length alone is not a signal.
	var long strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&long, "In step %d the loader reads section %d of the level table and checks it against the list before moving on.\n\n", i, i*7+3)
	}
	// One conclusion re-derived, paragraph after paragraph.
	var stuck strings.Builder
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&stuck, "Attempt %d looks at row %d of the table and it is not the cause, so the search has to go on.\n\n", i, i*3)
		stuck.WriteString("Actually, I think I found it! These should all be fine. Let me look more carefully at the collision test.\n\n")
		fmt.Fprintf(&stuck, "The frame counter is advanced by the loop in section %d, and that is where the next check will be made because of it.\n\n", i)
	}
	// The same code quoted while candidate edits are compared: working.
	var quoted strings.Builder
	for i := 0; i < 30; i++ {
		quoted.WriteString("```\nfunction isOverlapping(a, b) { return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h; }\n```\n\n")
		fmt.Fprintf(&quoted, "Candidate %d changes the comparison on line %d and leaves the others of the body as they were in the file.\n\n", i, 90+i)
	}
	// A checklist flipping between two states.
	var flip strings.Builder
	for i := 0; i < 200; i++ {
		flip.WriteString([]string{"- [ ] check the collision test of the guardian", "- [x] check the collision test of the guardian", "- [ ] then the portal", "- [x] then the portal"}[(i*7)%4] + "\n")
	}
	// Fewer lines than the window holds, so only the exact cycle can speak.
	cycle := "First the loader of the level table is read again from its start to its end and every section of it is compared with the list.\n" +
		"Then the platform list is read again from its start to its end and every entry of it is compared with the level table above.\n"
	return []Control{
		{Name: "word loop", Domain: "prose", Text: "The answer is " + rep("very ", 12) + "clear.", Fires: []string{ShortUnitRepeat}},
		{Name: "phrase loop", Domain: "prose", Text: "Let me check. " + rep("I will fix the jump. ", 9), Fires: []string{ShortUnitRepeat}},
		{Name: "letter run", Domain: "prose", Text: "Wait" + rep("a", 60) + " no.", Fires: []string{CharRun}},
		{Name: "sentence loop", Domain: "prose", Text: rep(line+"\n", 4) + "So it must be something else in the way the frame counter is advanced by the loop.", Fires: []string{SentenceRepeat}},
		{Name: "sentence loop among new text", Domain: "prose", Text: rep(line+"Then a short note. ", 8) + prose.String(), Fires: []string{SentenceRepeat}},
		{Name: "long block, no size declared", Domain: "prose", Text: long.String()},
		{Name: "paragraph loop", Domain: "prose", Text: stuck.String(), Fires: []string{ReasoningRepetition}},
		// The older sentence rule counts the quoted code; the SDK's paragraph
		// rule, which asks for a sentence, does not.
		{Name: "code quoted again and again", Domain: "code", Text: quoted.String(), Fires: []string{SentenceRepeat}},
		{Name: "two lines flipping", Domain: "prose", Text: flip.String(), Fires: []string{ReasoningLoop}},
		{Name: "two lines in a cycle", Domain: "prose", Text: rep(cycle, 22), Fires: []string{SentenceRepeat, ReasoningLoop}},
		{Name: "one phrase in an unbroken line", Domain: "prose", Text: prose.String() + prose.String() + "\n" + rep("and I'll do it, ", 200), Fires: []string{ShortUnitRepeat, ReasoningLoop}},
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
	same := []Call{{Turn: 1, Tool: "read_files", Input: `{"path":"a"}`}, {Turn: 2, Tool: "read_files", Input: `{"path":"a"}`}, {Turn: 3, Tool: "read_files", Input: `{"path":"a"}`}}
	varied := []Call{{Turn: 1, Tool: "read_files", Input: `{"path":"a"}`}, {Turn: 2, Tool: "read_files", Input: `{"path":"b"}`}, {Turn: 3, Tool: "editor", Input: `{"path":"a"}`}, {Turn: 4, Tool: "read_files", Input: `{"path":"a"}`}}
	for _, c := range []struct {
		name  string
		calls []Call
		fires bool
	}{{"same call again and again", same, true}, {"different calls", varied, false}} {
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
	// The call guard's controls (callguard.go).
	bad := func(turn int) Call {
		return Call{Turn: turn, Tool: "editor", Input: `{"path":"a"}`, Result: `{"error":"x"}`, Failed: true, Paths: []string{"a"}}
	}
	var failing, reread, healthy []Call
	for t := 1; t <= 8; t++ {
		failing = append(failing, bad(t))
		healthy = append(healthy, Call{Turn: t, Tool: "read_files", Input: `{"path":"f` + string(rune('0'+t)) + `"}`, Result: "ok"})
	}
	for t := 1; t <= 4; t++ {
		reread = append(reread, Call{Turn: t, Tool: "read_files", Input: `{"path":"a"}`, Result: "same #12"})
	}
	for _, c := range []struct {
		name  string
		calls []Call
		want  []string
	}{{"a failing edit sent again and again", failing, []string{FailedIdenticalCall}},
		{"the same read giving the same result", reread, []string{RepeatedCallResult}},
		{"reads of different files", healthy, nil}} {
		got := map[string]bool{}
		for _, f := range CallGuard(c.calls, nil) {
			if f.Rule == "same_result" {
				got[RepeatedCallResult] = true
			} else {
				got[FailedIdenticalCall] = true
			}
		}
		g, w := []string{}, []string{}
		for _, d := range []string{FailedIdenticalCall, RepeatedCallResult} {
			if got[d] {
				g = append(g, d)
			}
		}
		if c.want != nil {
			w = c.want
		}
		res := ControlResult{Control: c.name, Domain: "tool calls", Want: w, Got: g, OK: strings.Join(g, ",") == strings.Join(w, ",")}
		ok = ok && res.OK
		results = append(results, res)
	}
	// Swallowed arguments: an edit whose new_text holds the start_line it
	// is missing, against code that names the same word with the argument
	// passed.
	schema := map[string][]string{"editor": {"file_path", "new_text", "start_line"}}
	for _, c := range []struct {
		name  string
		input string
		want  []string
	}{{"an edit whose text swallowed its start_line", `{"file_path":"a","new_text":"x;\",\"start_line\": 3"}`, []string{SwallowedArgument}},
		{"an intent that swallowed new_text after a newline", `{"file_path":"a","start_line":3,"intent":"fix line 3 */\nnew_text:"}`, []string{SwallowedArgument}},
		{"an edit of code that names the word, the argument passed", `{"file_path":"a","start_line":3,"new_text":"{start_line: 3}"}`, []string{}}} {
		g := []string{}
		if len(SwallowedArgs([]Call{{Turn: 1, Tool: "editor", Input: c.input}}, schema)) > 0 {
			g = append(g, SwallowedArgument)
		}
		res := ControlResult{Control: c.name, Domain: "tool calls", Want: c.want, Got: g, OK: strings.Join(g, ",") == strings.Join(c.want, ",")}
		ok = ok && res.OK
		results = append(results, res)
	}
	// Repeated definitions: a file whose functions are written again and
	// again, against one class per method name.
	stubs := strings.Repeat("function draw() {}\nfunction step() {}\n", 3)
	for _, c := range []struct {
		name string
		text string
		want []string
	}{{"an edit that defines the same functions again and again", stubs, []string{DuplicateDefinitions}},
		{"an edit with one method of a name per class", "class A {\n  update() {\n  }\n}\nclass B {\n  update() {\n  }\n}\nfunction main() {}\n", []string{}},
		{"an edit that defines one name twice, as base models have", "function a() {}\nfunction a() {}\nfunction b() {}\n", []string{}},
		{"an edit that defines two names twice", "function a() {}\nfunction a() {}\nfunction b() {}\nfunction b() {}\n", []string{DuplicateDefinitions}}} {
		g := []string{}
		in, _ := json.Marshal(map[string]string{"file_path": "a", "new_text": c.text})
		if len(DuplicateDefinitionCalls([]Call{{Turn: 1, Tool: "editor", Input: string(in)}})) > 0 {
			g = append(g, DuplicateDefinitions)
		}
		res := ControlResult{Control: c.name, Domain: "tool calls", Want: c.want, Got: g, OK: strings.Join(g, ",") == strings.Join(c.want, ",")}
		ok = ok && res.OK
		results = append(results, res)
	}
	// The repetition guard: a reply that says one paragraph again and
	// again, one that restates its plan with small changes, healthy prose,
	// and a call that rewrites a file with stubs.
	var plan, prose strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&plan, "1. Line 90: the dDec method has a syntax error (pass %d)\n2. Line 95: the dBoss method has a syntax error\n3. Line 96: a semicolon is missing after the call\n", i%2)
		fmt.Fprintf(&prose, "Step %d reads the file at offset %d and writes the result to the log before it moves on to the next one.\n\n", i, i*512)
	}
	loop := strings.Repeat("Now let me fix each syntax error in the file properly, one at a time:\n\n", 120)
	for _, c := range []struct {
		name, channel, text string
		want                []string
	}{{"a reply that says one paragraph again and again", "text", loop, []string{RepetitionGuard}},
		{"a reply that restates its plan with small changes", "text", plan.String(), []string{RepetitionGuard}},
		{"a long reply of distinct steps", "text", prose.String(), []string{}},
		{"reasoning that says one paragraph again and again", "reasoning", loop, []string{RepetitionGuard}},
		{"a call that writes the same stubs again and again", "argument", stubs, []string{RepetitionGuard}},
		{"a call that writes distinct steps", "argument", prose.String(), []string{}}} {
		var f *GuardFire
		switch c.channel {
		case "text":
			f = GuardText(c.text, "")
		case "reasoning":
			f = GuardReasoning(c.text)
		default:
			f = GuardArgument(c.text, "")
		}
		g := []string{}
		if f != nil {
			g = append(g, RepetitionGuard)
		}
		res := ControlResult{Control: c.name, Domain: "repetition guard", Want: c.want, Got: g, OK: strings.Join(g, ",") == strings.Join(c.want, ",")}
		ok = ok && res.OK
		results = append(results, res)
	}
	return results, ok
}

var available = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, d := range append(append([]string{}, Text...), RepeatedToolCall, FailedIdenticalCall, RepeatedCallResult, SwallowedArgument, DuplicateDefinitions, RepetitionGuard) {
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
