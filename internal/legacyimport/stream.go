package legacyimport

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// The old harness recorded the CLI's wrapped event stream (--json), one JSON
// object per line:
//
//	{"ts":..., "type":"agent_event", "event":{"type":"content_start", ...}}
//	{"ts":..., "type":"hook_event", "hookEventName":"tool_call", ...}
//	{"ts":..., "type":"run_result", "finishReason":..., "model":{...}}
//	{"ts":..., "type":"run_aborted", "reason":..., "message":...}
//
// The SDK's event is the inner "event" object of an agent_event line. The
// other three line types are the CLI's own.

// DefaultMaxLine bounds one line of an event stream. A longer line is
// counted and dropped, never buffered.
const DefaultMaxLine = 64 << 20

// StreamStats is what one or more event streams of a run showed.
type StreamStats struct {
	Streams       int   `json:"streams"`
	Bytes         int64 `json:"bytes"` // uncompressed
	Lines         int64 `json:"lines"`
	BadLines      int64 `json:"bad_lines"`      // not JSON
	OversizeLines int64 `json:"oversize_lines"` // dropped unread

	TopLevel map[string]int64 `json:"top_level"`   // by line type
	Inner    map[string]int64 `json:"agent_event"` // by inner event type
	Hooks    map[string]int64 `json:"hook_event"`  // by hook name

	ReasoningDeltas int64 `json:"reasoning_deltas"`
	TextDeltas      int64 `json:"text_deltas"`
	// AccumulatedLines counts deltas that still carry the accumulated-text
	// field. Zero on a stream with text deltas means the field was stripped.
	AccumulatedLines int64 `json:"accumulated_lines"`
	ErrorEvents      int64 `json:"error_events"`
	UsageEvents      int64 `json:"usage_events"`
	Iterations       int   `json:"iterations"`

	FirstTS time.Time `json:"first_ts"`
	LastTS  time.Time `json:"last_ts"`

	FinishReason  string `json:"finish_reason,omitempty"` // run_result.finishReason
	DoneReason    string `json:"done_reason,omitempty"`   // inner done.reason
	AbortReason   string `json:"abort_reason,omitempty"`  // run_aborted.reason
	AbortMessage  string `json:"abort_message,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	ContextWindow int64  `json:"context_window,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	HasRunResult  bool   `json:"has_run_result"`
}

// StreamScanner scans an event stream as it is written to it. It is an
// io.Writer so a stream can be scanned while it is copied somewhere else
// (io.TeeReader), in one pass and without holding it.
//
// One scanner may be fed several streams one after the other (the atomic
// loop has one per transaction): call EndStream between them. Turns and
// sequence numbers continue across streams.
type StreamScanner struct {
	MaxLine int

	stats StreamStats
	calls []ToolCall

	buf      []byte
	skipping bool

	turn     int // iteration of the stream being read
	turnBase int // iterations of the streams before it
	maxTurn  int
	open     map[string]int // tool call id -> index in calls
	sumIn    int64
	sumOut   int64
	gotTotal bool // a run_result gave this stream's totals

	firstTS []byte
	lastTS  []byte
}

// NewStreamScanner returns a scanner with the default line limit.
func NewStreamScanner() *StreamScanner {
	return &StreamScanner{
		MaxLine: DefaultMaxLine,
		open:    map[string]int{},
		stats: StreamStats{
			TopLevel: map[string]int64{},
			Inner:    map[string]int64{},
			Hooks:    map[string]int64{},
		},
	}
}

// Write feeds bytes of the stream. It never fails.
func (s *StreamScanner) Write(p []byte) (int, error) {
	n := len(p)
	s.stats.Bytes += int64(n)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.keep(p)
			break
		}
		switch {
		case s.skipping:
			s.skipping = false
			s.stats.OversizeLines++
			s.stats.Lines++
		case len(s.buf) > 0:
			s.keep(p[:i])
			if s.skipping {
				s.skipping = false
				s.stats.OversizeLines++
				s.stats.Lines++
			} else {
				s.line(s.buf)
			}
			s.resetBuf()
		default:
			s.line(p[:i])
		}
		p = p[i+1:]
	}
	return n, nil
}

