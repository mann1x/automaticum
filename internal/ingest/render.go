package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Renderer turns the runner's event lines into a transcript a person reads
// (090 §1, `run tail`, `logs`): a heading per turn, each reasoning and
// text block whole when it ends, each tool call with its arguments and how
// it ended, notices, the runner's notes, the scripted user and the end. It
// keeps the turn it is in, so it is fed the lines in order.
type Renderer struct {
	// Max cuts a block or an argument longer than this many characters;
	// zero keeps everything.
	Max  int
	turn int
}

// Turn is the turn of the last line read.
func (r *Renderer) Turn() int { return r.turn }

// Line renders one line; it gives nothing for a line with nothing to show
// (a block's start, a token count).
func (r *Renderer) Line(line []byte) []string {
	var l eventLine
	if err := json.Unmarshal(line, &l); err != nil {
		return []string{"(unreadable line) " + r.cut(string(line))}
	}
	switch l.K {
	case "runner":
		return []string{"[runner] " + r.runnerNote(line, l)}
	case "user":
		var u map[string]any
		json.Unmarshal(line, &u)
		delete(u, "t")
		delete(u, "k")
		delete(u, "on")
		b, _ := json.Marshal(u)
		return []string{fmt.Sprintf("[user] on %s: %s", l.On, r.cut(string(b)))}
	case "end":
		var e map[string]any
		json.Unmarshal(line, &e)
		parts := []string{fmt.Sprint(e["outcome"])}
		for _, k := range []string{"reason", "agent_end", "claimed_success", "iterations", "requests"} {
			if v, ok := e[k]; ok && v != nil && v != "" {
				parts = append(parts, fmt.Sprintf("%s %v", strings.ReplaceAll(k, "_", " "), v))
			}
		}
		return []string{"[end] " + strings.Join(parts, "; ")}
	}
	a, ok := agentOf(l)
	if !ok {
		return nil
	}
	switch a.Type {
	case "iteration_start":
		r.turn = a.Iteration
		return []string{fmt.Sprintf("── turn %d ──", a.Iteration)}
	case "content_end":
		switch a.ContentType {
		case "reasoning":
			return []string{"[reasoning] " + r.cut(strings.TrimSpace(a.Reasoning+a.Text))}
		case "text":
			return []string{"[text] " + r.cut(strings.TrimSpace(a.Reasoning+a.Text))}
		case "tool":
			how := "ok"
			if failed(&a) {
				how = "error: " + r.cut(strings.TrimSpace(string(a.Error)+" "+string(a.Output)))
			}
			dur := ""
			if a.DurationMS != nil {
				dur = fmt.Sprintf(", %.1f s", *a.DurationMS/1000)
			}
			return []string{fmt.Sprintf("[tool] %s %s → %s%s", a.ToolName, r.cut(compact(a.Input)), how, dur)}
		}
	case "notice":
		key := NoticeKey(a.NoticeType, a.Metadata.Kind, a.Metadata.Phase, a.Reason)
		return []string{fmt.Sprintf("[notice] %s: %s", key, r.cut(a.Message))}
	case "done":
		return []string{"[done] " + a.Reason}
	}
	return nil
}

// agentOf is the SDK's agent event inside a line, in either spelling.
func agentOf(l eventLine) (agentEvent, bool) {
	var a agentEvent
	switch {
	case l.K == "event":
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Event agentEvent `json:"event"`
			} `json:"payload"`
		}
		if len(l.Event) == 0 || json.Unmarshal(l.Event, &e) != nil || e.Type != "agent_event" {
			return a, false
		}
		return e.Payload.Event, true
	case l.K == "" && l.Type == "agent_event" && len(l.Event) > 0:
		return a, json.Unmarshal(l.Event, &a) == nil
	}
	return a, false
}

func (r *Renderer) runnerNote(line []byte, l eventLine) string {
	var m map[string]any
	json.Unmarshal(line, &m)
	for _, k := range []string{"t", "k", "note", "derived"} {
		delete(m, k)
	}
	if len(m) == 0 {
		return l.Note
	}
	b, _ := json.Marshal(m)
	return l.Note + " " + r.cut(string(b))
}

func (r *Renderer) cut(s string) string {
	if r.Max > 0 && len(s) > r.Max {
		return s[:r.Max] + fmt.Sprintf(" … (%d more characters)", len(s)-r.Max)
	}
	return s
}
