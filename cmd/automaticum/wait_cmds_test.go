package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/wait"
)

// oldDaemon answers as a daemon without the wait route: /v1/wait/ is the
// mux's plain 404, runs come from /v1/runs.
type oldDaemon struct {
	mu   sync.Mutex
	runs []daemon.RunView
}

func (o *oldDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch r.URL.Path {
	case "/v1/runs":
		// A run being written is listed queued and finished at once.
		out := append([]daemon.RunView(nil), o.runs...)
		for _, v := range o.runs {
			if v.State == "finished" && v.ID == "b-002" {
				out = append(out, daemon.RunView{ID: v.ID, State: "queued"})
			}
		}
		json.NewEncoder(w).Encode(out)
	default:
		http.NotFound(w, r)
	}
}

func (o *oldDaemon) finish(id, verdict string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.runs {
		if o.runs[i].ID == id {
			o.runs[i].State, o.runs[i].Verdict, o.runs[i].EndedAt = "finished", verdict, time.Now()
		}
	}
}

func TestWaitFallback(t *testing.T) {
	o := &oldDaemon{runs: []daemon.RunView{{ID: "b-001", State: "running"}, {ID: "b-002", State: "queued"}}}
	srv := httptest.NewServer(o)
	defer srv.Close()
	w := &waiter{c: &client{base: srv.URL}, poll: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := w.now(ctx, "b")
	if err != nil || !w.fallback || s.Counts["finished"] != 0 || s.Counts["total"] != 2 {
		t.Fatalf("now: %v %v %+v", err, w.fallback, s)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		o.finish("b-001", "PASS")
	}()
	v, err := w.until(ctx, wait.KindBatch, "b", "finished>0", 0)
	if err != nil || !v.Met || len(v.New) != 1 || v.New[0].ID != "b-001" {
		t.Fatalf("next: %v %+v", err, v)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		o.finish("b-002", "FAIL")
	}()
	v, err = w.until(ctx, wait.KindBatch, "b", "", -1)
	if err != nil || !v.Met || v.Counts["total"] != 2 || v.Counts["fail"] != 1 {
		t.Fatalf("done: %v %+v", err, v)
	}
	v, err = w.until(ctx, wait.KindBatch, "b", "pass>=2", -1)
	if err != nil || v.Met || !v.Over || ended(v) != exitCode(waitOver) {
		t.Fatalf("over: %v %+v", err, v)
	}
	// A timeout ends the wait with the last state, exit 4.
	short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	o.runs = append(o.runs, daemon.RunView{ID: "b-003", State: "queued"})
	v, err = w.until(short, wait.KindBatch, "b", "", -1)
	if err != nil || v.Met || v.Over || ended(v) != exitCode(waitTimeout) || v.Counts["queued"] != 1 || v.Until != "done" {
		t.Fatalf("timeout: %v %+v", err, v)
	}
}

// A daemon that does not answer is waited for; the wait goes on once it is
// back.
func TestWaitDaemonAway(t *testing.T) {
	o := &oldDaemon{runs: []daemon.RunView{{ID: "b-001", State: "finished", Verdict: "PASS", EndedAt: time.Now()}}}
	srv := httptest.NewUnstartedServer(o)
	addr := srv.Listener.Addr().String()
	w := &waiter{c: &client{base: "http://" + addr}, poll: 10 * time.Millisecond, told: true}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		srv.Start()
	}()
	defer srv.Close()
	v, err := w.until(ctx, wait.KindBatch, "b", "", -1)
	if err != nil || !v.Met {
		t.Fatalf("%v %+v", err, v)
	}
}
