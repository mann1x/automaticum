package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

func runner(t, note string, extra string) string {
	return `{"t":"2026-01-02T03:04:` + t + `Z","k":"runner","note":"` + note + `"` + extra + `}`
}

// turnStream is a run that edits without checking, trips the call guard,
// writes a marker into its reply, thinks without answering, compacts and
// escalates.
func turnStream() string {
	edit := func(t, id, out string) string {
		return agentLine(t, `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"`+id+`","input":{"path":"a.go","old_text":"x"},"output":`+out+`}`)
	}
	refused := `[{"success":false,"error":"Editor operation failed: No change: line 3 already reads exactly this way"}]`
	return strings.Join([]string{
		agentLine("01", `{"type":"iteration_start","iteration":1}`),
		edit("02", "a", `[{"success":true}]`),
		edit("03", "b", `[{"success":true}]`),
		agentLine("04", `{"type":"content_end","contentType":"tool","toolName":"run_commands","toolCallId":"c","input":{"commands":["go test"]},"output":[{"result":"ok"}]}`),
		agentLine("05", `{"type":"iteration_start","iteration":2}`),
		edit("06", "d", refused),
		agentLine("07", `{"type":"iteration_start","iteration":3}`),
		edit("08", "e", refused),
		runner("09", "guard_trip", `,"guard":"loop_detection","phase":"soft"`),
		agentLine("10", `{"type":"content_end","contentType":"text","text":"fixed <|channel|>final"}`),
		agentLine("11", `{"type":"usage","outputSplit":{"reasoningTokens":0,"textTokens":4,"toolInputTokens":9,"method":"provider"}}`),
		agentLine("12", `{"type":"iteration_start","iteration":4}`),
		agentLine("13", `{"type":"notice","noticeType":"status","metadata":{"kind":"auto_compaction","phase":"completed","cause":"pressure","tokensBefore":90000,"tokensAfter":20000,"durationMs":4000}}`),
		agentLine("14", `{"type":"notice","noticeType":"status","metadata":{"kind":"expert_reply","changed":true,"usage":{"inputTokens":5000,"outputTokens":700}}}`),
		agentLine("15", `{"type":"content_end","contentType":"reasoning","reasoning":"I should plan the next steps."}`),
		agentLine("16", `{"type":"usage","outputSplit":{"reasoningTokens":120,"textTokens":0,"toolInputTokens":0,"method":"provider"}}`),
		agentLine("17", `{"type":"done","reason":"completed"}`),
	}, "\n")
}

func metricMap(ms []store.Metric) map[string]float64 {
	out := map[string]float64{}
	for _, m := range ms {
		out[m.Name] = m.Value
	}
	return out
}

// TestTurnMetrics proves the metrics and findings of analysis version 9
// that come from the event stream (070 §2, §4, D126 to D129).
func TestTurnMetrics(t *testing.T) {
	ev, err := ReadEvents(strings.NewReader(turnStream()))
	if err != nil {
		t.Fatal(err)
	}
	res := Analyse(&store.Run{ID: "r", Harness: "4.100.266"}, ev, nil)
	m := metricMap(res.Metrics)
	for name, want := range map[string]float64{
		"tool_calls.failed.by_class.No change": 2,
		"edits":                                2, "edits.failed": 2, "edits.files": 1, "edits.without_check": 1,
		"turns.thinking_only": 1, "turns.ended_thinking_only": 1, "turns.no_call_no_answer": 1,
		"hygiene.control_markers":     1,
		"guard_trips.next.text":       1,
		"thinking.tokens.per_block.n": 1, "thinking.tokens.per_block.max": 120,
		"context.compaction.tokens_before.max": 90000, "context.compaction.tokens_after.max": 20000,
		"context.compactions.by_cause.pressure": 1,
		"escalation.ended":                      1, "escalation.changed": 1, "escalation.expert_tokens.input": 5000,
		// The second refused edit: refused in advance, sent again.
		"detector." + detect.FailedIdenticalCall: 1, "detector." + detect.EditOscillation: 1,
		"guard_replay.loop_detection.missed": 0, "guard_replay.loop_detection.unconfirmed": 0,
		"analysis.version": AnalysisVersion,
	} {
		if got, ok := m[name]; !ok || got != want {
			t.Errorf("%s = %v (%v), want %v", name, got, ok, want)
		}
	}
	found := map[string]store.Finding{}
	for _, f := range res.Findings {
		found[f.Detector] = f
	}
	var trips struct {
		Trips []Trip `json:"trips"`
	}
	json.Unmarshal(found[GuardTrip].Evidence, &trips)
	if len(trips.Trips) != 1 || trips.Trips[0].Next != "text" || trips.Trips[0].Turn != 3 {
		t.Errorf("guard trip: %+v", trips)
	}
	var marks struct {
		Markers []struct {
			Marker string `json:"marker"`
		} `json:"markers"`
	}
	json.Unmarshal(found[ControlMarkers].Evidence, &marks)
	if len(marks.Markers) != 1 || marks.Markers[0].Marker != "<|channel|>" {
		t.Errorf("control markers: %+v", marks)
	}
}

