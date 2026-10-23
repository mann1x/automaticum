// Package stubmodel is a model server that plays a script. It speaks enough
// of the Ollama chat protocol for the agent SDK to run a whole session
// against it, tool calls included, so the harness can be tested end to end
// without a GPU. It measures nothing and must never be used as an arm of a
// comparison.
package stubmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Script is what the stub answers, turn by turn.
type Script struct {
	// Model is the model name the stub claims to serve.
	Model string `json:"model"`
	// ContextWindow is the num_ctx its model file declares.
	ContextWindow int `json:"context_window"`
	// Slots is the total_slots /props reports; 1 when not given. The
	// context of each slot is ContextWindow.
	Slots int `json:"slots,omitempty"`
	// Turns are the answers, in order. The answer to a request is chosen by
	// how many assistant messages the request already holds, so a retried
	// request gets the same answer. Past the last turn, the last is repeated.
	Turns []Turn `json:"turns"`
}

// Turn is one answer of the model.
type Turn struct {
	Thinking  string     `json:"thinking,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// DelayMS is waited before the answer starts.
	DelayMS int `json:"delay_ms,omitempty"`
	// Status, when not zero, makes the stub answer with this HTTP status
	// and no body: an endpoint error.
	Status int `json:"status,omitempty"`
	// Hang makes the stub hold the request open and send nothing.
	Hang bool `json:"hang,omitempty"`
}

// ToolCall is a tool call the model makes.
type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Request is what the stub saw of one chat request.
type Request struct {
	Time      time.Time
	Path      string
	Body      json.RawMessage
	Assistant int // assistant messages in the request
	Turn      int // index of the turn that answered it
}

// Server is a stub model server. Its zero value is not usable; use New.
type Server struct {
	script Script
	mu     sync.Mutex
	chats  []Request
	paths  map[string]int
}

// New returns a server that plays script.
func New(script Script) *Server {
	if script.Model == "" {
		script.Model = "stub:latest"
	}
	if script.ContextWindow == 0 {
		script.ContextWindow = 32768
	}
	return &Server{script: script, paths: map[string]int{}}
}

// Chats returns the chat requests received so far.
func (s *Server) Chats() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.chats...)
}

// Calls returns how many requests each path received.
func (s *Server) Calls() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for k, v := range s.paths {
		out[k] = v
	}
	return out
}

func (s *Server) digest() string {
	sum := sha256.Sum256([]byte("stubmodel:" + s.script.Model))
	return hex.EncodeToString(sum[:])
}

func (s *Server) slots() int {
	return max(s.script.Slots, 1)
}

const template = "{{ .Prompt }}"

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ServeHTTP implements the endpoints the SDK and the harness's probe call.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.paths[r.URL.Path]++
	s.mu.Unlock()
	model := map[string]any{
		"name": s.script.Model, "model": s.script.Model, "digest": s.digest(), "size": 1,
		"details": map[string]any{"format": "gguf", "family": "stub", "parameter_size": "0", "quantization_level": "none"},
	}
	switch r.URL.Path {
	case "/api/version":
		writeJSON(w, map[string]string{"version": "0.0.0-stub"})
	case "/api/tags":
		writeJSON(w, map[string]any{"models": []any{model}})
	case "/api/ps":
		running := map[string]any{"size_vram": 1, "context_length": s.script.ContextWindow, "expires_at": time.Now().Add(time.Hour)}
		for k, v := range model {
			running[k] = v
		}
		writeJSON(w, map[string]any{"models": []any{running}})
	case "/api/show":
		writeJSON(w, map[string]any{
			"modelfile":    fmt.Sprintf("FROM stub\nPARAMETER num_ctx %d\n", s.script.ContextWindow),
			"parameters":   fmt.Sprintf("num_ctx                        %d", s.script.ContextWindow),
			"template":     template,
			"details":      model["details"],
			"model_info":   map[string]any{"general.architecture": "stub", "stub.context_length": s.script.ContextWindow},
			"capabilities": []string{"completion", "tools", "thinking"},
		})
	case "/api/chat":
		s.chat(w, r)
	// What llama-server answers (mode C, 065 §3.1): the OpenAI-compatible
	// chat, the model list and the server's own properties.
	case "/v1/models":
		writeJSON(w, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": s.script.Model, "object": "model", "owned_by": "stub", "created": 0},
		}})
	case "/props":
		writeJSON(w, map[string]any{
			"default_generation_settings": map[string]any{"n_ctx": s.script.ContextWindow},
			"total_slots":                 s.slots(),
			"model_path":                  "stub/" + s.script.Model + ".gguf",
			"chat_template":               template,
			"chat_template_caps":          map[string]bool{"supports_tools": true, "supports_tool_calls": true},
			"build_info":                  "stub",
		})
	case "/v1/chat/completions":
		s.chat(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, `{"error":"bad request body"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		Model    string `json:"model"`
		Stream   *bool  `json:"stream"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	json.Unmarshal(raw, &req)
	assistant := 0
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			assistant++
		}
	}
	if len(s.script.Turns) == 0 {
		http.Error(w, `{"error":"the stub has no script"}`, http.StatusInternalServerError)
		return
	}
	idx := min(assistant, len(s.script.Turns)-1)
	turn := s.script.Turns[idx]
	s.mu.Lock()
	s.chats = append(s.chats, Request{Time: time.Now(), Path: r.URL.Path, Body: raw, Assistant: assistant, Turn: idx})
	s.mu.Unlock()

	if turn.DelayMS > 0 {
		select {
		case <-time.After(time.Duration(turn.DelayMS) * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
	if turn.Hang {
		<-r.Context().Done()
		return
	}
	if turn.Status != 0 {
		http.Error(w, fmt.Sprintf(`{"error":"stub status %d"}`, turn.Status), turn.Status)
		return
	}

	type call struct {
		Function struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"function"`
	}
	type message struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Thinking  string `json:"thinking,omitempty"`
		ToolCalls []call `json:"tool_calls,omitempty"`
	}
	line := func(m message, done bool, extra map[string]any) map[string]any {
		out := map[string]any{"model": s.script.Model, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "message": m, "done": done}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	var calls []call
	for _, tc := range turn.ToolCalls {
		var c call
		c.Function.Name, c.Function.Arguments = tc.Name, tc.Arguments
		if c.Function.Arguments == nil {
			c.Function.Arguments = map[string]any{}
		}
		calls = append(calls, c)
	}
	usage := map[string]any{
		"done_reason": "stop", "total_duration": 1000000, "load_duration": 0,
		"prompt_eval_count": len(raw) / 4, "prompt_eval_duration": 500000,
		"eval_count": (len(turn.Thinking)+len(turn.Content))/4 + 8*len(calls), "eval_duration": 500000,
	}
	if r.URL.Path == "/v1/chat/completions" {
		s.openAI(w, req.Stream != nil && *req.Stream, turn, usage)
		return
	}
	if req.Stream != nil && !*req.Stream {
		writeJSON(w, line(message{Role: "assistant", Content: turn.Content, Thinking: turn.Thinking, ToolCalls: calls}, true, usage))
		return
	}
	// Streamed: thinking and content in small pieces, then the tool calls,
	// then the closing line with the counts, one JSON object per line.
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	for _, piece := range pieces(turn.Thinking) {
		enc.Encode(line(message{Role: "assistant", Thinking: piece}, false, nil))
		flush()
	}
	for _, piece := range pieces(turn.Content) {
		enc.Encode(line(message{Role: "assistant", Content: piece}, false, nil))
		flush()
	}
	if len(calls) > 0 {
		enc.Encode(line(message{Role: "assistant", ToolCalls: calls}, false, nil))
		flush()
	}
	enc.Encode(line(message{Role: "assistant"}, true, usage))
	flush()
}

