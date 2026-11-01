package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

// TestCohort proves D120: a cohort is declared with its variable, its
// cells' runs say so, a cell gives its cohort, and a cohort whose cells
// differ in a fact other than the variable's is confounded.
func TestCohort(t *testing.T) {
	g := newRig(t, true)
	for _, c := range []struct {
		req  StartRequest
		code int
	}{
		{StartRequest{Arm: "stub", Task: "answer", Cohort: "c1"}, 422},
		{StartRequest{Arm: "stub", Task: "answer", Cohort: "c1", DeclaredVariable: "colour"}, 422},
		{StartRequest{Arm: "stub", Task: "answer", Cohort: "c1", DeclaredVariable: "sampler.temperature", DryRun: true}, 200},
	} {
		if code := g.call("POST", "/v1/runs", c.req, nil); code != c.code {
			t.Errorf("%+v: %d, want %d", c.req, code, c.code)
		}
	}
	start := func(key, cohort, variable string) StartResponse {
		var r StartResponse
		if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2, Key: key, Cohort: cohort, DeclaredVariable: variable}, &r); code != 202 {
			t.Fatalf("start %s: %d", key, code)
		}
		for _, id := range r.RunIDs {
			g.waitFinished(id)
		}
		return r
	}
	a := start("a", "c1", "sampler.temperature")
	b := start("b", "c1", "sampler.temperature")
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Cohort: "c1", DeclaredVariable: "thinking"}, nil); code != 422 {
		t.Errorf("another variable for the same cohort: %d", code)
	}
	c := start("c", "c2", "harness build")
	d := start("d", "c2", "harness build")

	// The second cell of each cohort ran on another SDK build.
	for _, id := range append(b.RunIDs, d.RunIDs...) {
		rec, _ := g.state.GetRun(context.Background(), id)
		rec.Provenance, _ = json.Marshal(map[string]any{"sdk": map[string]any{"release": "4.100.269", "package_sha256": "abc"}})
		g.state.UpsertRun(context.Background(), rec)
	}
	if rec, _ := g.state.GetRun(context.Background(), a.RunIDs[0]); rec.Cohort != "c1" {
		t.Errorf("the run's cohort: %q", rec.Cohort)
	}
	var cv CellView
	g.call("GET", "/v1/batches/"+a.Batch+"/cell", nil, &cv)
	if cv.Result.Cohort != "c1" || cv.Result.DeclaredVariable != "sampler.temperature" {
		t.Errorf("the cell's cohort: %q %q", cv.Result.Cohort, cv.Result.DeclaredVariable)
	}

	var ls []CohortSummary
	g.call("GET", "/v1/cohorts", nil, &ls)
	if len(ls) != 2 || ls[0].Name != "c1" || ls[0].Cells != 2 {
		t.Fatalf("cohort ls: %+v", ls)
	}
	var v1 CohortView
	g.call("GET", "/v1/cohorts/c1", nil, &v1)
	if !v1.Confounded || !slices.Contains(v1.ConfoundedBy, "harness build") || len(v1.Cells) != 2 {
		t.Errorf("c1 differs in the harness, not its variable: %+v", v1)
	}
	if len(v1.Warnings) == 0 {
		t.Errorf("c1's variable has one value in both cells and says nothing")
	}
	var v2 CohortView
	g.call("GET", "/v1/cohorts/c2", nil, &v2)
	if v2.Confounded || len(v2.Differs) != 1 || v2.Differs[0].Name != "harness build" || !v2.Differs[0].Variable {
		t.Errorf("c2 differs only in its variable: %+v", v2)
	}
	if v2.Cells[0].Value == v2.Cells[1].Value {
		t.Errorf("c2's values: %+v", v2.Cells)
	}
	_ = c
	if code := g.call("GET", "/v1/cohorts/none", nil, nil); code != 404 {
		t.Errorf("an unknown cohort: %d", code)
	}
}
