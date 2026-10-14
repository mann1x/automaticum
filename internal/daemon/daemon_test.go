package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/gate"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/task"
)

// memState is a store.State in memory.
type memState struct {
	store.State
	mu    sync.Mutex
	runs  map[string]store.Run
	queue []store.QueueItem
	lanes map[string]store.LaneState
	refs  []store.BlobRef
	// What ingest derived, by run.
	metrics  map[string][]store.Metric
	calls    map[string][]store.ToolCall
	findings map[string][]store.Finding
}

func (s *memState) Metrics(_ context.Context, id string) ([]store.Metric, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metrics[id], nil
}

func (s *memState) Findings(_ context.Context, id string) ([]store.Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findings[id], nil
}

func (s *memState) ReplaceMetrics(_ context.Context, id string, v []store.Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics[id] = v
	return nil
}

func (s *memState) ReplaceToolCalls(_ context.Context, id string, v []store.ToolCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[id] = v
	return nil
}

func (s *memState) ReplaceFindings(_ context.Context, id string, v []store.Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findings[id] = v
	return nil
}

func newMemState() *memState {
	return &memState{runs: map[string]store.Run{}, lanes: map[string]store.LaneState{},
		metrics: map[string][]store.Metric{}, calls: map[string][]store.ToolCall{}, findings: map[string][]store.Finding{}}
}

func (s *memState) UpsertRun(_ context.Context, r *store.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !r.Verdict.Valid() {
		return fmt.Errorf("not a verdict: %q", r.Verdict)
	}
	s.runs[r.ID] = *r
	return nil
}

func (s *memState) GetRun(_ context.Context, id string) (*store.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &r, nil
}

func (s *memState) ListRuns(_ context.Context, f store.RunFilter) ([]store.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Run
	for _, r := range s.runs {
		if (f.Batch == "" || r.Batch == f.Batch) && (f.Verdict == "" || r.Verdict == f.Verdict) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (s *memState) BlobRefs(_ context.Context, id string) ([]store.BlobRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.BlobRef
	for _, r := range s.refs {
		if r.RunID == id {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *memState) SetLaneState(_ context.Context, l store.LaneState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l.UpdatedAt = time.Now()
	s.lanes[l.Lane] = l
	return nil
}

func (s *memState) LaneStates(context.Context) ([]store.LaneState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.LaneState
	for _, l := range s.lanes {
		out = append(out, l)
	}
	return out, nil
}

func (s *memState) Enqueue(_ context.Context, items []store.QueueItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
next:
	for _, it := range items {
		for _, q := range s.queue {
			if q.RunID == it.RunID {
				continue next
			}
		}
		s.queue = append(s.queue, it)
	}
	return nil
}

func (s *memState) Queue(context.Context) ([]store.QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.QueueItem(nil), s.queue...), nil
}

func (s *memState) Unqueue(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.queue {
		if q.RunID == id {
			s.queue = append(s.queue[:i:i], s.queue[i+1:]...)
			break
		}
	}
	return nil
}

func (s *memState) lane(name string) store.LaneState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lanes[name]
}

// fakeRunner stands in for run.Executor.
type fakeRunner struct {
	state *memState
	mu    sync.Mutex
	seen  []string
	// hold, when set, makes a run wait until it is closed or the run is
	// cancelled.
	hold    chan struct{}
	started chan string
}

func (f *fakeRunner) Execute(ctx context.Context, req run.Request) (*store.Run, error) {
	f.mu.Lock()
	f.seen = append(f.seen, req.ID)
	hold := f.hold
	f.mu.Unlock()
	r := &store.Run{ID: req.ID, Batch: req.Batch, Suite: req.Task.Suite, TaskID: req.Task.ID, Lane: req.Lane, ArmID: req.Arm.ID,
		StartedAt: time.Now().UTC(), Harness: req.Arm.Harness.SDKRelease, Verdict: store.Pass, VerdictReason: "oracle passed"}
	if req.OnStart != nil {
		req.OnStart(r, "container-"+req.ID)
	}
	if req.OnLine != nil {
		req.OnLine([]byte(`{"k":"event","n":1}`))
		req.OnLine([]byte(`{"k":"event","n":2}`))
	}
	if f.started != nil {
		f.started <- req.ID
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			r.Verdict, r.VerdictReason = store.Aborted, context.Cause(ctx).Error()
		}
	}
	if req.OnLine != nil {
		req.OnLine([]byte(`{"k":"end"}`))
	}
	r.EndedAt = time.Now().UTC()
	if err := f.state.UpsertRun(context.Background(), r); err != nil {
		return nil, err
	}
	return r, nil
}

type fakeContainers struct {
	mu      sync.Mutex
	removed []string
}

func (c *fakeContainers) Kill(context.Context, string, string) error { return nil }
func (c *fakeContainers) Remove(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removed = append(c.removed, id)
	return nil
}

type memBlobs map[blob.Hash]string

func (b memBlobs) Reader(_ context.Context, h blob.Hash) (io.ReadCloser, blob.Info, error) {
	return io.NopCloser(strings.NewReader(b[h])), blob.Info{}, nil
}

const token = "0123456789abcdef0123456789abcdef"

type rig struct {
	t      *testing.T
	d      *Daemon
	state  *memState
	runner *fakeRunner
	cont   *fakeContainers
	srv    *httptest.Server
	stop   context.CancelFunc
	served chan struct{}
	life   context.Context
}

// ctx is the context Serve runs with; the test's end cancels it.
func (g *rig) ctx() context.Context { return g.life }

func newRig(t *testing.T, serve bool) *rig {
	t.Helper()
	state := newMemState()
	g := &rig{t: t, state: state, runner: &fakeRunner{state: state}, cont: &fakeContainers{}, served: make(chan struct{})}
	g.d = &Daemon{
		Local: &config.Local{
			Lanes:     map[string]*config.Lane{"lane-1": {Name: "lane-1", Host: "linux-gpu-1", Endpoints: []string{"stub"}}},
			Endpoints: map[string]*config.Endpoint{"stub": {Name: "stub", Kind: "ollama"}},
		},
		State: state, Runner: g.runner, Containers: g.cont, Blobs: memBlobs{"h1": "the events"},
		Repo: "../..", Version: "test", Token: token,
	}
	g.srv = httptest.NewServer(g.d.Handler())
	t.Cleanup(g.srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	g.stop, g.life = cancel, ctx
	t.Cleanup(cancel)
	if serve {
		go func() { g.d.Serve(ctx); close(g.served) }()
	}
	return g
}

func (g *rig) call(method, path string, body any, out any) int {
	g.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, g.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			g.t.Fatalf("%s %s: %s", method, path, data)
		}
	}
	return resp.StatusCode
}