func (s *StreamScanner) keep(p []byte) {
	if s.skipping {
		return
	}
	if len(s.buf)+len(p) > s.MaxLine {
		s.skipping = true
		s.resetBuf()
		return
	}
	s.buf = append(s.buf, p...)
}

func (s *StreamScanner) resetBuf() {
	if cap(s.buf) > 1<<20 {
		s.buf = nil
		return
	}
	s.buf = s.buf[:0]
}

// EndStream finishes the current stream: a last line without a newline is
// processed, tool calls that never got a result are closed as failed, and
// the turn counter is carried over to the next stream.
func (s *StreamScanner) EndStream() {
	if s.skipping {
		s.skipping = false
		s.stats.OversizeLines++
		s.stats.Lines++
	} else if len(s.buf) > 0 {
		s.line(s.buf)
	}
	s.resetBuf()
	for id, idx := range s.open {
		s.calls[idx].OK = false
		s.calls[idx].ErrorClass = "no_result"
		delete(s.open, id)
	}
	s.stats.Streams++
	s.turnBase += s.maxTurn
	if !s.gotTotal {
		// No run_result: the stream was cut. Fall back to what it showed.
		s.stats.Iterations += s.maxTurn
		s.stats.InputTokens += s.sumIn
		s.stats.OutputTokens += s.sumOut
	}
	s.turn, s.maxTurn = 0, 0
	s.sumIn, s.sumOut, s.gotTotal = 0, 0, false
	if t, err := time.Parse(time.RFC3339Nano, string(s.firstTS)); err == nil && s.stats.FirstTS.IsZero() {
		s.stats.FirstTS = t
	}
	if t, err := time.Parse(time.RFC3339Nano, string(s.lastTS)); err == nil {
		s.stats.LastTS = t
	}
	s.firstTS, s.lastTS = s.firstTS[:0], s.lastTS[:0]
}

// Stats returns what has been seen so far. Call EndStream first.
func (s *StreamScanner) Stats() StreamStats { return s.stats }

// ToolCalls returns the tool calls seen so far, in order. RunID is empty.
func (s *StreamScanner) ToolCalls() []ToolCall { return s.calls }

var (
	markReasoningDelta = []byte(`"type":"content_start","contentType":"reasoning"`)
	markTextDelta      = []byte(`"type":"content_start","contentType":"text"`)
	markAccumulated    = []byte(`"accumulated":`)
	markTS             = []byte(`{"ts":"`)
)

type rawEvent struct {
	Type         string          `json:"type"`
	ContentType  string          `json:"contentType"`
	ToolName     string          `json:"toolName"`
	ToolCallID   string          `json:"toolCallId"`
	Error        json.RawMessage `json:"error"`
	Output       json.RawMessage `json:"output"`
	DurationMs   float64         `json:"durationMs"`
	Iteration    int             `json:"iteration"`
	Reason       string          `json:"reason"`
	InputTokens  int64           `json:"inputTokens"`
	OutputTokens int64           `json:"outputTokens"`
	Accumulated  json.RawMessage `json:"accumulated"`
}

type rawLine struct {
	TS            string    `json:"ts"`
	Type          string    `json:"type"`
	HookEventName string    `json:"hookEventName"`
	Event         *rawEvent `json:"event"`
	FinishReason  string    `json:"finishReason"`
	Iterations    int       `json:"iterations"`
	DurationMs    float64   `json:"durationMs"`
	Reason        string    `json:"reason"`
	Message       string    `json:"message"`
	Usage         *struct {
		InputTokens  int64 `json:"inputTokens"`
		OutputTokens int64 `json:"outputTokens"`
	} `json:"usage"`
	AggregateUsage *struct {
		InputTokens  int64 `json:"inputTokens"`
		OutputTokens int64 `json:"outputTokens"`
	} `json:"aggregateUsage"`
	Model *struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Info     *struct {
			ContextWindow int64 `json:"contextWindow"`
		} `json:"info"`
	} `json:"model"`
}

