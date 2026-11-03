package ingest

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// The reason a client's run had (942763-004): the SDK gave the server up
// after 595 s while it decoded 79,893 tokens.
const silentReason = "the server failed after the SDK's retries (transport: Failed to process successful response: OpencotiServerSilentError: ETIMEDOUT: the opencoti server sent nothing for 595s (3 keepalive periods); presumed dead (ETIMEDOUT)); not the model's failure"

func TestHeldStream(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	var slots []run.SlotSample
	// Task 7 answered; task 9 is held: 60 readings, ten seconds apart, the
	// count growing by 1,000 tokens each, then the slot stops.
	slots = append(slots, run.SlotSample{At: t0, Slot: 0, Task: 7, Processing: true, Decoded: 300})
	for i := range 60 {
		slots = append(slots, run.SlotSample{At: t0.Add(time.Duration(10*(i+1)) * time.Second), Slot: 0, Task: 9, Processing: true, Decoded: 1000 * (i + 1)})
	}
	slots = append(slots, run.SlotSample{At: t0.Add(610 * time.Second), Slot: 0, Task: 9, Decoded: 0})
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, s := range slots {
		enc.Encode(s)
	}
	read, err := ReadSlots(&b)
	if err != nil || len(read) != len(slots) {
		t.Fatalf("ReadSlots: %d readings, %v", len(read), err)
	}

	prov, _ := json.Marshal(map[string]any{"runner": map[string]any{"agent_error": "Failed to process successful response: OpencotiServerSilentError: ETIMEDOUT: the opencoti server sent nothing for 595s (3 keepalive periods); presumed dead"}})
	for _, c := range []struct {
		name   string
		run    store.Run
		slots  []run.SlotSample
		by     string
		silent float64
		dec    int
		grew   bool
	}{
		{"reason and slots", store.Run{ID: "a", Verdict: store.Aborted, VerdictReason: silentReason}, read, "slots", 595, 60000, true},
		{"reason alone", store.Run{ID: "b", Verdict: store.Aborted, VerdictReason: silentReason}, nil, "error text", 595, 0, false},
		{"the runner's error", store.Run{ID: "c", Verdict: store.Aborted, VerdictReason: "the runner failed", Provenance: prov}, nil, "error text", 595, 0, false},
		{"another fault", store.Run{ID: "d", Verdict: store.Aborted, VerdictReason: "the server failed: ECONNREFUSED"}, read, "", 0, 0, false},
	} {
		h := HeldStreamOf(&c.run, c.slots)
		if c.by == "" {
			if h != nil {
				t.Errorf("%s: held stream %+v, want none", c.name, h)
			}
			continue
		}
		if h == nil {
			t.Fatalf("%s: no held stream", c.name)
		}
		if h.By != c.by || h.SilentS != c.silent || h.Decoded != c.dec || h.Grew != c.grew {
			t.Errorf("%s: %+v, want by %s, silent %v, n_decoded %d, grew %v", c.name, h, c.by, c.silent, c.dec, c.grew)
		}
	}
	h := HeldStreamOf(&store.Run{VerdictReason: silentReason}, read)
	if h.Task != 9 || h.DecodedSilent != 59000 || h.Readings != 60 {
		t.Errorf("task %d, %d in silence, %d readings; want 9, 59000, 60", h.Task, h.DecodedSilent, h.Readings)
	}

	// Through the analysis: the finding and its metrics.
	res := Derive(&Material{Run: &store.Run{ID: "a", Verdict: store.Aborted, VerdictReason: silentReason}, Events: &Events{}, Slots: read, Oracles: map[string]string{}})
	var found bool
	for _, f := range res.Findings {
		found = found || f.Detector == HeldStream
	}
	got := map[string]float64{}
	for _, m := range res.Metrics {
		got[m.Name] = m.Value
	}
	if !found || got[HeldStream] != 1 || got[HeldStream+".n_decoded"] != 60000 || got[HeldStream+".silent_seconds"] != 595 {
		t.Errorf("finding %v, metrics %v", found, got)
	}
	res = Derive(&Material{Run: &store.Run{ID: "e", Verdict: store.Fail}, Events: &Events{}, Oracles: map[string]string{}})
	for _, m := range res.Metrics {
		if m.Name == HeldStream && m.Value != 0 {
			t.Errorf("a failed run has held_stream %v", m.Value)
		}
	}
}
