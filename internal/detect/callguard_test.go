package detect

import (
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
	if got, want := kinds(CallGuard(calls, nil)), "1:failed/soft 2:failed/soft 3:failed/soft 4:failed/soft 5:failed/soft 6:failed/hard 7:failed/hard "; got != want {
		t.Errorf("failed:\n got %s\nwant %s", got, want)
	}
	// A parallel sibling in the same turn is neither looked at nor counted.
	sib := []Call{fail(1), fail(1), fail(2)}
	if got, want := kinds(CallGuard(sib, nil)), "2:failed/soft "; got != want {
		t.Errorf("siblings: got %s want %s", got, want)
	}
	// Refused in advance: warned on the 1st to 3rd repeat, escalated on
	// the 4th.
	var ref []Call
	for i := 1; i <= 5; i++ {
		ref = append(ref, Call{Turn: i, Tool: "editor", Input: `{"path":"a"}`, Result: "No change: x", Failed: true, Refused: true})
	}
	if got, want := kinds(CallGuard(ref, nil)), "1:refused/soft 2:refused/soft 3:refused/soft 4:refused/hard "; got != want {
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
	if got, want := kinds(CallGuard(same, nil)), "2:same_result/soft 3:same_result/soft 4:same_result/soft 5:same_result/hard 6:same_result/hard "; got != want {
		t.Errorf("same result, writing: got %s want %s", got, want)
	}
	if got, want := kinds(CallGuard(reads, nil)), "2:same_result/soft 3:same_result/soft 4:same_result/soft 5:same_result/soft 6:same_result/soft "; got != want {
		t.Errorf("same result, reading: got %s want %s", got, want)
	}
	// A successful edit of the same file clears the failures of another
	// call naming it, not its run of equal results; a fork starts every
	// count again.
	clears := []Call{fail(1), fail(2), {Turn: 3, Tool: "editor", Input: `{"path":"a","x":1}`, Result: "ok", Paths: []string{"a"}}, fail(4)}
	if got, want := kinds(CallGuard(clears, nil)), "1:failed/soft 3:same_result/soft "; got != want {
		t.Errorf("cleared by an edit: got %s want %s", got, want)
	}
	if got, want := kinds(CallGuard([]Call{fail(1), fail(2), fail(3)}, []int{2})), "1:failed/soft "; got != want {
		t.Errorf("fork: got %s want %s", got, want)
	}
	// run_commands and the status tools are never looked at.
	rc := []Call{{Turn: 1, Tool: "run_commands", Input: "x", Failed: true}, {Turn: 2, Tool: "run_commands", Input: "x", Failed: true}}
	if got := CallGuard(rc, nil); len(got) != 0 {
		t.Errorf("run_commands: %v", got)
	}
}
