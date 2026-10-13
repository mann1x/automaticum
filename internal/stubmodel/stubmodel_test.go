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