func (s *StreamScanner) line(b []byte) {
	b = bytes.TrimRight(b, "\r")
	if len(bytes.TrimSpace(b)) == 0 {
		return
	}
	s.stats.Lines++

	// The timestamp sits at a fixed place; reading it there avoids decoding
	// the reasoning deltas, which are almost all of a stream.
	if bytes.HasPrefix(b, markTS) {
		rest := b[len(markTS):]
		if j := bytes.IndexByte(rest, '"'); j > 0 && j <= 40 {
			s.stamp(rest[:j])
		}
	}

	head := b
	if len(head) > 200 {
		head = head[:200]
	}
	// A quote inside a JSON string is escaped, so these markers can only
	// match the structure of the line, never text carried in it.
	if bytes.Contains(head, markReasoningDelta) {
		s.stats.TopLevel["agent_event"]++
		s.stats.Inner["content_start"]++
		s.stats.ReasoningDeltas++
		if bytes.Contains(b, markAccumulated) {
			s.stats.AccumulatedLines++
		}
		return
	}
	if bytes.Contains(head, markTextDelta) {
		s.stats.TopLevel["agent_event"]++
		s.stats.Inner["content_start"]++
		s.stats.TextDeltas++
		if bytes.Contains(b, markAccumulated) {
			s.stats.AccumulatedLines++
		}
		return
	}

	var l rawLine
	if err := json.Unmarshal(b, &l); err != nil {
		s.stats.BadLines++
		return
	}
	if l.TS != "" && !bytes.HasPrefix(b, markTS) {
		s.stamp([]byte(l.TS))
	}
	top := l.Type
	if top == "" {
		top = "(none)"
	}
	s.stats.TopLevel[top]++
	switch l.Type {
	case "agent_event":
		if l.Event != nil {
			s.event(l.Event)
		} else {
			s.stats.Inner["(none)"]++
		}
	case "hook_event":
		name := l.HookEventName
		if name == "" {
			name = "(none)"
		}
		s.stats.Hooks[name]++
	case "run_result":
		s.stats.HasRunResult = true
		s.stats.FinishReason = l.FinishReason
		s.stats.Iterations += l.Iterations
		if l.Iterations > s.maxTurn {
			s.maxTurn = l.Iterations
		}
		s.gotTotal = true
		s.stats.DurationMS += int64(l.DurationMs)
		u := l.AggregateUsage
		if u == nil {
			u = l.Usage
		}
		if u != nil {
			s.stats.InputTokens += u.InputTokens
			s.stats.OutputTokens += u.OutputTokens
		} else {
			s.stats.InputTokens += s.sumIn
			s.stats.OutputTokens += s.sumOut
		}
		if l.Model != nil {
			s.stats.Model = l.Model.ID
			s.stats.Provider = l.Model.Provider
			if l.Model.Info != nil {
				s.stats.ContextWindow = l.Model.Info.ContextWindow
			}
		}
	case "run_aborted":
		s.stats.AbortReason = l.Reason
		s.stats.AbortMessage = truncate(l.Message, 400)
	}
}

// stamp remembers the first and the last timestamp of the stream as text;
// they are parsed once, in EndStream.
func (s *StreamScanner) stamp(ts []byte) {
	if len(s.firstTS) == 0 {
		s.firstTS = append(s.firstTS, ts...)
	}
	s.lastTS = append(s.lastTS[:0], ts...)
}

func (s *StreamScanner) event(e *rawEvent) {
	typ := e.Type
	if typ == "" {
		typ = "(none)"
	}
	s.stats.Inner[typ]++
	switch e.Type {
	case "iteration_start":
		s.turn = e.Iteration
		if e.Iteration > s.maxTurn {
			s.maxTurn = e.Iteration
		}
	case "usage":
		s.stats.UsageEvents++
		s.sumIn += e.InputTokens
		s.sumOut += e.OutputTokens
	case "error":
		s.stats.ErrorEvents++
	case "done":
		s.stats.DoneReason = e.Reason
	case "content_start":
		switch e.ContentType {
		case "reasoning":
			s.stats.ReasoningDeltas++
		case "text":
			s.stats.TextDeltas++
		case "tool":
			s.toolStart(e)
		}
		if len(e.Accumulated) > 0 {
			s.stats.AccumulatedLines++
		}
	case "content_end":
		if e.ContentType == "tool" {
			s.toolEnd(e)
		}
	}
}

