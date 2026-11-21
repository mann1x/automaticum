package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// A batch's seed list (D159) gives the k-th round of every arm on every
// task the k-th seed, in both orders; a count that differs, a seed named
// twice or out of range is refused.
func TestStartSeeds(t *testing.T) {
	g := newRig(t, false)
	req := StartRequest{Arms: []string{"stub", "stub-sdk-4.100.251"}, Tasks: []string{"demo/answer", "demo/scripted"}, Seeds: []int{11, 22, 33}, DryRun: true}
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", req, &resp); code != 200 {
		t.Fatalf("dry run: %d", code)
	}
	want := []int{11, 11, 11, 11, 22, 22, 22, 22, 33, 33, 33, 33}
	if len(resp.RunIDs) != 12 || !slices.Equal(resp.Seeds, want) {
		t.Errorf("interleaved: %d runs, seeds %v", len(resp.RunIDs), resp.Seeds)
	}
	req.Order = "blocks"
	resp = StartResponse{}
	g.call("POST", "/v1/runs", req, &resp)
	if want := []int{11, 11, 22, 22, 33, 33, 11, 11, 22, 22, 33, 33}; !slices.Equal(resp.Seeds, want) {
		t.Errorf("blocks: seeds %v, order %v", resp.Seeds, resp.Order)
	}
	for name, bad := range map[string]StartRequest{
		"count":  {Arm: "stub", Task: "answer", Count: 2, Seeds: []int{1, 2, 3}, DryRun: true},
		"twice":  {Arm: "stub", Task: "answer", Seeds: []int{1, 1}, DryRun: true},
		"range":  {Arm: "stub", Task: "answer", Seeds: []int{-1}, DryRun: true},
		"range2": {Arm: "stub", Task: "answer", Seeds: []int{1 << 31}, DryRun: true},
	} {
		if code := g.call("POST", "/v1/runs", bad, nil); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	// The same count given is fine.
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2, Seeds: []int{1, 2}, DryRun: true}, nil); code != 200 {
		t.Errorf("a matching count: %d", code)
	}
}

// Each run is started with its seed; a retry keeps its run's.
func TestRunSeedsReachRunsAndRetries(t *testing.T) {
	g := newRig(t, true)
	g.runner.mu.Lock()
	g.runner.end = &[2]string{"SKIPPED", "a gate said no"}
	g.runner.mu.Unlock()
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Seeds: []int{5, 9}}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	for _, id := range resp.RunIDs {
		g.waitFinished(id)
	}
	g.runner.mu.Lock()
	g.runner.end = nil
	g.runner.mu.Unlock()
	var rv RetryView
	if code := g.call("POST", "/v1/batches/"+resp.Batch+"/retry", nil, &rv); code != 202 || len(rv.Retries) != 2 {
		t.Fatalf("retry: %d %+v", code, rv)
	}
	for nu := range rv.Retries {
		g.waitFinished(nu)
	}
	g.runner.mu.Lock()
	defer g.runner.mu.Unlock()
	got := map[string]int{}
	for _, r := range g.runner.reqs {
		if r.Seed == nil {
			t.Fatalf("run %s was started without a seed", r.ID)
		}
		got[r.ID] = *r.Seed
	}
	if got[resp.RunIDs[0]] != 5 || got[resp.RunIDs[1]] != 9 {
		t.Errorf("seeds: %v", got)
	}
	for nu, old := range rv.Retries {
		if got[nu] != got[old] {
			t.Errorf("retry %s of %s: seed %d, was %d", nu, old, got[nu], got[old])
		}
	}
}

// compare pairs two arms' runs by task and seed when every run has one,
// else in the order they ran.
func TestComparePairsBySeed(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	add := func(id, arm string, verdict store.Verdict, seed int) {
		prov := json.RawMessage(`{}`)
		if seed >= 0 {
			prov = json.RawMessage(fmt.Sprintf(`{"automaticum":"x","seed":%d}`, seed))
		}
		g.state.UpsertRun(ctx, &store.Run{ID: id, Batch: "b1", Suite: "demo", TaskID: "answer", ArmID: "id-" + arm, Verdict: verdict,
			Arm: json.RawMessage(`{"name":"` + arm + `","model":{"model":"m-` + arm + `"}}`), Provenance: prov})
	}
	// In the order they ran, a's 1st (seed 1, PASS) would meet b's 1st
	// (seed 3, FAIL); by seed, seed 1 passes in both.
	add("r1", "a", store.Pass, 1)
	add("r2", "a", store.Fail, 2)
	add("r3", "a", store.Pass, 3)
	add("r4", "b", store.Fail, 3)
	add("r5", "b", store.Pass, 1)
	add("r6", "b", store.Fail, 2)
	v, code, err := g.d.compareView(ctx, []string{"b1"}, nil, "", false)
	if err != nil || code != 0 && code != 200 || len(v.Pairs) != 1 {
		t.Fatalf("compare: %d %v %+v", code, err, v.Pairs)
	}
	p := v.Pairs[0].Paired
	if p.By != "seed" || p.Pairs != 3 || p.OnlyA+p.OnlyB != 1 || p.Unpaired != 0 {
		t.Errorf("by seed: %+v", p)
	}
	// One run without a seed: the order, as before.
	add("r7", "b", store.Pass, -1)
	v, _, _ = g.d.compareView(ctx, []string{"b1"}, nil, "", false)
	if p := v.Pairs[0].Paired; p.By != "order" || p.Pairs != 3 || p.Unpaired != 1 {
		t.Errorf("by order: %+v", p)
	}
}

// A run given a seed by its batch's list has the list as its basis fact,
// the same for every run of the group (D159).
func TestBasisSeedList(t *testing.T) {
	arm := json.RawMessage(`{"name":"a","sampler":{"owner":"tag"}}`)
	seeded := basisOfRun(&store.Run{ID: "r1", Arm: arm, Provenance: json.RawMessage(`{"seed":4}`)}, nil)
	other := basisOfRun(&store.Run{ID: "r2", Arm: arm, Provenance: json.RawMessage(`{"seed":9}`)}, nil)
	plain := basisOfRun(&store.Run{ID: "r3", Arm: arm, Provenance: json.RawMessage(`{}`)}, nil)
	if seeded["seed"] != "per run, from the batch's seed list" || other["seed"] != seeded["seed"] || plain["seed"] != "none set" {
		t.Errorf("seed facts: %q %q %q", seeded["seed"], other["seed"], plain["seed"])
	}
}
