package export

import (
	"encoding/json"
	"strings"
	"testing"
)

// streamOf is a response body of these chunks' deltas, with a finish
// reason, usage and [DONE] at the end.
func streamOf(deltas ...map[string]any) string {
	var b strings.Builder
	for _, d := range deltas {
		c, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": d}}})
		b.WriteString("data: " + string(c) + "\n\n")
	}
	end, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": map[string]any{"completion_tokens": 9}})
	return b.String() + "data: " + string(end) + "\n\ndata: [DONE]\n\n"
}

func responseFile(body string) []byte {
	b, _ := json.Marshal(map[string]any{"at": "2026-11-21T09:30:00Z", "status": 200, "body": body, "complete": true})
	return b
}

func TestOnset(t *testing.T) {
	if _, ok := Onset(nil); ok {
		t.Fatal("an onset without spans")
	}
	lines := []Line{
		{Request: "0003", Field: "reasoning", Start: 1},
		{Request: "0002", Field: "tool_calls[1].function.arguments", Start: 0},
		{Request: "0002", Field: "content", Start: 50},
		{Request: "0002", Field: "content", Start: 40},
		{Request: "0002", Field: "tool_calls[0].function.arguments", Start: 0},
	}
	got, _ := Onset(lines)
	if got.Request != "0002" || got.Field != "content" || got.Start != 40 {
		t.Fatalf("onset %+v", got)
	}
	got, _ = Onset([]Line{lines[1], lines[4]})
	if got.Field != "tool_calls[0].function.arguments" {
		t.Fatalf("onset %+v", got)
	}
}

func TestCutResponse(t *testing.T) {
	block := "Wait, check w[1] again.\n"
	cases := []struct {
		name   string
		deltas []map[string]any
		at     Line
		// What is left of each field, and of the calls by index.
		fields map[string]string
		calls  map[int]string
	}{
		{"reasoning, mid-delta", []map[string]any{{"role": "assistant", "reasoning_content": "Start.\n"}, {"reasoning_content": block + "Wa"}, {"reasoning_content": "it, check w[1] again.\n" + block}, {"content": "the answer"}},
			Line{Request: "r", Field: "reasoning", Start: len("Start.\n" + block)}, map[string]string{"reasoning_content": "Start.\n" + block}, nil},
		{"reasoning key, at a delta's end", []map[string]any{{"reasoning": "Start.\n"}, {"reasoning": block}, {"reasoning": block}},
			Line{Request: "r", Field: "reasoning", Start: len("Start.\n")}, map[string]string{"reasoning": "Start.\n"}, nil},
		{"loop at the very start", []map[string]any{{"reasoning_content": block}, {"reasoning_content": block}},
			Line{Request: "r", Field: "reasoning", Start: 0}, map[string]string{}, nil},
		{"reply, after the whole reasoning", []map[string]any{{"reasoning_content": "Think."}, {"content": "Done. 𝄞 Done. "}, {"content": "Done. "}},
			Line{Request: "r", Field: "content", Start: len([]rune("Done. 𝄞 "))}, map[string]string{"reasoning_content": "Think.", "content": "Done. 𝄞 "}, nil},
		{"a repeated tool call", []map[string]any{{"content": "Reading."}, {"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"name": "read_file", "arguments": `{"path":"a"}`}}}},
			{"tool_calls": []any{map[string]any{"index": 1, "function": map[string]any{"name": "read_file", "arguments": `{"path":"a"}`}}}}},
			Line{Request: "r", Field: "tool_calls[1].function.arguments", End: 12}, map[string]string{"content": "Reading."}, map[int]string{0: `{"path":"a"}`}},
	}
	for _, c := range cases {
		b, err := CutResponse(responseFile(streamOf(c.deltas...)), c.at)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		var f map[string]any
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if f["complete"] != false || f["status"] != float64(200) || f["at"] != "2026-11-21T09:30:00Z" {
			t.Errorf("%s: file %v", c.name, f)
		}
		body := f["body"].(string)
		if strings.Contains(body, "finish_reason") || strings.Contains(body, "usage") || strings.Contains(body, "[DONE]") || strings.Contains(body, "answer") {
			t.Errorf("%s: the stream goes on after the cut: %q", c.name, body)
		}
		r, err := ReadResponse("r", b)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Fields) != len(c.fields) {
			t.Errorf("%s: fields %q, want %q", c.name, r.Fields, c.fields)
		}
		for k, want := range c.fields {
			if r.Fields[k] != want {
				t.Errorf("%s: %s = %q, want %q", c.name, k, r.Fields[k], want)
			}
		}
		if len(r.Calls) != len(c.calls) {
			t.Errorf("%s: calls %+v", c.name, r.Calls)
		}
		for _, call := range r.Calls {
			if c.calls[call.Index] != call.Arguments {
				t.Errorf("%s: call %d = %q", c.name, call.Index, call.Arguments)
			}
		}
	}
	// A span the stream never reaches hands out nothing.
	if _, err := CutResponse(responseFile(streamOf(map[string]any{"reasoning_content": "short"})), Line{Request: "r", Field: "reasoning", Start: 99}); err == nil {
		t.Error("a span beyond the stream was cut")
	}
	if _, err := CutResponse(responseFile(streamOf(map[string]any{"content": "x"})), Line{Request: "r", Field: "tool_calls[0].function.arguments"}); err == nil {
		t.Error("a call the stream never has was cut")
	}
	// The same response cuts to the same bytes.
	f := responseFile(streamOf(map[string]any{"reasoning_content": "Start.\n" + block + block}))
	at := Line{Request: "r", Field: "reasoning", Start: 7}
	a, _ := CutResponse(f, at)
	b, _ := CutResponse(f, at)
	if string(a) != string(b) {
		t.Error("a cut is not deterministic")
	}
}