func (g *rig) waitFinished(id string) RunView {
	g.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var v RunView
		if g.call("GET", "/v1/runs/"+id, nil, &v) == 200 && v.State == "finished" {
			// The lane is freed a moment after the record is written.
			if g.d.flight(id) == nil {
				return v
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	g.t.Fatalf("run %s did not finish", id)
	return RunView{}
}

func TestStartRunsInOrder(t *testing.T) {
	g := newRig(t, true)
	var dry StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2, DryRun: true}, &dry); code != 200 || len(dry.RunIDs) != 2 || !dry.DryRun {
		t.Fatalf("dry run: %d %+v", code, dry)
	}
	if q, _ := g.state.Queue(context.Background()); len(q) != 0 {
		t.Fatal("a dry run queued something")
	}
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 3, Key: "once"}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	if len(resp.RunIDs) != 3 || resp.Lane != "lane-1" || resp.Task != "demo/answer" || resp.Image != "automaticum-lane:base" || resp.ArmID == "" {
		t.Fatalf("start: %+v", resp)
	}
	for _, id := range resp.RunIDs {
		if v := g.waitFinished(id); v.Verdict != "PASS" || v.Batch != "k-once" || v.SDKRelease != "4.100.250" {
			t.Fatalf("run: %+v", v)
		}
	}
	// A finished run is analysed before its lane is freed, and the record
	// shows what was derived.
	var shown RunView
	g.call("GET", "/v1/runs/"+resp.RunIDs[0], nil, &shown)
	turns := -1.0
	for _, m := range shown.Metrics {
		if m.Name == "turns" {
			turns = m.Value
		}
	}
	if turns != 0 {
		t.Errorf("the run was not analysed: metrics %+v", shown.Metrics)
	}
	g.state.mu.Lock()
	g.state.metrics[resp.RunIDs[0]] = nil
	g.state.mu.Unlock()
	var again2 ReanalyseStatus
	if code := g.call("POST", "/v1/reanalyse", ReanalyseRequest{Batch: "k-once"}, &again2); code != 202 || again2.Total != 3 {
		t.Errorf("reanalyse: %d %+v", code, again2)
	}
	for deadline := time.Now().Add(10 * time.Second); again2.Running && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		g.call("GET", "/v1/reanalyse", nil, &again2)
	}
	if again2.Running || again2.Done != 3 || len(again2.Failed) != 0 || again2.Ended.IsZero() {
		t.Errorf("reanalyse ended as %+v", again2)
	}
	if g.call("GET", "/v1/runs/"+resp.RunIDs[0], nil, &shown); len(shown.Metrics) == 0 {
		t.Error("reanalyse derived nothing")
	}
	if code := g.call("POST", "/v1/reanalyse", ReanalyseRequest{Run: "nothing"}, nil); code != 404 {
		t.Errorf("reanalyse of a run that does not exist: %d", code)
	}
	g.runner.mu.Lock()
	seen := strings.Join(g.runner.seen, ",")
	g.runner.mu.Unlock()
	if seen != strings.Join(resp.RunIDs, ",") {
		t.Errorf("runs ran as %s, queued as %v", seen, resp.RunIDs)
	}
	// The same key again starts nothing.
	var again StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 3, Key: "once"}, &again); code != 200 || !again.Repeated || strings.Join(again.RunIDs, ",") != seen {
		t.Fatalf("repeat: %d %+v", code, again)
	}
	var list []RunView
	if g.call("GET", "/v1/runs?batch=k-once", nil, &list); len(list) != 3 {
		t.Fatalf("list: %+v", list)
	}
	var st Status
	g.call("GET", "/v1/status", nil, &st)
	if len(st.Lanes) != 1 || st.Lanes[0].State != "idle" || len(st.Queue) != 0 || st.Version != "test" {
		t.Fatalf("status: %+v", st)
	}
	if ls := g.state.lane("lane-1"); ls.State != "idle" || ls.RunID != "" {
		t.Fatalf("lane state: %+v", ls)
	}
}

