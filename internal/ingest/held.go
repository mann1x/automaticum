package ingest

import (
	"bufio"
	"encoding/json"
	"io"

	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// HeldStream is the finding that the SDK gave the server up as dead while
// the server was still decoding for the run (run.Held): its parser held
// back what the model wrote, so nothing reached the wire until the SDK's
// silence ceiling cancelled the request. A run whose readings of /slots
// prove it is decided by its oracle (D136); one known from the SDK's
// message alone stays ABORTED, and the finding says which.
const HeldStream = "held_stream"

// ReadSlots reads a run's slots blob, one reading of /slots per line.
func ReadSlots(r io.Reader) ([]run.SlotSample, error) {
	var out []run.SlotSample
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var s run.SlotSample
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			out = append(out, s)
		}
	}
	return out, sc.Err()
}

// HeldStreamOf says whether a run ended on the SDK's silence ceiling: the
// held stream its record carries (the run's own proof, or one an operator
// recorded from the server's log), else from the agent's error the runner
// reported, or the verdict's reason for a run without it, with what the
// readings of /slots say of it; nil if it did not.
func HeldStreamOf(r *store.Run, slots []run.SlotSample) *run.Held {
	var p struct {
		Runner *struct {
			AgentError string `json:"agent_error"`
		} `json:"runner"`
		HeldStream *run.Held `json:"held_stream"`
	}
	if json.Unmarshal(r.Provenance, &p) == nil && p.HeldStream != nil {
		return p.HeldStream
	}
	if p.Runner != nil {
		if h := run.HeldStream(p.Runner.AgentError, slots); h != nil {
			return h
		}
	}
	return run.HeldStream(r.VerdictReason, slots)
}

// heldStream writes held_stream (1 for a run that ended on it) and its
// finding: a warning, as cap_hit is, so that the two are counted side by
// side across checkpoints.
func heldStream(r *store.Run, slots []run.SlotSample, m func(string, float64), res *Result) {
	h := HeldStreamOf(r, slots)
	if h == nil {
		m(HeldStream, 0)
		return
	}
	m(HeldStream, 1)
	if h.SilentS > 0 {
		m(HeldStream+".silent_seconds", h.SilentS)
	}
	if h.Decoded > 0 {
		m(HeldStream+".n_decoded", float64(h.Decoded))
	}
	if h.By == "slots" {
		m(HeldStream+".n_decoded_in_silence", float64(h.DecodedSilent))
	}
	raw, _ := json.Marshal(struct {
		*run.Held
		Proven bool `json:"proven"`
	}{h, h.Proven()})
	res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: HeldStream, DetectorVersion: Versions[HeldStream],
		Severity: "warning", Count: 1, Evidence: raw})
}
