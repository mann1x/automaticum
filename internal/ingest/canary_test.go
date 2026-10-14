package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// wireOf writes one wire line for an agent request whose answer is content
// and ends with reason.
func wireOf(seq int, content, reason string) string {
	resp, _ := json.Marshal(map[string]any{"message": map[string]any{"content": content}, "done": true, "done_reason": reason})
	l, _ := json.Marshal(map[string]any{"seq": seq, "url": "http://h/api/chat", "status": 200,
		"request_body": `{"tools":[{"type":"function"}],"messages":[]}`, "response_body": string(resp) + "\n"})
	return string(l) + "\n"
}

func TestCanary(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"seq":0,"url":"http://h/api/engine?endpoint=tokenize","status":404}` + "\n")
	for i := 1; i <= 4; i++ {
		b.WriteString(wireOf(i, fmt.Sprintf("answer %d", i), "stop"))
	}
	reqs, err := ReadWire(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	fin := Finished("r1", reqs)
	if len(fin) != 4 || fin[0].Visible != "answer 1" {
		t.Fatalf("finished: %+v", fin)
	}
	if v := Canary(fin, []string{"<|im_end|>"}); v != nil {
		t.Fatalf("judged on four requests: %+v", v)
	}
	sound := append(fin, CanaryRequest{Run: "r2", Request: Request{Seq: 1, Agent: true, Visible: "fine", DoneReason: "stop"}})
	v := Canary(sound, []string{"<|im_end|>"})
	if v == nil || len(v.Problems) != 0 || len(v.Requests) != 5 || v.Requests[4] != "r2#1" {
		t.Fatalf("sound: %+v", v)
	}
	if len(v.Unchecked) != 1 || !strings.Contains(v.Unchecked[0], "no band") {
		t.Errorf("unchecked: %v", v.Unchecked)
	}
	bad := append(append([]CanaryRequest(nil), fin[:2]...),
		CanaryRequest{Run: "r1", Request: Request{Seq: 3, Agent: true, Empty: true}},
		CanaryRequest{Run: "r1", Request: Request{Seq: 4, Agent: true, Visible: "cut", DoneReason: "length"}},
		CanaryRequest{Run: "r1", Request: Request{Seq: 5, Agent: true, Visible: "done<|im_end|>more", DoneReason: "stop"}},
		CanaryRequest{Run: "r1", Request: Request{Seq: 6, Agent: true, Empty: true}})
	v = Canary(bad, []string{"<|im_end|>"})
	want := []string{"r1#3: empty answer", "r1#4: ended by length", "r1#5: control token <|im_end|> in the visible text"}
	if v == nil || strings.Join(v.Problems, "|") != strings.Join(want, "|") {
		t.Errorf("problems: %+v", v)
	}
	if v := Canary(sound, nil); len(v.Unchecked) != 2 {
		t.Errorf("control tokens not read but not said: %v", v.Unchecked)
	}
}
