package daemon

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

// runArm is what a run's arm says of itself: its id, model tag and
// endpoint, or why it cannot be read.
type runArm struct {
	ID       string
	Model    string
	Endpoint string
	Problem  string
}

// armCache holds the arm of each queued run's batch and arm name for one
// answer, so a long queue reads each arm file once.
type armCache map[string]runArm

// queuedArm is the arm of a run without a record: the one it started with
// while it runs, else the one it would start with now (fly reads the arm
// when the run starts).
func (d *Daemon) queuedArm(it store.QueueItem, c armCache) runArm {
	if f := d.flight(it.RunID); f != nil {
		f.mu.Lock()
		a := f.armed
		f.mu.Unlock()
		if a.ID != "" {
			return a
		}
	}
	key := it.Batch + "\x00" + it.Arm
	if a, ok := c[key]; ok {
		return a
	}
	var a runArm
	if arm, _, err := d.armFor(it.Batch, it.Arm); err != nil {
		a.Problem, _, _ = strings.Cut(err.Error(), "\n")
	} else {
		a = runArm{ID: arm.ID, Model: arm.Model.Model, Endpoint: arm.Endpoint}
	}
	c[key] = a
	return a
}

// queuedArmView is queuedView with the run's arm filled in.
func (d *Daemon) queuedArmView(it store.QueueItem, c armCache) RunView {
	v := d.queuedView(it)
	a := d.queuedArm(it, c)
	v.ArmID, v.Model, v.Endpoint = a.ID, a.Model, a.Endpoint
	return v
}

// ArmActivity is one arm with runs running or queued (GET /v1/arms). An
// arm whose file changed while a run of it was in flight is two entries:
// the run keeps the arm it started with, and the queued ones take the new.
type ArmActivity struct {
	Arm      string `json:"arm"`
	ArmID    string `json:"arm_id,omitempty"`
	Model    string `json:"model,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	// Problem is why the arm cannot be read now; its queued runs will be
	// skipped when they start unless it is fixed.
	Problem string `json:"problem,omitempty"`
	Running int    `json:"running"`
	Queued  int    `json:"queued"`
	// RunningRuns are the runs in flight and Lanes where they run.
	RunningRuns []string `json:"running_runs,omitempty"`
	Lanes       []string `json:"lanes,omitempty"`
	Batches     []string `json:"batches"`
	// Since is when the oldest of its runs was queued.
	Since time.Time `json:"since"`
	// Started is when the oldest of its runs in flight started.
	Started time.Time `json:"started,omitzero"`
}

// handleArms lists the arms with runs running or queued, those running
// first; ?running=1 keeps only the arms with a run in flight.
func (d *Daemon) handleArms(w http.ResponseWriter, r *http.Request) {
	queue, err := d.State.Queue(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	cache := armCache{}
	by := map[string]*ArmActivity{}
	batches := map[string]map[string]bool{}
	for _, it := range queue {
		a := d.queuedArm(it, cache)
		key := it.Arm + "\x00" + a.ID
		e := by[key]
		if e == nil {
			e = &ArmActivity{Arm: it.Arm, ArmID: a.ID, Model: a.Model, Endpoint: a.Endpoint, Problem: a.Problem, Since: it.EnqueuedAt}
			by[key], batches[key] = e, map[string]bool{}
		}
		if it.EnqueuedAt.Before(e.Since) {
			e.Since = it.EnqueuedAt
		}
		batches[key][it.Batch] = true
		if f := d.flight(it.RunID); f != nil {
			e.Running++
			e.RunningRuns = append(e.RunningRuns, it.RunID)
			e.Lanes = append(e.Lanes, it.Lane)
			if e.Started.IsZero() || f.started.Before(e.Started) {
				e.Started = f.started
			}
		} else {
			e.Queued++
		}
	}
	only := r.URL.Query().Get("running") == "1"
	out := []ArmActivity{}
	for key, e := range by {
		if only && e.Running == 0 {
			continue
		}
		e.Batches = sortedKeys(batches[key])
		sort.Strings(e.RunningRuns)
		sort.Strings(e.Lanes)
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Running > 0) != (out[j].Running > 0) {
			return out[i].Running > 0
		}
		if out[i].Arm != out[j].Arm {
			return out[i].Arm < out[j].Arm
		}
		return out[i].ArmID < out[j].ArmID
	})
	reply(w, http.StatusOK, out)
}
