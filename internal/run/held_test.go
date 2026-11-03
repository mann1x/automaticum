package run

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

// The agent's error of a client's run (942763-004): the SDK gave the
// server up after 595 s while it decoded 79,893 tokens.
const silentError = "Failed to process successful response: OpencotiServerSilentError: ETIMEDOUT: the opencoti server sent nothing for 595s (3 keepalive periods); presumed dead"

func heldSlots(grow bool) []SlotSample {
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	out := []SlotSample{{At: t0, Task: 7, Processing: true, Decoded: 300}}
	for i := range 60 {
		d := 79893
		if grow {
			d = 1000*(i+1) + 19893
		}
		out = append(out, SlotSample{At: t0.Add(time.Duration(10*(i+1)) * time.Second), Task: 9, Processing: true, Decoded: d})
	}
	return append(out, SlotSample{At: t0.Add(610 * time.Second), Task: 9})
}

func TestHeldStreamVerdict(t *testing.T) {
	h := HeldStream(silentError, heldSlots(true))
	if !h.Proven() || h.Decoded != 79893 || h.DecodedSilent != 59000 || h.SilentS != 595 {
		t.Fatalf("held %+v", h)
	}
	if got, want := h.Reason(), "held stream: model output unparseable for 9m 55s (n_decoded 79,893)"; got != want {
		t.Errorf("reason %q, want %q", got, want)
	}
	// Not proven: no readings, or a count that did not move, or another
	// fault altogether.
	for name, x := range map[string]*Held{
		"error text alone":  HeldStream(silentError, nil),
		"count not growing": HeldStream(silentError, heldSlots(false)),
	} {
		if x == nil || x.Proven() {
			t.Errorf("%s: %+v, want a held stream not proven", name, x)
		}
	}
	if x := HeldStream("connect ECONNREFUSED 127.0.0.1:8261", heldSlots(true)); x != nil {
		t.Errorf("a refused connection is a held stream: %+v", x)
	}

	// The oracle decides a proven held stream (D136); a fault alone stays
	// ABORTED.
	for _, c := range []struct {
		f    Facts
		want store.Verdict
		has  string
	}{
		{Facts{OracleRan: true, Held: h.Reason()}, store.Fail, "held stream: model output unparseable for 9m 55s (n_decoded 79,893); the oracle said no"},
		{Facts{OracleRan: true, OraclePassed: true, Held: h.Reason()}, store.Pass, "oracle passed; held stream"},
		{Facts{OracleRan: true, Infra: "transport: " + silentError}, store.Aborted, "not the model's failure"},
		{Facts{Aborted: "the harness stopped", Held: h.Reason()}, store.Aborted, "the harness stopped"},
	} {
		v, why := Decide(c.f)
		if v != c.want || !strings.Contains(why, c.has) {
			t.Errorf("Decide(%+v) = %s %q, want %s with %q", c.f, v, why, c.want, c.has)
		}
	}

	// Rescore keeps the held stream the run recorded.
	prov, _ := json.Marshal(map[string]any{"runner": End{AgentEnd: "error", AgentError: silentError}, "held_stream": h})
	v, why, ok := Rescore(&store.Run{Verdict: store.Fail, Provenance: prov}, true)
	if !ok || v != store.Pass || !strings.Contains(why, "held stream") {
		t.Errorf("Rescore = %s %q %v", v, why, ok)
	}
	prov, _ = json.Marshal(map[string]any{"runner": End{AgentEnd: "error", AgentError: silentError}})
	if v, _, _ := Rescore(&store.Run{Verdict: store.Fail, Provenance: prov}, false); v != store.Aborted {
		t.Errorf("Rescore without the proof = %s, want ABORTED", v)
	}
}
