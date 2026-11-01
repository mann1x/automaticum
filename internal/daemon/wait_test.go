package daemon

import (
	"net/url"
	"testing"
	"time"
)

func (g *rig) wait(kind string, q url.Values) (WaitView, int) {
	g.t.Helper()
	var v WaitView
	code := g.call("GET", "/v1/wait/"+kind+"?"+q.Encode(), nil, &v)
	return v, code
}

func TestWait(t *testing.T) {
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

	// Nothing holds yet: a short hold answers with the state, not met.
	v, code := g.wait("batch", url.Values{"id": {b}, "until": {"finished>=1"}, "hold": {"0s"}})
	if code != 200 || v.Met || v.Over || v.Counts["running"] != 1 || v.Counts["queued"] != 2 || v.Counts["total"] != 3 {
		t.Fatalf("at once: %d %+v", code, v)
	}
	if v, _ := g.wait("idle", url.Values{"hold": {"0s"}}); v.Met || v.State != "busy" {
		t.Errorf("idle while busy: %+v", v)
	}

	// The second run is waited for until it starts, the batch until its
	// first run has finished; one run let go answers both.
	type answer struct {
		v    WaitView
		code int
	}
	started, first := make(chan answer, 1), make(chan answer, 1)
	go func() {
		v, c := g.wait("run", url.Values{"id": {ids[1]}, "until": {"started"}, "hold": {"10s"}})
		started <- answer{v, c}
	}()
	go func() {
		v, c := g.wait("batch", url.Values{"id": {b}, "until": {"finished>=1"}, "seen": {"0"}, "hold": {"10s"}})
		first <- answer{v, c}
	}()
	select {
	case a := <-first:
		t.Fatalf("the wait did not wait: %+v", a)
	case <-time.After(100 * time.Millisecond):
	}
	hold <- struct{}{}
	a := <-first
	if a.code != 200 || !a.v.Met || len(a.v.New) != 1 || a.v.New[0].ID != ids[0] || a.v.New[0].Verdict != "PASS" {
		t.Fatalf("the first run: %d %+v", a.code, a.v)
	}
	if a := <-started; !a.v.Met || a.v.State != "running" {
		t.Fatalf("the second run's start: %+v", a.v)
	}
	<-g.runner.started

	// The whole batch, and the lane; then a condition that can no longer
	// hold ends at once, over.
	done, lane := make(chan answer, 1), make(chan answer, 1)
	go func() {
		v, c := g.wait("batch", url.Values{"id": {b}, "hold": {"10s"}})
		done <- answer{v, c}
	}()
	go func() {
		v, c := g.wait("lane", url.Values{"id": {"lane-1"}, "hold": {"10s"}})
		lane <- answer{v, c}
	}()
	hold <- struct{}{}
	<-g.runner.started
	hold <- struct{}{}
	if a := <-done; !a.v.Met || a.v.State != "done" || a.v.Counts["pass"] != 3 || a.v.Until != "done" {
		t.Fatalf("the batch: %+v", a.v)
	}
	if a := <-lane; !a.v.Met || !a.v.Facts["idle"] {
		t.Fatalf("the lane: %+v", a.v)
	}
	if v, _ := g.wait("batch", url.Values{"id": {b}, "until": {"fail>=1"}}); v.Met || !v.Over {
		t.Errorf("over: %+v", v)
	}
	if v, _ := g.wait("batch", url.Values{"id": {b}, "until": {"finished>3"}, "seen": {"3"}}); !v.Over || len(v.New) != 0 {
		t.Errorf("no next run: %+v", v)
	}
	if v, _ := g.wait("run", url.Values{"id": {ids[2]}}); !v.Met || v.State != "finished" || v.Counts["pass"] != 1 {
		t.Errorf("a finished run: %+v", v)
	}

	for _, c := range []struct {
		kind string
		q    url.Values
		want int
	}{
		{"batch", url.Values{"id": {"no-such"}}, 404},
		{"run", url.Values{"id": {"no-such"}}, 404},
		{"campaign", url.Values{"id": {"no-such"}}, 404},
		{"lane", url.Values{"id": {"no-such"}}, 404},
		{"batch", url.Values{"id": {b}, "until": {"pass>="}}, 400},
		{"batch", url.Values{"id": {b}, "until": {"idle"}}, 400},
		{"batch", url.Values{}, 400},
		{"batch", url.Values{"id": {b}, "seen": {"-1"}}, 400},
		{"nothing", url.Values{"id": {b}}, 400},
	} {
		if _, code := g.wait(c.kind, c.q); code != c.want {
			t.Errorf("%s %v: %d, want %d", c.kind, c.q, code, c.want)
		}
	}
}
