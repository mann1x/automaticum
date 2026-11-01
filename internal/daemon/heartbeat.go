package daemon

import (
	"context"
	"net/http"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

// heartbeatEvery is 050 §6's "every ten minutes".
const heartbeatEvery = 10 * time.Minute

// Heartbeat is the daemon's counts at a moment (050 §6): its lanes by
// state, the runs in flight and queued, and the runs that ended since the
// heartbeat before, by verdict.
type Heartbeat struct {
	At       time.Time      `json:"at"`
	Since    time.Time      `json:"since"`
	Lanes    map[string]int `json:"lanes"` // by state
	Running  int            `json:"running"`
	Queued   int            `json:"queued"`
	Ended    map[string]int `json:"ended"` // since Since, by verdict
	Draining bool           `json:"draining,omitempty"`
}

// heartbeatLoop logs a heartbeat every HeartbeatEvery (ten minutes unless
// set) and keeps the last one for GET /v1/heartbeat and `status`.
func (d *Daemon) heartbeatLoop(ctx context.Context) {
	every := d.HeartbeatEvery
	if every == 0 {
		every = heartbeatEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h, err := d.beat(ctx)
		if err != nil {
			d.Log.Error("the heartbeat", "error", err)
			continue
		}
		d.mu.Lock()
		d.lastBeat = &h
		d.mu.Unlock()
		d.Log.Info("heartbeat", "lanes", h.Lanes, "running", h.Running, "queued", h.Queued, "ended", h.Ended, "draining", h.Draining)
	}
}

// beat counts now; the runs ended are those since the last heartbeat, or
// since the daemon started.
func (d *Daemon) beat(ctx context.Context) (Heartbeat, error) {
	st, err := d.status(ctx)
	if err != nil {
		return Heartbeat{}, err
	}
	d.mu.Lock()
	since := d.started
	if d.lastBeat != nil {
		since = d.lastBeat.At
	}
	d.mu.Unlock()
	h := Heartbeat{At: time.Now().UTC(), Since: since.UTC(), Lanes: map[string]int{}, Ended: map[string]int{}, Draining: st.Draining}
	for _, l := range st.Lanes {
		h.Lanes[l.State]++
	}
	for _, q := range st.Queue {
		if q.Running {
			h.Running++
		} else {
			h.Queued++
		}
	}
	// A run that ended in the window started at most its limits before;
	// two days covers every limit the harness sets.
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Since: since.Add(-48 * time.Hour)})
	if err != nil {
		return h, err
	}
	for _, r := range recs {
		if r.EndedAt.After(since) && !r.EndedAt.After(h.At) {
			h.Ended[string(r.Verdict)]++
		}
	}
	return h, nil
}

// handleHeartbeat gives the last heartbeat, or with ?now=1 the counts now
// (which does not move the window of the next one).
func (d *Daemon) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("now") == "1" {
		h, err := d.beat(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		reply(w, http.StatusOK, h)
		return
	}
	d.mu.Lock()
	h := d.lastBeat
	d.mu.Unlock()
	if h == nil {
		fail(w, http.StatusNotFound, "no heartbeat yet: the first comes ten minutes after the start; ask with ?now=1")
		return
	}
	reply(w, http.StatusOK, h)
}
