package ingest

import (
	"encoding/json"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// Firing is the evidence of one text detector's finding on a run: the blocks
// of one channel it fired on.
type Firing struct {
	Channel string `json:"channel"` // reasoning or text
	Blocks  int    `json:"blocks"`  // blocks it fired on
	Of      int    `json:"of"`      // blocks of that channel in the run
	Turns   []int  `json:"turns"`   // the turn of each, the first maxTurns
	// First is the firing on the first of those blocks.
	First detect.Hit `json:"first"`
}

// maxTurns is how many turns a finding lists.
const maxTurns = 50

// detectorFindings runs the detector library over what the model wrote and
// the calls it made. One finding per detector and channel; a detector's
// result is never merged with another's (070 §3).
func detectorFindings(runID string, ev *Events, opt detect.Options) ([]store.Finding, []store.Metric) {
	var findings []store.Finding
	var metrics []store.Metric
	m := func(name string, v float64) {
		metrics = append(metrics, store.Metric{RunID: runID, Name: name, Value: v})
	}
	type key struct{ detector, channel string }
	fired := map[key]*Firing{}
	blocks := map[string]int{}
	looped := 0
	for _, b := range ev.Blocks {
		blocks[b.Kind]++
		loop := false
		for _, h := range detect.Block(b.Text, opt) {
			if !detect.Available(h.Detector) {
				continue
			}
			f := fired[key{h.Detector, b.Kind}]
			if f == nil {
				f = &Firing{Channel: b.Kind, First: h}
				fired[key{h.Detector, b.Kind}] = f
			}
			f.Blocks++
			if len(f.Turns) < maxTurns {
				f.Turns = append(f.Turns, b.Turn)
			}
			if h.Detector != detect.Runaway {
				loop = true
			}
		}
		if loop {
			looped++
		}
	}
	for _, d := range detect.Text {
		if !detect.Available(d) {
			m("detector."+d+".available", 0)
			continue
		}
		m("detector."+d+".available", 1)
		for _, channel := range []string{"reasoning", "text"} {
			f := fired[key{d, channel}]
			if f == nil {
				m("detector."+d+"."+channel, 0)
				continue
			}
			m("detector."+d+"."+channel, float64(f.Blocks))
			f.Of = blocks[channel]
			raw, _ := json.Marshal(f)
			sev := "warning"
			if d == detect.Runaway {
				sev = "info" // a long block is a size, not yet a fault
			}
			turn := 0
			if len(f.Turns) > 0 {
				turn = f.Turns[0]
			}
			findings = append(findings, store.Finding{RunID: runID, Detector: d, DetectorVersion: detect.Version,
				Severity: sev, Turn: turn, Count: f.Blocks, Evidence: raw})
		}
	}
	// Blocks on which a looping detector fired: derived here for reading,
	// with its components above; the OR alone is never what is stored.
	m("detector.looped_blocks", float64(looped))

	if d := detect.RepeatedToolCall; !detect.Available(d) {
		m("detector."+d+".available", 0)
	} else {
		m("detector."+d+".available", 1)
		seqs := detect.RepeatedCalls(ev.Calls, 0)
		m("detector."+d, float64(len(seqs)))
		for _, s := range seqs {
			raw, _ := json.Marshal(s)
			sev := "info" // where the SDK would warn the model
			if s.Count >= detect.RepeatedCallsHard {
				sev = "warning" // where it would escalate
			}
			findings = append(findings, store.Finding{RunID: runID, Detector: d, DetectorVersion: detect.Version,
				Severity: sev, Turn: s.Turn, Count: s.Count, Evidence: raw})
		}
	}
	return findings, metrics
}
