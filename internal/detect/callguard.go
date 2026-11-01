package detect

import (
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
// "same_result").
type GuardFiring struct {
	Index int    `json:"index"`
	Turn  int    `json:"turn"`
	Tool  string `json:"tool"`
	Input string `json:"input"`
	Rule  string `json:"rule"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type callState struct {
	failed     int
	refused    bool
	strikes    int
	lastResult string
	repeats    int
	paths      []string
}

// CallGuard replays the guard over a run's calls. forks are the indexes of
// the calls that came first after each fork of the session.
func CallGuard(calls []Call, forks []int) []GuardFiring {
	var out []GuardFiring
	state := map[string]*callState{}
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
		fire := func(rule, kind string, n int) {
			out = append(out, GuardFiring{Index: i, Turn: c.Turn, Tool: c.Tool, Input: cut(c.Input), Rule: rule, Kind: kind, Count: n})
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
		if s.repeats > 0 && sig == s.lastResult {
			s.repeats++
		} else {
			s.repeats = 1
		}
		s.lastResult = sig
		switch {
		case !c.Failed:
			s.failed, s.refused, s.strikes = 0, false, 0
			if WritingTools[c.Tool] {
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
