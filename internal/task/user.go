package task

import (
	"fmt"
	"regexp"
	"strings"
)

// User is the scripted user of a task (040 §6.2): rules that answer the
// agent's approval requests and questions, and act at chosen moments, and a
// default for every event no rule matches. It is part of the task, so part
// of its version. A task without one is answered as protocol 1 always did:
// every tool approved, every question given DefaultAnswer.
type User struct {
	Default UserDefault `toml:"default" json:"default"`
	Rules   []UserRule  `toml:"rule" json:"rules,omitempty"`
}

// UserDefault is what happens to an approval or a question no rule matches.
// Both are stated: an unanswered question never hangs a run.
type UserDefault struct {
	// Approval is "approve" or "deny".
	Approval string `toml:"approval" json:"approval"`
	// Feedback goes with a default denial; optional.
	Feedback string `toml:"feedback" json:"feedback,omitempty"`
	// Answer is given to a question no rule answers.
	Answer string `toml:"answer" json:"answer"`
}

// UserRule matches one kind of event and says what the user does.
type UserRule struct {
	// On is the event: "approval" (a tool asks to run), "question" (the
	// agent asks with ask_question), "turn" (the agent starts turn Turn),
	// "elapsed" (AfterS seconds of the run have passed, time held left out)
	// or "completion" (the agent ends the task by itself).
	On string `toml:"on" json:"on"`
	// Tool narrows an approval to one tool, by name.
	Tool string `toml:"tool" json:"tool,omitempty"`
	// Match narrows an approval (on the tool's input as JSON) or a question
	// (on its text) to those a pattern finds. The pattern must mean the
	// same in Go and in JavaScript: no inline flags.
	Match  string `toml:"match" json:"match,omitempty"`
	Turn   int    `toml:"turn" json:"turn,omitempty"`
	AfterS int    `toml:"after_s" json:"after_s,omitempty"`
	// Times is how often the rule may act: 1 when not stated, 0 for every
	// time it matches.
	Times *int `toml:"times" json:"times"`
	// Do is the response: "approve" or "deny" to an approval; "answer" or
	// "option" to a question; "steer" (a message now, into the running
	// turn), "queue" (a message for when the turn ends), "cancel" (cancel
	// the running work, then resume with Text) or "restore" (a checkpoint)
	// at a turn, a time or a completion.
	Do string `toml:"do" json:"do"`
	// Text is the denial's feedback, the answer, or the message.
	Text string `toml:"text" json:"text,omitempty"`
	// Option picks a question's suggested option, from 1.
	Option int `toml:"option" json:"option,omitempty"`
	// Restore is what a restore brings back: "workspace" so far.
	Restore string `toml:"restore" json:"restore,omitempty"`
	// Checkpoint is the checkpoint restored, by the SDK's run count.
	Checkpoint int `toml:"checkpoint" json:"checkpoint,omitempty"`
}

// allowed is what each event may be answered with. Mode switches and a
// restore of the messages wait for the SDK's answers (stage 2).
var allowed = map[string][]string{
	"approval":   {"approve", "deny"},
	"question":   {"answer", "option"},
	"turn":       {"steer", "queue", "cancel", "restore"},
	"elapsed":    {"steer", "queue", "cancel", "restore"},
	"completion": {"queue"},
}

// validate says what is wrong with a scripted user, all of it at once, and
// states each rule's Times.
func (u *User) validate() []string {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	switch u.Default.Approval {
	case "approve", "deny":
	default:
		bad(`user.default.approval must be "approve" or "deny"`)
	}
	if u.Default.Feedback != "" && u.Default.Approval != "deny" {
		bad("user.default.feedback goes with a default denial")
	}
	if strings.TrimSpace(u.Default.Answer) == "" {
		bad("user.default.answer must be stated: an unanswered question never hangs a run")
	}
	for i := range u.Rules {
		r := &u.Rules[i]
		where := fmt.Sprintf("user.rule %d (on %q)", i+1, r.On)
		dos, ok := allowed[r.On]
		if !ok {
			bad("user.rule %d: on must be approval, question, turn, elapsed or completion, not %q", i+1, r.On)
			continue
		}
		switch {
		case r.Do == "mode":
			bad("%s: a mode switch is not built yet (stage 2)", where)
		case !contains(dos, r.Do):
			bad("%s: do must be %s, not %q", where, strings.Join(dos, ", "), r.Do)
		}
		if r.Tool != "" && r.On != "approval" {
			bad("%s: tool narrows an approval only", where)
		}
		if r.Match != "" {
			if r.On != "approval" && r.On != "question" {
				bad("%s: match narrows an approval or a question only", where)
			} else if strings.Contains(r.Match, "(?") {
				bad("%s: match must mean the same in Go and JavaScript: no inline flags or (?...) groups", where)
			} else if _, err := regexp.Compile(r.Match); err != nil {
				bad("%s: match: %v", where, err)
			}
		}
		if (r.On == "turn") != (r.Turn > 0) {
			bad("%s: turn, from 1, goes with on = \"turn\" and only with it", where)
		}
		if (r.On == "elapsed") != (r.AfterS > 0) {
			bad("%s: after_s, more than 0, goes with on = \"elapsed\" and only with it", where)
		}
		if r.Times == nil {
			one := 1
			r.Times = &one
		} else if *r.Times < 0 {
			bad("%s: times must be 0 (every time) or more", where)
		}
		needText := r.Do == "answer" || r.Do == "steer" || r.Do == "queue" || r.Do == "cancel"
		switch {
		case needText && strings.TrimSpace(r.Text) == "":
			bad("%s: do = %q needs text", where, r.Do)
		case r.Text != "" && !needText && r.Do != "deny":
			bad("%s: text does not go with do = %q", where, r.Do)
		}
		if (r.Do == "option") != (r.Option > 0) {
			bad("%s: option, from 1, goes with do = \"option\" and only with it", where)
		}
		if r.Do == "restore" {
			switch {
			case r.Restore == "messages" || r.Restore == "both":
				bad("%s: a restore of the messages is not built yet (stage 2, D86)", where)
			case r.Restore != "workspace":
				bad("%s: restore must be \"workspace\"", where)
			}
			if r.Checkpoint < 1 {
				bad("%s: checkpoint, from 1, names the checkpoint restored", where)
			}
		} else if r.Restore != "" || r.Checkpoint != 0 {
			bad("%s: restore and checkpoint go with do = \"restore\" only", where)
		}
	}
	return problems
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
