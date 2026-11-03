package ingest

import (
	"strings"
	"testing"
	"time"
)

// TestWireAnswers is a client's run: a turn the output limit cut, then
// the SDK's retry, which only the wire has (bug-077).
func TestWireAnswers(t *testing.T) {
	at := func(sec int) string { return time.Date(2026, 11, 3, 1, 47, sec, 0, time.UTC).Format(time.RFC3339) }
	ag := func(sec int, event string) string {
		return `{"t":"` + at(sec) + `","k":"event","event":{"type":"agent_event","payload":{"event":` + event + `}}}`
	}
	stream := strings.Join([]string{
		ag(0, `{"type":"iteration_start","iteration":7}`),
		ag(1, `{"type":"content_start","contentType":"text","text":"Now let me fix "}`),
		ag(9, `{"type":"content_start","contentType":"text","text":"it properly:"}`),
		ag(10, `{"type":"usage","outputTokens":96000}`),
		// The retry ran from 10 to 40 and printed nothing.
		ag(41, `{"type":"iteration_start","iteration":8}`),
		ag(42, `{"type":"content_start","contentType":"text","text":"The file"}`),
		ag(43, `{"type":"content_end","contentType":"text","text":"The file parses."}`),
	}, "\n")
	start := func(sec int) time.Time { return time.Date(2026, 11, 3, 1, 47, sec, 0, time.UTC) }
	reqs := []Request{
		{Seq: 16, Agent: true, Start: start(0), MS: 10000, Visible: "Now let me fix it properly:"},
		{Seq: 17, Agent: true, Start: start(10), MS: 30000, Visible: "Now let me fix it again:", Reasoning: "loop loop"},
		{Seq: 18, Agent: false, Start: start(20), MS: 1000, Visible: "a probe"},
		{Seq: 21, Agent: true, Start: start(41), MS: 2000, Visible: "The file parses."},
	}
	ev, err := ReadEvents(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if n := ev.WireAnswers(reqs); n != 2 {
		t.Fatalf("added %d, want the retry's reasoning and text: %+v", n, ev.Blocks)
	}
	if n := ev.WireAnswers(reqs); n != 0 {
		t.Errorf("a second call added %d", n)
	}
	var wire []Block
	for _, b := range ev.Blocks {
		if b.Request != 0 {
			wire = append(wire, b)
		}
	}
	if len(wire) != 2 || wire[0].Kind != "reasoning" || wire[0].Text != "loop loop" || wire[1].Text != "Now let me fix it again:" ||
		wire[1].Turn != 7 || wire[1].Request != 17 {
		t.Errorf("wire blocks %+v", wire)
	}

	// A stream without times adds nothing.
	bare, _ := ReadEvents(strings.NewReader(`{"k":"event","event":{"type":"agent_event","payload":{"event":{"type":"content_end","contentType":"text","text":"x"}}}}`))
	if n := bare.WireAnswers(reqs); n != 0 {
		t.Errorf("a stream without times added %d", n)
	}
}