func TestRefusals(t *testing.T) {
	g := newRig(t, false)
	for _, c := range []struct {
		req  StartRequest
		want string
	}{
		{StartRequest{Arm: "base", Task: "answer"}, "template"},
		{StartRequest{Arm: "absent", Task: "answer"}, "no arm"},
		{StartRequest{Arm: "stub", Task: "absent"}, "absent"},
		{StartRequest{Arm: "stub", Task: "answer", Lane: "lane-9"}, "no lane"},
		{StartRequest{Arm: "stub"}, "needs an arm"},
	} {
		var e apiError
		if code := g.call("POST", "/v1/runs", c.req, &e); code < 400 || !strings.Contains(e.Error, c.want) {
			t.Errorf("%+v: %d %q", c.req, code, e.Error)
		}
	}
	// Changing something needs the token; reading from this host does not.
	resp, err := http.Post(g.srv.URL+"/v1/runs", "application/json", strings.NewReader(`{"arm":"stub","task":"answer"}`))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post without a token: %v %v", resp, err)
	}
	resp.Body.Close()
	resp, err = http.Get(g.srv.URL + "/v1/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("get without a token: %v %v", resp, err)
	}
	resp.Body.Close()
}

func TestStreamAbortStopInvalidate(t *testing.T) {
	g := newRig(t, true)
	g.runner.hold = make(chan struct{})
	g.runner.started = make(chan string, 4)
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp)
	first, second := resp.RunIDs[0], resp.RunIDs[1]
	if id := <-g.runner.started; id != first {
		t.Fatalf("%s started first", id)
	}
	var st Status
	g.call("GET", "/v1/status", nil, &st)
	if st.Lanes[0].State != "running" || st.Lanes[0].RunID != first || len(st.Queue) != 2 || !st.Queue[0].Running || st.Queue[1].Running {
		t.Fatalf("status: %+v", st)
	}
	// The lane's state holds what a restart needs.
	var det laneDetail
	json.Unmarshal(g.state.lane("lane-1").Detail, &det)
	if det.ContainerID != "container-"+first || det.Run == nil || det.Run.ID != first {
		t.Fatalf("lane detail: %+v", det)
	}

	// A client that attaches late gets what was printed, then the rest.
	sresp, err := http.Get(g.srv.URL + "/v1/runs/" + first + "/stream")
	if err != nil || sresp.StatusCode != 200 {
		t.Fatalf("stream: %v %v", sresp, err)
	}
	defer sresp.Body.Close()
	sc := bufio.NewScanner(sresp.Body)
	read := func() string {
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				return line
			}
		}
		return ""
	}
	if a, b := read(), read(); a != `data: {"k":"event","n":1}` || b != `data: {"k":"event","n":2}` {
		t.Fatalf("stream: %q %q", a, b)
	}

	// A run in flight is not stopped; the queued one is.
	var e apiError
	if code := g.call("POST", "/v1/runs/"+first+"/stop", nil, &e); code != http.StatusConflict {
		t.Fatalf("stop of a run in flight: %d", code)
	}
	var stopped RunView
	if code := g.call("POST", "/v1/runs/"+second+"/stop", nil, &stopped); code != 200 || stopped.Verdict != "SKIPPED" {
		t.Fatalf("stop: %d %+v", code, stopped)
	}
	if code := g.call("POST", "/v1/runs/"+first+"/abort", nil, nil); code != http.StatusAccepted {
		t.Fatalf("abort: %d", code)
	}
	if a, b := read(), read(); a != `data: {"k":"end"}` || b != "event: end" {
		t.Fatalf("end of stream: %q %q", a, b)
	}
	v := g.waitFinished(first)
	if v.Verdict != "ABORTED" || v.VerdictReason != "operator abort" {
		t.Fatalf("aborted run: %+v", v)
	}
	g.runner.mu.Lock()
	ran := len(g.runner.seen)
	g.runner.mu.Unlock()
	if ran != 1 {
		t.Errorf("the stopped run ran")
	}
	if code := g.call("GET", "/v1/runs/"+first+"/stream", nil, &e); code != http.StatusGone {
		t.Errorf("stream of a finished run: %d", code)
	}

	// Invalid by the operator: the verdict it had is kept.
	if code := g.call("POST", "/v1/runs/"+first+"/invalidate", map[string]string{"reason": ""}, &e); code != http.StatusBadRequest {
		t.Errorf("invalidate without a reason: %d", code)
	}
	var inv RunView
	g.call("POST", "/v1/runs/"+first+"/invalidate", map[string]string{"reason": "wrong model tag"}, &inv)
	g.call("POST", "/v1/runs/"+first+"/invalidate", map[string]string{"reason": "and the GPU was shared"}, &inv)
	if inv.Verdict != "INVALID" || inv.PriorVerdict != "ABORTED" || inv.VerdictReason != "operator: and the GPU was shared" {
		t.Fatalf("invalidate: %+v", inv)
	}

	// Raw material.
	g.state.refs = append(g.state.refs, store.BlobRef{RunID: first, Kind: "events", Hash: "h1", Size: 10})
	resp2, _ := http.Get(g.srv.URL + "/v1/runs/" + first + "/blobs/events")
	data, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(data) != "the events" {
		t.Errorf("blob: %q", data)
	}
	var full RunView
	g.call("GET", "/v1/runs/"+first, nil, &full)
	if len(full.Blobs) != 1 || full.Blobs[0].Kind != "events" {
		t.Errorf("blobs of the run: %+v", full.Blobs)
	}
}

