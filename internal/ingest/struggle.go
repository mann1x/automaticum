package ingest

import (
	"encoding/json"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// StruggleSignals is the finding that lists the struggle signals of a run
// (070 §4, D126), each with the turn it first held. It judges nothing: how
// well each signal tells a run that will not pass is a question across
// runs, which `automaticum report struggle` answers.
const StruggleSignals = "struggle_signals"

// StruggleTriggers are the signals of D126, in the order they are shown:
// the SDK's own (its struggle nudge and offer, the compaction budget's
// emergency, the loop guard's hard warning or stop, the reasoning-loop
// guard's abort), a second compaction (the owner's escalation study: the
// first happens in runs that pass, the second is the cliff) and the SDK's
// struggle detector replayed (its nudge, its suggestion).
var StruggleTriggers = []string{"sdk_nudge", "sdk_offer", "compaction_emergency", "loop_hard", "reasoning_aborted",
	"second_compaction", "replay_nudge", "replay_suggest"}

// sdkSignals are the notice keys behind each of the SDK's own signals.
var sdkSignals = map[string][]string{
	"sdk_nudge":            {"status.struggle.nudge"},
	"sdk_offer":            {"status.struggle.offer"},
	"compaction_emergency": {"status.compaction_budget_emergency"},
	"loop_hard":            {"status.loop_detection.hard", "status.loop_detection.stopped"},
	"reasoning_aborted":    {"status.reasoning_loop.aborted"},
}

// struggleSignals evaluates every trigger on the run: struggle.<trigger>
// (1) and struggle.<trigger>.turn where it held, struggle.signals (how
// many held) always, and the finding when any did.
func struggleSignals(runID string, ev *Events, m func(string, float64), res *Result) {
	type held struct {
		Trigger string `json:"trigger"`
		Turn    int    `json:"turn"`
		Count   int    `json:"count"`
	}
	at := map[string]held{}
	for trigger, keys := range sdkSignals {
		for _, k := range keys {
			n := ev.Notices[k]
			if n == 0 {
				continue
			}
			h, seen := at[trigger]
			if t := ev.NoticeTurns[k]; !seen || t < h.Turn {
				h.Turn = t
			}
			h.Trigger, h.Count = trigger, h.Count+n
			at[trigger] = h
		}
	}
	if ev.Compactions >= 2 {
		h := held{Trigger: "second_compaction", Count: ev.Compactions - 1}
		if len(ev.CompactionLog) >= 2 {
			h.Turn = ev.CompactionLog[1].Turn
		}
		at[h.Trigger] = h
	}
	firings := detect.Struggle(ev.Struggle)
	byReason := map[string]int{}
	for _, f := range firings {
		trigger := "replay_" + f.Kind
		h, seen := at[trigger]
		if !seen {
			h = held{Trigger: trigger, Turn: f.Turn}
		}
		h.Count++
		at[trigger] = h
		byReason[f.Kind+"."+f.Reason]++
	}
	for k, n := range byReason {
		m("struggle.replay."+k, float64(n))
	}
	var list []held
	for _, trigger := range StruggleTriggers {
		h, ok := at[trigger]
		if !ok {
			continue
		}
		list = append(list, h)
		m("struggle."+trigger, 1)
		m("struggle."+trigger+".turn", float64(h.Turn))
	}
	m("struggle.signals", float64(len(list)))
	if len(list) == 0 {
		return
	}
	first := list[0].Turn
	for _, h := range list {
		first = min(first, h.Turn)
	}
	raw, _ := json.Marshal(struct {
		Signals []held                  `json:"signals"`
		Replay  []detect.StruggleFiring `json:"replay,omitempty"`
	}{list, firings})
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: StruggleSignals, DetectorVersion: Versions[StruggleSignals],
		Severity: "note", Turn: first, Count: len(list), Evidence: raw})
}

// transactions counts the change protocol's transactions (070 §2): how
// many settled, were kept, put back, carried on, had their check give
// output, the files put back, the attempts submitted with nothing changed,
// the plans, and how long a transaction took.
func transactions(ev *Events, m func(string, float64)) {
	if len(ev.Transactions) == 0 && ev.EmptyAttempts == 0 && ev.Plans == 0 {
		return
	}
	kept, discarded, carried, checked, putBack := 0, 0, 0, 0, 0
	var ms []float64
	for _, t := range ev.Transactions {
		switch {
		case t.Kept:
			kept++
		case t.Carried:
			carried++
		default:
			discarded++
		}
		if t.Checked {
			checked++
		}
		putBack += t.FilesPutBack
		if t.ElapsedMS > 0 {
			ms = append(ms, t.ElapsedMS)
		}
	}
	m("transactions.settled", float64(len(ev.Transactions)))
	m("transactions.kept", float64(kept))
	m("transactions.discarded", float64(discarded))
	m("transactions.carried", float64(carried))
	m("transactions.checked", float64(checked))
	m("transactions.files_put_back", float64(putBack))
	m("transactions.empty_attempts", float64(ev.EmptyAttempts))
	m("transactions.plans", float64(ev.Plans))
	distribution(m, "transactions.elapsed_ms", ms)
}
