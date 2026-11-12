package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

func agentEventLine(sec int, event string) string {
	return fmt.Sprintf(`{"t":"2026-01-02T03:04:%02dZ","k":"event","event":{"type":"agent_event","payload":{"event":%s}}}`, sec, event)
}

// swallowStream is a run with two editor calls whose intent swallowed
// new_text after a newline (the client's case), and one that passed it.
var swallowStream = strings.Join([]string{
	agentEventLine(1, `{"type":"iteration_start","iteration":1}`),
	agentEventLine(2, `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"a","input":{"file_path":"a.js","intent":"fix line 9 */\nnew_text:"},"output":[{"success":false,"error":"Missing required argument `+"`new_text`"+`. Send it and call again."}]}`),
	agentEventLine(3, `{"type":"iteration_start","iteration":2}`),
	agentEventLine(4, `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"b","input":{"file_path":"a.js","intent":"again */\nnew_text:"},"output":[{"success":false,"error":"Missing required argument `+"`new_text`"+`. Send it and call again."}]}`),
	agentEventLine(5, `{"type":"iteration_start","iteration":3}`),
	agentEventLine(6, `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"c","input":{"file_path":"a.js","start_line":9,"new_text":"x"},"output":[{"success":true}]}`),
}, "\n")

// readDetect reads the stream of /v1/detect.
func readDetect(t *testing.T, g *rig, method, query string, withToken bool) (int, *DetectHeader, []*DetectRow, *DetectEnd, string) {
	t.Helper()
	req, _ := http.NewRequest(method, g.srv.URL+"/v1/detect?"+query, nil)
	if withToken {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e apiError
		json.NewDecoder(resp.Body).Decode(&e)
		return resp.StatusCode, nil, nil, nil, e.Error
	}
	var h *DetectHeader
	var rows []*DetectRow
	var end *DetectEnd
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var l DetectLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("%v: %s", err, sc.Text())
		}
		switch {
		case l.Header != nil:
			h = l.Header
		case l.Run != nil:
			rows = append(rows, l.Run)
		case l.End != nil:
			end = l.End
		}
	}
	return 200, h, rows, end, ""
}