func TestDaemonStopAndDrain(t *testing.T) {
	// The daemon's end aborts the run in flight, and says so.
	g := newRig(t, true)
	g.runner.hold = make(chan struct{})
	g.runner.started = make(chan string, 4)
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp)
	<-g.runner.started
	g.stop()
	<-g.served
	r, err := g.state.GetRun(context.Background(), resp.RunIDs[0])
	if err != nil || r.Verdict != store.Aborted || r.VerdictReason != "daemon stop" {
		t.Fatalf("%+v %v", r, err)
	}
	if q, _ := g.state.Queue(context.Background()); len(q) != 1 || q[0].RunID != resp.RunIDs[1] {
		t.Fatalf("queue after the stop: %+v", q)
	}

	// Draining lets the run in flight finish and starts no other.
	g = newRig(t, true)
	g.runner.hold = make(chan struct{})
	g.runner.started = make(chan string, 4)
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp)
	<-g.runner.started
	if code := g.call("POST", "/v1/drain", nil, nil); code != 200 {
		t.Fatalf("drain: %d", code)
	}
	var e apiError
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &e); code != http.StatusConflict {
		t.Errorf("a draining daemon took a run: %d", code)
	}
	close(g.runner.hold)
	<-g.served
	r, _ = g.state.GetRun(context.Background(), resp.RunIDs[0])
	if r == nil || r.Verdict != store.Pass {
		t.Fatalf("the run in flight did not finish: %+v", r)
	}
	if q, _ := g.state.Queue(context.Background()); len(q) != 1 {
		t.Fatalf("queue after draining: %+v", q)
	}
}

