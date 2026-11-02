package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
)

// putRun writes a finished run of arm "a" on demo/answer straight into the
// state, with its harness build and tool calls.
func putRun(t *testing.T, g *rig, id, batch, armID, sdk, automaticum, model string, calls ...store.ToolCall) {
	t.Helper()
	ctx := context.Background()
	arm, _ := json.Marshal(map[string]any{"name": "a"})
	prov, _ := json.Marshal(map[string]any{"automaticum": automaticum, "runner_protocol": 3,
		"sdk": map[string]string{"release": sdk, "package_sha256": "abc"}, "image": map[string]string{"id": "sha256:img"}})
	r := &store.Run{ID: id, Batch: batch, Suite: "demo", TaskID: "answer", ArmID: armID, Arm: arm, Model: model, Harness: sdk,
		Verdict: store.Pass, StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC(), Provenance: prov}
	if err := g.state.UpsertRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	g.state.ReplaceToolCalls(ctx, id, calls)
}

func TestReportRegressionAndCensus(t *testing.T) {
	g := newRig(t, false)
	ok := store.ToolCall{Tool: "read_file", OK: true}
	bad := store.ToolCall{Tool: "run", ErrorClass: "tool_error"}
	// The same arm on two SDK releases (two arm ids), and a third build
	// that only changed automaticum (the same arm id as the second).
	putRun(t, g, "r1", "b1", "id-old", "4.100.266", "v1", "m", ok, bad)
	putRun(t, g, "r2", "b1", "id-old", "4.100.266", "v1", "m", ok)
	putRun(t, g, "r3", "b2", "id-new", "4.100.269", "v1", "m", bad, bad)
	putRun(t, g, "r4", "b2", "id-new", "4.100.269", "v2", "m2")

	var plain CompareView
	g.call("GET", "/v1/compare?batch=b1&batch=b2", nil, &plain)
	if len(plain.Groups) != 2 {
		t.Fatalf("compare by arm id: %d groups", len(plain.Groups))
	}
	var reg CompareView
	if code := g.call("GET", "/v1/compare?batch=b1&batch=b2&split=build", nil, &reg); code != 200 || reg.Split != "build" || len(reg.Groups) != 3 {
		t.Fatalf("regression: %d %+v", code, reg)
	}
	for _, gr := range reg.Groups {
		if gr.Build == "" || gr.Arm != "a" {
			t.Errorf("group without a build or arm: %+v", gr)
		}
	}
	if len(reg.Pairs) != 3 {
		t.Errorf("pairs of one arm over three builds: %d", len(reg.Pairs))
	}
	if code := g.call("GET", "/v1/compare?batch=b1&split=arm", nil, nil); code != 400 {
		t.Errorf("unknown split: %d", code)
	}

	var c Census
	if code := g.call("GET", "/v1/report/census?batch=b1&batch=b2", nil, &c); code != 200 || c.Runs != 4 || c.NoCalls != 1 || len(c.Rows) != 2 {
		t.Fatalf("census: %d %+v", code, c)
	}
	if r := c.Rows[0]; r.Tool != "run" || r.Model != "m" || r.Calls != 3 || r.Runs != 2 || r.Failed != 3 || r.Errors["tool_error"] != 3 || r.Parse["not recorded"] != 3 {
		t.Errorf("first row: %+v", r)
	}
	c = Census{}
	if g.call("GET", "/v1/report/census?where=model=m2", nil, &c); c.Runs != 1 || len(c.Rows) != 0 {
		t.Errorf("census of m2: %+v", c)
	}
	if code := g.call("GET", "/v1/report/census?where=nope=1", nil, nil); code != 400 {
		t.Errorf("bad where: %d", code)
	}
}

// Two lanes on one endpoint of capacity 1 take turns; with capacity 2
// they run together (050 §1).
func TestEndpointCapacity(t *testing.T) {
	g := newRig(t, false)
	g.d.Local.Lanes["lane-2"] = &config.Lane{Name: "lane-2", Host: "linux-gpu-1", Endpoints: []string{"stub"}}
	g.d.Local.Endpoints["stub"].Capacity = 1
	g.runner.hold, g.runner.started = make(chan struct{}), make(chan string, 8)
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var a, b StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Lane: "lane-1"}, &a)
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Lane: "lane-2"}, &b)
	<-g.runner.started
	time.Sleep(200 * time.Millisecond)
	st := g.status()
	waiting := ""
	for _, q := range st.Queue {
		if !q.Running {
			waiting = q.Stopped
		}
	}
	if waiting != "endpoint stub serves its capacity of 1" {
		t.Fatalf("the second run: %q in %+v", waiting, st.Queue)
	}
	g.runner.hold <- struct{}{}
	<-g.runner.started
	close(g.runner.hold)
	g.waitFinished(a.RunIDs[0])
	g.waitFinished(b.RunIDs[0])
}