// TestEditsWithoutCheck: an edit counts as checked when run_commands
// follows it before the file is edited again or the run ends (D127).
func TestEditsWithoutCheck(t *testing.T) {
	ev := &Events{}
	call := func(tool, in string) {
		ev.Calls = append(ev.Calls, detect.Call{Tool: tool, Input: in})
		ev.ToolCalls = append(ev.ToolCalls, store.ToolCall{Tool: tool, OK: true})
	}
	call("editor", `{"path":"a"}`)
	call("editor", `{"path":"a"}`) // the first was never checked
	call("run_commands", `{}`)
	call("editor", `{"path":"b"}`) // nor is this one, before the end
	m := map[string]float64{}
	edits(func(n string, v float64) { m[n] = v }, ev)
	if m["edits"] != 3 || m["edits.without_check"] != 2 || m["edits.files"] != 2 {
		t.Errorf("%v", m)
	}
}

// TestErrorClass: the head of the tool's own sentence, digits as N, a
// nested JSON error unwrapped, nothing said is tool_error.
func TestErrorClass(t *testing.T) {
	for raw, want := range map[string]string{
		`{"error":"Editor operation failed: Read before editing: line 124 of x"}`: "Read before editing",
		`{"error":"timed out after 30s. try again"}`:                              "timed out after Ns",
		`{"error":"{\"error\":\"✖ Invalid input\\n  → at files\"}"}`:              "✖ Invalid input",
		`{"success":false}`: "tool_error",
	} {
		a := &agentEvent{Output: json.RawMessage(raw)}
		if got := ErrorClass(a); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
}

// TestRereadsAndCarried: a request after the first whose prompt nothing
// was served from cache is a re-read, by its cause; reasoning sent back in
// the history is counted per request.
func TestRereadsAndCarried(t *testing.T) {
	m := map[string]float64{}
	put := func(n string, v float64) { m[n] = v }
	agent := []Request{{PromptTokens: 100, CachedTokens: 0}, {PromptTokens: 200, CachedTokens: 100}, {PromptTokens: 50, CachedTokens: 0}, {PromptTokens: 60, CachedTokens: 0, LoadNS: 2e9}}
	rereads(put, agent, nil, nil)
	if m["context.rereads"] != 2 || m["context.rereads.reload"] != 1 || m["context.rereads.unknown"] != 1 {
		t.Errorf("%v", m)
	}
	m = map[string]float64{}
	rereads(put, []Request{{PromptTokens: 1}, {PromptTokens: 2}}, nil, nil)
	if len(m) != 0 {
		t.Errorf("a server that reports no cache says nothing: %v", m)
	}
	var q Request
	q.historyReasoning(json.RawMessage(`[{"role":"user","content":"x"},{"role":"assistant","thinking":"abc"},{"role":"assistant","reasoning_content":"de"},{"role":"assistant","content":"y"}]`))
	if q.HistoryReasoning != 2 || q.HistoryReasoningChars != 5 {
		t.Errorf("history reasoning: %+v", q)
	}
}
