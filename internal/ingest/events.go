// Package ingest turns a run's stored raw material into what is queried:
// metrics, tool calls and findings (spec 070). It reads the event stream and
// the wire capture from blobs, so the same code serves a run that has just
// ended and a run analysed again years later.
package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

// Block is one finished piece of what the model wrote: its reasoning or its
// visible text. Detectors work on blocks.
type Block struct {
	Turn int
	Kind string // "reasoning" or "text"
	Text string
}

// Events is what one pass over a run's event stream gives.
type Events struct {
	Lines, BadLines int
	First, Last     time.Time
	Iterations      int
	AgentEnd        string
	ToolCalls       []store.ToolCall
	Blocks          []Block
	// Notices counts the SDK's notices by reason; a notice without a reason
	// is counted under its type and message.
	Notices map[string]int
	// FirstTokenMS is the time to the first token of the first request.
	FirstTokenMS float64
	// Tokens by kind, as the SDK split them, and how it split.
	ReasoningTokens, TextTokens, ToolInputTokens int
	SplitMethod                                  string
	// Derived holds the values the SDK worked out from the arm, as the
	// runner noted them.
	Derived map[string]float64
	// Questions is how many questions the scripted user answered.
	Questions int
}

type eventLine struct {
	T     time.Time `json:"t"`
	K     string    `json:"k"`
	Note  string    `json:"note"`
	Event *struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	} `json:"event"`
	Derived map[string]float64 `json:"derived"`
}

type agentEvent struct {
	Type        string          `json:"type"`
	ContentType string          `json:"contentType"`
	Reasoning   string          `json:"reasoning"`
	Text        string          `json:"text"`
	ToolName    string          `json:"toolName"`
	ToolCallID  string          `json:"toolCallId"`
	Iteration   int             `json:"iteration"`
	Error       json.RawMessage `json:"error"`
	Output      json.RawMessage `json:"output"`
	DurationMS  *float64        `json:"durationMs"`
	NoticeType  string          `json:"noticeType"`
	Reason      string          `json:"reason"`
	Message     string          `json:"message"`
	Timings     struct {
		FirstTokenMS float64 `json:"firstTokenMs"`
	} `json:"timings"`
	OutputSplit *struct {
		ReasoningTokens int    `json:"reasoningTokens"`
		TextTokens      int    `json:"textTokens"`
		ToolInputTokens int    `json:"toolInputTokens"`
		Method          string `json:"method"`
	} `json:"outputSplit"`
}

// chunkMark is how a "chunk" event starts. Chunks repeat the agent's events
// as text and make up half the stream; they are passed over unparsed.
var chunkMark = []byte(`"event":{"type":"chunk"`)

// ReadEvents reads a run's event stream: the JSON lines the runner printed.
func ReadEvents(r io.Reader) (*Events, error) {
	ev := &Events{Notices: map[string]int{}}
	rd := bufio.NewReaderSize(r, 1<<20)
	turn := 0
	started := map[string]time.Time{}
	for {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			ev.Lines++
			if !bytes.Contains(line[:min(len(line), 200)], chunkMark) {
				ev.line(line, &turn, started)
			}
		}
		if err == io.EOF {
			return ev, nil
		}
		if err != nil {
			return ev, err
		}
	}
}

func (ev *Events) line(line []byte, turn *int, started map[string]time.Time) {
	var l eventLine
	if err := json.Unmarshal(line, &l); err != nil {
		ev.BadLines++
		return
	}
	if !l.T.IsZero() {
		if ev.First.IsZero() {
			ev.First = l.T
		}
		ev.Last = l.T
	}
	switch l.K {
	case "runner":
		switch l.Note {
		case "config_resolved":
			ev.Derived = l.Derived
		case "ask_question":
			ev.Questions++
		}
		return
	case "event":
	default:
		return
	}
	if l.Event == nil || l.Event.Type != "agent_event" {
		return
	}
	var p struct {
		Event agentEvent `json:"event"`
	}
	if json.Unmarshal(l.Event.Payload, &p) != nil {
		ev.BadLines++
		return
	}
	a := &p.Event
	switch a.Type {
	case "iteration_start":
		*turn = a.Iteration
		if a.Iteration > ev.Iterations {
			ev.Iterations = a.Iteration
		}
	case "content_start":
		if a.ContentType == "tool" && a.ToolCallID != "" {
			started[a.ToolCallID] = l.T
		}
	case "content_end":
		switch a.ContentType {
		case "reasoning":
			ev.Blocks = append(ev.Blocks, Block{Turn: *turn, Kind: "reasoning", Text: a.Reasoning})
		case "text":
			ev.Blocks = append(ev.Blocks, Block{Turn: *turn, Kind: "text", Text: a.Text})
		case "tool":
			call := store.ToolCall{Turn: *turn, Seq: len(ev.ToolCalls), Tool: a.ToolName, OK: true}
			if failed(a) {
				call.OK, call.ErrorClass = false, "tool_error"
			}
			switch t0, ok := started[a.ToolCallID]; {
			case a.DurationMS != nil:
				call.DurationMS = int64(*a.DurationMS)
			case ok && !l.T.IsZero():
				call.DurationMS = l.T.Sub(t0).Milliseconds()
			}
			delete(started, a.ToolCallID)
			ev.ToolCalls = append(ev.ToolCalls, call)
		}
	case "usage":
		if ev.FirstTokenMS == 0 {
			ev.FirstTokenMS = a.Timings.FirstTokenMS
		}
		if s := a.OutputSplit; s != nil {
			ev.ReasoningTokens += s.ReasoningTokens
			ev.TextTokens += s.TextTokens
			ev.ToolInputTokens += s.ToolInputTokens
			ev.SplitMethod = s.Method
		}
	case "notice":
		key := a.Reason
		if key == "" {
			key = a.NoticeType + ":" + a.Message
		}
		ev.Notices[key]++
	case "done":
		ev.AgentEnd = a.Reason
	}
}

// failed reports whether a tool call's end says it failed: an error on the
// event itself, or a result that says so.
func failed(a *agentEvent) bool {
	if e := bytes.TrimSpace(a.Error); len(e) > 0 && !bytes.Equal(e, []byte("null")) && !bytes.Equal(e, []byte(`""`)) {
		return true
	}
	type result struct {
		Success *bool           `json:"success"`
		Error   json.RawMessage `json:"error"`
	}
	bad := func(r result) bool {
		if r.Success != nil && !*r.Success {
			return true
		}
		e := bytes.TrimSpace(r.Error)
		return len(e) > 0 && !bytes.Equal(e, []byte("null")) && !bytes.Equal(e, []byte(`""`))
	}
	out := bytes.TrimSpace(a.Output)
	switch {
	case len(out) == 0:
		return false
	case out[0] == '[':
		var list []result
		if json.Unmarshal(out, &list) == nil {
			for _, r := range list {
				if bad(r) {
					return true
				}
			}
		}
	case out[0] == '{':
		var one result
		return json.Unmarshal(out, &one) == nil && bad(one)
	case out[0] == '"':
		// Some tools answer with a JSON document inside a string.
		var s string
		if json.Unmarshal(out, &s) == nil && len(s) > 0 && s[0] == '{' {
			var one result
			return json.Unmarshal([]byte(s), &one) == nil && bad(one)
		}
	}
	return false
}
