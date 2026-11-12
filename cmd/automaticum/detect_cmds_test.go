package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/detect"
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

// The loop blocks in the totals (D148): summed beside the cuts; a run
// loops with a cut or a loop block; loops per model hour counts both; the
// share is of all thinking characters; every loops total carries the
// thresholds the header read with.
func TestDetectLoopBlockTotals(t *testing.T) {
	rows := []*daemon.DetectRow{
		{Run: "a", Batch: "b1", Metrics: map[string]float64{"guard_trips.reasoning_loop.cut": 1, "time.model_ms": 1_800_000,
			"thinking.loop_blocks": 2, "thinking.loop_block_chars": 300, "thinking.chars.sum": 1000, "thinking.max_repeat": 31}},
		{Run: "b", Batch: "b1", Metrics: map[string]float64{"time.model_ms": 1_800_000, "thinking.loop_blocks": 1, "thinking.loop_block_chars": 200,
			"thinking.chars.sum": 1000, "thinking.max_repeat": 22}},
		{Run: "c", Batch: "b2", Metrics: map[string]float64{"time.model_ms": 3_600_000, "thinking.chars.sum": 500, "thinking.max_repeat": 4}},
	}
	o := &detectOut{json: true, groups: []string{"batch"}, end: &daemon.DetectEnd{Runs: 3},
		head: &daemon.DetectHeader{Thresholds: []detect.Effective{{Name: detect.BlockMaxRepeat, Value: 20, From: "owner default"}, {Name: "other.x", Value: 1}}}}
	for _, r := range rows {
		o.rows = append(o.rows, r)
	}
	out := captureStdout(t, func() { o.finish(nil, "total") })
	var doc struct {
		Total  total
		Groups map[string][]total
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	l := doc.Total.Loops
	if l == nil || l.LoopBlocks != 3 || l.RunsLooping != 2 || l.LoopChars != 500 || l.ThinkingChars != 2500 || l.LoopShare != 0.2 ||
		l.LoopsPerHr != 2 || l.PerModelHr != 0.5 || l.MaxRepeat != 31 || len(l.Thresholds) != 1 || l.Thresholds[0].Name != detect.BlockMaxRepeat {
		t.Errorf("total loops %+v", l)
	}
	if g := doc.Groups["batch"]; len(g) != 2 || g[0].Loops == nil || len(g[0].Loops.Thresholds) != 1 || g[0].Loops.RunsLooping != 2 {
		t.Errorf("batch groups %+v", g)
	}
	all := newTotal("all")
	for _, r := range rows {
		all.add(r)
	}
	all.Loops.Thresholds = o.head.Thresholds[:1]
	if got := loopText(all); !strings.Contains(got, "loop blocks 3 (max_repeat >= 20, owner default), 20.0% of thinking; 2 runs looping; 2.00 loops per model hour; largest repeat 31") {
		t.Errorf("%q", got)
	}
}

// captureStdout runs f and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}