func (s *StreamScanner) toolStart(e *rawEvent) int {
	if idx, ok := s.open[e.ToolCallID]; ok && e.ToolCallID != "" {
		return idx
	}
	// ParsePath is not in the stream: the CLI does not say whether a call
	// arrived on the native channel or was repaired from text.
	c := ToolCall{
		Turn:      s.turnBase + s.turn,
		Seq:       len(s.calls),
		Tool:      e.ToolName,
		ParsePath: Unknown,
	}
	s.calls = append(s.calls, c)
	idx := len(s.calls) - 1
	if e.ToolCallID != "" {
		s.open[e.ToolCallID] = idx
	}
	return idx
}

func (s *StreamScanner) toolEnd(e *rawEvent) {
	idx, ok := s.open[e.ToolCallID]
	if !ok || e.ToolCallID == "" {
		idx = s.toolStart(e) // a result with no start in the stream
	}
	delete(s.open, e.ToolCallID)
	c := &s.calls[idx]
	if c.Tool == "" {
		c.Tool = e.ToolName
	}
	c.DurationMS = int64(e.DurationMs)
	c.OK, c.ErrorClass = ClassifyToolResult(e.Error, e.Output)
}

// ClassifyToolResult decides from the end event of a tool call whether the
// call succeeded and, when it did not, what kind of failure the stream
// shows. errRaw and output are the raw JSON of the event's "error" and
// "output" fields.
//
// A call failed when the event carries an error, when its output is an
// object with an error, or when its output is a list of per-item results
// and at least one says success=false.
func ClassifyToolResult(errRaw, output json.RawMessage) (ok bool, errorClass string) {
	if len(errRaw) > 0 && string(errRaw) != "null" && string(errRaw) != `""` {
		return false, classifyError(strings.ToLower(string(errRaw)))
	}
	out := bytes.TrimSpace(output)
	if len(out) == 0 {
		return true, ""
	}
	type item struct {
		Success *bool           `json:"success"`
		Error   json.RawMessage `json:"error"`
	}
	failed := func(it item) bool {
		if it.Success != nil && !*it.Success {
			return true
		}
		e := strings.TrimSpace(string(it.Error))
		return it.Success == nil && e != "" && e != "null" && e != `""`
	}
	switch out[0] {
	case '[':
		var items []item
		if json.Unmarshal(out, &items) != nil || len(items) == 0 {
			return true, ""
		}
		bad := 0
		for _, it := range items {
			if failed(it) {
				bad++
			}
		}
		switch {
		case bad == 0:
			return true, ""
		case bad == len(items):
			return false, "command_failed"
		default:
			return false, "partial_failure"
		}
	case '{':
		var it item
		if json.Unmarshal(out, &it) == nil && failed(it) {
			return false, "tool_reported_error"
		}
	}
	return true, ""
}

func classifyError(e string) string {
	switch {
	case strings.Contains(e, "missing required argument"):
		return "missing_argument"
	case strings.Contains(e, "json array that was sent as a string"),
		strings.Contains(e, "did not parse"), strings.Contains(e, "does not parse"):
		return "malformed_arguments"
	case strings.Contains(e, "invalid input"):
		return "invalid_input"
	case strings.Contains(e, "nosuchtool"), strings.Contains(e, "rejected before execution"):
		return "unknown_tool"
	case strings.Contains(e, "timed out"), strings.Contains(e, "timeout"):
		return "timeout"
	case strings.Contains(e, "denied"), strings.Contains(e, "not permitted"):
		return "denied"
	}
	return "tool_error"
}

// ConvertLine maps one line of the CLI's wrapped stream to the SDK's event
// shape. For an agent_event line it returns the inner event with the line's
// timestamp added as "ts", and sdk=true. For the CLI's own lines
// (hook_event, run_result, run_aborted) it returns the line unchanged and
// sdk=false: the SDK has no such event. A line that is not JSON returns an
// error.
//
// The importer stores the stream as it was written; this function is the
// mapping a reader applies on the way out.
func ConvertLine(line []byte) (out []byte, sdk bool, err error) {
	var l struct {
		TS    json.RawMessage            `json:"ts"`
		Type  string                     `json:"type"`
		Event map[string]json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(line, &l); err != nil {
		return nil, false, err
	}
	if l.Type != "agent_event" || l.Event == nil {
		return bytes.TrimSpace(line), false, nil
	}
	if len(l.TS) > 0 {
		if _, has := l.Event["ts"]; !has {
			l.Event["ts"] = l.TS
		}
	}
	out, err = json.Marshal(l.Event)
	return out, true, err
}
