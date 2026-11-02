package daemon

import (
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/detect"
)

// The cell's numbers that need each run's verdict (070 §2.1, §3.1).
func TestVerdictNumbers(t *testing.T) {
	res := &CellResult{Scores: map[string]float64{}}
	rows := []CellRow{
		{Verdict: "PASS", Metrics: map[string]float64{"tokens.completion": 100, "time.model_ms": 2000, "requests": 10, "completions.empty": 1}},
		{Verdict: "PASS", Metrics: map[string]float64{"tokens.completion": 300, "time.model_ms": 2000, "requests": 10, "completions.empty": 0, "cap.hits": 1},
			Findings: map[string]int{detect.ReasoningLoop: 1}},
		{Verdict: "FAIL", Metrics: map[string]float64{"tokens.completion": 600, "time.model_ms": 6000, "requests": 10, "completions.empty": 1, "cap.hits": 2},
			Findings: map[string]int{detect.ReasoningLoop: 1}},
		{Verdict: "FAIL", SupersededBy: "x", Metrics: map[string]float64{"tokens.completion": 9999}},
		{Verdict: "ABORTED", Metrics: map[string]float64{"tokens.completion": 9999}},
	}
	verdictNumbers(res, rows)
	if s := res.TokenStatsPass["tokens.completion"]; s.N != 2 || s.Median != 200 {
		t.Errorf("tokens over PASS: %+v", s)
	}
	if res.Scores["tokens_per_solved"] != 500 || res.Scores["throughput_tok_s"] != 100 {
		t.Errorf("scores: %+v", res.Scores)
	}
	if p, f := res.LoopByVerdict["PASS"], res.LoopByVerdict["FAIL"]; p != (LoopCross{Runs: 2, Looped: 1, Capped: 1, LoopedCapped: 1}) || f != (LoopCross{Runs: 1, Looped: 1, Capped: 1, LoopedCapped: 1}) {
		t.Errorf("loops by verdict: %+v", res.LoopByVerdict)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "6.7%") {
		t.Errorf("empty completions warning: %q (share %v)", res.Warnings, res.Scores["empty_completion_share"])
	}
}

func TestStreaks(t *testing.T) {
	p := map[string]bool{"a1": true, "a2": true, "a3": false, "a4": true, "a5": true, "a6": true, "a0": false}
	if s := streaks([]string{"a6", "a1", "a3", "a0", "a5", "a2", "a4"}, p); s != (Streaks{Pass: 3, Fail: 1}) {
		t.Errorf("%+v", s)
	}
}

// loop_rate's components (070 §3.1): the runs by detector, by channel and
// their first loop's turn, over the runs that count.
func TestLoopComponents(t *testing.T) {
	seen := func(first int, dets, chans []string) *loopSeen {
		l := &loopSeen{detectors: map[string]bool{}, channels: map[string]bool{}, first: first}
		for _, d := range dets {
			l.detectors[d] = true
		}
		for _, c := range chans {
			l.channels[c] = true
		}
		return l
	}
	rows := []CellRow{{RunID: "a", Verdict: "PASS"}, {RunID: "b", Verdict: "FAIL"}, {RunID: "c", Verdict: "TIMEOUT"},
		{RunID: "d", Verdict: "FAIL", SupersededBy: "e"}, {RunID: "f", Verdict: "INVALID"}, {RunID: "g", Verdict: "PASS"}}
	loops := map[string]*loopSeen{
		"a": seen(12, []string{detect.ReasoningLoop}, []string{"reasoning"}),
		"b": seen(30, []string{detect.ReasoningLoop, detect.ReasoningRepetition}, []string{"reasoning", "text"}),
		"c": seen(50, []string{detect.ReasoningRepetition}, []string{"text"}),
		"d": seen(1, []string{detect.ReasoningLoop}, []string{"reasoning"}),
		"f": seen(1, []string{detect.ReasoningLoop}, []string{"reasoning"}),
	}
	n, c := loopComponents(rows, loops)
	if n != 3 || c == nil {
		t.Fatalf("%d %+v", n, c)
	}
	if c.ByDetector[detect.ReasoningLoop] != 2 || c.ByDetector[detect.ReasoningRepetition] != 2 || c.ByChannel["reasoning"] != 2 || c.ByChannel["text"] != 2 {
		t.Errorf("components %+v", c)
	}
	if c.FirstTurn.N != 3 || c.FirstTurn.Median != 30 || c.FirstTurn.Min != 12 {
		t.Errorf("first turn %+v", c.FirstTurn)
	}
	if n, c := loopComponents(rows, nil); n != 0 || c != nil {
		t.Errorf("no loops: %d %+v", n, c)
	}
}
