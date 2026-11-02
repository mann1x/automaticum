package ingest

import (
	"encoding/json"
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
