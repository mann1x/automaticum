package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// TestStruggleSignals proves the struggle signals of D126 and the change
// protocol's metrics (070 §2, §4) from an event stream: the SDK's nudge, a
// second compaction, and transactions kept, put back and carried.
func TestStruggleSignals(t *testing.T) {
	notice := func(t, md string) string {
		return agentLine(t, `{"type":"notice","noticeType":"status","metadata":`+md+`}`)
	}
	stream := strings.Join([]string{
		agentLine("01", `{"type":"iteration_start","iteration":1}`),
		notice("02", `{"kind":"atomic_plan","transaction":1,"from":"reasoning"}`),
		notice("03", `{"kind":"atomic_transaction","transaction":1,"kept":false,"source":"check","elapsedMs":4000,"output":"FAIL","filesPutBack":2}`),
		agentLine("04", `{"type":"iteration_start","iteration":2}`),
		notice("05", `{"kind":"auto_compaction","phase":"completed","cause":"pressure","tokensBefore":90000,"tokensAfter":20000,"durationMs":4000}`),
		notice("06", `{"kind":"atomic_empty_attempt","transaction":2,"continued":true}`),
		agentLine("07", `{"type":"iteration_start","iteration":3}`),
		notice("08", `{"kind":"struggle","phase":"nudge","reason":"transactions","remaining":1}`),
		notice("09", `{"kind":"auto_compaction","phase":"completed","cause":"pressure","tokensBefore":90000,"tokensAfter":20000,"durationMs":3000}`),
		notice("10", `{"kind":"atomic_transaction","transaction":3,"kept":true,"source":"check","elapsedMs":2000,"output":"ok"}`),
		notice("11", `{"kind":"atomic_transaction","transaction":4,"kept":false,"carried":true,"source":"end"}`),
		agentLine("12", `{"type":"iteration_end","iteration":3}`),
		agentLine("13", `{"type":"done","reason":"completed"}`),
	}, "\n")
	ev, err := ReadEvents(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	res := Analyse(&store.Run{ID: "r", Harness: "4.100.266"}, ev, nil)
	m := metricMap(res.Metrics)
	for name, want := range map[string]float64{
		"analysis.version":                AnalysisVersion,
		"struggle.sdk_nudge":              1,
		"struggle.sdk_nudge.turn":         3,
		"struggle.second_compaction":      1,
		"struggle.second_compaction.turn": 3,
		// One put back and one empty attempt: the replay nudges on the
		// second attempt thrown away, at the next point the SDK inspects
		// (the end of turn 3; turn 2 ends no block, call or iteration).
		"struggle.replay_nudge":              1,
		"struggle.replay_nudge.turn":         3,
		"struggle.replay.nudge.transactions": 1,
		"struggle.signals":                   3,
		"transactions.settled":               3,
		"transactions.kept":                  1,
		"transactions.discarded":             1,
		"transactions.carried":               1,
		"transactions.checked":               2,
		"transactions.files_put_back":        2,
		"transactions.empty_attempts":        1,
		"transactions.plans":                 1,
	} {
		if m[name] != want {
			t.Errorf("%s = %v, want %v", name, m[name], want)
		}
	}
	for _, absent := range []string{"struggle.sdk_offer", "struggle.loop_hard", "struggle.replay_suggest"} {
		if _, ok := m[absent]; ok {
			t.Errorf("%s set", absent)
		}
	}
	var f *store.Finding
	for i := range res.Findings {
		if res.Findings[i].Detector == StruggleSignals {
			f = &res.Findings[i]
		}
	}
	if f == nil || f.Count != 3 || f.Turn != 3 || f.Severity != "note" {
		t.Fatalf("finding %+v", f)
	}
	var e struct {
		Signals []struct {
			Trigger string `json:"trigger"`
			Turn    int    `json:"turn"`
		} `json:"signals"`
	}
	if json.Unmarshal(f.Evidence, &e) != nil || len(e.Signals) != 3 || e.Signals[0].Trigger != "sdk_nudge" || e.Signals[2].Trigger != "replay_nudge" {
		t.Errorf("evidence %s", f.Evidence)
	}
	// A quiet run evaluates every trigger and holds none.
	quiet, _ := ReadEvents(strings.NewReader(agentLine("01", `{"type":"iteration_start","iteration":1}`)))
	qm := metricMap(Analyse(&store.Run{ID: "q"}, quiet, nil).Metrics)
	if v, ok := qm["struggle.signals"]; !ok || v != 0 {
		t.Errorf("quiet signals %v %v", v, ok)
	}
}