func TestRecover(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	started := time.Now().Add(-time.Minute).UTC()
	// What a daemon killed mid-run leaves: a queue, and a lane marked
	// running with the run's record so far.
	g.state.Enqueue(ctx, []store.QueueItem{
		{RunID: "r1", Arm: "stub", Task: "demo/answer", Lane: "lane-1"},
		{RunID: "r2", Arm: "stub", Task: "demo/answer", Lane: "lane-1"},
		{RunID: "r0", Arm: "stub", Task: "demo/answer", Lane: "lane-1"}, // finished, not yet unqueued
	})
	g.state.UpsertRun(ctx, &store.Run{ID: "r0", Verdict: store.Fail})
	det, _ := json.Marshal(laneDetail{ContainerID: "c1", Run: &store.Run{ID: "r1", Lane: "lane-1", Model: "stub:latest", StartedAt: started}})
	g.state.SetLaneState(ctx, store.LaneState{Lane: "lane-1", State: "running", RunID: "r1", Detail: det})

	if err := g.d.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	r1, err := g.state.GetRun(ctx, "r1")
	if err != nil || r1.Verdict != store.Aborted || r1.VerdictReason != "daemon restart" || r1.Model != "stub:latest" || r1.WallMS < 59000 {
		t.Fatalf("r1: %+v %v", r1, err)
	}
	if len(g.cont.removed) != 1 || g.cont.removed[0] != "c1" {
		t.Errorf("containers removed: %v", g.cont.removed)
	}
	if r0, _ := g.state.GetRun(ctx, "r0"); r0.Verdict != store.Fail {
		t.Errorf("a finished run was touched: %+v", r0)
	}
	if q, _ := g.state.Queue(ctx); len(q) != 1 || q[0].RunID != "r2" {
		t.Fatalf("queue: %+v", q)
	}
	if ls := g.state.lane("lane-1"); ls.State != "idle" || ls.RunID != "" {
		t.Errorf("lane: %+v", ls)
	}
	// Recovering twice changes nothing, and what was queued then runs once.
	if err := g.d.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go g.d.Serve(sctx)
	g.waitFinished("r2")
	g.runner.mu.Lock()
	defer g.runner.mu.Unlock()
	if strings.Join(g.runner.seen, ",") != "r2" {
		t.Errorf("ran after recovery: %v", g.runner.seen)
	}
}

// A refusing gate skips the run with the gate's name and tells the webhook;
// a waiting gate leaves the run queued, tells once, and the run starts when
// the gate lets it (D64).
func TestGates(t *testing.T) {
	var mu sync.Mutex
	var told []Notification
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n Notification
		json.NewDecoder(r.Body).Decode(&n)
		mu.Lock()
		told = append(told, n)
		mu.Unlock()
	}))
	defer hook.Close()
	answer := gate.Result{Gate: "free", Version: "1", Status: gate.Wait, Detail: "the endpoint holds another model"}
	asked := 0
	g := newRig(t, false)
	g.d.Local.Daemon.Notify.Webhook = hook.URL
	g.d.GateRetry = 20 * time.Millisecond
	g.d.Gates = func(context.Context, string, *config.Arm, *task.Task, string) []gate.Result {
		mu.Lock()
		defer mu.Unlock()
		asked++
		return []gate.Result{{Gate: "lane", Status: gate.Pass}, answer, {Gate: "expert", Status: gate.Unchecked}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.d.Serve(ctx)

	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	id := resp.RunIDs[0]
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("never happened: %s", what)
	}
	// It waits: queued, not run, the lane says why, and it is asked again.
	waitFor("the gates are asked three times", func() bool { mu.Lock(); defer mu.Unlock(); return asked >= 3 })
	var st Status
	st = Status{}
	g.call("GET", "/v1/status", nil, &st)
	if len(st.Queue) != 1 || st.Lanes[0].State != "waiting" || st.Lanes[0].RunID != id || !strings.Contains(st.Lanes[0].Waiting, "free: the endpoint holds another model") {
		t.Fatalf("status while waiting: %+v", st)
	}
	g.runner.mu.Lock()
	ran := len(g.runner.seen)
	g.runner.mu.Unlock()
	mu.Lock()
	n := len(told)
	mu.Unlock()
	if ran != 0 || n != 1 || told[0].Status != "wait" || told[0].Gate != "free" || told[0].Run != id || told[0].Source != "automaticum" {
		t.Fatalf("while waiting: ran %d, told %+v", ran, told)
	}
	// The endpoint is free: the run starts, with the gates' answers.
	mu.Lock()
	answer = gate.Result{Gate: "free", Status: gate.Pass}
	mu.Unlock()
	if v := g.waitFinished(id); v.Verdict != "PASS" {
		t.Fatalf("after the wait: %+v", v)
	}
	st = Status{}
	g.call("GET", "/v1/status", nil, &st)
	if st.Lanes[0].State != "idle" || st.Lanes[0].Waiting != "" {
		t.Errorf("status after the run: %+v", st.Lanes[0])
	}

	// A refusal: SKIPPED, with the gate's name, told once, never run.
	mu.Lock()
	answer = gate.Result{Gate: "oracle", Status: gate.Refuse, Detail: "not proven"}
	told = nil
	mu.Unlock()
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &resp)
	v := g.waitFinished(resp.RunIDs[0])
	if v.Verdict != "SKIPPED" || v.VerdictReason != "gate oracle: not proven" {
		t.Errorf("refused run: %+v", v)
	}
	mu.Lock()
	if len(told) != 1 || told[0].Status != "refuse" || told[0].Gate != "oracle" {
		t.Errorf("told %+v", told)
	}
	mu.Unlock()
	g.runner.mu.Lock()
	if len(g.runner.seen) != 1 {
		t.Errorf("runs started: %v", g.runner.seen)
	}
	g.runner.mu.Unlock()

	// A waiting run that is taken off the queue frees the lane.
	mu.Lock()
	answer = gate.Result{Gate: "free", Status: gate.Wait, Detail: "busy"}
	mu.Unlock()
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer"}, &resp)
	waitFor("the lane waits", func() bool {
		st = Status{}
		g.call("GET", "/v1/status", nil, &st)
		return st.Lanes[0].State == "waiting"
	})
	if code := g.call("POST", "/v1/runs/"+resp.RunIDs[0]+"/stop", nil, nil); code != 200 {
		t.Fatalf("taking a waiting run off the queue: %d", code)
	}
	waitFor("the lane is idle again", func() bool { st = Status{}; g.call("GET", "/v1/status", nil, &st); return st.Lanes[0].State == "idle" })

	// doctor asks without starting anything.
	var entries []DoctorEntry
	if code := g.call("GET", "/v1/doctor?task=answer&arm=stub", nil, &entries); code != 200 || len(entries) != 1 || len(entries[0].Gates) != 3 || entries[0].Lane != "lane-1" {
		t.Errorf("doctor: %d %+v", code, entries)
	}
}

