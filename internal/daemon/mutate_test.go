package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// keyed sends a request with an idempotency key and says whether the
// answer was a replay.
func (g *rig) keyed(method, path, key string, body any, out any) (int, bool) {
	g.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, g.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil {
		json.Unmarshal(data, out)
	}
	return resp.StatusCode, resp.Header.Get("Idempotent-Replay") == "true"
}

func TestDryRunAndIdempotency(t *testing.T) {
	g := newRig(t, false)
	hold := make(chan struct{})
	g.runner.hold, g.runner.started = hold, make(chan string, 8)
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 3}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	b, ids := resp.Batch, resp.RunIDs
	<-g.runner.started

	// Dry runs answer what would happen and change nothing.
	var v RunView
	if code := g.call("POST", "/v1/runs/"+ids[2]+"/stop?dry_run=1", nil, &v); code != 200 || !v.DryRun || v.Verdict != "SKIPPED" {
		t.Fatalf("dry stop: %d %+v", code, v)
	}
	var lc LaneControl
	if code := g.call("POST", "/v1/lanes/lane-1/quarantine?dry_run=1", map[string]string{"reason": "flaky GPU"}, &lc); code != 200 || !lc.DryRun || lc.State != "quarantined" || lc.InFlight != ids[0] {
		t.Fatalf("dry quarantine: %d %+v", code, lc)
	}
	var bc BatchControl
	if code := g.call("POST", "/v1/batches/"+b+"/abort?dry_run=1", nil, &bc); code != 200 || !bc.DryRun || len(bc.Stopped) != 2 || bc.Action != "aborting" {
		t.Fatalf("dry abort: %d %+v", code, bc)
	}
	var dn Done
	if code := g.call("POST", "/v1/runs/"+ids[0]+"/abort?dry_run=1", nil, &dn); code != 200 || !dn.DryRun {
		t.Fatalf("dry run abort: %d %+v", code, dn)
	}
	st := g.status()
	if len(st.Queue) != 3 || st.Lanes[0].State != "running" || st.Lanes[0].Quarantined != "" {
		t.Fatalf("a dry run changed something: %+v", st)
	}

	// A key: the first stop is done, the same request again is a replay,
	// the same key on another request is refused.
	var first, again RunView
	code, replay := g.keyed("POST", "/v1/runs/"+ids[2]+"/stop", "k-stop-3", nil, &first)
	if code != 200 || replay || first.Verdict != "SKIPPED" {
		t.Fatalf("keyed stop: %d %v %+v", code, replay, first)
	}
	code, replay = g.keyed("POST", "/v1/runs/"+ids[2]+"/stop", "k-stop-3", nil, &again)
	if code != 200 || !replay || again.ID != first.ID {
		t.Fatalf("again: %d %v %+v", code, replay, again)
	}
	if code, _ := g.keyed("POST", "/v1/runs/"+ids[1]+"/stop", "k-stop-3", nil, nil); code != 422 {
		t.Errorf("the key on another request: %d", code)
	}
	// Without a key, the second stop is refused: the run is no longer
	// queued.
	if code := g.call("POST", "/v1/runs/"+ids[2]+"/stop", nil, nil); code != 404 {
		t.Errorf("stop again without a key: %d", code)
	}

	// The lane: quarantined (shown with its reason), resumed.
	if code := g.call("POST", "/v1/lanes/lane-1/quarantine", map[string]string{}, nil); code != 400 {
		t.Errorf("a quarantine without a reason: %d", code)
	}
	if code := g.call("POST", "/v1/lanes/lane-1/quarantine", map[string]string{"reason": "flaky GPU"}, &lc); code != 200 {
		t.Fatalf("quarantine: %d", code)
	}
	close(hold)
	g.waitFinished(ids[0])
	st = g.status()
	if st.Lanes[0].State != "quarantined" || st.Lanes[0].Quarantined != "flaky GPU" || st.Queue[0].Stopped != "lane quarantined: flaky GPU" {
		t.Fatalf("quarantined: %+v", st)
	}
	if code := g.call("POST", "/v1/lanes/lane-1/resume", nil, &lc); code != 200 || lc.State != "resumed" {
		t.Fatalf("resume: %d %+v", code, lc)
	}
	g.waitFinished(ids[1])
	if code := g.call("POST", "/v1/lanes/lane-1/abort", nil, nil); code != 404 {
		t.Errorf("abort with nothing in flight: %d", code)
	}
}

// A lane paused holds its run in flight; resuming releases it.
func TestLanePause(t *testing.T) {
	g := newRig(t, false)
	g.runner.started = make(chan string, 4)
	h := &holdRunner{fakeRunner: g.runner}
	g.d.Runner = h
	g.runner.hold = make(chan struct{})
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp)
	<-g.runner.started
	var lc LaneControl
	if code := g.call("POST", "/v1/lanes/lane-1/pause", nil, &lc); code != 200 || lc.State != "paused" || lc.InFlight != resp.RunIDs[0] {
		t.Fatalf("pause: %d %+v", code, lc)
	}
	if h.held() != resp.RunIDs[0] {
		t.Errorf("held %q", h.held())
	}
	if code := g.call("POST", "/v1/lanes/lane-1/resume", nil, &lc); code != 200 || lc.Action == "" {
		t.Fatalf("resume: %d %+v", code, lc)
	}
	if h.released() != resp.RunIDs[0] {
		t.Errorf("released %q", h.released())
	}
	close(g.runner.hold)
	g.waitFinished(resp.RunIDs[1])
}

