package ingest

import (
	"strings"
	"testing"
)

func TestRenderer(t *testing.T) {
	lines := []string{
		`{"t":"2026-11-01T18:00:00Z","k":"runner","note":"prepared"}`,
		`{"t":"2026-11-01T18:00:01Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"iteration_start","iteration":1}}}}`,
		`{"t":"2026-11-01T18:00:02Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"content_start","contentType":"text","text":"Hel"}}}}`,
		`{"t":"2026-11-01T18:00:03Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"content_end","contentType":"text","text":"Hello there"}}}}`,
		`{"t":"2026-11-01T18:00:04Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"content_end","contentType":"tool","toolName":"read_file","input":{"path":"a.go"},"durationMs":1500}}}}`,
		`{"t":"2026-11-01T18:00:05Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"content_end","contentType":"tool","toolName":"run","input":{"cmd":"x"},"error":"boom"}}}}`,
		`{"t":"2026-11-01T18:00:06Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"notice","noticeType":"status","metadata":{"kind":"loop_detection","phase":"warn"},"message":"you repeat"}}}}`,
		`{"t":"2026-11-01T18:00:07Z","k":"user","on":"question","rule":1,"do":"answer"}`,
		`{"t":"2026-11-01T18:00:08Z","k":"end","outcome":"completed","agent_end":"completed","claimed_success":true}`,
		`not json`,
	}
	r := &Renderer{Max: 40}
	var out []string
	for _, l := range lines {
		out = append(out, r.Line([]byte(l))...)
	}
	got := strings.Join(out, "\n")
	for _, want := range []string{
		"[runner] prepared",
		"── turn 1 ──",
		"[text] Hello there",
		`[tool] read_file {"path":"a.go"} → ok, 1.5 s`,
		"[tool] run {\"cmd\":\"x\"} → error: \"boom\"",
		"[notice] status.loop_detection.warn: you repeat",
		"[user] on question:",
		"[end] completed; agent end completed; claimed success true",
		"(unreadable line) not json",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "Hel\n") || r.Turn() != 1 {
		t.Errorf("a block's start was shown, or the turn is %d:\n%s", r.Turn(), got)
	}
}