// Several arms in one batch are queued one run of each in turn, or in
// blocks when asked; two arms that would run on different lanes are refused.
func TestStartSeveralArms(t *testing.T) {
	g := newRig(t, false)
	var resp StartResponse
	req := StartRequest{Arms: []string{"stub", "stub-sdk-4.100.251"}, Task: "answer", Count: 3, DryRun: true}
	if code := g.call("POST", "/v1/runs", req, &resp); code != 200 {
		t.Fatalf("dry run: %d", code)
	}
	if got := strings.Join(resp.Order, " "); got != "stub stub-sdk-4.100.251 stub stub-sdk-4.100.251 stub stub-sdk-4.100.251" || len(resp.RunIDs) != 6 || len(resp.ArmIDs) != 2 {
		t.Errorf("interleaved: %q, %d runs, arms %v", got, len(resp.RunIDs), resp.ArmIDs)
	}
	req.Order = "blocks"
	resp = StartResponse{}
	g.call("POST", "/v1/runs", req, &resp)
	if got := strings.Join(resp.Order, " "); got != "stub stub stub stub-sdk-4.100.251 stub-sdk-4.100.251 stub-sdk-4.100.251" {
		t.Errorf("blocks: %q", got)
	}
	req.Order = "random"
	if code := g.call("POST", "/v1/runs", req, nil); code != 400 {
		t.Errorf("an unknown order: %d", code)
	}
	req.Order, req.Arms = "", []string{"stub", "stub"}
	if code := g.call("POST", "/v1/runs", req, nil); code != 400 {
		t.Errorf("an arm named twice: %d", code)
	}
}

// The compare view groups the runs of a batch by arm, leaves out runs that
// say nothing of the arm, and gives each number without a conclusion.
func TestCompare(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	add := func(id, arm string, verdict store.Verdict, wall float64) {
		g.state.UpsertRun(ctx, &store.Run{ID: id, Batch: "b1", Suite: "demo", TaskID: "answer", ArmID: "id-" + arm, Verdict: verdict,
			Arm: json.RawMessage(`{"name":"` + arm + `","model":{"model":"m-` + arm + `"}}`)})
		g.state.ReplaceMetrics(ctx, id, []store.Metric{{RunID: id, Name: "time.wall_ms", Value: wall}, {RunID: id, Name: "detector.char_run.available", Value: 1}})
	}
	add("r1", "a", store.Pass, 1000)
	add("r2", "a", store.Pass, 2000)
	add("r3", "a", store.Fail, 3000)
	add("r4", "a", store.Invalid, 9000)
	add("r5", "b", store.Pass, 4000)
	add("r6", "b", store.Fail, 5000)
	add("r7", "b", store.Fail, 6000)
	g.state.UpsertRun(ctx, &store.Run{ID: "r8", Batch: "b1", Suite: "demo", TaskID: "answer", Verdict: store.Skipped, VerdictReason: "stopped by the operator before it started"})
	g.state.ReplaceFindings(ctx, "r5", []store.Finding{{RunID: "r5", Detector: "reasoning_loop"}, {RunID: "r5", Detector: "reasoning_loop"}})
	var v CompareView
	if code := g.call("GET", "/v1/compare?batch=b1", nil, &v); code != 200 || len(v.Groups) != 2 || len(v.Pairs) != 1 {
		t.Fatalf("%d: %+v", code, v)
	}
	a, b := v.Groups[v.Pairs[0].A], v.Groups[v.Pairs[0].B]
	if a.Arm != "a" {
		a, b = b, a
	}
	if len(a.Runs) != 3 || a.Pass != 2 || len(a.Excluded) != 1 || a.Verdicts["INVALID"] != 1 || a.Metrics["time.wall_ms"].Median != 2000 {
		t.Errorf("a: %+v", a)
	}
	if b.Pass != 1 || b.Findings["reasoning_loop"] != 1 || b.Metrics["time.wall_ms"].Max != 6000 {
		t.Errorf("b: %+v", b)
	}
	if _, has := a.Metrics["detector.char_run.available"]; has {
		t.Error("a detector's availability is not a measurement of the runs")
	}
	p := v.Pairs[0]
	if r := p.Metrics["time.wall_ms"]; !r.Exact || r.P != 0.1 || (r.Superiority != 0 && r.Superiority != 1) {
		t.Errorf("wall time, apart: %+v", r)
	}
	if p.PassP <= 0 || p.PassP > 1 || len(p.Differs) == 0 {
		t.Errorf("pair: %+v", p)
	}
	if len(v.NeverStarted) != 1 {
		t.Errorf("a run that never started is not a group of its own: %+v", v.NeverStarted)
	}
	if code := g.call("GET", "/v1/compare?batch=none", nil, nil); code != 404 {
		t.Errorf("an unknown batch: %d", code)
	}
}

