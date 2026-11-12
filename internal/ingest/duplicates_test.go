package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// A call the output limit cut has "{}" in the event stream: its repeated
// definitions are read from the wire, by request. Without a capture, the
// event stream's calls are read.
func TestDuplicateDefinitions(t *testing.T) {
	stubs := strings.Repeat(`function draw() {}\nfunction step() {}\n`, 4)
	args, _ := json.Marshal(`{"file_path":"a.js","new_text":"` + stubs)
	stream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"editor","arguments":` + string(args) + `}}]}}]}` + "\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}` + "\n"
	line, _ := json.Marshal(map[string]any{"seq": 7, "url": "http://h/v1/chat/completions", "status": 200,
		"request_body": `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"editor"}}],"stream":true}`, "response_body": stream})
	reqs, err := ReadWire(strings.NewReader(string(line) + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := ReadEvents(strings.NewReader(agentLine("01", `{"type":"iteration_start","iteration":1}`) + "\n" +
		agentLine("02", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"a","input":{},"output":[{"success":false,"error":"Tool call editor was cut off by the output budget"}]}`)))
	res := Analyse(&store.Run{ID: "r", Arm: json.RawMessage(`{"name":"a"}`)}, ev, reqs)
	m := metricMap(res.Metrics)
	if m["tool_calls.duplicate_definitions"] != 1 || m["tool_calls.duplicate_definitions.by_tool.editor"] != 1 {
		t.Errorf("metrics %v", m)
	}
	var f *store.Finding
	for i := range res.Findings {
		if res.Findings[i].Detector == detect.DuplicateDefinitions {
			f = &res.Findings[i]
		}
	}
	if f == nil || f.Count != 1 || !strings.Contains(string(f.Evidence), `"source":"wire"`) || !strings.Contains(string(f.Evidence), `"request":7`) || !strings.Contains(string(f.Evidence), `"cut":true`) {
		t.Fatalf("finding %+v", f)
	}

	whole, _ := json.Marshal(map[string]string{"file_path": "a.js", "new_text": strings.ReplaceAll(stubs, `\n`, "\n")})
	ev2, _ := ReadEvents(strings.NewReader(agentLine("01", `{"type":"iteration_start","iteration":1}`) + "\n" +
		agentLine("02", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"a","input":`+string(whole)+`,"output":[{"success":true}]}`)))
	if fs, source := DefinitionFirings(ev2, nil, nil); len(fs) != 1 || source != "events" || fs[0].Turn != 1 {
		t.Errorf("from events: %s %+v", source, fs)
	}
}
