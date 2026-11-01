package daemon

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/wait"
)

// WaitView is the answer to GET /v1/wait/{kind}: the state followed, whether
// the condition holds, whether it never will, and the runs that finished
// after the first `seen` of them.
type WaitView struct {
	wait.Snapshot
	Until string `json:"until"`
	Met   bool   `json:"met"`
	// Over says that what is followed has ended without the condition
	// holding, so it never will.
	Over bool `json:"over,omitempty"`
	// New are the finished runs past the first Seen, in the order they
	// ended; asked with ?seen=N.
	Seen int        `json:"seen,omitempty"`
	New  []wait.Run `json:"new,omitempty"`
}

// The longest a request is held before it is answered with the condition
// not yet met; a client asks again. The check is also made this often when
// nothing pokes the daemon.
const (
	waitHoldMax = 55 * time.Second
	waitRecheck = 2 * time.Second
)

// errNoSuch is a wait for something the daemon does not know.
var errNoSuch = errors.New("not known")

// handleWait holds the request until the condition holds, what is followed
// ends without it, or the hold runs out (?hold=, at most 55 s). It blocks in
// the daemon so that a caller, an assistant first of all, does not poll
// (090 §5).
func (d *Daemon) handleWait(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind, id := r.PathValue("kind"), r.URL.Query().Get("id")
	if id == "" && kind != wait.KindIdle {
		fail(w, http.StatusBadRequest, "a wait for a %s needs ?id=", kind)
		return
	}
	cond, err := wait.Parse(kind, r.URL.Query().Get("until"))
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	seen := -1
	if v := r.URL.Query().Get("seen"); v != "" {
		if seen, err = strconv.Atoi(v); err != nil || seen < 0 {
			fail(w, http.StatusBadRequest, "seen is a count of runs, not %q", v)
			return
		}
	}
	hold := waitHoldMax
	if v := r.URL.Query().Get("hold"); v != "" {
		h, err := time.ParseDuration(v)
		if err != nil || h < 0 {
			fail(w, http.StatusBadRequest, "hold is a duration such as 30s, not %q", v)
			return
		}
		hold = min(h, waitHoldMax)
	}
	deadline := time.NewTimer(hold)
	defer deadline.Stop()
	for {
		changed := d.change()
		s, err := d.waitSnapshot(ctx, kind, id)
		if errors.Is(err, errNoSuch) {
			fail(w, http.StatusNotFound, "there is no %s %s", kind, id)
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		v := WaitView{Snapshot: s, Until: cond.String(), Met: cond.Eval(s)}
		v.Over = !v.Met && s.Over()
		if seen >= 0 {
			v.Seen = seen
			if seen < len(s.Finished) {
				v.New = s.Finished[seen:]
			}
		}
		if v.Met || v.Over {
			reply(w, http.StatusOK, v)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			reply(w, http.StatusOK, v)
			return
		case <-changed:
		case <-time.After(waitRecheck):
		}
	}
}

// waitSnapshot reads what a wait follows, now.
func (d *Daemon) waitSnapshot(ctx context.Context, kind, id string) (wait.Snapshot, error) {
	switch kind {
	case wait.KindRun:
		rec, err := d.State.GetRun(ctx, id)
		if err == nil {
			return wait.Runs(kind, id, []wait.Run{waitRun(view(rec, false))}), nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return wait.Snapshot{}, err
		}
		queue, err := d.State.Queue(ctx)
		if err != nil {
			return wait.Snapshot{}, err
		}
		for _, it := range queue {
			if it.RunID == id {
				return wait.Runs(kind, id, []wait.Run{waitRun(d.queuedView(it))}), nil
			}
		}
		return wait.Snapshot{}, errNoSuch
	case wait.KindBatch, wait.KindCell:
		runs, err := d.batchRuns(ctx, id)
		if err != nil {
			return wait.Snapshot{}, err
		}
		if len(runs) == 0 {
			return wait.Snapshot{}, errNoSuch
		}
		return wait.Runs(kind, id, runs), nil
	case wait.KindCampaign:
		d.mu.Lock()
		c := d.ctl.Campaigns[id]
		var cs CampaignState
		if c != nil {
			cs = c.snapshot()
		}
		d.mu.Unlock()
		if c == nil {
			return wait.Snapshot{}, errNoSuch
		}
		var states []string
		for _, b := range cs.Batches {
			states = append(states, b.State)
		}
		return wait.Campaign(id, cs.State, cs.Reason, states), nil
	case wait.KindLane, wait.KindIdle:
		st, err := d.status(ctx)
		if err != nil {
			return wait.Snapshot{}, err
		}
		return statusSnapshot(st, kind, id)
	}
	return wait.Snapshot{}, errNoSuch
}

// statusSnapshot is a lane's, or the daemon's, snapshot from its status.
func statusSnapshot(st Status, kind, lane string) (wait.Snapshot, error) {
	running, queued, laneQueued := 0, 0, 0
	for _, q := range st.Queue {
		if q.Running {
			continue
		}
		queued++
		if q.Lane == lane {
			laneQueued++
		}
	}
	for _, l := range st.Lanes {
		inFlight := l.State == "running" || l.State == "paused"
		if inFlight {
			running++
		}
		if kind == wait.KindLane && l.Lane == lane {
			return wait.Lane(lane, l.State, inFlight, laneQueued), nil
		}
	}
	if kind == wait.KindLane {
		return wait.Snapshot{}, errNoSuch
	}
	return wait.Idle(running, queued), nil
}

// StatusSnapshot is statusSnapshot for a client that reads GET /v1/status
// from a daemon that does not wait itself.
func StatusSnapshot(st Status, kind, lane string) (wait.Snapshot, bool) {
	s, err := statusSnapshot(st, kind, lane)
	return s, err == nil
}

// batchRuns are a batch's runs: queued, in flight and finished. A run is
// written before it is taken off the queue, so a run found in both is
// finished.
func (d *Daemon) batchRuns(ctx context.Context, batch string) ([]wait.Run, error) {
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return nil, err
	}
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: batch})
	if err != nil {
		return nil, err
	}
	var out []wait.Run
	done := map[string]bool{}
	for i := range recs {
		out = append(out, waitRun(view(&recs[i], false)))
		done[recs[i].ID] = true
	}
	for _, it := range queue {
		if it.Batch == batch && !done[it.RunID] {
			out = append(out, waitRun(d.queuedView(it)))
		}
	}
	return out, nil
}

// WaitRun is what a wait reads of a run's view.
func WaitRun(v RunView) wait.Run { return waitRun(v) }

func waitRun(v RunView) wait.Run {
	return wait.Run{ID: v.ID, State: v.State, Verdict: v.Verdict, Reason: v.VerdictReason, Task: v.Task, Arm: v.Arm,
		Lane: v.Lane, Batch: v.Batch, RetryOf: v.RetryOf, WallMS: v.WallMS, Started: v.StartedAt, Ended: v.EndedAt}
}
