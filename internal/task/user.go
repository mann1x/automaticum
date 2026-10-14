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
	// Categories is the extension's map of approval categories, sent to
	// the runner with the script; it is not the task's to state.
	Categories map[string][]string `toml:"-" json:"categories"`
}

// ApprovalCategories are the extension's approval categories and the tools
// in each (Cline 4.100.251, apps/vscode/src/sdk/sdk-tool-policies.ts, as
// the Cerebriline session read it on 2026-10-14). A tool in none of them
// never asks for approval in the extension, so it never does here either:
// check_file, restore_file, task_progress, ask_question, spawn_agent and
// the rest run without asking. There is no "safe command" test: one
// category holds every command. MCP tools are not here: no arm starts an
// MCP server yet.
var ApprovalCategories = map[string][]string{
	"read":     {"read_files", "read_file", "list_files", "list_code_definition_names", "search_codebase", "search_files", "grep", "awk"},
	"edit":     {"editor", "replace_in_file", "write_to_file", "apply_patch", "delete_file", "sed"},
	"commands": {"run_commands", "execute_command"},
	"web":      {"fetch_web_content", "web_fetch", "web_search"},
	"browser":  {"browser", "browser_action", "render_image"},
}

// categoryOf is the category of a tool, or "" when it never asks.
func categoryOf(tool string) string {
	for c, tools := range ApprovalCategories {
		if contains(tools, tool) {
			return c
		}
	}
	return ""
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
	// Tool narrows an approval to one tool, by name; Category to the tools
	// of one of the extension's approval categories.
	Tool     string `toml:"tool" json:"tool,omitempty"`
	Category string `toml:"category" json:"category,omitempty"`
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
	// Mode is the mode a switch goes to: "act" or "plan".
	Mode string `toml:"mode" json:"mode,omitempty"`
	// Restore is what a restore brings back: "workspace" (the files, in
	// the same session), "messages" (the conversation up to the
	// checkpoint, in a new session: D86) or "both".
	Restore string `toml:"restore" json:"restore,omitempty"`
	// Checkpoint is the checkpoint restored, by the SDK's run count.
	Checkpoint int `toml:"checkpoint" json:"checkpoint,omitempty"`
}

// allowed is what each event may be answered with. A plan is a run of
// the session in plan mode that the agent ended by itself.
var allowed = map[string][]string{
	"approval":   {"approve", "deny"},
	"question":   {"answer", "option"},
	"turn":       {"steer", "queue", "cancel", "restore", "mode"},
	"elapsed":    {"steer", "queue", "cancel", "restore", "mode"},
	"completion": {"queue", "mode"},
	"plan":       {"queue", "mode"},
}

// validate says what is wrong with a scripted user, all of it at once, and
// states each rule's Times.
func (u *User) validate() []string {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	u.Categories = ApprovalCategories
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
			bad("user.rule %d: on must be approval, question, turn, elapsed, completion or plan, not %q", i+1, r.On)
			continue
		}
		switch {
		case !contains(dos, r.Do):
			bad("%s: do must be %s, not %q", where, strings.Join(dos, ", "), r.Do)
		}
		if (r.Tool != "" || r.Category != "") && r.On != "approval" {
			bad("%s: tool and category narrow an approval only", where)
		}
		switch {
		case r.Tool != "" && r.Category != "":
			bad("%s: name a tool or a category, not both", where)
		case r.Tool != "" && categoryOf(r.Tool) == "":
			bad("%s: %s never asks for approval (it is in no approval category of the extension), so the rule could never act", where, r.Tool)
		case r.Category == "mcp":
			bad("%s: category mcp: no arm starts an MCP server yet", where)
		case r.Category != "" && ApprovalCategories[r.Category] == nil:
			bad("%s: category must be read, edit, commands, web or browser, not %q", where, r.Category)
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
		// A switch to act may say nothing: the extension then sends its own
		// sentence. A fork starts with the message in text.
		fork := r.Do == "restore" && (r.Restore == "messages" || r.Restore == "both")
		needText := r.Do == "answer" || r.Do == "steer" || r.Do == "queue" || r.Do == "cancel" || fork || (r.Do == "mode" && r.Mode == "plan")
		mayText := needText || r.Do == "deny" || r.Do == "mode"
		switch {
		case needText && strings.TrimSpace(r.Text) == "":
			bad("%s: do = %q needs text", where, r.Do)
		case r.Text != "" && !mayText:
			bad("%s: text does not go with do = %q", where, r.Do)
		}
		if r.Do == "mode" {
			if r.Mode != "act" && r.Mode != "plan" {
				bad("%s: do = \"mode\" needs mode = \"act\" or \"plan\"", where)
			}
		} else if r.Mode != "" {
			bad("%s: mode goes with do = \"mode\" only", where)
		}
		if (r.Do == "option") != (r.Option > 0) {
			bad("%s: option, from 1, goes with do = \"option\" and only with it", where)
		}
		if r.Do == "restore" {
			if r.Restore != "workspace" && r.Restore != "messages" && r.Restore != "both" {
				bad("%s: restore must be \"workspace\", \"messages\" or \"both\"", where)
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
