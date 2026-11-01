package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

// putRun writes a finished run of arm "a" on demo/answer straight into the
// state, with its harness build and tool calls.
func putRun(t *testing.T, g *rig, id, batch, armID, sdk, automaticum, model string, calls ...store.ToolCall) {
	t.Helper()
	ctx := context.Background()
	arm, _ := json.Marshal(map[string]any{"name": "a"})
	prov, _ := json.Marshal(map[string]any{"automaticum": automaticum, "runner_protocol": 3,
		"sdk": map[string]string{"release": sdk, "package_sha256": "abc"}, "image": map[string]string{"id": "sha256:img"}})
	r := &store.Run{ID: id, Batch: batch, Suite: "demo", TaskID: "answer", ArmID: armID, Arm: arm, Model: model, Harness: sdk,
		Verdict: store.Pass, StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC(), Provenance: prov}
	if err := g.state.UpsertRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	g.state.ReplaceToolCalls(ctx, id, calls)
}

func TestReportRegressionAndCensus(t *testing.T) {
	g := newRig(t, false)
	ok := store.ToolCall{Tool: "read_file", OK: true}
	bad := store.ToolCall{Tool: "run", ErrorClass: "tool_error"}
	// The same arm on two SDK releases (two arm ids), and a third build
	// that only changed automaticum (the same arm id as the second).
	putRun(t, g, "r1", "b1", "id-old", "4.100.266", "v1", "m", ok, bad)
	putRun(t, g, "r2", "b1", "id-old", "4.100.266", "v1", "m", ok)
	putRun(t, g, "r3", "b2", "id-new", "4.100.269", "v1", "m", bad, bad)
	putRun(t, g, "r4", "b2", "id-new", "4.100.269", "v2", "m2")

	var plain CompareView
	g.call("GET", "/v1/compare?batch=b1&batch=b2", nil, &plain)
	if len(plain.Groups) != 2 {
		t.Fatalf("compare by arm id: %d groups", len(plain.Groups))
	}
	var reg CompareView
	if code := g.call("GET", "/v1/compare?batch=b1&batch=b2&split=build", nil, &reg); code != 200 || reg.Split != "build" || len(reg.Groups) != 3 {
		t.Fatalf("regression: %d %+v", code, reg)
	}
	for _, gr := range reg.Groups {
		if gr.Build == "" || gr.Arm != "a" {
			t.Errorf("group without a build or arm: %+v", gr)
		}
	}
	if len(reg.Pairs) != 3 {
		t.Errorf("pairs of one arm over three builds: %d", len(reg.Pairs))
	}
	if code := g.call("GET", "/v1/compare?batch=b1&split=arm", nil, nil); code != 400 {
		t.Errorf("unknown split: %d", code)
	}

	var c Census
	if code := g.call("GET", "/v1/report/census?batch=b1&batch=b2", nil, &c); code != 200 || c.Runs != 4 || c.NoCalls != 1 || len(c.Rows) != 2 {
		t.Fatalf("census: %d %+v", code, c)
	}
	if r := c.Rows[0]; r.Tool != "run" || r.Model != "m" || r.Calls != 3 || r.Runs != 2 || r.Failed != 3 || r.Errors["tool_error"] != 3 || r.Parse["not recorded"] != 3 {
		t.Errorf("first row: %+v", r)
	}
	c = Census{}
	if g.call("GET", "/v1/report/census?where=model=m2", nil, &c); c.Runs != 1 || len(c.Rows) != 0 {
		t.Errorf("census of m2: %+v", c)
	}
	if code := g.call("GET", "/v1/report/census?where=nope=1", nil, nil); code != 400 {
		t.Errorf("bad where: %d", code)
	}
}