// openAI answers in the shape llama-server gives on /v1/chat/completions:
// one completion, or, when the request asks to stream, server-sent "data:"
// chunks with reasoning_content, content and tool_calls, a chunk with the
// finish_reason, the usage and timings, and "data: [DONE]".
func (s *Server) openAI(w http.ResponseWriter, stream bool, turn Turn, usage map[string]any) {
	type function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type call struct {
		Index    int      `json:"index"`
		ID       string   `json:"id"`
		Type     string   `json:"type"`
		Function function `json:"function"`
	}
	var calls []call
	for i, tc := range turn.ToolCalls {
		args := tc.Arguments
		if args == nil {
			args = map[string]any{}
		}
		b, _ := json.Marshal(args)
		calls = append(calls, call{Index: i, ID: fmt.Sprintf("call_%d", i), Type: "function", Function: function{Name: tc.Name, Arguments: string(b)}})
	}
	finish := "stop"
	if len(calls) > 0 {
		finish = "tool_calls"
	}
	prompt, completion := usage["prompt_eval_count"].(int), usage["eval_count"].(int)
	counts := map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
	timings := map[string]any{"cache_n": 0, "prompt_n": prompt, "prompt_ms": 0.5, "predicted_n": completion, "predicted_ms": 0.5}
	id, created := "chatcmpl-stub", time.Now().Unix()
	chunk := func(choice map[string]any) map[string]any {
		return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": s.script.Model, "choices": []any{choice}}
	}
	if !stream {
		msg := map[string]any{"role": "assistant", "content": turn.Content}
		if turn.Thinking != "" {
			msg["reasoning_content"] = turn.Thinking
		}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		writeJSON(w, map[string]any{"id": id, "object": "chat.completion", "created": created, "model": s.script.Model,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
			"usage":   counts, "timings": timings})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	send(chunk(map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": nil}, "finish_reason": nil}))
	for _, piece := range pieces(turn.Thinking) {
		send(chunk(map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": piece}, "finish_reason": nil}))
	}
	for _, piece := range pieces(turn.Content) {
		send(chunk(map[string]any{"index": 0, "delta": map[string]any{"content": piece}, "finish_reason": nil}))
	}
	if len(calls) > 0 {
		send(chunk(map[string]any{"index": 0, "delta": map[string]any{"tool_calls": calls}, "finish_reason": nil}))
	}
	last := chunk(map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish})
	last["usage"], last["timings"] = counts, timings
	send(last)
	fmt.Fprint(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// pieces cuts text into word-sized parts, as a model streams tokens.
func pieces(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for len(text) > 0 {
		i := strings.IndexByte(text[1:], ' ')
		if i < 0 {
			out = append(out, text)
			break
		}
		out = append(out, text[:i+1])
		text = text[i+1:]
	}
	return out
}
