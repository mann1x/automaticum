package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// TestIntegrity proves outside_workspace (070 §4): the paths the agent's
// container changed in its own filesystem, parents only changed for what
// is under them left out, grouped by directory; nothing for a run without
// changes, one whose changes could not be read, or an older record.
func TestIntegrity(t *testing.T) {
	prov := `{"container":{"changes":{"count":7,"paths":[
		{"path":"/home","kind":0},{"path":"/home/agent","kind":0},{"path":"/home/agent/.cache","kind":1},
		{"path":"/home/agent/.cache/x","kind":1},{"path":"/home/agent/.bashrc","kind":0},
		{"path":"/etc/hosts","kind":0},{"path":"/usr/local/bin/guard","kind":2}]}}}`
	got := map[string]float64{}
	res := &Result{}
	integrity(&store.Run{ID: "r", Provenance: json.RawMessage(prov)}, func(n string, v float64) { got[n] = v }, res)
	if got["integrity.outside_workspace"] != 5 || got["integrity.outside_workspace.added"] != 2 || got["integrity.outside_workspace.changed"] != 2 ||
		got["integrity.outside_workspace.deleted"] != 1 {
		t.Errorf("metrics %v", got)
	}
	if len(res.Findings) != 1 || res.Findings[0].Detector != OutsideWorkspace || res.Findings[0].Count != 5 || res.Findings[0].Severity != "info" {
		t.Fatalf("findings %+v", res.Findings)
	}
	var e struct {
		Dirs []struct {
			Dir   string `json:"dir"`
			Paths int    `json:"paths"`
		} `json:"dirs"`
	}
	json.Unmarshal(res.Findings[0].Evidence, &e)
	if len(e.Dirs) != 4 || e.Dirs[0].Dir != "/home/agent/.cache" || e.Dirs[0].Paths != 2 {
		t.Errorf("dirs %s", res.Findings[0].Evidence)
	}
	for _, p := range []string{`{"container":{"changes":{"count":0,"paths":[]}}}`, `{"container":{"changes":{"error":"no such container","paths":[]}}}`, `{"container":{}}`} {
		got, res = map[string]float64{}, &Result{}
		integrity(&store.Run{ID: "q", Provenance: json.RawMessage(p)}, func(n string, v float64) { got[n] = v }, res)
		if len(res.Findings) != 0 {
			t.Errorf("%s: %+v", p, res.Findings)
		}
	}
}

// TestSwallowed proves swallowed_argument (D131) from a run's calls and
// the schemas its requests offered.
func TestSwallowed(t *testing.T) {
	stream := agentLine("01", `{"type":"iteration_start","iteration":1}`) + "\n" +
		agentLine("02", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"a","input":{"file_path":"a.js","new_text":"x;\",\"start_line\": 4"},"output":[{"success":false,"error":"no start_line"}]}`) + "\n" +
		agentLine("03", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"b","input":{"file_path":"a.js","start_line":4,"new_text":"y"},"output":[{"success":true}]}`)
	ev, err := ReadEvents(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	q := toolArgs(json.RawMessage(`[{"type":"function","function":{"name":"editor","parameters":{"type":"object","properties":{"file_path":{},"new_text":{},"start_line":{},"end_line":{}}}}}]`))
	if got := q["editor"]; len(got) != 4 || got[0] != "end_line" {
		t.Fatalf("schema %v", q)
	}
	res := Analyse(&store.Run{ID: "r"}, ev, []Request{{Agent: true, Chat: true, ToolArgs: q}})
	m := metricMap(res.Metrics)
	if m["tool_calls.swallowed_argument"] != 1 || m["tool_calls.swallowed_argument.by_tool.editor"] != 1 {
		t.Errorf("metrics %v", m)
	}
	var f *store.Finding
	for i := range res.Findings {
		if res.Findings[i].Detector == "swallowed_argument" {
			f = &res.Findings[i]
		}
	}
	if f == nil || f.Count != 1 || f.Severity != "warning" || !strings.Contains(string(f.Evidence), `"start_line":1`) {
		t.Fatalf("finding %+v", f)
	}
}
