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
	// Matching says which way of matching made the finding: every match
	// (the reference) or the older first match only (D76).
	Matching string `json:"matching"`
	// First is the firing on the first of those blocks.
	First detect.Hit `json:"first"`
	// Rules is the reasoning-loop guard's rule set the finding was read
	// with: the run's SDK release decides it (detect.RulesFor).
	Rules string `json:"rules,omitempty"`
}

// DetectOptions are the detector options a run is read with: the
// reasoning-loop and call-guard rules of its SDK build, and its task's prompt as the
// start of what the model was given.
func DetectOptions(r *store.Run) detect.Options {
	var task struct {
		Task struct {
			Prompt string `json:"prompt"`
		} `json:"task"`
	}
	json.Unmarshal(r.Provenance, &task)
	return detect.Options{Rules: detect.RulesFor(r.Harness), CallRules: detect.CallGuardFor(r.Harness), Given: task.Task.Prompt}
}

// BlockOptions are the options for one of a run's blocks.
func BlockOptions(opt detect.Options, ev *Events, b Block) detect.Options {
	return blockOptions(opt, ev.Given.String(), b)
}

// blockOptions are the options for one block: with the rules of 4.100.258
// on, what the model was given before it (opt.Given is the task's prompt).
func blockOptions(opt detect.Options, given string, b Block) detect.Options {
	if opt.Rules != detect.Rules258 {
		return opt
	}
	o := opt
	if b.GivenTo > 0 && b.GivenTo <= len(given) {
		if o.Given != "" {
			o.Given += "\n"
		}
		o.Given += given[:b.GivenTo]
	}
	return o
}

// maxTurns is how many turns a finding lists.
const maxTurns = 50

// detectorFindings runs the detector library over what the model wrote and
// the calls it made. One finding per detector and channel; a detector's
// result is never merged with another's (070 §3).
func detectorFindings(runID string, ev *Events, opt detect.Options) ([]store.Finding, []store.Metric) {
	f, m, _ := detectorRun(runID, ev, opt)
	return f, m
}

// FiredTurns are, by detector, the turns it fired in; the call guard's by
// "loop_detection", the guard it replays.
type FiredTurns map[string]map[int]bool

func (ft FiredTurns) add(d string, turn int) {
	if ft[d] == nil {
		ft[d] = map[int]bool{}
	}
	ft[d][turn] = true
}

