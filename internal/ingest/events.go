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

	"github.com/mann1x/automaticum/internal/detect"
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
	// Calls holds the same calls with their arguments, for the detectors.
	Calls  []detect.Call
	Blocks []Block
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
	// UserActions is how many times a rule of the scripted user acted;
	// UserDenials how many tool calls it denied, by a rule or by default.
	UserActions int
	UserDenials int
	// UserFailed is how many of the scripted user's actions the SDK
	// refused: the run did not play the script it was meant to.
	UserFailed int
	// Forks are the session forks a restore of messages started (D86), in
	// order; ModeSwitches how often the scripted user switched the mode.
	Forks        []Fork
	ModeSwitches int
}

// Fork is one session fork of a run (D86): when it started, the
// checkpoint it went back to, and what the SDK started again from nothing
// in the new session, as the runner noted it.
type Fork struct {
	T          time.Time `json:"t"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Checkpoint int       `json:"checkpoint"`
	Workspace  bool      `json:"workspace"`
	Restarted  []string  `json:"restarted"`
}

// eventLine is one line of a stream. The runner writes
//
//	{"t":..., "k":"event", "event":{"type":"agent_event", "payload":{"event":{...}}}}
//
// and the older harness, whose runs are imported, wrote
//
//	{"ts":..., "type":"agent_event", "event":{...}}
//
// The innermost object is the SDK's event in both.
type eventLine struct {
	T       time.Time          `json:"t"`
	K       string             `json:"k"`
	Type    string             `json:"type"`
	Note    string             `json:"note"`
	Event   json.RawMessage    `json:"event"`
	Derived map[string]float64 `json:"derived"`
	// A scripted user's line (040 §6.2): the event it answered, the rule
	// (0 for the default) and what it did.
	On       string `json:"on"`
	Rule     int    `json:"rule"`
	Approved *bool  `json:"approved"`
	// A runner's fork note (D86).
	From       string   `json:"from"`
	To         string   `json:"to"`
	Checkpoint int      `json:"checkpoint"`
	Workspace  bool     `json:"workspace"`
	Restarted  []string `json:"restarted"`
}

type agentEvent struct {
	Type        string          `json:"type"`
	ContentType string          `json:"contentType"`
	Reasoning   string          `json:"reasoning"`
	Text        string          `json:"text"`
	ToolName    string          `json:"toolName"`
	ToolCallID  string          `json:"toolCallId"`
	Iteration   int             `json:"iteration"`
	Input       json.RawMessage `json:"input"`
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

// scan is what is carried from line to line while a stream is read.
type scan struct {
	turn    int
	started map[string]time.Time // tool calls, by id
	inputs  map[string]string
	// What a block's deltas add up to, by kind, for a stream whose
	// "content_end" does not repeat the whole block.
	partial map[string]*bytes.Buffer
}

// ReadEvents reads a run's event stream: the JSON lines the runner printed.
func ReadEvents(r io.Reader) (*Events, error) {
	ev := &Events{Notices: map[string]int{}}
	return ev, ev.Read(r)
}

// Read adds one more stream to what was read: an imported run may have
// several.
func (ev *Events) Read(r io.Reader) error {
	rd := bufio.NewReaderSize(r, 1<<20)
	sc := &scan{started: map[string]time.Time{}, inputs: map[string]string{}, partial: map[string]*bytes.Buffer{}}
	for {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			ev.Lines++
			if !bytes.Contains(line[:min(len(line), 200)], chunkMark) {
				ev.line(line, sc)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (ev *Events) line(line []byte, sc *scan) {
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
	var a agentEvent
	switch {
	case l.K == "runner":
		switch l.Note {
		case "config_resolved":
			ev.Derived = l.Derived
		case "ask_question":
			// Protocol 1.
			ev.Questions++
		case "user_action_failed":
			ev.UserFailed++
		case "fork":
			ev.Forks = append(ev.Forks, Fork{T: l.T, From: l.From, To: l.To, Checkpoint: l.Checkpoint, Workspace: l.Workspace, Restarted: l.Restarted})
		case "mode_switched":
			ev.ModeSwitches++
		}
		return
	case l.K == "user":
		if l.On == "question" {
			ev.Questions++
		}
		if l.Rule > 0 && l.On != "resume" {
			ev.UserActions++
		}
		if l.On == "approval" && l.Approved != nil && !*l.Approved {
			ev.UserDenials++
		}
		return
	case l.K == "event":
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Event agentEvent `json:"event"`
			} `json:"payload"`
		}
		if len(l.Event) == 0 {
			return
		}
		if json.Unmarshal(l.Event, &e) != nil {
			ev.BadLines++
			return
		}
		if e.Type != "agent_event" {
			return
		}
		a = e.Payload.Event
	case l.K == "" && l.Type == "agent_event" && len(l.Event) > 0:
		if json.Unmarshal(l.Event, &a) != nil {
			ev.BadLines++
			return
		}
	default:
		return
	}
	switch a.Type {
	case "iteration_start":
		sc.turn = a.Iteration
		if a.Iteration > ev.Iterations {
			ev.Iterations = a.Iteration
		}
	case "content_start":
		switch a.ContentType {
		case "tool":
			if a.ToolCallID != "" {
				sc.started[a.ToolCallID] = l.T
				sc.inputs[a.ToolCallID] = compact(a.Input)
			}
		case "reasoning", "text":
			buf := sc.partial[a.ContentType]
			if buf == nil {
				buf = &bytes.Buffer{}
				sc.partial[a.ContentType] = buf
			}
			buf.WriteString(a.Reasoning + a.Text)
		}
	case "content_end":
		switch a.ContentType {
		case "reasoning", "text":
			text := a.Reasoning + a.Text
			if buf := sc.partial[a.ContentType]; buf != nil {
				if text == "" {
					text = buf.String()
				}
				buf.Reset()
			}
			ev.Blocks = append(ev.Blocks, Block{Turn: sc.turn, Kind: a.ContentType, Text: text})
		case "tool":
			call := store.ToolCall{Turn: sc.turn, Seq: len(ev.ToolCalls), Tool: a.ToolName, OK: true}
			if failed(&a) {
				call.OK, call.ErrorClass = false, "tool_error"
			}
			switch t0, ok := sc.started[a.ToolCallID]; {
			case a.DurationMS != nil:
				call.DurationMS = int64(*a.DurationMS)
			case ok && !l.T.IsZero():
				call.DurationMS = l.T.Sub(t0).Milliseconds()
			}
			input := compact(a.Input)
			if input == "" {
				input = sc.inputs[a.ToolCallID]
			}
			delete(sc.started, a.ToolCallID)
			delete(sc.inputs, a.ToolCallID)
			ev.ToolCalls = append(ev.ToolCalls, call)
			ev.Calls = append(ev.Calls, detect.Call{Turn: sc.turn, Tool: a.ToolName, Input: input})
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

// compact gives a call's arguments one spelling, so that the same arguments
// compare equal however they were spaced.
func compact(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v) // maps are written with sorted keys
	return string(b)
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
