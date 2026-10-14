package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Request is one HTTP request of a run, as the wire capture holds it.
type Request struct {
	Seq    int
	Path   string
	Status int
	Failed bool // no response, or an error status
	MS     float64
	// Agent is true for a request the agent's loop made: a chat request
	// that offers tools. The SDK's own probes and lookups are not.
	Agent bool
	// What was sent, for an agent request.
	Options map[string]any
	Body    map[string]json.RawMessage // top-level fields but messages and tools
	// What the server said at the end of its answer.
	PromptTokens, CachedTokens, EvalTokens int
	PromptNS, EvalNS                       int64
	// LoadNS is how long the server spent loading the model for this
	// request: milliseconds when it was in memory, seconds when it was not.
	LoadNS            int64
	DoneReason        string
	ThinkBudget       string
	ThinkBudgetTokens int
	// Empty is true when the answer held no text, no thinking and no call.
	Empty bool
	// Visible is the answer's text as the user would see it: not its
	// thinking, not its calls.
	Visible string
}

type wireLine struct {
	Seq          int             `json:"seq"`
	URL          string          `json:"url"`
	Status       int             `json:"status"`
	MSTotal      float64         `json:"ms_total"`
	RequestBody  *string         `json:"request_body"`
	ResponseBody *string         `json:"response_body"`
	Error        json.RawMessage `json:"error"`
}

// ReadWire reads a run's wire capture, in the order the requests were made.
func ReadWire(r io.Reader) ([]Request, error) {
	rd := bufio.NewReaderSize(r, 1<<20)
	var out []Request
	for {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var l wireLine
			if json.Unmarshal(line, &l) == nil {
				out = append(out, request(&l))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
	}
	// Lines are written when a response ends, so not in order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Seq < out[j-1].Seq; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func request(l *wireLine) Request {
	q := Request{Seq: l.Seq, Status: l.Status, MS: l.MSTotal}
	q.Path = l.URL
	if i := strings.Index(l.URL, "://"); i >= 0 {
		if j := strings.IndexByte(l.URL[i+3:], '/'); j >= 0 {
			q.Path = l.URL[i+3+j:]
		}
	}
	if i := strings.IndexByte(q.Path, '?'); i >= 0 {
		q.Path = q.Path[:i]
	}
	q.Failed = l.Status == 0 || l.Status >= 400 || (len(l.Error) > 0 && !bytes.Equal(l.Error, []byte("null")))
	if q.Path != "/api/chat" || l.RequestBody == nil {
		return q
	}
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(*l.RequestBody), &body) != nil {
		return q
	}
	if tools := bytes.TrimSpace(body["tools"]); len(tools) > 2 {
		q.Agent = true
	}
	if !q.Agent {
		// The SDK's own chat requests still say how the model was loaded.
		if l.ResponseBody != nil {
			q.answer(*l.ResponseBody)
			q.Empty = false
		}
		return q
	}
	json.Unmarshal(body["options"], &q.Options)
	delete(body, "messages")
	delete(body, "tools")
	q.Body = body
	if l.ResponseBody != nil {
		q.answer(*l.ResponseBody)
	}
	return q
}

// answer reads a streamed chat answer: one JSON object per line, the last
// with the server's counts.
func (q *Request) answer(body string) {
	type msg struct {
		Message struct {
			Content   string            `json:"content"`
			Thinking  string            `json:"thinking"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
		Done              bool   `json:"done"`
		DoneReason        string `json:"done_reason"`
		ThinkBudget       string `json:"think_budget"`
		ThinkBudgetTokens int    `json:"think_budget_tokens"`
		LoadNS            int64  `json:"load_duration"`
		PromptEvalCount   int    `json:"prompt_eval_count"`
		PromptCached      int    `json:"prompt_eval_cached_count"`
		PromptEvalNS      int64  `json:"prompt_eval_duration"`
		EvalCount         int    `json:"eval_count"`
		EvalNS            int64  `json:"eval_duration"`
	}
	something := false
	var visible strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m msg
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		if m.Message.Content != "" || m.Message.Thinking != "" || len(m.Message.ToolCalls) > 0 {
			something = true
		}
		visible.WriteString(m.Message.Content)
		if m.Done {
			q.DoneReason, q.ThinkBudget, q.ThinkBudgetTokens = m.DoneReason, m.ThinkBudget, m.ThinkBudgetTokens
			q.PromptTokens, q.CachedTokens, q.PromptNS = m.PromptEvalCount, m.PromptCached, m.PromptEvalNS
			q.EvalTokens, q.EvalNS = m.EvalCount, m.EvalNS
			q.LoadNS = m.LoadNS
		}
	}
	q.Empty = !q.Failed && !something
	q.Visible = visible.String()
}