// The heartbeat counts lanes, runs and the runs ended since the one before
// (050 §6), on GET /v1/heartbeat and in the status; the loop keeps the last.
func TestHeartbeat(t *testing.T) {
	g := newRig(t, false)
	if code := g.call("GET", "/v1/heartbeat", nil, nil); code != 404 {
		t.Errorf("before the first heartbeat: %d", code)
	}
	g.d.HeartbeatEvery = time.Hour
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &resp)
	g.waitFinished(resp.RunIDs[0])
	// No heartbeat yet: the window starts at the daemon's start.
	var h Heartbeat
	if code := g.call("GET", "/v1/heartbeat?now=1", nil, &h); code != 200 || h.Ended["PASS"] != 1 || h.Running+h.Queued != 0 || len(h.Lanes) == 0 {
		t.Fatalf("now: %d %+v", code, h)
	}
	if st := g.status(); st.Heartbeat != nil {
		t.Error("a heartbeat before its time")
	}

	g2 := newRig(t, false)
	g2.d.HeartbeatEvery = 20 * time.Millisecond
	go func() { g2.d.Serve(g2.ctx()); close(g2.served) }()
	deadline := time.Now().Add(5 * time.Second)
	for g2.status().Heartbeat == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if g2.status().Heartbeat == nil || g2.call("GET", "/v1/heartbeat", nil, &h) != 200 || h.At.IsZero() {
		t.Fatalf("the loop kept no heartbeat: %+v", h)
	}
}

// A lane whose runs end for infrastructure reasons N times in a row is
// quarantined by the daemon, with a lane notification; another end resets
// the count; `lane resume` lifts it (D124).
func TestAutoQuarantine(t *testing.T) {
	g := newRig(t, false)
	three := 3
	g.d.Local.Daemon.QuarantineAfter = &three
	g.runner.end = &[2]string{"ABORTED", "the container engine failed: EOF"}
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp)
	g.waitFinished(resp.RunIDs[1])
	// A pass in between resets the streak.
	g.runner.mu.Lock()
	g.runner.end = nil
	g.runner.mu.Unlock()
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &resp)
	g.waitFinished(resp.RunIDs[0])
	g.runner.mu.Lock()
	g.runner.end = &[2]string{"ABORTED", "the container engine failed: EOF"}
	g.runner.mu.Unlock()
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 4}, &resp)
	g.waitFinished(resp.RunIDs[2])
	deadline := time.Now().Add(5 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		if st = g.status(); st.Lanes[0].Quarantined != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(st.Lanes[0].Quarantined, "3 runs in a row") {
		t.Fatalf("not quarantined: %+v", st.Lanes[0])
	}
	time.Sleep(100 * time.Millisecond)
	if st := g.status(); len(st.Queue) != 1 || !strings.HasPrefix(st.Queue[0].Stopped, "lane quarantined") {
		t.Fatalf("the fourth run started: %+v", st.Queue)
	}
	var lc LaneControl
	g.call("POST", "/v1/lanes/lane-1/resume", nil, &lc)
	g.waitFinished(resp.RunIDs[3])
}

// A lane's busy command holds its runs while it exits 0, with its output as
// the reason, notified once (D123).
func TestBusyCommand(t *testing.T) {
	g := newRig(t, false)
	flag := filepath.Join(t.TempDir(), "busy")
	os.WriteFile(flag, []byte("someone's training job"), 0o644)
	g.d.Local.Lanes["lane-1"].BusyCommand = []string{"sh", "-c", "test -f " + flag + " && cat " + flag}
	g.d.GateRetry = 50 * time.Millisecond
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &resp)
	deadline := time.Now().Add(5 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		if st = g.status(); st.Lanes[0].Waiting != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Lanes[0].Waiting != "busy: someone's training job" {
		t.Fatalf("waiting: %+v", st.Lanes[0])
	}
	os.Remove(flag)
	g.waitFinished(resp.RunIDs[0])

	g.d.Local.Lanes["lane-1"].BusyCommand = []string{"/no/such/command"}
	if busy, why := g.d.laneBusy(context.Background(), "lane-1"); !busy || !strings.Contains(why, "cannot be run") {
		t.Errorf("a command that cannot run: %v %q", busy, why)
	}
}

