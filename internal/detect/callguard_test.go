package detect

import (
	"encoding/json"
	"fmt"
	"testing"
)

// kinds is the rule and kind of each firing, in order, as "index:rule/kind".
func kinds(fs []GuardFiring) string {
	out := ""
	for _, f := range fs {
		out += fmt.Sprintf("%d:%s/%s ", f.Index, f.Rule, f.Kind)
	}
	return out
}

// TestCallGuard pins the replay of the SDK's call guard to its rules and
// numbers (@cerebriline/core 4.100.269).
func TestCallGuard(t *testing.T) {
	fail := func(turn int) Call {
		return Call{Turn: turn, Tool: "editor", Input: `{"path":"a"}`, Result: "err", Failed: true, Paths: []string{"a"}}
	}
	var calls []Call
	for i := 1; i <= 8; i++ {
		calls = append(calls, fail(i))
	}
	// Warned from the first earlier failure, escalated once it has
	// failed six times.
	if got, want := kinds(CallGuard(calls, nil, "", nil)), "1:failed/soft 2:failed/soft 3:failed/soft 4:failed/soft 5:failed/soft 6:failed/hard 7:failed/hard "; got != want {
		t.Errorf("failed:\n got %s\nwant %s", got, want)
	}
	// A parallel sibling in the same turn is neither looked at nor counted.
	sib := []Call{fail(1), fail(1), fail(2)}
	if got, want := kinds(CallGuard(sib, nil, "", nil)), "2:failed/soft "; got != want {
		t.Errorf("siblings: got %s want %s", got, want)
	}
	// Refused in advance: warned on the 1st to 3rd repeat, escalated on
	// the 4th.
	var ref []Call
	for i := 1; i <= 5; i++ {
		ref = append(ref, Call{Turn: i, Tool: "editor", Input: `{"path":"a"}`, Result: "No change: x", Failed: true, Refused: true})
	}
	if got, want := kinds(CallGuard(ref, nil, "", nil)), "1:refused/soft 2:refused/soft 3:refused/soft 4:refused/hard "; got != want {
		t.Errorf("refused: got %s want %s", got, want)
	}
	// The same result: warned from the third call, escalated from the
	// sixth for a writing tool, never for another; revision numbers do
	// not count.
	var same, reads []Call
	for i := 1; i <= 7; i++ {
		same = append(same, Call{Turn: i, Tool: "editor", Input: `{"path":"b"}`, Result: fmt.Sprintf("ok #%d", i)})
		reads = append(reads, Call{Turn: i, Tool: "read_files", Input: `{"path":"b"}`, Result: "text"})
	}
	if got, want := kinds(CallGuard(same, nil, "", nil)), "2:same_result/soft 3:same_result/soft 4:same_result/soft 5:same_result/hard 6:same_result/hard "; got != want {
		t.Errorf("same result, writing: got %s want %s", got, want)
	}
	if got, want := kinds(CallGuard(reads, nil, "", nil)), "2:same_result/soft 3:same_result/soft 4:same_result/soft 5:same_result/soft 6:same_result/soft "; got != want {
		t.Errorf("same result, reading: got %s want %s", got, want)
	}
	// A successful edit of the same file clears the failures of another
	// call naming it, not its run of equal results; a fork starts every
	// count again.
	clears := []Call{fail(1), fail(2), {Turn: 3, Tool: "editor", Input: `{"path":"a","x":1}`, Result: "ok", Paths: []string{"a"}}, fail(4)}
	if got, want := kinds(CallGuard(clears, nil, "", nil)), "1:failed/soft 3:same_result/soft "; got != want {
		t.Errorf("cleared by an edit: got %s want %s", got, want)
	}
	if got, want := kinds(CallGuard([]Call{fail(1), fail(2), fail(3)}, []int{2}, "", nil)), "1:failed/soft "; got != want {
		t.Errorf("fork: got %s want %s", got, want)
	}
	// run_commands and the status tools are never looked at.
	rc := []Call{{Turn: 1, Tool: "run_commands", Input: "x", Failed: true}, {Turn: 2, Tool: "run_commands", Input: "x", Failed: true}}
	if got := CallGuard(rc, nil, "", nil); len(got) != 0 {
		t.Errorf("run_commands: %v", got)
	}
}

