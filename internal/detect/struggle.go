package detect

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The SDK's struggle detector, replayed after the run (070 §4, D126): the
// class `Pa` of @cerebriline/core and the observer `Tl` that feeds it from
// the agent's events, the same code and numbers in every build from
// 4.100.250 to 4.100.269. Over its last 10 turns it counts the tool calls
// that failed or were refused, the times the reasoning said it was stuck
// ("distress") and how much it hedged against its first 10 turns. From turn
// 20, 6 failures with 2 distress hits or hedging at or above the opening
// rate make it suggest an escalation (at most 2 a task, once a transaction,
// not twice for the same signals and changed files); one failure short of
// that, it nudges. Apart from that it nudges on every 3rd file-changing
// call in a row that failed (editor, apply_patch) and on every 2nd attempt
// thrown away (a transaction put back after its check, or one submitted
// with nothing changed). A compaction clears the window and the streak; a
// new transaction lets it fire again.
//
// The main agent's SDK runs the detector only when it can escalate or
// spawn agents, and says so with the struggle notices (nudge, offer); the
// replay says what it would have said on any run.
const (
	StruggleWindow             = 10
	StruggleFailedCalls        = 6
	StruggleDistressHits       = 2
	StruggleMinIteration       = 20
	StruggleMaxPerTask         = 2
	StruggleEditStreak         = 3
	StruggleFailedTransactions = 2
)

// What the replay's verdicts are, and why.
const (
	StruggleNudge   = "nudge"
	StruggleSuggest = "suggest"
)