// A run goes to the first lane, by name, that has every label its arm and
// tasks require; an asked lane without them is refused (D122).
func TestLaneLabels(t *testing.T) {
	g := newRig(t, false)
	g.d.Local.Lanes["lane-2"] = &config.Lane{Name: "lane-2", Host: "linux-gpu-1", Endpoints: []string{"stub"}, Labels: map[string]string{"gpu": "a", "os": "linux"}}
	arm := &config.Arm{Endpoint: "stub", Requires: []string{"gpu=a"}}
	if l, err := g.d.laneFor(arm, "", []string{"gpu=a", "os=linux"}); err != nil || l != "lane-2" {
		t.Errorf("labelled: %q %v", l, err)
	}
	if l, err := g.d.laneFor(arm, "", nil); err != nil || l != "lane-1" {
		t.Errorf("no labels: %q %v", l, err)
	}
	if _, err := g.d.laneFor(arm, "lane-1", []string{"gpu=a"}); err == nil || !strings.Contains(err.Error(), "lacks the labels") {
		t.Errorf("asked lane without the label: %v", err)
	}
	if _, err := g.d.laneFor(arm, "", []string{"gpu=b"}); err == nil || !strings.Contains(err.Error(), "gpu=b") {
		t.Errorf("no lane has it: %v", err)
	}
}

// A batch of higher priority starts first; a reserved lane starts only the
// work it is reserved for until the reservation ends (D125).
func TestPriorityAndReservation(t *testing.T) {
	g := newRig(t, false)
	g.runner.started = make(chan string, 16)
	var low, high, other StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2, Key: "low"}, &low)
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 1, Key: "high", Priority: 5}, &high)
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	if first := <-g.runner.started; first != high.RunIDs[0] {
		t.Fatalf("first started %s, want the high-priority %s", first, high.RunIDs[0])
	}
	for _, id := range low.RunIDs {
		g.waitFinished(id)
	}

	until := time.Now().Add(time.Hour).Format(time.RFC3339)
	if code := g.call("POST", "/v1/lanes/lane-1/reserve", ReserveRequest{For: "k-mine", Until: until}, nil); code != 200 {
		t.Fatalf("reserve: %d", code)
	}
	if st := g.status(); st.Lanes[0].Reserved == nil || st.Lanes[0].Reserved.For != "k-mine" {
		t.Fatalf("status: %+v", st.Lanes[0])
	}
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Key: "other"}, &other)
	var mine StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Key: "mine"}, &mine)
	g.waitFinished(mine.RunIDs[0])
	if st := g.status(); len(st.Queue) != 1 || !strings.HasPrefix(st.Queue[0].Stopped, "lane reserved for k-mine") {
		t.Fatalf("the other batch: %+v", st.Queue)
	}
	if code := g.call("POST", "/v1/lanes/lane-1/reserve", ReserveRequest{For: "x", Until: "2020-01-01T00:00:00Z"}, nil); code != 400 {
		t.Errorf("a past reservation: %d", code)
	}
	g.call("POST", "/v1/lanes/lane-1/unreserve", nil, nil)
	g.waitFinished(other.RunIDs[0])
}

