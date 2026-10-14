package ingest

import (
	"encoding/json"
	"strings"
	"testing"
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