// detectorRun is detectorFindings, with the turns each detector fired in.
func detectorRun(runID string, ev *Events, opt detect.Options) ([]store.Finding, []store.Metric, FiredTurns) {
	turns := FiredTurns{}
	var findings []store.Finding
	var metrics []store.Metric
	m := func(name string, v float64) {
		metrics = append(metrics, store.Metric{RunID: runID, Name: name, Value: v})
	}
	type key struct{ detector, channel string }
	fired := map[key]*Firing{}
	blocks := map[string]int{}
	looped := 0
	given := ev.Given.String()
	for _, b := range ev.Blocks {
		blocks[b.Kind]++
		loop := false
		for _, h := range detect.Block(b.Text, blockOptions(opt, given, b)) {
			if !detect.Available(h.Detector) {
				continue
			}
			f := fired[key{h.Detector, b.Kind}]
			if f == nil {
				f = &Firing{Channel: b.Kind, Matching: opt.Matching(), First: h}
				if h.Detector == detect.ReasoningLoop {
					f.Rules = detect.Rules250
					if opt.Rules != "" {
						f.Rules = opt.Rules
					}
				}
				fired[key{h.Detector, b.Kind}] = f
			}
			f.Blocks++
			turns.add(h.Detector, b.Turn)
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
	// What the reasoning-loop guard cut off, for its replay only: it is
	// not a block of the run and is counted nowhere else.
	for _, b := range ev.CutBlocks {
		for _, h := range detect.Block(b.Text, blockOptions(opt, given, b)) {
			if h.Detector == detect.ReasoningLoop && detect.Available(h.Detector) {
				turns.add(h.Detector, b.Turn)
			}
		}
	}
	// Blocks on which a looping detector fired: derived here for reading,
	// with its components above; the OR alone is never what is stored.
	m("detector.looped_blocks", float64(looped))

	if d := detect.RepeatedToolCall; !detect.Available(d) {
		m("detector."+d+".available", 0)
	} else {
		m("detector."+d+".available", 1)
		seqs := detect.RepeatedCallsFor(ev.Calls, opt.T.Int(detect.ThCallsSoft), opt.CallRules)
		m("detector."+d, float64(len(seqs)))
		for _, s := range seqs {
			// The SDK warns from the second call of the sequence; a
			// sibling in the turn of the call before is not one. From
			// 4.100.282 only a call that fails is warned.
			for k := s.Index + 1; k <= s.End && k < len(ev.Calls); k++ {
				if ev.Calls[k].Turn != 0 && ev.Calls[k].Turn == ev.Calls[k-1].Turn {
					continue
				}
				if opt.CallRules == detect.CallGuard282 && !ev.Calls[k].Failed {
					continue
				}
				turns.add("loop_detection", ev.Calls[k].Turn)
			}
			raw, _ := json.Marshal(s)
			sev := "info" // where the SDK would warn the model
			if s.Given >= opt.T.Int(detect.ThCallsHard) {
				sev = "warning" // where it escalated
			}
			findings = append(findings, store.Finding{RunID: runID, Detector: d, DetectorVersion: detect.Version,
				Severity: sev, Turn: s.Turn, Count: s.Count, Evidence: raw})
		}
	}
	// The SDK's call guard replayed (070 §3.2), and the two views of it
	// that 070 §3 names (D129).
	firings := detect.CallGuard(ev.Calls, ev.ForkCalls, opt.CallRules, opt.T)
	views := map[string][]detect.GuardFiring{}
	for _, f := range firings {
		if f.Given {
			turns.add("loop_detection", f.Turn)
		}
		if f.Rule == "same_result" {
			views[detect.RepeatedCallResult] = append(views[detect.RepeatedCallResult], f)
		} else {
			views[detect.FailedIdenticalCall] = append(views[detect.FailedIdenticalCall], f)
		}
		switch {
		case detect.WritingTools[f.Tool]:
			views[detect.EditOscillation] = append(views[detect.EditOscillation], f)
		case f.Rule == "same_result":
			views[detect.RereadLoop] = append(views[detect.RereadLoop], f)
		}
	}
	for _, d := range []string{detect.FailedIdenticalCall, detect.RepeatedCallResult, detect.EditOscillation, detect.RereadLoop} {
		base := d
		switch d {
		case detect.EditOscillation, detect.RereadLoop:
			base = detect.RepeatedCallResult
		}
		if !detect.Available(base) || (d == detect.EditOscillation && !detect.Available(detect.FailedIdenticalCall)) {
			m("detector."+d+".available", 0)
			continue
		}
		m("detector."+d+".available", 1)
		fs := views[d]
		m("detector."+d, float64(len(fs)))
		given, hard := 0, 0
		for _, f := range fs {
			if f.Given {
				given++
			}
		}
		// The verdicts the model was told of: all of them before 4.100.282,
		// from it those on a call that failed or landed again (CallGuard282).
		m("detector."+d+".given", float64(given))
		if len(fs) == 0 {
			continue
		}
		sev := "info" // where the SDK would warn the model
		for _, f := range fs {
			if f.Kind == "hard" {
				hard++
				if f.Given {
					sev = "warning" // where it escalated
				}
			}
		}
		keep := fs
		if len(keep) > maxTurns {
			keep = keep[:maxTurns]
		}
		raw, _ := json.Marshal(map[string]any{"firings": keep, "hard": hard, "given": given})
		findings = append(findings, store.Finding{RunID: runID, Detector: d, DetectorVersion: detect.Version,
			Severity: sev, Turn: fs[0].Turn, Count: len(fs), Evidence: raw})
	}
	return findings, metrics, turns
}