// The struggle report (D126) measures each signal against "did not pass":
// recall over the runs that did not pass, false alarms over those that
// passed; runs left out and runs not yet analysed are counted apart.
func TestReportStruggle(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	put := func(id string, v store.Verdict, version float64, metrics map[string]float64) {
		putRun(t, g, id, "b", "id", "4.100.266", "v1", "m")
		rec, _ := g.state.GetRun(ctx, id)
		rec.Verdict = v
		g.state.UpsertRun(ctx, rec)
		ms := []store.Metric{{RunID: id, Name: "analysis.version", Value: version}}
		for k, x := range metrics {
			ms = append(ms, store.Metric{RunID: id, Name: k, Value: x})
		}
		g.state.ReplaceMetrics(ctx, id, ms)
	}
	put("f1", store.Fail, 10, map[string]float64{"struggle.second_compaction": 1, "struggle.second_compaction.turn": 40, "struggle.replay_nudge": 1, "struggle.replay_nudge.turn": 25})
	put("f2", store.Timeout, 10, map[string]float64{"struggle.second_compaction": 1, "struggle.second_compaction.turn": 60})
	put("f3", store.Fail, 10, nil)
	put("p1", store.Pass, 10, map[string]float64{"struggle.replay_nudge": 1, "struggle.replay_nudge.turn": 30})
	put("p2", store.Pass, 10, nil)
	put("old", store.Fail, 9, nil)
	put("ab", store.Aborted, 10, nil)

	var rep StruggleReport
	if code := g.call("GET", "/v1/report/struggle?batch=b", nil, &rep); code != 200 {
		t.Fatalf("code %d", code)
	}
	if rep.Passed != 2 || rep.NotPassed != 3 || rep.LeftOut != 1 || rep.NotAnalysed != 1 {
		t.Fatalf("counts %+v", rep)
	}
	rows := map[string]StruggleRow{}
	for _, r := range rep.Rows {
		rows[r.Trigger] = r
	}
	if len(rep.Rows) != 9 || rep.Rows[8].Trigger != "any" {
		t.Errorf("rows %+v", rep.Rows)
	}
	sc := rows["second_compaction"]
	if sc.HeldNotPassed != 2 || sc.HeldPassed != 0 || sc.Recall == nil || *sc.Recall != 2.0/3 || sc.FalseAlarmRate == nil || *sc.FalseAlarmRate != 0 ||
		sc.Precision == nil || *sc.Precision != 1 || sc.FirstTurn == nil || sc.FirstTurn.Median != 50 {
		t.Errorf("second compaction %+v", sc)
	}
	rn := rows["replay_nudge"]
	if rn.HeldNotPassed != 1 || rn.HeldPassed != 1 || *rn.FalseAlarmRate != 0.5 || *rn.Precision != 0.5 {
		t.Errorf("replay nudge %+v", rn)
	}
	// Any signal: f1 (first at 25), f2, p1.
	if a := rows["any"]; a.HeldNotPassed != 2 || a.HeldPassed != 1 || a.FirstTurn.Min != 25 {
		t.Errorf("any %+v", a)
	}
	if r := rows["sdk_offer"]; r.HeldNotPassed != 0 || r.Precision != nil || r.Recall == nil || *r.Recall != 0 {
		t.Errorf("never held %+v", r)
	}
}

// The guard review queue gathers the guard_replay findings of the runs
// selected: unconfirmed trips first, missed turns after, one row a turn.
func TestReportGuards(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	putRun(t, g, "r1", "b", "id", "4.100.266", "v1", "m")
	putRun(t, g, "r2", "b", "id", "4.100.266", "v1", "m")
	ev := func(s string) json.RawMessage { return json.RawMessage(s) }
	g.state.ReplaceFindings(ctx, "r1", []store.Finding{{RunID: "r1", Detector: "guard_replay", Severity: "warning", Count: 3,
		Evidence: ev(`{"guards":[{"guard":"reasoning_loop","phase":"cut","missed":[40],"unconfirmed":[12,30]}]}`)}})
	g.state.ReplaceFindings(ctx, "r2", []store.Finding{{RunID: "r2", Detector: "guard_replay", Severity: "note", Count: 1,
		Evidence: ev(`{"guards":[{"guard":"loop_detection","missed":[7],"unconfirmed":[]}]}`)}})
	var q GuardReview
	if code := g.call("GET", "/v1/report/guards?batch=b", nil, &q); code != 200 || q.Runs != 2 || len(q.Rows) != 4 {
		t.Fatalf("queue %d %+v", code, q)
	}
	if r := q.Rows[0]; r.Kind != "unconfirmed" || r.Run != "r1" || r.Turn != 12 || r.Guard != "reasoning_loop" || r.Phase != "cut" {
		t.Errorf("first %+v", r)
	}
	if r := q.Rows[3]; r.Kind != "missed" || r.Run != "r2" || r.Turn != 7 {
		t.Errorf("last %+v", r)
	}
	q = GuardReview{}
	if g.call("GET", "/v1/report/guards?batch=b&kind=missed", nil, &q); len(q.Rows) != 2 {
		t.Errorf("missed only %+v", q.Rows)
	}
	if code := g.call("GET", "/v1/report/guards?kind=other", nil, nil); code != 400 {
		t.Errorf("bad kind %d", code)
	}
}

