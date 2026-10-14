package stubmodel

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatFollowsTheScript(t *testing.T) {
	s := New(Script{Turns: []Turn{
		{Thinking: "let me look", ToolCalls: []ToolCall{{Name: "read_files", Arguments: map[string]any{"path": "a.txt"}}}},
		{Content: "all done here"},
	}})
	srv := httptest.NewServer(s)
	defer srv.Close()

	post := func(body string) []map[string]any {
		resp, err := http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var lines []map[string]any
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatalf("line is not JSON: %s", sc.Text())
			}
			lines = append(lines, m)
		}
		return lines
	}
	first := post(`{"model":"stub:latest","messages":[{"role":"user","content":"hi"}]}`)
	last := first[len(first)-1]
	if last["done"] != true || last["done_reason"] != "stop" || last["eval_count"] == nil {
		t.Errorf("closing line: %v", last)
	}
	var thinking string
	var calls int
	for _, l := range first {
		m := l["message"].(map[string]any)
		if th, ok := m["thinking"].(string); ok {
			thinking += th
		}
		if tc, ok := m["tool_calls"].([]any); ok {
			calls += len(tc)
			fn := tc[0].(map[string]any)["function"].(map[string]any)
			if fn["name"] != "read_files" || fn["arguments"].(map[string]any)["path"] != "a.txt" {
				t.Errorf("tool call: %v", fn)
			}
		}
	}
	if thinking != "let me look" || calls != 1 {
		t.Errorf("thinking %q, calls %d", thinking, calls)
	}
	// One assistant message in the request: the second turn answers, and
	// keeps answering past the end of the script.
	for i := 0; i < 2; i++ {
		second := post(`{"messages":[{"role":"user"},{"role":"assistant"},{"role":"tool"}` + strings.Repeat(`,{"role":"assistant"}`, i) + `]}`)
		var content string
		for _, l := range second {
			content += l["message"].(map[string]any)["content"].(string)
		}
		if content != "all done here" {
			t.Errorf("request %d: content %q", i, content)
		}
	}
	// A request repeated gets the same answer.
	again := post(`{"model":"stub:latest","messages":[{"role":"user","content":"hi"}]}`)
	if len(again) != len(first) {
		t.Errorf("a retried request got another answer")
	}
	chats := s.Chats()
	if len(chats) != 4 || chats[0].Turn != 0 || chats[1].Turn != 1 || chats[2].Turn != 1 || chats[3].Turn != 0 {
		t.Errorf("recorded chats: %+v", chats)
	}
	// Not streamed: one object.
	one := post(`{"stream":false,"messages":[{"role":"user"},{"role":"assistant"}]}`)
	if len(one) != 1 || one[0]["message"].(map[string]any)["content"] != "all done here" {
		t.Errorf("non-streamed: %v", one)
	}
}

func TestProbesAndErrors(t *testing.T) {
	s := New(Script{Model: "m:1", ContextWindow: 4096, Turns: []Turn{{Status: 503}}})
	srv := httptest.NewServer(s)
	defer srv.Close()
	for _, path := range []string{"/api/version", "/api/tags", "/api/ps"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", path, resp, err)
		}
		resp.Body.Close()
	}
	resp, err := http.Post(srv.URL+"/api/show", "application/json", strings.NewReader(`{"model":"m:1"}`))
	if err != nil {
		t.Fatal(err)
	}
	var show map[string]any
	json.NewDecoder(resp.Body).Decode(&show)
	resp.Body.Close()
	if !strings.Contains(show["parameters"].(string), "4096") || show["template"] == "" {
		t.Errorf("show: %v", show)
	}
	resp, _ = http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(`{"messages":[]}`))
	if resp.StatusCode != 503 {
		t.Errorf("status turn gave %d", resp.StatusCode)
	}
	resp.Body.Close()
	if s.Calls()["/api/chat"] != 1 || s.Calls()["/api/show"] != 1 {
		t.Errorf("calls: %v", s.Calls())
	}
}

// On /v1/chat/completions the stub answers as llama-server does: SSE chunks
// when asked to stream, one completion when not, tool-call arguments as JSON
// text; /props and /v1/models say what it serves.
func TestOpenAICompatible(t *testing.T) {
	s := New(Script{Model: "m.gguf", ContextWindow: 8192, Slots: 2, Turns: []Turn{
		{Thinking: "let me look", ToolCalls: []ToolCall{{Name: "read_files", Arguments: map[string]any{"path": "a.txt"}}}},
		{Content: "all done here"},
	}})
	srv := httptest.NewServer(s)
	defer srv.Close()
	get := func(path string, v any) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(v)
	}
	var props struct {
		Settings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		Slots     int    `json:"total_slots"`
		ModelPath string `json:"model_path"`
		Template  string `json:"chat_template"`
	}
	get("/props", &props)
	if props.Settings.NCtx != 8192 || props.Slots != 2 || props.ModelPath == "" || props.Template == "" {
		t.Errorf("props: %+v", props)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	get("/v1/models", &models)
	if len(models.Data) != 1 || models.Data[0].ID != "m.gguf" {
		t.Errorf("models: %+v", models)
	}

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"m.gguf","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var chunks []map[string]any
	done := false
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		switch {
		case !ok:
		case data == "[DONE]":
			done = true
		default:
			var m map[string]any
			if err := json.Unmarshal([]byte(data), &m); err != nil {
				t.Fatalf("chunk is not JSON: %s", data)
			}
			chunks = append(chunks, m)
		}
	}
	resp.Body.Close()
	if !done || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: done %v, type %q", done, resp.Header.Get("Content-Type"))
	}
	last := chunks[len(chunks)-1]
	choice := last["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" || last["usage"] == nil || last["timings"] == nil {
		t.Errorf("closing chunk: %v", last)
	}
	var args string
	for _, c := range chunks {
		delta := c["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		if calls, ok := delta["tool_calls"].([]any); ok {
			args = calls[0].(map[string]any)["function"].(map[string]any)["arguments"].(string)
		}
	}
	if args != `{"path":"a.txt"}` {
		t.Errorf("tool call arguments: %q", args)
	}

	resp, err = http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"m.gguf","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var one struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&one)
	resp.Body.Close()
	if len(one.Choices) != 1 || one.Choices[0].Message.Content != "all done here" || one.Choices[0].FinishReason != "stop" {
		t.Errorf("completion: %+v", one)
	}
	if got := s.Calls()["/v1/chat/completions"]; got != 2 || len(s.Chats()) != 2 {
		t.Errorf("recorded %d calls, %d chats", got, len(s.Chats()))
	}
}
