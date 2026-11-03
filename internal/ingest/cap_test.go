package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// A cap hit counts at any turn: the server's own "length", or a completion
// that reached the limit the request declared, less the tolerance, from a
// server that did not say so. A probe or a failed request is not one.
func TestCapHits(t *testing.T) {
	opts := map[string]any{"num_predict": float64(1000)}
	agent := []Request{
		{Seq: 1, Agent: true, Options: opts, EvalTokens: 400, DoneReason: "stop"},
		{Seq: 2, Agent: true, Options: opts, EvalTokens: 1000, DoneReason: "length"},
		{Seq: 3, Agent: true, Options: opts, EvalTokens: 1000 - CapTolerance, DoneReason: "stop"},
		{Seq: 4, Agent: true, Options: opts, EvalTokens: 1000 - CapTolerance - 1, DoneReason: "stop"},
		{Seq: 5, Agent: true, EvalTokens: 50, DoneReason: "length"},
		{Seq: 6, Agent: true, Options: opts, EvalTokens: 1000, Failed: true},
		{Seq: 7, Options: opts, EvalTokens: 1000},
	}
	got := CapHits(agent)
	want := []Capped{{2, 1000, 1000, "length"}, {3, 984, 1000, "declared limit"}, {5, 50, 0, "length"}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A request that got no answer (one the agent's side cut, a steer or a
// cancel, or one that failed) has no finish reason and cannot have one: it
// is counted as failed, not as unexplained, so it does not leave the cap
// audit unverified.
func TestFailedRequestFinish(t *testing.T) {
	reqs := []Request{{Agent: true, Chat: true, DoneReason: "stop"}, {Agent: true, Chat: true, Failed: true}, {Agent: true, Chat: true}}
	res := Analyse(&store.Run{ID: "r", Arm: json.RawMessage(`{}`)}, &Events{}, reqs)
	got := map[string]float64{}
	for _, m := range res.Metrics {
		got[m.Name] = m.Value
	}
	if got["finish.stop"] != 1 || got["finish.failed"] != 1 || got["finish.none"] != 1 {
		t.Errorf("finish: stop %g, failed %g, none %g", got["finish.stop"], got["finish.failed"], got["finish.none"])
	}
}

// A run with an arm on record keeps the findings written before the wire
// against the arm: the cap audit's and the turns' (analysis version 14; the
// wire's findings replaced them before).
func TestFindingsBeforeWireKept(t *testing.T) {
	ev, err := ReadEvents(strings.NewReader(`{"t":"2026-01-02T03:04:00Z","k":"runner","note":"guard_trip","guard":"nudge","phase":"no_tool_call","iteration":3,"message":"carry on"}`))
	if err != nil {
		t.Fatal(err)
	}
	reqs := []Request{{Seq: 1, Agent: true, Chat: true, EvalTokens: 96000, DoneReason: "length"}}
	res := Analyse(&store.Run{ID: "r", Arm: json.RawMessage(`{"name":"a"}`)}, ev, reqs)
	got := map[string]bool{}
	for _, f := range res.Findings {
		got[f.Detector] = true
	}
	if !got[CapHit] || !got[GuardTrip] {
		t.Errorf("findings %v", got)
	}
}
