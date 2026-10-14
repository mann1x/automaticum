package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/stubmodel"
)

// A mode C cell runs its arm on the caller's endpoint with the caller's
// model and values, spreads its runs over the lanes asked for, records
// what the server's /props said, refuses a run whose server contradicts
// the caller's facts, and is remembered by a restarted daemon (065 §3.1).
func TestModeCCell(t *testing.T) {
	stub := httptest.NewServer(stubmodel.New(stubmodel.Script{Model: "served", ContextWindow: 32768, Slots: 2}))
	defer stub.Close()
	g := newRig(t, false)
	g.d.ControlFile = filepath.Join(t.TempDir(), "control.json")
	g.d.Local.Lanes["lane-2"] = &config.Lane{Name: "lane-2", Host: "linux-gpu-1"}
	go func() { g.d.Serve(g.ctx()); close(g.served) }()

	temp := 0.6
	cell := &CellSpec{EndpointURL: stub.URL + "/v1", ServedModel: "served", NLanes: 2,
		CallerValues: &config.CallerValues{Sampler: &config.Sampler{Owner: "arm", Name: "caller-sampler", Temperature: &temp}},
		BasisFacts:   &config.BasisFacts{NParallel: 2, CtxPerSlot: 32768, GPU: "a GPU"}}
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 4, Key: "mc", Cell: cell}, &resp); code != 202 {
		t.Fatalf("start: %d %+v", code, resp)
	}
	if strings.Join(resp.Lanes, ",") != "lane-1,lane-2" {
		t.Errorf("lanes: %v", resp.Lanes)
	}
	for _, id := range resp.RunIDs {
		if v := g.waitFinished(id); v.Verdict != "PASS" {
			t.Errorf("run %s: %+v", id, v)
		}
	}
	g.runner.mu.Lock()
	reqs := append(g.runner.reqs[:0:0], g.runner.reqs...)
	g.runner.mu.Unlock()
	lanes := map[string]int{}
	for _, r := range reqs {
		lanes[r.Lane]++
		a := r.Arm
		if a.Endpoint != "cell-mc" || a.Model.Model != "served" || a.Model.Provider != "openai-compatible" || a.Sampler.Name != "caller-sampler" || a.Origins["sampler.temperature"] != "caller" {
			t.Errorf("arm: %s %s %s %+v %v", a.Endpoint, a.Model.Model, a.Model.Provider, a.Sampler, a.Origins)
		}
		rec, _ := r.Cell.(*CellRecord)
		if rec == nil || rec.Facts != factsChecked || rec.Props == nil || rec.Props.TotalSlots != 2 || rec.Lanes != 2 {
			t.Errorf("cell record: %+v", r.Cell)
		}
	}
	if len(reqs) != 4 || lanes["lane-1"] != 2 || lanes["lane-2"] != 2 {
		t.Errorf("runs by lane: %v", lanes)
	}

	// The same key with other values is another cell.
	other := *cell
	other.NLanes = 1
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 4, Key: "mc", Cell: &other}, nil); code != 409 {
		t.Errorf("same key, other lanes: %d", code)
	}
	var again StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 4, Key: "mc", Cell: cell}, &again); code != 200 || !again.Repeated {
		t.Errorf("same key, same cell: %d %+v", code, again)
	}
	// More lanes than the installation has.
	wide := *cell
	wide.NLanes = 3
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Key: "wide", Cell: &wide}, nil); code != 422 {
		t.Errorf("three lanes: %d", code)
	}

	// A server that contradicts the caller refuses every run.
	wrong := *cell
	wrong.BasisFacts = &config.BasisFacts{NParallel: 4}
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2, Key: "wrong", Cell: &wrong}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	for _, id := range resp.RunIDs {
		v := g.waitFinished(id)
		if v.Verdict != "SKIPPED" || !strings.HasPrefix(v.VerdictReason, "gate basis_facts: ") || !strings.Contains(v.VerdictReason, "n_parallel is 4, the server has 2 slots") {
			t.Errorf("refused run: %+v", v)
		}
	}

	// A restarted daemon still knows where the cell's runs go.
	local := &config.Local{Lanes: g.d.Local.Lanes, Endpoints: g.d.Local.Endpoints}
	restarted := &Daemon{Local: local, ControlFile: g.d.ControlFile}
	restarted.init()
	if ep := local.Endpoint("cell-mc"); ep == nil || !ep.Cell || !local.MayUse("lane-2", "cell-mc") || local.MayUse("lane-2", "stub") {
		t.Errorf("after a restart: %+v", ep)
	}
}

// A cell's basis says which facts /props confirmed and which are the
// caller's word, and cells at different lane counts differ in it.
func TestModeCBasis(t *testing.T) {
	rec := func(lanes int) *store.Run {
		c := CellRecord{Kind: "llamacpp", ServedModel: "served", Lanes: lanes, Facts: factsChecked,
			BasisFacts: &config.BasisFacts{WeightsSHA256: "w1", Quantization: "Q4_K_M", LlamaCppCommit: "ebe30d93f", NParallel: 2, CtxPerSlot: 32768, CtxTotal: 65536, GPU: "a GPU", Driver: "1.0"},
			Props:      &run.Props{NCtx: 32768, TotalSlots: 2, ModelPath: "/private/dir/m.gguf", ChatTemplateSHA256: "t1", ChatTemplateBytes: 9, BuildInfo: "b7000-ebe30d93f"}}
		b, _ := json.Marshal(map[string]any{"cell": c})
		return &store.Run{Suite: "demo", TaskID: "answer", Provenance: b, Arm: json.RawMessage(`{"sampler":{"owner":"arm"}}`)}
	}
	one, two := basisOfRun(rec(1), nil), basisOfRun(rec(2), nil)
	diff := DiffBasis(basisOf([]map[string]string{one}), basisOf([]map[string]string{two}))
	if len(diff) != 1 || diff[0].Name != "concurrency" {
		t.Errorf("diff: %+v", diff)
	}
	want := map[string]string{
		"weights":     "served as served (caller), sha256 w1 (caller), file m.gguf (from /props)",
		"template":    "sha256 t1, 9 bytes (checked against /props)",
		"server":      "llamacpp, llama.cpp ebe30d93f (checked against /props)",
		"concurrency": "2 lanes, chosen by the caller, 2 slots (checked against /props)",
		"context":     "32768 tokens per slot (checked against /props), 65536 in all (caller)",
		"GPU":         "a GPU (caller)",
		"imatrix":     notRecorded + "the caller did not say",
	}
	for k, v := range want {
		if two[k] != v {
			t.Errorf("%s: %q, want %q", k, two[k], v)
		}
	}
	for k, v := range two {
		if strings.Contains(v, "/private") {
			t.Errorf("%s holds the caller's path: %q", k, v)
		}
	}
}