func TestDetectRuns(t *testing.T) {
	g := newRig(t, false)
	g.d.Blobs = memBlobs{"ev1": swallowStream, "ev2": agentEventLine(1, `{"type":"iteration_start","iteration":1}`)}
	putRun(t, g, "r1", "k-gate-a", "id-a", "4.100.266", "v1", "m1")
	putRun(t, g, "r2", "k-gate-b", "id-b", "4.100.266", "v1", "m2")
	putRun(t, g, "r3", "other", "id-a", "4.100.266", "v1", "m1")
	for _, id := range []string{"r1", "r2", "r3"} {
		h := "ev2"
		if id == "r1" {
			h = "ev1"
		}
		g.state.refs = append(g.state.refs, store.BlobRef{RunID: id, Kind: "events", Hash: blob.Hash(h)})
	}

	// Nothing named or narrowed, an unknown detector, a bad verdict.
	for _, q := range []string{"", "batch=x&detector=nope", "verdict=green"} {
		if code, _, _, _, msg := readDetect(t, g, "GET", q, false); code != 400 || msg == "" {
			t.Errorf("%q: %d %q", q, code, msg)
		}
	}

	// A glob of batches; every detector; nothing written.
	code, h, rows, end, _ := readDetect(t, g, "GET", "batch=k-gate-*", false)
	if code != 200 || h == nil || h.Runs != 2 || h.Library != detect.Version || len(h.Detectors) < 10 || end == nil || end.Runs != 2 {
		t.Fatalf("%d %+v %+v", code, h, end)
	}
	if len(rows) != 2 || rows[0].Run != "r1" || rows[1].Run != "r2" {
		t.Fatalf("rows %+v", rows)
	}
	r1 := rows[0]
	if r1.Firings[detect.SwallowedArgument] != 2 || r1.Stored[detect.SwallowedArgument] != 0 || r1.State != "finished" || r1.Arm != "a" || r1.Batch != "k-gate-a" {
		t.Errorf("r1 %+v", r1)
	}
	if r1.ByTool["tool_calls.swallowed_argument"]["editor"] != 2 {
		t.Errorf("by tool %+v", r1.ByTool)
	}
	if len(r1.Outcomes) != 1 || r1.Outcomes[0].Tool != "editor" || r1.Outcomes[0].Calls != 3 || r1.Outcomes[0].Failed != 2 || len(r1.Outcomes[0].Errors) != 1 {
		t.Errorf("outcomes %+v", r1.Outcomes)
	}
	if rows[1].Firings[detect.SwallowedArgument] != 0 || len(r1.Findings) != 0 || len(r1.Swallowed) != 0 {
		t.Errorf("r2 %+v, or firings without --firings", rows[1])
	}
	if fs, _ := g.state.Findings(context.Background(), "r1"); len(fs) != 0 {
		t.Errorf("a read wrote findings: %+v", fs)
	}
	// Every metric by default (070 §3.3), and the header says so.
	if h.Metrics != "all" || len(r1.Metrics) < 20 || r1.Metrics["tool_calls.swallowed_argument"] != 2 {
		t.Errorf("default metrics: %q, %d metrics", h.Metrics, len(r1.Metrics))
	}
	// none leaves them out, as null, and the header says so; it takes no
	// other metric.
	_, h, rows, _, _ = readDetect(t, g, "GET", "run=r1&metric=none", false)
	if h.Metrics != "none" || rows[0].Metrics != nil {
		t.Errorf("metric=none: %q %v", h.Metrics, rows[0].Metrics)
	}
	if code, _, _, _, msg := readDetect(t, g, "GET", "run=r1&metric=none&metric=time.*", false); code != 400 || msg == "" {
		t.Errorf("none with another metric: %d %q", code, msg)
	}
	// Thresholds (D148): every one in the header with its value and where it
	// came from; a command's override is read with and named; an unknown
	// one is refused.
	_, h, rows, _, _ = readDetect(t, g, "GET", "run=r1&threshold=reasoning_block.max_repeat%3D3", false)
	var mr detect.Effective
	for _, e := range h.Thresholds {
		if e.Name == detect.BlockMaxRepeat {
			mr = e
		}
	}
	if mr.Value != 3 || mr.From != "cli" || mr.Default != 20 || len(h.Thresholds) != len(detect.Thresholds()) {
		t.Errorf("thresholds in the header: %+v", h.Thresholds)
	}
	if _, ok := rows[0].Metrics["thinking.loop_block_threshold"]; ok && rows[0].Metrics["thinking.loop_block_threshold"] != 3 {
		t.Errorf("read at %v, asked 3", rows[0].Metrics["thinking.loop_block_threshold"])
	}
	if code, _, _, _, msg := readDetect(t, g, "GET", "run=r1&threshold=nope%3D1", false); code != 400 || !strings.Contains(msg, "no threshold") {
		t.Errorf("an unknown threshold: %d %q", code, msg)
	}
	g.d.Local.Daemon.Thresholds = map[string]float64{detect.BlockMaxRepeat: 7}
	_, h, _, _, _ = readDetect(t, g, "GET", "run=r1", false)
	for _, e := range h.Thresholds {
		if e.Name == detect.BlockMaxRepeat && (e.Value != 7 || e.From != "config") {
			t.Errorf("the installation's: %+v", e)
		}
	}
	g.d.Local.Daemon.Thresholds = nil
	// A pattern that matches nothing gives an empty block, not a missing one.
	_, h, rows, _, _ = readDetect(t, g, "GET", "run=r1&metric=no.such.metric", false)
	if h.Metrics != "matching" || len(h.MetricPatterns) != 1 || rows[0].Metrics == nil || len(rows[0].Metrics) != 0 {
		t.Errorf("a pattern that matches nothing: %q %v %v", h.Metrics, h.MetricPatterns, rows[0].Metrics)
	}

	// One detector, its firings, narrowed by model; runs named bare.
	_, h, rows, _, _ = readDetect(t, g, "GET", "run=r1&run=r3&detector=swallowed_argument&firings=1&model=m1&metric=tool_calls.swallowed_argument", false)
	if len(h.Detectors) != 1 || len(rows) != 2 || len(rows[0].Firings) != 1 {
		t.Fatalf("%+v %+v", h, rows)
	}
	if len(rows[0].Swallowed) != 2 || rows[0].Swallowed[0].In != "intent" || len(rows[0].Findings) != 1 || rows[0].Metrics["tool_calls.swallowed_argument"] != 2 {
		t.Errorf("firings %+v", rows[0])
	}

	// A group axis from the daemon.
	_, _, rows, _, _ = readDetect(t, g, "GET", "all=1&group=finding.swallowed_argument", false)
	if len(rows) != 3 || rows[0].Group != "with swallowed_argument" || rows[1].Group != "without swallowed_argument" {
		t.Errorf("group %+v", rows)
	}

	// A cohort's and a campaign's batches.
	g.d.mu.Lock()
	g.d.ctl.Cohorts["c1"] = &Cohort{Name: "c1", Batches: []string{"k-gate-b"}}
	g.d.ctl.Campaigns["camp"] = &CampaignState{Batches: []CampaignBatchState{{Batch: "other"}}}
	g.d.mu.Unlock()
	_, _, rows, _, _ = readDetect(t, g, "GET", "cohort=c*&campaign=camp", false)
	if len(rows) != 2 || rows[0].Run != "r2" || rows[0].Cohort != "c1" || rows[1].Campaign != "camp" {
		t.Errorf("cohort and campaign %+v", rows)
	}

	// A run in flight is read from what it printed so far.
	// Without a wire capture the argument names come from the run's own
	// calls, so the call that passed new_text is among the lines.
	var lines [][]byte
	for _, l := range strings.Split(swallowStream, "\n") {
		lines = append(lines, []byte(l))
	}
	f := &flight{runID: "r9", started: time.Now(), lines: lines}
	g.d.mu.Lock()
	g.d.flights = map[string]*flight{"lane-1": f}
	g.d.mu.Unlock()
	g.state.Enqueue(context.Background(), []store.QueueItem{{RunID: "r9", Batch: "k-gate-c", Arm: "stub", Task: "demo/answer", Lane: "lane-1"}})
	_, h, rows, _, _ = readDetect(t, g, "GET", "batch=k-gate-c", false)
	if h.InFlight != 1 || len(rows) != 1 || rows[0].State != "in flight" || rows[0].Partial == "" || rows[0].Firings[detect.SwallowedArgument] != 2 || rows[0].Error != "" {
		t.Errorf("in flight %+v %+v", h, rows)
	}
	if _, _, rows, _, _ = readDetect(t, g, "GET", "batch=k-gate-c&finished=1", false); len(rows) != 0 {
		t.Errorf("--finished %+v", rows)
	}

	// Recording needs the token, and writes on finished runs.
	if code, _, _, _, _ := readDetect(t, g, "POST", "run=r1", false); code == 200 {
		t.Errorf("a record without the token: %d", code)
	}
	_, _, rows, _, _ = readDetect(t, g, "POST", "run=r1", true)
	if len(rows) != 1 || !rows[0].Recorded {
		t.Fatalf("record %+v", rows)
	}
	fs, _ := g.state.Findings(context.Background(), "r1")
	found := false
	for _, f := range fs {
		found = found || f.Detector == detect.SwallowedArgument && f.Count == 2
	}
	if !found {
		t.Errorf("recorded findings %+v", fs)
	}
	_, _, rows, _, _ = readDetect(t, g, "GET", "run=r1", false)
	if rows[0].Stored[detect.SwallowedArgument] != 2 {
		t.Errorf("stored after the record %+v", rows[0].Stored)
	}
}
