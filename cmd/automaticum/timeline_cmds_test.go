package main

import (
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/ingest"
)

// A run's events become its timeline: a line a turn with its calls and
// failures, and a line for each trip, notice and the end.
func TestTimeline(t *testing.T) {
	line := func(ts, ev string) string {
		return `{"t":"2026-01-02T03:04:` + ts + `Z","k":"event","event":{"type":"agent_event","payload":{"event":` + ev + `}}}`
	}
	stream := strings.Join([]string{
		line("00", `{"type":"iteration_start","iteration":1}`),
		line("01", `{"type":"content_end","contentType":"tool","toolName":"read_files","toolCallId":"a","output":[{"success":true}]}`),
		line("02", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"b","output":[{"success":false,"error":"No replacement performed"}]}`),
		line("03", `{"type":"notice","noticeType":"status","metadata":{"kind":"context_breakdown"}}`),
		line("04", `{"type":"notice","noticeType":"status","metadata":{"kind":"loop_detection","phase":"soft"}}`),
		`{"t":"2026-01-02T03:04:04Z","k":"runner","note":"guard_trip","guard":"loop_detection","phase":"soft"}`,
		line("10", `{"type":"iteration_start","iteration":2}`),
		line("11", `{"type":"notice","noticeType":"status","metadata":{"kind":"auto_compaction","phase":"completed"}}`),
		line("15", `{"type":"done","reason":"completed"}`),
	}, "\n")
	ev := &ingest.Events{KeepTimeline: true}
	if err := ev.Read(strings.NewReader(stream)); err != nil {
		t.Fatal(err)
	}
	got := timelineLines(ev.Timeline, ev.First)
	want := []string{
		"turn 1: 2 calls, 1 failed (editor: No replacement performed 1)",
		"guard trip: loop_detection soft",
		"turn 2",
		"notice status.auto_compaction.completed",
		"the agent ended: completed",
	}
	if len(got) != len(want) {
		t.Fatalf("lines %+v", got)
	}
	for i, w := range want {
		if got[i].what != w {
			t.Errorf("line %d: %q, want %q", i, got[i].what, w)
		}
	}
	if got[2].after != "10s" || got[4].after != "15s" || got[1].turn != 1 {
		t.Errorf("times %+v", got)
	}
	// Without KeepTimeline nothing is kept.
	plain := &ingest.Events{}
	plain.Read(strings.NewReader(stream))
	if len(plain.Timeline) != 0 {
		t.Errorf("timeline kept unasked: %d", len(plain.Timeline))
	}
}
