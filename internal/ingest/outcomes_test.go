package ingest

import (
	"strings"
	"testing"
)

func TestOutcomes(t *testing.T) {
	stream := strings.Join([]string{
		agentLine("01", `{"type":"iteration_start","iteration":1}`),
		agentLine("02", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"a","input":{"file_path":"a.js","intent":"x */\nnew_text:"},"output":[{"success":false,"error":"Missing required argument `+"`new_text`"+`. Send it and call again."}]}`),
		agentLine("03", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"b","input":{"<|\"|>file_path":"a.js","new_text":"y"},"output":[{"success":true}]}`),
		agentLine("04", `{"type":"content_end","contentType":"tool","toolName":"read_files","toolCallId":"c","input":"{\"paths\": [","output":[{"success":true}]}`),
		agentLine("05", `{"type":"content_end","contentType":"tool","toolName":"mystery","toolCallId":"d","input":{},"output":[{"success":true}]}`),
	}, "\n")
	ev, err := ReadEvents(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	reqs := []Request{{Agent: true, ToolArgs: map[string][]string{"editor": {"file_path", "new_text"}, "read_files": {"paths"}}}}
	got := Outcomes(ev, reqs)
	if len(got) != 3 || got[0].Tool != "editor" {
		t.Fatalf("%+v", got)
	}
	e := got[0]
	if e.Calls != 2 || e.Failed != 1 || e.ControlNames != 1 || e.Unreadable != 0 || e.NotOffered || len(e.Errors) != 1 {
		t.Errorf("editor %+v", e)
	}
	for class := range e.Errors {
		if !strings.Contains(class, "new_text") {
			t.Errorf("class %q", class)
		}
	}
	by := map[string]ToolOutcome{}
	for _, o := range got {
		by[o.Tool] = o
	}
	if by["read_files"].Unreadable != 1 || by["read_files"].NotOffered {
		t.Errorf("read_files %+v", by["read_files"])
	}
	if !by["mystery"].NotOffered {
		t.Errorf("mystery %+v", by["mystery"])
	}
	// Without a capture, no tool is said not to be offered.
	for _, o := range Outcomes(ev, nil) {
		if o.NotOffered {
			t.Errorf("not offered without a capture: %+v", o)
		}
	}
}
