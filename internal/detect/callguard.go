package detect

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The SDK's loop guard on tool calls, replayed after the run (070 §3.2,
// D75): the rules it applies beside identical calls in a row, with its own
// numbers and lists, as @cerebriline/core 4.100.269 holds them (class `Bu`
// of runtime/safety/loop-detection). Before each call it looks at what the
// same call (name and arguments, keys sorted) gave before:
//
//   - refused in advance: a call the tool answered without running it
//     ("No change: ", "Duplicated instead of replaced: ") and sent again is
//     warned on its 1st to 3rd repeat and escalated on the 4th;
//   - failed: a call that failed before is warned on, and escalated once
//     it has failed 6 times;
//   - same call, same result: a call whose last results were the same
//     (revision numbers #N blanked) 2 times in a row is warned on, and,
//     for a writing tool, escalated from 5.
//
// A success clears the call's failures and refusals and, for a writing
// tool, those of every call naming one of its files. A second identical
// call in the same turn (a parallel sibling) is neither looked at nor
// counted. The agents' and teams' status tools are never looked at, nor is
// run_commands. The counts run across turns and start again at a fork of
// the session.
const (
	RefusedHard    = 4 // repeats of a refused call: the last is escalated
	FailedHard     = 6 // failures before the call is escalated
	SameResultSoft = 3 // results in a row, the next call included
	SameResultHard = 6 // the same, for a writing tool
)

// The call guard's rule sets. CallGuard269 is the guard described above,
// as @cerebriline/core 4.100.269 to 4.100.274 hold it. From 4.100.275
// (CallGuard275) a failure or a refusal is a verdict on the files the call
// names as they were then: the guard keeps their size and modification
// time, and when the same call comes again after they changed, by any
// write at all, its failures and refusals are forgotten and it is judged
// afresh. Its run of equal results is kept. After the run the files are
// gone, so a write is known from the record: a writing tool's success on
// the file, or run_commands saying it changed or deleted the file
// (ShellWrites). A write the record does not show -- a command that
// changed a file the model had not read -- is not seen here either.
//
// From 4.100.282 (CallGuard282) a verdict waits for its call's result. It
// is given -- the warning attached to the result, the loop notice sent,
// a hard verdict's mistake recorded -- only if the call fails (or is
// refused). A repeat that works gets none of it. The exception is the
// landed cycle: a call that writes the file it names (a writing tool, less
// restore_file, and sed or awk only with in_place) whose same-result
// verdict is followed by the same answer once more; that success is the
// loop, so the verdict is given. The verdicts themselves are formed as in
// CallGuard275, so every firing is still reported and Given says which the
// model was told of. Before 4.100.282 every firing is Given: its notice and
// a hard verdict's mistake went out when it was formed. A run of identical
// calls in a row restarts after a call of it that worked (RepeatedCallsFor).
const (
	CallGuard269 = "call-guard-269"
	CallGuard275 = "call-guard-275"
	CallGuard282 = "call-guard-282"
)

// CallGuardFor is the call guard's rule set of an SDK release.
func CallGuardFor(release string) string {
	if releaseAtLeast(release, 4, 100, 282) {
		return CallGuard282
	}
	if releaseAtLeast(release, 4, 100, 275) {
		return CallGuard275
	}
	return CallGuard269
}

// The detectors of the call guard, and the two views of 070 §3 they give
// (D129): edit oscillation is the guard's firings on writing tools (the
// same edit giving the same result, or refused in advance), the re-read
// loop its same-result firings on every other tool (a read or a search
// giving what it gave before).
const (
	FailedIdenticalCall = "failed_identical_call"
	RepeatedCallResult  = "repeated_call_result"
	EditOscillation     = "edit_oscillation"
	RereadLoop          = "reread_loop"
)

// WritingTools are the SDK's tools that write a file.
var WritingTools = map[string]bool{"editor": true, "apply_patch": true, "sed": true, "awk": true, "write_to_file": true, "new_file": true, "restore_file": true}

// unwatched are the tools the guard never looks at.
var unwatched = map[string]bool{"agents_status": true, "await_agents": true, "team_status": true, "team_list_runs": true,
	"team_await_runs": true, "team_read_mailbox": true, "run_commands": true}

var revisionRE = regexp.MustCompile(`#\d+`)

// ResultSignature is how the guard compares two results: the result with
// its revision numbers blanked.
func ResultSignature(result string) string { return revisionRE.ReplaceAllString(result, "#n") }