// What the server said of the model at each run's start is kept per arm,
// every distinct answer: a quantization that changed between runs shows.
func TestCompareServed(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	for i, q := range []string{"FP4", "FP4", "FP8"} {
		g.state.UpsertRun(ctx, &store.Run{ID: fmt.Sprintf("r%d", i), Batch: "b", Suite: "demo", TaskID: "answer", ArmID: "a", Verdict: store.Pass,
			Arm:        json.RawMessage(`{"name":"a","model":{"model":"m"}}`),
			Provenance: json.RawMessage(`{"endpoint_before":{"digest":"abc","server_version":"1.0","details":{"quantization_level":"` + q + `"}}}`)})
	}
	var v CompareView
	if code := g.call("GET", "/v1/compare?batch=b", nil, &v); code != 200 || len(v.Groups) != 1 {
		t.Fatalf("%d %+v", code, v)
	}
	s := v.Groups[0].Served
	if strings.Join(s["quantization"], ",") != "FP4,FP8" && strings.Join(s["quantization"], ",") != "FP8,FP4" {
		t.Errorf("quantization: %v", s["quantization"])
	}
	if len(s["model digest"]) != 1 || len(s["server version"]) != 1 {
		t.Errorf("unchanged facts: %v", s)
	}
}

// status reads the daemon's status.
func (g *rig) status() Status {
	g.t.Helper()
	var st Status
	if code := g.call("GET", "/v1/status", nil, &st); code != 200 {
		g.t.Fatalf("status: %d", code)
	}
	return st
}

// stays checks for a while that a queued run does not start, and gives why
// the queue says it does not.
func (g *rig) stays(id string) string {
	g.t.Helper()
	time.Sleep(150 * time.Millisecond)
	if g.d.flight(id) != nil {
		g.t.Fatalf("run %s started", id)
	}
	for _, e := range g.status().Queue {
		if e.RunID == id {
			return e.Stopped
		}
	}
	g.t.Fatalf("run %s is not queued", id)
	return ""
}

// A drained lane finishes its run in flight and starts nothing else until
// it is resumed, and a restarted daemon still knows it is drained.
func TestLaneDrain(t *testing.T) {
	g := newRig(t, false)
	g.d.ControlFile = filepath.Join(t.TempDir(), "control.json")
	hold := make(chan struct{})
	g.runner.hold, g.runner.started = hold, make(chan string, 4)
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	first, second := resp.RunIDs[0], resp.RunIDs[1]
	<-g.runner.started
	var lc LaneControl
	if code := g.call("POST", "/v1/lanes/lane-1/drain", nil, &lc); code != 200 || !lc.Drained || lc.InFlight != first {
		t.Fatalf("drain: %d %+v", code, lc)
	}
	if code := g.call("POST", "/v1/lanes/lane-9/drain", nil, nil); code != 404 {
		t.Errorf("an unknown lane: %d", code)
	}
	close(hold)
	g.waitFinished(first)
	if why := g.stays(second); why != "lane drained" {
		t.Errorf("the queue says %q", why)
	}
	if ls := g.status().Lanes[0]; ls.State != "drained" || !ls.Drained {
		t.Errorf("lane: %+v", ls)
	}
	// What a new daemon reads from the same file.
	again := &Daemon{Local: g.d.Local, ControlFile: g.d.ControlFile}
	again.init()
	if _, ok := again.ctl.Drained["lane-1"]; !ok {
		t.Error("a restarted daemon forgot the drained lane")
	}
	if code := g.call("POST", "/v1/lanes/lane-1/resume", nil, &lc); code != 200 || lc.Drained {
		t.Fatalf("resume: %d %+v", code, lc)
	}
	if v := g.waitFinished(second); v.Verdict != "PASS" {
		t.Errorf("after the resume: %+v", v)
	}
}

