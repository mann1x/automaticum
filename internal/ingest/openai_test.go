package ingest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/stubmodel"
)

// An OpenAI-compatible request, as llama-server and vLLM take it, reads
// like an Ollama one: its sampler as options, max_tokens as the output
// limit, the stream's text, thinking, calls, finish reason and counts.
func TestOpenAIWire(t *testing.T) {
	req := `{"model":"m","messages":[],"tools":[{"type":"function"}],"temperature":1,"top_k":20,"max_tokens":4096,"stream":true,"reasoning_effort":"high"}`
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"think"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"hello<|im_end|>"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"id":"a"}]},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":900,"completion_tokens":120,"prompt_tokens_details":{"cached_tokens":800}},"timings":{"prompt_ms":50,"predicted_n":120,"predicted_ms":2000}}`,
		`data: [DONE]`, ``}, "\n")
	line := func(seq int, body, resp string) string {
		b, _ := json.Marshal(map[string]any{"seq": seq, "url": "http://h:8080/v1/chat/completions", "status": 200, "request_body": body, "response_body": resp})
		return string(b)
	}
	capped := `{"choices":[{"message":{"content":"cut"},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":4096}}`
	wire := line(1, req, stream) + "\n" + line(2, req, capped) + "\n" +
		`{"seq":3,"url":"http://h:8080/v1/models","status":200}` + "\n"
	reqs, err := ReadWire(strings.NewReader(wire))
	if err != nil || len(reqs) != 3 {
		t.Fatalf("%v %+v", err, reqs)
	}
	q := reqs[0]
	if !q.Chat || !q.Agent || q.Options["temperature"] != float64(1) || q.Options["num_predict"] != float64(4096) || q.Options["top_k"] != float64(20) {
		t.Errorf("request: %+v", q)
	}
	if _, has := q.Body["max_tokens"]; has {
		t.Error("max_tokens stayed in the body as well as in the options")
	}
	var think string
	json.Unmarshal(q.Body["think"], &think)
	if think != "high" {
		t.Errorf("thinking effort: %q", think)
	}
	if q.DoneReason != "tool_calls" || q.PromptTokens != 900 || q.CachedTokens != 800 || q.EvalTokens != 120 || q.EvalNS != 2e9 || q.Visible != "hello<|im_end|>" || q.Empty {
		t.Errorf("answer: %+v", q)
	}
	if hits := CapHits(reqs); len(hits) != 1 || hits[0].Seq != 2 || hits[0].By != "length" || hits[0].Limit != 4096 {
		t.Errorf("cap: %+v", hits)
	}
	if reqs[2].Chat || reqs[2].Agent {
		t.Errorf("models list: %+v", reqs[2])
	}
}