// A drain releases a held run so that it finishes (D119): a lane paused
// and then drained, and a daemon drained under a batch pause.
func TestDrainReleasesHeld(t *testing.T) {
	g := newRig(t, false)
	g.runner.started = make(chan string, 4)
	h := &holdRunner{fakeRunner: g.runner}
	g.d.Runner = h
	g.runner.hold = make(chan struct{})
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 3}, &resp)
	<-g.runner.started
	var lc LaneControl
	g.call("POST", "/v1/lanes/lane-1/pause", nil, &lc)
	if code := g.call("POST", "/v1/lanes/lane-1/drain?dry_run=1", nil, &lc); code != 200 || !strings.HasPrefix(lc.Action, "released") || h.released() != "" {
		t.Fatalf("drain, dry run: %d %+v released %q", code, lc, h.released())
	}
	if code := g.call("POST", "/v1/lanes/lane-1/drain", nil, &lc); code != 200 || lc.State != "drained" || !strings.HasPrefix(lc.Action, "released") {
		t.Fatalf("drain: %d %+v", code, lc)
	}
	if h.released() != resp.RunIDs[0] {
		t.Errorf("released %q", h.released())
	}
	if st := g.status(); st.Lanes[0].State == "paused" || st.Queue[1].Stopped != "lane drained" {
		t.Errorf("after the drain: %+v", st)
	}
	g.runner.hold <- struct{}{}
	g.waitFinished(resp.RunIDs[0])
	if st := g.status(); len(st.Queue) != 2 {
		t.Fatalf("a drained lane started a run: %+v", st.Queue)
	}

	// The daemon drained under a batch pause: the run in flight is
	// released, finishes, and Serve returns.
	g.call("POST", "/v1/lanes/lane-1/resume", nil, &lc)
	if id := <-g.runner.started; id != resp.RunIDs[1] {
		t.Fatalf("started %s", id)
	}
	if code := g.call("POST", "/v1/batches/"+resp.Batch+"/pause", nil, nil); code/100 != 2 || h.held() != resp.RunIDs[1] {
		t.Fatalf("batch pause: %d held %q", code, h.held())
	}
	if code := g.call("POST", "/v1/drain", nil, nil); code != 200 || h.released() != resp.RunIDs[1] {
		t.Fatalf("daemon drain: %d released %q", code, h.released())
	}
	close(g.runner.hold)
	<-g.served
	if r, _ := g.state.GetRun(context.Background(), resp.RunIDs[1]); r == nil || r.Verdict != store.Pass {
		t.Fatalf("the released run: %+v", r)
	}
}

// holdRunner is a fakeRunner that can be asked to hold and release a run.
type holdRunner struct {
	*fakeRunner
	mu   sync.Mutex
	h, r string
}

func (h *holdRunner) Hold(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.h = id
	return nil
}

func (h *holdRunner) Release(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.r = id
	return nil
}

func (h *holdRunner) held() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.h
}

func (h *holdRunner) released() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.r
}

// lockedBuffer is a log sink the test can read while the daemon writes.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Every change records who made it (010 §5): the log names the client of
// each mutating call, and a batch keeps who started it.
func TestClientRecorded(t *testing.T) {
	g := newRig(t, false)
	var logs lockedBuffer
	g.d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	go func() { g.d.Serve(g.ctx()); close(g.served) }()

	b, _ := json.Marshal(StartRequest{Arm: "stub", Task: "answer"})
	req, _ := http.NewRequest("POST", g.srv.URL+"/v1/runs", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(ClientHeader, "  assistant\tsession  ")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sr StartResponse
	json.NewDecoder(resp.Body).Decode(&sr)
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	g.waitFinished(sr.RunIDs[0])
	var bv BatchView
	if code := g.call("GET", "/v1/batches/"+sr.Batch, nil, &bv); code != 200 || bv.StartedBy != "assistant session" {
		t.Errorf("batch: %d %+v", code, bv)
	}
	// Without the header the change is still logged, by "unnamed"; a dry
	// run and a read change nothing and are not.
	g.call("POST", "/v1/lanes/lane-1/pause", nil, nil)
	g.call("POST", "/v1/lanes/lane-1/resume?dry_run=1", nil, nil)
	out := logs.String()
	for _, want := range []string{`msg=change client="assistant session" method=POST path=/v1/runs status=202`, `msg=change client=unnamed method=POST path=/v1/lanes/lane-1/pause status=200`} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "resume") || strings.Contains(out, "path=/v1/batches/") {
		t.Errorf("a dry run or a read was logged as a change:\n%s", out)
	}
}
