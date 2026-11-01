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
