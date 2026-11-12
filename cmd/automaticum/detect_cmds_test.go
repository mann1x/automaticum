package main

import (
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/ingest"
)

func TestDetectFailIf(t *testing.T) {
	for _, bad := range []string{"swallowed_argument", "nope>1", "swallowed_argument >> 1", "runs_with.nope>0"} {
		if _, err := parseFailIf(list{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	tests, err := parseFailIf(list{"swallowed_argument>0", "runs_with.swallowed_argument>=2", "share.swallowed_argument>0.4", "failed==3", "metric.tool_calls.failed>2", "runs!=2"})
	if err != nil {
		t.Fatal(err)
	}
	rows := []*daemon.DetectRow{
		{Run: "a", Firings: map[string]int{"swallowed_argument": 18}, Metrics: map[string]float64{"tool_calls.failed": 3},
			Outcomes: []ingest.ToolOutcome{{Tool: "editor", Calls: 24, Failed: 3, Errors: map[string]int{"Missing required argument `new_text`": 3}}}},
		{Run: "b", Firings: map[string]int{"swallowed_argument": 0}},
		{Run: "c", Error: "unreadable"},
	}
	all := newTotal("all")
	for _, r := range rows {
		all.add(r)
	}
	if all.Runs != 2 || all.Calls != 24 || all.RunsWith["swallowed_argument"] != 1 || all.Errors["editor: Missing required argument `new_text`"] != 3 {
		t.Fatalf("%+v", all)
	}
	want := []bool{true, false, true, true, true, false}
	for i, f := range tests {
		if f.holds(all) != want[i] {
			t.Errorf("%s: %v (value %v)", f.text, !want[i], all.get(f.name))
		}
	}
	if got := firingsText(&daemon.DetectRow{Firings: map[string]int{"swallowed_argument": 18, "runaway": 0, "reasoning_loop": 0},
		Stored: map[string]int{"swallowed_argument": 0, "reasoning_loop": 2}, ByTool: map[string]map[string]float64{"tool_calls.swallowed_argument": {"editor": 18}}}); got != "reasoning_loop=0 (stored 2)  swallowed_argument=18 (stored 0) [editor 18]" {
		t.Errorf("firings %q", got)
	}
	if axisValue(&daemon.DetectRow{Group: "with x"}, "finding.x") != "with x" || axisValue(&daemon.DetectRow{}, "cohort") != "-" {
		t.Error("axis values")
	}
	if !strings.Contains(outcomeText(rows[0].Outcomes[0]), "3 Missing required argument") {
		t.Error(outcomeText(rows[0].Outcomes[0]))
	}
}

// The totals' reasoning-loop line: the guard's cuts (the exact key, not
// its causes under it) against the model's time, over the runs that carry
// metrics; a run without the cut key is a run without cuts, and a mean is
// over every run with metrics.
func TestDetectLoopTotals(t *testing.T) {
	rows := []*daemon.DetectRow{
		{Run: "a", Metrics: map[string]float64{"guard_trips.reasoning_loop.cut": 3, "guard_trips.reasoning_loop.cut.answer_loop": 3, "time.model_ms": 1_800_000}},
		{Run: "b", Metrics: map[string]float64{"time.model_ms": 1_800_000}},
		{Run: "c"},
	}
	all := newTotal("all")
	for _, r := range rows {
		all.add(r)
	}
	l := all.Loops
	if l == nil || l.Cuts != 3 || l.RunsWith != 1 || l.ModelMS != 3_600_000 || l.PerModelHr != 3 || all.MetricRuns != 2 || all.Runs != 3 {
		t.Fatalf("%+v %+v", all, l)
	}
	if got := loopText(all); got != "reasoning-loop cuts 3 in 1 of 2 runs, 3.00 per model hour (model time 1h 00m 00s)" {
		t.Errorf("%q", got)
	}
	none := newTotal("x")
	none.add(rows[2])
	if none.Loops != nil || none.MetricRuns != 0 {
		t.Errorf("no metrics: %+v", none)
	}
}
