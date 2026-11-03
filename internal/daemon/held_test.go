package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// run held: a run that ended on the SDK's silence ceiling, its oracle
// having said no, is a FAIL once the server's log shows it was decoding
// (D136); what it was is kept beside it.
func TestRunHeld(t *testing.T) {
	g := newRig(t, true)
	g.d.Blobs = &syncBlobs{m: map[blob.Hash]string{}}
	ctx := context.Background()
	prov := func(agentError string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{
			"runner": map[string]any{"outcome": "completed", "agent_end": "error", "agent_error": agentError},
			"oracle": map[string]any{"Oracle": "game", "Passed": false, "Err": ""},
		})
		return b
	}
	silent := "Failed to process successful response: OpencotiServerSilentError: ETIMEDOUT: the opencoti server sent nothing for 595s (3 keepalive periods); presumed dead (ETIMEDOUT)"
	g.state.UpsertRun(ctx, &store.Run{ID: "h1", Suite: "custom", TaskID: "manic_miner", Verdict: store.Aborted,
		VerdictReason: "the server failed after the SDK's retries (transport: " + silent + "); not the model's failure", Provenance: prov(silent)})
	g.state.UpsertRun(ctx, &store.Run{ID: "h2", Suite: "custom", TaskID: "manic_miner", Verdict: store.Aborted,
		VerdictReason: "the server failed", Provenance: prov("connect ECONNREFUSED 172.17.0.1:8261")})

	in := map[string]any{"task": 308924, "n_decoded": 79893, "source": "server log, 2026-11-03 09:19:06Z"}
	if code := g.call("POST", "/v1/runs/h1/held", map[string]any{"task": 308924, "source": "log"}, nil); code != 400 {
		t.Errorf("without n_decoded: %d, want 400", code)
	}
	if code := g.call("POST", "/v1/runs/h2/held", in, nil); code != 409 {
		t.Errorf("a refused connection: %d, want 409", code)
	}
	var dry RunView
	if code := g.call("POST", "/v1/runs/h1/held?dry_run=1", in, &dry); code != 200 || dry.Verdict != "FAIL" || !dry.DryRun {
		t.Fatalf("dry run: %d %+v", code, dry)
	}
	if r, _ := g.state.GetRun(ctx, "h1"); r.Verdict != store.Aborted {
		t.Fatalf("the dry run changed the verdict to %s", r.Verdict)
	}
	var v RunView
	if code := g.call("POST", "/v1/runs/h1/held", in, &v); code != 200 {
		t.Fatalf("held: %d", code)
	}
	want := "held stream: model output unparseable for 9m 55s (n_decoded 79,893); the oracle said no"
	if v.Verdict != "FAIL" || v.PriorVerdict != "ABORTED" || v.VerdictReason != want {
		t.Fatalf("held: %s (was %s) %q", v.Verdict, v.PriorVerdict, v.VerdictReason)
	}
	r, _ := g.state.GetRun(ctx, "h1")
	var p struct {
		Held struct {
			By, Source string
			Task       int64
			Decoded    int `json:"n_decoded"`
		} `json:"held_stream"`
	}
	json.Unmarshal(r.Provenance, &p)
	if p.Held.By != "server log" || p.Held.Task != 308924 || p.Held.Decoded != 79893 || !strings.Contains(p.Held.Source, "09:19:06Z") {
		t.Errorf("recorded %+v", p.Held)
	}
}
