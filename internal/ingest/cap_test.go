package ingest

import "testing"

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
