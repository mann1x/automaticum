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
