package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Request is one HTTP request of a run, as the wire capture holds it.
type Request struct {
	Seq    int
	Path   string
	Start  time.Time // when the request was sent
	Status int
	Failed bool // no response, or an error status
	MS     float64
	// Chat is true for a chat request, of either protocol: Ollama's
	// /api/chat or an OpenAI-compatible .../chat/completions.
	Chat bool
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
	TStart       time.Time       `json:"t_start"`
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
	q := Request{Seq: l.Seq, Status: l.Status, MS: l.MSTotal, Start: l.TStart}
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
	if strings.HasSuffix(q.Path, "/chat/completions") && l.RequestBody != nil {
		q.Chat = true
		openAIRequest(&q, l)
		return q
	}
	if q.Path != "/api/chat" || l.RequestBody == nil {
		return q
	}
	q.Chat = true
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

// openAIFields maps the sampler fields of an OpenAI-compatible request body
// to the option names of an Ollama request, so one comparison serves both
// (070 §4.1). max_tokens is the output limit, num_predict on Ollama.
var openAIFields = map[string]string{
	"temperature": "temperature", "top_p": "top_p", "top_k": "top_k", "min_p": "min_p", "typical_p": "typical_p",
	"repeat_penalty": "repeat_penalty", "repeat_last_n": "repeat_last_n", "presence_penalty": "presence_penalty",
	"frequency_penalty": "frequency_penalty", "seed": "seed", "stop": "stop",
	"max_tokens": "num_predict", "max_completion_tokens": "num_predict",
}

// openAIRequest reads an OpenAI-compatible chat request and its answer:
// llama-server and vLLM speak it.
func openAIRequest(q *Request, l *wireLine) {
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(*l.RequestBody), &body) != nil {
		return
	}
	if tools := bytes.TrimSpace(body["tools"]); len(tools) > 2 {
		q.Agent = true
	}
	q.Options = map[string]any{}
	for field, option := range openAIFields {
		if raw, ok := body[field]; ok {
			var v any
			json.Unmarshal(raw, &v)
			q.Options[option] = v
			delete(body, field)
		}
	}
	// The thinking effort, where the request names one.
	if raw, ok := body["reasoning_effort"]; ok {
		body["think"] = raw
	}
	delete(body, "messages")
	delete(body, "tools")
	q.Body = body
	if l.ResponseBody != nil {
		q.openAIAnswer(*l.ResponseBody)
	}
}

// openAIAnswer reads an OpenAI-compatible answer: a stream of "data:" lines
// of chunks, or one JSON object. The counts are in its usage, and
// llama-server adds its timings.
func (q *Request) openAIAnswer(body string) {
	type choice struct {
		Delta, Message struct {
			Content          string            `json:"content"`
			ReasoningContent string            `json:"reasoning_content"`
			Reasoning        string            `json:"reasoning"`
			ToolCalls        []json.RawMessage `json:"tool_calls"`
		}
		FinishReason *string `json:"finish_reason"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
		Usage   *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			Details          *struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
		Timings *struct {
			CacheN      int     `json:"cache_n"`
			PromptMS    float64 `json:"prompt_ms"`
			PredictedN  int     `json:"predicted_n"`
			PredictedMS float64 `json:"predicted_ms"`
		} `json:"timings"`
	}
	something := false
	var visible strings.Builder
	read := func(c chunk) {
		for _, ch := range c.Choices {
			for _, m := range []struct {
				Content, ReasoningContent, Reasoning string
				ToolCalls                            []json.RawMessage
			}{{ch.Delta.Content, ch.Delta.ReasoningContent, ch.Delta.Reasoning, ch.Delta.ToolCalls},
				{ch.Message.Content, ch.Message.ReasoningContent, ch.Message.Reasoning, ch.Message.ToolCalls}} {
				if m.Content != "" || m.ReasoningContent != "" || m.Reasoning != "" || len(m.ToolCalls) > 0 {
					something = true
				}
				visible.WriteString(m.Content)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				q.DoneReason = *ch.FinishReason
			}
		}
		if u := c.Usage; u != nil {
			q.PromptTokens, q.EvalTokens = u.PromptTokens, u.CompletionTokens
			if u.Details != nil {
				q.CachedTokens = u.Details.Cached
			}
		}
		if t := c.Timings; t != nil {
			q.PromptNS, q.EvalNS = int64(t.PromptMS*1e6), int64(t.PredictedMS*1e6)
			if q.CachedTokens == 0 {
				q.CachedTokens = t.CacheN
			}
			if q.EvalTokens == 0 {
				q.EvalTokens = t.PredictedN
			}
		}
	}
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") {
		var c chunk
		if json.Unmarshal([]byte(trimmed), &c) == nil {
			read(c)
		}
	} else {
		for _, line := range strings.Split(body, "\n") {
			data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "" || data == "[DONE]" {
				continue
			}
			var c chunk
			if json.Unmarshal([]byte(data), &c) == nil {
				read(c)
			}
		}
	}
	q.Empty = !q.Failed && !something
	q.Visible = visible.String()
}
