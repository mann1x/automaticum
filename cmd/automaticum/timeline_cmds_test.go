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

// With -requests, each of the agent's requests has a line at the time it
// was sent, in its turn, with its first byte, its time and what the
// endpoint said (D139); a probe has none.
func TestTimelineRequests(t *testing.T) {
	ev := &ingest.Events{KeepTimeline: true}
	ev.Read(strings.NewReader(strings.Join([]string{
		`{"t":"2026-01-02T03:04:00Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"iteration_start","iteration":1}}}}`,
		`{"t":"2026-01-02T03:05:00Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"iteration_start","iteration":2}}}}`,
	}, "\n")))
	reqs, err := ingest.ReadWire(strings.NewReader(strings.Join([]string{
		`{"seq":1,"url":"http://e/v1/models","status":200,"t_start":"2026-01-02T03:03:59Z"}`,
		`{"seq":2,"url":"http://e/v1/chat/completions","status":200,"t_start":"2026-01-02T03:04:01Z","ms_total":45000,"ms_first_byte":31000,"endpoint_meta":[{"attempts":2}],"request_body":"{\"tools\":[{}]}"}`,
		`{"seq":3,"url":"http://e/v1/chat/completions","status":200,"t_start":"2026-01-02T03:05:01Z","ms_total":900,"request_body":"{\"tools\":[{}]}"}`,
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	got := timelineLines(withRequestMoments(ev.Timeline, reqs), ev.First)
	want := []struct {
		turn int
		what string
	}{
		{1, "turn 1"},
		{1, `request 2 (200): first byte 31s, 45s in all; endpoint [{"attempts":2}]`},
		{2, "turn 2"},
		{2, "request 3 (200): 0.9s in all"},
	}
	if len(got) != len(want) {
		t.Fatalf("lines %+v", got)
	}
	for i, w := range want {
		if got[i].turn != w.turn || got[i].what != w.what {
			t.Errorf("line %d: %d %q, want %d %q", i, got[i].turn, got[i].what, w.turn, w.what)
		}
	}
}