// GuardFiring is one call the guard would have warned on ("soft") or
// escalated ("hard"), and by which rule ("refused", "failed",
// "same_result"). Given is whether the model was told (CallGuard282: the
// call failed, or landed on the cycle's answer again; always before).
type GuardFiring struct {
	Index int    `json:"index"`
	Turn  int    `json:"turn"`
	Tool  string `json:"tool"`
	Input string `json:"input"`
	Rule  string `json:"rule"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Given bool   `json:"given"`
}

// writesItsFile is the SDK's test for a landed cycle (4.100.282,
// loop-detection.ts): a writing tool other than restore_file, and sed or
// awk only when the call says in_place.
func writesItsFile(c Call) bool {
	if !WritingTools[c.Tool] || c.Tool == "restore_file" {
		return false
	}
	if c.Tool == "sed" || c.Tool == "awk" {
		var in struct {
			InPlace any `json:"in_place"`
		}
		return json.Unmarshal([]byte(c.Input), &in) == nil && in.InPlace == true
	}
	return true
}

type callState struct {
	failed     int
	refused    bool
	strikes    int
	lastResult string
	repeats    int
	paths      []string
	// stamp is what the call's files were when it last failed or was
	// refused, under CallGuard275: their write counts.
	stamp   string
	stamped bool
}

// CallGuard replays the guard over a run's calls with a rule set
// (CallGuardFor the run's release; empty is CallGuard269). forks are the
// indexes of the calls that came first after each fork of the session.
func CallGuard(calls []Call, forks []int, rules string, v Values) []GuardFiring {
	RefusedHard, FailedHard, SameResultSoft, SameResultHard := v.Int(ThRefusedHard), v.Int(ThFailedHard), v.Int(ThSameResultSoft), v.Int(ThSameResultHard)
	var out []GuardFiring
	state := map[string]*callState{}
	stamps := rules == CallGuard275 || rules == CallGuard282
	waits := rules == CallGuard282
	// How many writes each file has had: a stamp's size and time stand
	// for the writes before it.
	writes := map[string]int{}
	stampOf := func(paths []string) string {
		var b strings.Builder
		for _, p := range paths {
			fmt.Fprintf(&b, "%s=%d\x00", stampPath(p), writes[stampPath(p)])
		}
		return b.String()
	}
	wrote := func(paths []string) {
		for _, p := range paths {
			writes[stampPath(p)]++
		}
	}
	forkAt := map[int]bool{}
	for _, f := range forks {
		forkAt[f] = true
	}
	turn, seen := -1, map[string]bool{}
	for i, c := range calls {
		if forkAt[i] {
			state = map[string]*callState{}
		}
		if c.Turn != turn {
			turn, seen = c.Turn, map[string]bool{}
		}
		if unwatched[c.Tool] {
			if stamps && c.Tool == "run_commands" {
				wrote(ShellWrites(c.Result))
			}
			continue
		}
		key := c.Tool + ":" + c.Input
		if seen[key] && c.Turn != 0 {
			continue
		}
		seen[key] = true
		s := state[key]
		if s == nil {
			s = &callState{paths: c.Paths}
			state[key] = s
		}
		if stamps && s.stamped && stampOf(c.Paths) != s.stamp {
			s.failed, s.refused, s.strikes, s.stamped = 0, false, 0, false
		}
		fired := -1
		fire := func(rule, kind string, n int) {
			fired = len(out)
			out = append(out, GuardFiring{Index: i, Turn: c.Turn, Tool: c.Tool, Input: cut(c.Input), Rule: rule, Kind: kind, Count: n, Given: true})
		}
		switch {
		case s.refused:
			s.strikes++
			if RefusedHard-s.strikes > 0 {
				fire("refused", "soft", s.strikes)
			} else {
				fire("refused", "hard", s.strikes)
			}
		case s.failed >= FailedHard:
			fire("failed", "hard", s.failed)
		case s.failed > 0:
			fire("failed", "soft", s.failed)
		case s.repeats >= SameResultSoft-1:
			if WritingTools[c.Tool] && s.repeats >= SameResultHard-1 {
				fire("same_result", "hard", s.repeats)
			} else {
				fire("same_result", "soft", s.repeats)
			}
		}
		// What the call gave.
		sig := ResultSignature(c.Result)
		if waits && fired >= 0 {
			f := &out[fired]
			f.Given = c.Failed || (f.Rule == "same_result" && writesItsFile(c) && sig == s.lastResult)
		}
		if s.repeats > 0 && sig == s.lastResult {
			s.repeats++
		} else {
			s.repeats = 1
		}
		s.lastResult = sig
		switch {
		case !c.Failed:
			s.failed, s.refused, s.strikes, s.stamped = 0, false, 0, false
			if WritingTools[c.Tool] {
				wrote(c.Paths)
				for k, o := range state {
					if k != key && overlap(o.paths, c.Paths) {
						o.failed, o.refused, o.strikes = 0, false, 0
					}
				}
			}
		case c.Refused:
			s.refused = true
		default:
			s.failed++
		}
		if stamps && c.Failed && len(c.Paths) > 0 {
			s.stamp, s.stamped = stampOf(c.Paths), true
		}
	}
	return out
}

// stampPath is a file as both a call and run_commands' note can name it:
// lower-cased, with slashes, relative to the workspace. The note names
// files as the SDK resolved them, the call as the model wrote them.
func stampPath(p string) string {
	p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	p = strings.TrimPrefix(p, "/workspace/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

// ShellWrites are the files a run_commands result says the command changed
// or deleted: the note the SDK adds after a command, "NOTE: this command
// changed 2 files you had read:" (or "... were changed while this command
// ran -- by it, or by another command running at the same time:"), then one
// "  - <file>" line per file. The result is the tool's output as recorded,
// JSON or text.
func ShellWrites(result string) []string {
	var texts []string
	var v any
	if json.Unmarshal([]byte(result), &v) == nil {
		texts = jsonStrings(v, texts)
	} else {
		texts = []string{result}
	}
	var out []string
	for _, text := range texts {
		listing := false
		for _, line := range strings.Split(text, "\n") {
			switch {
			case strings.HasPrefix(line, "NOTE: ") && strings.HasSuffix(line, ":") &&
				(strings.Contains(line, " changed ") || strings.Contains(line, " deleted ")):
				listing = true
			case listing && strings.HasPrefix(line, "  - "):
				out = append(out, strings.TrimPrefix(line, "  - "))
			default:
				listing = false
			}
		}
	}
	return out
}

// jsonStrings are the strings in a JSON value, appended to out.
func jsonStrings(v any, out []string) []string {
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case []any:
		for _, e := range x {
			out = jsonStrings(e, out)
		}
	case map[string]any:
		for _, e := range x {
			out = jsonStrings(e, out)
		}
	}
	return out
}

func overlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// Refusal says whether a tool's error is a refusal in advance: the tool
// compared the call against the file and answered it without running it.
func Refusal(err string) bool {
	return strings.Contains(err, "No change: ") || strings.Contains(err, "Duplicated instead of replaced: ")
}