var (
	distressRE = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bi ?'?m confusing myself\b`), regexp.MustCompile(`(?i)\bi ?'?m confused\b`),
		regexp.MustCompile(`(?i)\bkeeps? failing\b`), regexp.MustCompile(`(?i)\bi ?'?m stuck\b`),
		regexp.MustCompile(`(?i)\bi ?'?m not sure\b`), regexp.MustCompile(`(?i)\bthat makes no sense\b`),
		regexp.MustCompile(`(?i)\bregressing\b`),
	}
	hedgingRE = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bwait\b`), regexp.MustCompile(`(?i)\bhmm+\b`),
		regexp.MustCompile(`(?i)\blet me reconsider\b`), regexp.MustCompile(`(?i)\bdead end\b`),
		regexp.MustCompile(`(?i)\bin circles\b`), regexp.MustCompile(`(?i)\bunexpected\b`),
	}
	// The tools whose failures make the edit streak, and those whose
	// file the detector notes as changed.
	streakTools  = map[string]bool{"editor": true, "apply_patch": true}
	changedTools = map[string]bool{"editor": true, "apply_patch": true, "restore_file": true}
)

// StruggleEvent is one thing the detector observes, in the order of the
// stream: a turn's start ("turn") or end ("turn_end"), a finished block of
// reasoning or text, a tool call starting ("call", with the file it
// changes) or ending ("tool"), a compaction, a transaction seen for the
// first time, one put back after its check ("discarded") or one submitted
// with nothing changed ("empty").
type StruggleEvent struct {
	Kind        string
	Turn        int
	Text        string
	Tool        string
	Path        string
	Failed      bool
	Transaction int
}

// StruggleFiring is one verdict of the replay: a nudge or a suggestion,
// why ("failures", "edit-streak", "transactions") and the signals then.
type StruggleFiring struct {
	Turn         int      `json:"turn"`
	Kind         string   `json:"kind"`
	Reason       string   `json:"reason"`
	FailedCalls  int      `json:"failed_calls,omitempty"`
	Distress     int      `json:"distress,omitempty"`
	HedgingRatio *float64 `json:"hedging_ratio,omitempty"`
	EditStreak   int      `json:"edit_streak,omitempty"`
	ThrownAway   int      `json:"thrown_away,omitempty"`
}

type struggleTurn struct {
	turn, failed, distress, hedging, words int
}

type struggle struct {
	history                    []*struggleTurn
	baseline                   *float64
	firedInTask                int
	firedInTransaction         bool
	transaction                int
	lastFiredKey, lastNudgeKey string
	editStreak                 int
	discarded, empty           int
	changed                    map[string]bool
}

// Struggle replays the detector over a run's events.
func Struggle(events []StruggleEvent) []StruggleFiring {
	s := &struggle{changed: map[string]bool{}}
	var out []StruggleFiring
	turn, reasoned := 0, false
	see := func(f *StruggleFiring) {
		if f != nil {
			out = append(out, *f)
		}
	}
	for _, e := range events {
		switch e.Kind {
		case "turn":
			turn = e.Turn
		case "call":
			if changedTools[e.Tool] && e.Path != "" {
				s.changed[e.Path] = true
			}
		case "reasoning":
			reasoned = reasoned || strings.TrimSpace(e.Text) != ""
			s.noteTurn(turn, e.Text)
			see(s.inspect(turn))
		case "text":
			// Once the model has reasoned, its visible text is not read.
			if reasoned {
				continue
			}
			s.noteTurn(turn, e.Text)
			see(s.inspect(turn))
		case "tool":
			if streakTools[e.Tool] {
				if e.Failed {
					s.editStreak++
				} else {
					s.editStreak = 0
				}
			}
			r := s.record(turn)
			if e.Failed {
				r.failed++
			}
			see(s.inspect(turn))
		case "turn_end":
			see(s.inspect(e.Turn))
		case "compaction":
			s.history, s.editStreak, s.lastNudgeKey = nil, 0, ""
		case "transaction":
			if e.Transaction != s.transaction {
				s.transaction, s.firedInTransaction, s.editStreak, s.lastNudgeKey = e.Transaction, false, 0, ""
			}
		case "discarded":
			s.discarded++
		case "empty":
			s.empty++
		}
	}
	return out
}

func (s *struggle) record(turn int) *struggleTurn {
	if n := len(s.history); n > 0 && s.history[n-1].turn == turn {
		return s.history[n-1]
	}
	r := &struggleTurn{turn: turn}
	s.history = append(s.history, r)
	for len(s.history) > StruggleWindow*2 {
		s.history = s.history[1:]
	}
	return r
}

func (s *struggle) noteTurn(turn int, text string) {
	r := s.record(turn)
	if text == "" {
		return
	}
	for _, re := range distressRE {
		if re.MatchString(text) {
			r.distress++
		}
	}
	for _, re := range hedgingRE {
		r.hedging += len(re.FindAllStringIndex(text, -1))
	}
	r.words += len(strings.Fields(text))
	// The baseline is the opening turns' hedging rate.
	if turn > StruggleWindow {
		return
	}
	h, w := 0, 0
	for _, t := range s.history {
		if t.turn <= StruggleWindow {
			h, w = h+t.hedging, w+t.words
		}
	}
	s.baseline = perThousand(h, w)
}

func perThousand(n, words int) *float64 {
	if words == 0 {
		return nil
	}
	v := float64(n) * 1000 / float64(words)
	return &v
}

func (s *struggle) signals(turn int) StruggleFiring {
	f := StruggleFiring{Turn: turn}
	h, w := 0, 0
	for _, t := range s.history {
		if t.turn > turn-StruggleWindow {
			f.FailedCalls += t.failed
			f.Distress += t.distress
			h, w = h+t.hedging, w+t.words
		}
	}
	if r := perThousand(h, w); r != nil && s.baseline != nil && *s.baseline > 0 {
		v := *r / *s.baseline
		f.HedgingRatio = &v
	}
	return f
}

func (s *struggle) inspect(turn int) *StruggleFiring {
	if s.firedInTransaction {
		return nil
	}
	if n := s.editStreak; n >= StruggleEditStreak && n%StruggleEditStreak == 0 {
		if f := s.nudge("edit-streak", "e"+strconv.Itoa(n), StruggleFiring{Turn: turn, EditStreak: n}); f != nil {
			return f
		}
	}
	if n := s.discarded + s.empty; n >= StruggleFailedTransactions && n%StruggleFailedTransactions == 0 {
		if f := s.nudge("transactions", "t"+strconv.Itoa(n), StruggleFiring{Turn: turn, ThrownAway: n}); f != nil {
			return f
		}
	}
	if turn < StruggleMinIteration {
		return nil
	}
	sig := s.signals(turn)
	if sig.FailedCalls < StruggleFailedCalls || s.firedInTask >= StruggleMaxPerTask {
		return s.nudgeAt(sig)
	}
	distress := sig.Distress >= StruggleDistressHits
	hedging := sig.HedgingRatio != nil && *sig.HedgingRatio >= 1
	if !distress && !hedging {
		return s.nudgeAt(sig)
	}
	key := ""
	if distress {
		key += "d"
	}
	if hedging {
		key += "h"
	}
	files := make([]string, 0, len(s.changed))
	for f := range s.changed {
		files = append(files, f)
	}
	sort.Strings(files)
	key += "|" + strings.Join(files, "\x00")
	if key == s.lastFiredKey {
		return s.nudgeAt(sig)
	}
	s.lastFiredKey, s.firedInTransaction = key, true
	s.firedInTask++
	sig.Kind, sig.Reason = StruggleSuggest, "failures"
	return &sig
}

func (s *struggle) nudgeAt(sig StruggleFiring) *StruggleFiring {
	if StruggleFailedCalls <= 1 || sig.FailedCalls < StruggleFailedCalls-1 {
		return nil
	}
	return s.nudge("failures", "f"+strconv.Itoa(sig.FailedCalls), sig)
}

func (s *struggle) nudge(reason, tag string, f StruggleFiring) *StruggleFiring {
	key := reason + ":" + tag
	if key == s.lastNudgeKey {
		return nil
	}
	s.lastNudgeKey = key
	f.Kind, f.Reason = StruggleNudge, reason
	return &f
}

// StruggleFailure is how the detector reads a tool's result: the failure
// it states, and whether it states one (the SDK's `ET`). A list fails only
// when every item does; a string is read when it holds a JSON document; an
// object fails on success false or on an error without success true, and
// is read through its result when it says neither.
func StruggleFailure(raw json.RawMessage) (string, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return structFailure(v)
}

func structFailure(v any) (string, bool) {
	switch t := v.(type) {
	case []any:
		if len(t) == 0 {
			return "", false
		}
		first, set := "", false
		for _, e := range t {
			msg, ok := structFailure(e)
			if !ok {
				return "", false
			}
			if !set {
				first, set = msg, true
			}
		}
		return first, true
	case string:
		s := strings.TrimSpace(t)
		if s == "" || !(strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) {
			return "", false
		}
		var in any
		if json.Unmarshal([]byte(s), &in) != nil {
			return "", false
		}
		return structFailure(in)
	case map[string]any:
		msg, hasMsg := "", false
		if e, ok := t["error"].(string); ok && strings.TrimSpace(e) != "" {
			msg, hasMsg = e, true
		}
		success, hasSuccess := t["success"]
		if b, ok := success.(bool); ok && !b {
			if hasMsg {
				return msg, true
			}
			return "the call did not succeed", true
		}
		if b, ok := success.(bool); hasMsg && !(ok && b) {
			return msg, true
		}
		if result, ok := t["result"]; !hasSuccess && ok {
			return structFailure(result)
		}
	}
	return "", false
}