// A paused batch starts none of its queued runs; a stopped one records them
// SKIPPED with what they were asked to be; a retry runs them again in the
// same batch, once, and never runs a counted verdict again.
func TestBatchPauseStopRetry(t *testing.T) {
	g := newRig(t, false)
	g.d.ControlFile = filepath.Join(t.TempDir(), "control.json")
	hold := make(chan struct{})
	g.runner.hold, g.runner.started = hold, make(chan string, 8)
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 3}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	b := resp.Batch
	<-g.runner.started
	var bc BatchControl
	if code := g.call("POST", "/v1/batches/"+b+"/pause", nil, &bc); code != 202 || bc.InFlight != resp.RunIDs[0] || len(bc.Queued) != 2 {
		t.Fatalf("pause: %d %+v", code, bc)
	}
	if bc.Action == "" {
		t.Error("the pause does not say what it did to the run in flight")
	}
	close(hold)
	g.waitFinished(resp.RunIDs[0])
	if why := g.stays(resp.RunIDs[1]); why != "batch paused" {
		t.Errorf("the queue says %q", why)
	}
	if code := g.call("POST", "/v1/batches/no-such/pause", nil, nil); code != 404 {
		t.Errorf("an unknown batch: %d", code)
	}
	bc = BatchControl{}
	if code := g.call("POST", "/v1/batches/"+b+"/stop", nil, &bc); code != 200 || len(bc.Stopped) != 2 || bc.InFlight != "" {
		t.Fatalf("stop: %d %+v", code, bc)
	}
	for _, id := range bc.Stopped {
		v := g.waitFinished(id)
		if v.Verdict != "SKIPPED" || !strings.Contains(v.VerdictReason, "batch stopped") {
			t.Errorf("%s: %+v", id, v)
		}
	}
	if code := g.call("POST", "/v1/batches/"+b+"/resume", nil, nil); code != 404 {
		t.Errorf("a stopped batch is not paused: %d", code)
	}

	var e apiError
	if code := g.call("POST", "/v1/runs/"+resp.RunIDs[0]+"/retry", nil, &e); code != 409 || !strings.Contains(e.Error, "PASS is a result") {
		t.Errorf("a PASS was retried: %d %+v", code, e)
	}
	var rv RetryView
	if code := g.call("POST", "/v1/batches/"+b+"/retry", map[string]bool{"dry_run": true}, &rv); code != 200 || len(rv.Retries) != 2 {
		t.Fatalf("dry run: %d %+v", code, rv)
	}
	if len(g.status().Queue) != 0 {
		t.Error("a dry run queued runs")
	}
	rv = RetryView{}
	if code := g.call("POST", "/v1/batches/"+b+"/retry", nil, &rv); code != 202 || len(rv.Retries) != 2 {
		t.Fatalf("retry: %d %+v", code, rv)
	}
	for nu, old := range rv.Retries {
		v := g.waitFinished(nu)
		if v.Verdict != "PASS" || v.Batch != b || v.RetryOf != old {
			t.Errorf("%s: %+v", nu, v)
		}
		var was RunView
		g.call("GET", "/v1/runs/"+old, nil, &was)
		if was.RetriedBy != nu || was.Verdict != "SKIPPED" {
			t.Errorf("the retried run: %+v", was)
		}
	}
	if code := g.call("POST", "/v1/batches/"+b+"/retry", nil, &e); code != 409 || !strings.Contains(e.Error, "already retried by") {
		t.Errorf("retried twice: %d %+v", code, e)
	}
}

// A run that never started and whose record does not say what it was asked
// to be cannot be retried; one that does can.
func TestAskedOf(t *testing.T) {
	if _, err := askedOf(&store.Run{ID: "r", Lane: "lane-1", Suite: "demo", TaskID: "answer"}); err == nil {
		t.Error("a record without its arm was retried")
	}
	it := store.QueueItem{RunID: "r", Arm: "stub", Task: "demo/answer", Lane: "lane-1"}
	got, err := askedOf(&store.Run{ID: "r", Provenance: askedProvenance(it)})
	if err != nil || got.Arm != "stub" || got.Task != "demo/answer" || got.Lane != "lane-1" {
		t.Errorf("%+v %v", got, err)
	}
	got, err = askedOf(&store.Run{ID: "r", Lane: "lane-1", Suite: "demo", TaskID: "answer", Arm: json.RawMessage(`{"name":"stub"}`)})
	if err != nil || got.Arm != "stub" {
		t.Errorf("%+v %v", got, err)
	}
}
