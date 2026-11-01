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