// Lane skew and group-by (070 §6): the same arm on two hosts is two
// groups, paired; a group-by axis groups the runs by its value on each
// task, whatever their arm.
func TestCompareSplits(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	host := func(id, h string) {
		rec, _ := g.state.GetRun(ctx, id)
		var p map[string]any
		json.Unmarshal(rec.Provenance, &p)
		p["inference"] = map[string]any{"lane_host": h}
		rec.Provenance, _ = json.Marshal(p)
		g.state.UpsertRun(ctx, rec)
	}
	for _, id := range []string{"r1", "r2", "r3", "r4"} {
		putRun(t, g, id, "b", "id-1", "4.100.266", "v1", "m")
	}
	putRun(t, g, "r5", "b", "id-2", "4.100.266", "v1", "m")
	host("r1", "linux-gpu-1")
	host("r2", "linux-gpu-1")
	host("r3", "blackswan-2")
	host("r4", "blackswan-2")
	host("r5", "blackswan-2")
	g.state.ReplaceFindings(ctx, "r2", []store.Finding{{RunID: "r2", Detector: "reasoning_loop", Severity: "warning", Count: 1}})
	g.state.ReplaceFindings(ctx, "r5", []store.Finding{{RunID: "r5", Detector: "reasoning_loop", Severity: "warning", Count: 1}})

	var v CompareView
	if code := g.call("GET", "/v1/compare?batch=b&split=lane", nil, &v); code != 200 || len(v.Groups) != 3 {
		t.Fatalf("lane: %d %+v", code, v.Groups)
	}
	// Only the two hosts of arm id-1 are paired; id-2 has one host.
	if len(v.Pairs) != 1 || v.Groups[v.Pairs[0].A].ArmID != "id-1" || v.Groups[v.Pairs[0].A].Value == v.Groups[v.Pairs[0].B].Value {
		t.Errorf("lane pairs %+v", v.Pairs)
	}
	v = CompareView{}
	if g.call("GET", "/v1/compare?batch=b&split=finding.reasoning_loop", nil, &v); len(v.Groups) != 2 || len(v.Pairs) != 1 {
		t.Fatalf("finding: %+v", v.Groups)
	}
	for _, gr := range v.Groups {
		want := map[string]int{"with reasoning_loop": 2, "without reasoning_loop": 3}[gr.Value]
		if len(gr.Runs) != want {
			t.Errorf("group %q has %d runs, want %d", gr.Value, len(gr.Runs), want)
		}
	}
	v = CompareView{}
	if g.call("GET", "/v1/compare?batch=b&split=arm.name", nil, &v); len(v.Groups) != 1 || v.Groups[0].Value != "a" || len(v.Groups[0].Runs) != 5 {
		t.Errorf("arm.name: %+v", v.Groups)
	}
	for _, bad := range []string{"arm.", "colour", "label"} {
		if code := g.call("GET", "/v1/compare?batch=b&split="+bad, nil, nil); code != 400 {
			t.Errorf("split %q: %d", bad, code)
		}
	}
	// The values of an axis, read from the run.
	run := &store.Run{Arm: json.RawMessage(`{"name":"a","thinking":{"level":"high","budget":0},"sampler":{"seed":null}}`)}
	for split, want := range map[string]string{"arm.thinking.level": "high", "arm.thinking.budget": "0", "arm.sampler.seed": "not stated",
		"arm.model.model": "not stated", "arm.thinking": `{"budget":0,"level":"high"}`} {
		if got := groupValue(split, run, nil, nil); got != want {
			t.Errorf("%s = %q, want %q", split, got, want)
		}
	}
	if got := groupValue("label.difficulty", run, map[string]string{"difficulty": "hard"}, nil); got != "hard" {
		t.Errorf("label %q", got)
	}
	if got := groupValue("label.category", run, map[string]string{"difficulty": "hard"}, nil); got != "not labelled" {
		t.Errorf("no label %q", got)
	}
}