// TestCallGuard275 pins the rule @cerebriline/core 4.100.275 adds: a
// failure or a refusal holds only while the files the call names have not
// been written since.
func TestCallGuard275(t *testing.T) {
	fail := func(turn int) Call {
		return Call{Turn: turn, Tool: "editor", Input: `{"path":"game.js"}`, Result: "err", Failed: true, Paths: []string{"game.js"}}
	}
	// The note run_commands adds, as the SDK writes it (bash.ts,
	// describeSweptFiles), in the recorded result.
	shell := func(turn int, file string) Call {
		return Call{Turn: turn, Tool: "run_commands", Input: `{"commands":["sed -i s/a/b/ game.js"]}`,
			Result: `{"result":"done\n\nNOTE: this command changed 1 file you had read:\n  - ` + file + `\n\nWhat you remember of these files is out of date. Read any of them again before editing it — earlier line numbers may now point at different code."}`}
	}
	calls := []Call{fail(1), fail(2), shell(3, "/workspace/game.js"), fail(4), fail(5)}
	// On 4.100.274 the shell's write is not seen: the third failure is
	// warned on as the third.
	if got, want := kinds(CallGuard(calls, nil, CallGuard269, nil)), "1:failed/soft 3:failed/soft 4:failed/soft "; got != want {
		t.Errorf("4.100.274:\n got %s\nwant %s", got, want)
	}
	// On 4.100.275 the call after the write is judged afresh: only its
	// run of equal results ("err" three times) is warned on.
	if got, want := kinds(CallGuard(calls, nil, CallGuard275, nil)), "1:failed/soft 3:same_result/soft 4:failed/soft "; got != want {
		t.Errorf("4.100.275:\n got %s\nwant %s", got, want)
	}
	// A note about another file changes nothing.
	other := []Call{fail(1), fail(2), shell(3, "/workspace/index.html"), fail(4), fail(5)}
	if got, want := kinds(CallGuard(other, nil, CallGuard275, nil)), "1:failed/soft 3:failed/soft 4:failed/soft "; got != want {
		t.Errorf("another file:\n got %s\nwant %s", got, want)
	}
	// A refusal is forgotten the same way, and so it is after a writing
	// tool's success on the file through a call that names it differently.
	ref := func(turn int) Call {
		return Call{Turn: turn, Tool: "editor", Input: `{"path":"./game.js","x":1}`, Result: "No change: x", Failed: true, Refused: true, Paths: []string{"./game.js"}}
	}
	sed := Call{Turn: 3, Tool: "sed", Input: `{"path":"/workspace/game.js"}`, Result: "written", Paths: []string{"/workspace/game.js"}}
	refusals := []Call{ref(1), ref(2), sed, ref(4), ref(5)}
	if got, want := kinds(CallGuard(refusals, nil, CallGuard275, nil)), "1:refused/soft 3:same_result/soft 4:refused/soft "; got != want {
		t.Errorf("refusal after a write:\n got %s\nwant %s", got, want)
	}
	// With no write between them the count runs on as before.
	var plain []Call
	for i := 1; i <= 8; i++ {
		plain = append(plain, fail(i))
	}
	if got, want := kinds(CallGuard(plain, nil, CallGuard275, nil)), kinds(CallGuard(plain, nil, CallGuard269, nil)); got != want {
		t.Errorf("no write:\n got %s\nwant %s", got, want)
	}
}

func TestCallGuardFor(t *testing.T) {
	for in, want := range map[string]string{"4.100.269": CallGuard269, "4.100.274": CallGuard269, "4.100.275": CallGuard275,
		"4.100.275+da478ecc8a": CallGuard275, "4.100.275-gda478ecc8a": CallGuard275, "4.100.276": CallGuard275, "4.100.277": CallGuard275, "": CallGuard269} {
		if got := CallGuardFor(in); got != want {
			t.Errorf("CallGuardFor(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestShellWrites(t *testing.T) {
	// Both wordings of the note, changed and deleted, as the SDK writes
	// them; text that only looks like a list is not one.
	text := "out\n\nNOTE: 2 files you had read were changed while this command ran — by it, or by another command running at the same time:\n  - /workspace/a.js\n  - /workspace/b.js\n\nWhat you remember of these files is out of date.\n\nNOTE: this command deleted 1 file you had read:\n  - /workspace/c.js\n\nThey no longer exist: what you remember of them is all that is left.\n  - not-a-file"
	raw := `{"result":` + strconvQuote(text) + `}`
	for _, in := range []string{text, raw} {
		got := ShellWrites(in)
		if fmt.Sprint(got) != "[/workspace/a.js /workspace/b.js /workspace/c.js]" {
			t.Errorf("ShellWrites = %v", got)
		}
	}
	if got := ShellWrites(`{"result":"NOTE: nothing changed\n  - x"}`); len(got) != 0 {
		t.Errorf("no note: %v", got)
	}
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
