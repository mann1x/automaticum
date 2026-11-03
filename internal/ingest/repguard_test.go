package ingest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// TestRepetitionGuard is a client's run: a reply that says one paragraph
// again and again until the output limit cut it. The guard records where
// it would have tripped and how far the request had gone (D135).
func TestRepetitionGuard(t *testing.T) {
	at := func(sec int) string { return time.Date(2026, 11, 3, 1, 47, sec, 0, time.UTC).Format(time.RFC3339) }
	ag := func(sec int, event string) string {
		return `{"t":"` + at(sec) + `","k":"event","event":{"type":"agent_event","payload":{"event":` + event + `}}}`
	}
	para := "Now let me fix each syntax error in the file properly, one at a time:\n\n"
	lines := []string{ag(0, `{"type":"iteration_start","iteration":7}`)}
	for i := range 200 {
		b, _ := json.Marshal(para)
		lines = append(lines, ag(1+i/10, `{"type":"content_start","contentType":"text","text":`+string(b)+`}`))
	}
	lines = append(lines, ag(30, `{"type":"usage","outputTokens":96000}`))
	ev, err := ReadEvents(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat(para, 200)
	reqs := []Request{{Seq: 16, Agent: true, Start: time.Date(2026, 11, 3, 1, 47, 0, 0, time.UTC), MS: 30000, Visible: text, EvalTokens: 96000}}
	var res Result
	var metrics []store.Metric
	m := func(n string, v float64) { metrics = append(metrics, store.Metric{Name: n, Value: v}) }
	repetitionGuard("r", ev, reqs, detect.Options{}, m, &res)
	if len(res.Findings) != 1 || res.Findings[0].Detector != detect.RepetitionGuard || res.Findings[0].Severity != "info" {
		t.Fatalf("findings %+v", res.Findings)
	}
	var e struct {
		Mode  string
		First GuardFire
	}
	json.Unmarshal(res.Findings[0].Evidence, &e)
	f := e.First
	if e.Mode != "observe" || f.Channel != "text" || f.Rule != detect.RuleParagraph || f.Turn != 7 || f.Request != 16 || !f.Unfinished ||
		f.Offset <= 0 || f.Offset >= f.Chars || f.Tokens <= 0 || f.Tokens >= 96000 || f.Seconds <= 0 {
		t.Errorf("first %+v (mode %q)", f, e.Mode)
	}
	found := false
	for _, x := range metrics {
		found = found || x.Name == "repetition_guard.would_fire" && x.Value == 1
	}
	if !found {
		t.Errorf("metrics %+v", metrics)
	}

	// A healthy run: the metric, no finding.
	res, metrics = Result{}, nil
	healthy, _ := ReadEvents(strings.NewReader(ag(0, `{"type":"iteration_start","iteration":1}`) + "\n" +
		ag(1, `{"type":"content_end","contentType":"text","text":"Done: the file parses and the tests pass."}`)))
	repetitionGuard("r", healthy, nil, detect.Options{}, m, &res)
	if len(res.Findings) != 0 || len(metrics) != 1 || metrics[0].Value != 0 {
		t.Errorf("healthy %+v %+v", res.Findings, metrics)
	}
}