// What the stub model server streams on /v1/chat/completions reads as the
// same answer through ingest, so tests of mode C on the stub test ingest.
func TestOpenAIWireFromStub(t *testing.T) {
	srv := httptest.NewServer(stubmodel.New(stubmodel.Script{Turns: []stubmodel.Turn{
		{Thinking: "let me look", Content: "done now", ToolCalls: []stubmodel.ToolCall{{Name: "read_files"}}},
	}}))
	defer srv.Close()
	req := `{"model":"stub:latest","stream":true,"max_tokens":512,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	b, _ := json.Marshal(map[string]any{"seq": 1, "url": srv.URL + "/v1/chat/completions", "status": 200, "request_body": req, "response_body": string(body)})
	reqs, err := ReadWire(strings.NewReader(string(b) + "\n"))
	if err != nil || len(reqs) != 1 {
		t.Fatalf("%v %+v", err, reqs)
	}
	q := &reqs[0]
	// The server holds the context of an OpenAI-compatible request: an arm's
	// context window is not expected on the wire, its output limit is.
	q.Agent = true
	a := arm(t, `{"model":{"model":"stub:latest","context_window":32768,"output_tokens":512},"sampler":{"owner":"tag"}}`)
	if devs := CompareWire(a, "", reqs); len(devs) != 0 {
		t.Errorf("deviations: %+v", devs)
	}
	if !q.OpenAI || !q.Chat || q.DoneReason != "tool_calls" || q.Visible != "done now" || q.EvalTokens == 0 || q.PromptTokens == 0 || q.Options["num_predict"] != float64(512) || q.Empty {
		t.Errorf("answer: %+v", q)
	}
}

// The SDK sends a thinking effort to llama-server and opencoti as
// reasoning_budget_tokens, a share of the output limit (bug-029): that is
// the arm's effort sent, and the harness speaking where the tag owns
// thinking.
func TestOpenAIThinkingBudget(t *testing.T) {
	req := `{"model":"g","messages":[],"tools":[{"type":"function"}],"max_tokens":96000,"reasoning_budget_tokens":48000}`
	b, _ := json.Marshal(map[string]any{"seq": 1, "url": "http://h:8080/v1/chat/completions", "status": 200, "request_body": req,
		"response_body": `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`})
	reqs, err := ReadWire(strings.NewReader(string(b) + "\n"))
	if err != nil || len(reqs) != 1 {
		t.Fatalf("%v %+v", err, reqs)
	}
	if reqs[0].Options["think_budget_tokens"] != float64(48000) {
		t.Fatalf("budget: %+v", reqs[0].Options)
	}
	if _, has := reqs[0].Body["reasoning_budget_tokens"]; has {
		t.Error("reasoning_budget_tokens stayed in the body as well as in the options")
	}
	high := arm(t, `{"model":{"model":"g"},"sampler":{"owner":"tag"},"thinking":{"owner":"arm","enabled":true,"effort":"high"}}`)
	if devs := CompareWire(high, "", reqs); len(devs) != 0 {
		t.Errorf("an effort sent as a budget: %+v", devs)
	}
	tag := arm(t, `{"model":{"model":"g"},"sampler":{"owner":"tag"},"thinking":{"owner":"tag"}}`)
	devs := CompareWire(tag, "", reqs)
	if len(devs) != 1 || devs[0].Kind != SentNotStated || devs[0].Field != "options.think_budget_tokens" {
		t.Errorf("a budget the tag owns: %+v", devs)
	}
}

// The tool calls an answer made are kept whole from the wire: OpenAI's
// pieces joined by index, Ollama's whole calls with arguments as an
// object, and a call cut off by the length limit as far as it went.
func TestWireCalls(t *testing.T) {
	req := `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"editor"}}],"stream":true}`
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"editor","arguments":"{\"file_path\":\"a\","}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"read_files","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"new_text\":\"x\\ny"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`data: [DONE]`, ``}, "\n")
	line, _ := json.Marshal(map[string]any{"seq": 1, "url": "http://h/v1/chat/completions", "status": 200, "request_body": req, "response_body": stream})
	ollama, _ := json.Marshal(map[string]any{"seq": 2, "url": "http://h/api/chat", "status": 200,
		"request_body":  `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"editor"}}]}`,
		"response_body": `{"message":{"tool_calls":[{"function":{"name":"editor","arguments":{"file_path":"b"}}}]},"done":true,"done_reason":"stop"}`})
	reqs, err := ReadWire(strings.NewReader(string(line) + "\n" + string(ollama) + "\n"))
	if err != nil || len(reqs) != 2 {
		t.Fatalf("%v %+v", err, reqs)
	}
	got := reqs[0].Calls
	if len(got) != 2 || got[0].Name != "editor" || got[0].Arguments != `{"file_path":"a","new_text":"x\ny` || got[1].Name != "read_files" || got[1].Arguments != "{}" || reqs[0].DoneReason != "length" {
		t.Errorf("openai calls %+v", got)
	}
	if c := reqs[1].Calls; len(c) != 1 || c[0].Name != "editor" || c[0].Arguments != `{"file_path":"b"}` {
		t.Errorf("ollama calls %+v", c)
	}
}
