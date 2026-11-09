package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

func TestBatchesAndLaneShow(t *testing.T) {
	g := newRig(t, true)
	var a, b StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &a)
	for _, id := range a.RunIDs {
		g.waitFinished(id)
	}
	// A cell's start names its suite in a header; the batch is listed as a
	// cell.
	req, _ := http.NewRequest("POST", g.srv.URL+"/v1/runs", jsonBody(StartRequest{Arm: "stub", Task: "answer", Key: "c1"}))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Automaticum-Cell", "demo:answer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	b.Batch = "k-c1"
	g.waitFinished(g.status().Queue[0].RunID)

	var list []BatchSummary
	if code := g.call("GET", "/v1/batches", nil, &list); code != 200 || len(list) != 2 {
		t.Fatalf("batches: %d %+v", code, list)
	}
	byName := map[string]BatchSummary{}
	for _, s := range list {
		byName[s.Batch] = s
	}
	if s := byName[a.Batch]; s.State != "done" || s.Total != 2 || s.Finished["PASS"] != 2 || s.Tasks != 1 || len(s.Lanes) != 1 {
		t.Errorf("batch a: %+v", s)
	}
	if s := byName[b.Batch]; s.Cell != "demo:answer" {
		t.Errorf("cell: %+v", s)
	}
	list = nil
	if g.call("GET", "/v1/batches?cell=1", nil, &list); len(list) != 1 || list[0].Batch != b.Batch {
		t.Errorf("cells: %+v", list)
	}
	var cv CellView
	if g.call("GET", "/v1/batches/"+b.Batch+"/cell", nil, &cv); cv.Result.Suite != "demo" || cv.Result.Subset != "answer" {
		t.Errorf("the cell's suite and subset: %q %q", cv.Result.Suite, cv.Result.Subset)
	}
	var lv LaneView
	if code := g.call("GET", "/v1/lanes/lane-1?recent=2", nil, &lv); code != 200 || lv.Lane != "lane-1" || len(lv.Recent) != 2 || lv.Config == nil {
		t.Errorf("lane show: %d %+v", code, lv)
	}
	if code := g.call("GET", "/v1/lanes/no-such", nil, nil); code != 404 {
		t.Errorf("unknown lane: %d", code)
	}
	var runs []RunView
	if g.call("GET", "/v1/runs?lane=lane-1&limit=100", nil, &runs); len(runs) != 3 {
		t.Errorf("runs of lane-1: %d", len(runs))
	}
	// A limit out of range is refused, not replaced by the default (bug-078).
	for _, l := range []string{"100000", "0", "-1", "many"} {
		if code := g.call("GET", "/v1/runs?limit="+l, nil, nil); code != 400 {
			t.Errorf("limit %s: %d, want 400", l, code)
		}
	}
	if code := g.call("GET", "/v1/runs?limit=10000", nil, &runs); code != 200 {
		t.Errorf("limit 10000: %d", code)
	}
}

func TestWhereFilter(t *testing.T) {
	f, err := whereFilter("task=demo/answer, verdict=pass,lane=lane-1,since=2026-10-01,arm=gate-g1")
	if err != nil || f.Suite != "demo" || f.TaskID != "answer" || f.Verdict != store.Pass || f.Lane != "lane-1" || f.Since.IsZero() || f.ArmName != "gate-g1" {
		t.Fatalf("%v %+v", err, f)
	}
	for _, bad := range []string{"nope=1", "verdict=MAYBE", "since=yesterday", "task"} {
		if _, err := whereFilter(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
