package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// control is what the operator asked of lanes and batches, beyond one run
// (050 §4): lanes drained, batches held, runs retried. It is kept in a small
// file next to the database, so a restart forgets none of it.
type control struct {
	// Drained lanes finish the run in flight and start nothing else.
	Drained map[string]time.Time `json:"drained,omitempty"`
	// Held batches start none of their queued runs.
	Held map[string]time.Time `json:"held,omitempty"`
	// Retries names, for each run made by a retry, the run it retries.
	Retries map[string]string `json:"retries,omitempty"`
	// Canary holds the canary's verdict on each batch it judged.
	Canary map[string]*CanaryRecord `json:"canary,omitempty"`
	// Campaigns are the campaigns started, by name.
	Campaigns map[string]*CampaignState `json:"campaigns,omitempty"`
	// Cells are the mode C cells' endpoints, by batch (065 §3.1).
	Cells map[string]*CellSpec `json:"cells,omitempty"`
	// Capture names the batches whose runs save every model request and
	// response the SDK sends and reads (D104): opt-in, off by default.
	Capture map[string]bool `json:"capture,omitempty"`
	// Heldout names the batches started held out (D115): their runs are
	// exported only when the export asks for held-out runs by name.
	Heldout map[string]bool `json:"heldout,omitempty"`
	// Limits are the batches' own limits (D107), in place of the arm's and
	// the task's, for the runs that start after they are set.
	Limits map[string]*run.Override `json:"limits,omitempty"`
	// Quarantined lanes start nothing until resumed, with the reason they
	// were taken out (050 §5).
	Quarantined map[string]Quarantine `json:"quarantined,omitempty"`
	// InfraStreak are, per lane, the runs in a row that ended for the
	// harness's or the server's reason (D124).
	InfraStreak map[string][]InfraEnd `json:"infra_streak,omitempty"`
	// StartedBy is the client that started each batch (010 §5).
	StartedBy map[string]string `json:"started_by,omitempty"`
	// Priority is each batch's priority, where not 0 (D125).
	Priority map[string]int `json:"priority,omitempty"`
	// Reserved are the lanes reserved for a batch or a campaign until a
	// time (D125).
	Reserved map[string]Reservation `json:"reserved,omitempty"`
	// LaneHeld are the lanes paused with their run in flight held, so a
	// resume releases it.
	LaneHeld map[string]time.Time `json:"lane_held,omitempty"`
	// CellBatches are the batches started by `cell run`, with the suite
	// (and subset) asked.
	CellBatches map[string]string `json:"cell_batches,omitempty"`
	// Idem are the kept answers of requests made with an idempotency key.
	Idem map[string]*IdemEntry `json:"idempotency,omitempty"`
	// Rescores are the cells' rescores, by batch (D121).
	Rescores map[string]*RescoreJob `json:"rescores,omitempty"`
	// Cohorts are the declared cohorts, by name (D120).
	Cohorts map[string]*Cohort `json:"cohorts,omitempty"`
}

// Reservation is a lane kept for one batch or campaign until a time.
type Reservation struct {
	For   string    `json:"for"`
	Until time.Time `json:"until"`
	At    time.Time `json:"at"`
}

// Quarantine is why and since when a lane is out of use.
type Quarantine struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// loadControl reads the control file once. A missing file is an empty one.
// It is called with d.mu held.
func (d *Daemon) loadControl() {
	d.ctl = control{Drained: map[string]time.Time{}, Held: map[string]time.Time{}, Retries: map[string]string{}, Canary: map[string]*CanaryRecord{}, Campaigns: map[string]*CampaignState{}, Cells: map[string]*CellSpec{}, Capture: map[string]bool{}, Heldout: map[string]bool{}, Limits: map[string]*run.Override{}, Quarantined: map[string]Quarantine{}, LaneHeld: map[string]time.Time{}, InfraStreak: map[string][]InfraEnd{}, Priority: map[string]int{}, StartedBy: map[string]string{}, Reserved: map[string]Reservation{}, CellBatches: map[string]string{}, Idem: map[string]*IdemEntry{}, Rescores: map[string]*RescoreJob{}, Cohorts: map[string]*Cohort{}}
	if d.ControlFile == "" {
		return
	}
	b, err := os.ReadFile(d.ControlFile)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(b, &d.ctl)
	}
	if err != nil {
		// Starting with lanes undrained would run what the operator stopped.
		d.Log.Error("the control file cannot be read; every lane is drained until it is mended", "file", d.ControlFile, "error", err)
		for name := range d.Local.Lanes {
			d.ctl.Drained[name] = time.Now().UTC()
		}
	}
	for _, m := range []*map[string]time.Time{&d.ctl.Drained, &d.ctl.Held} {
		if *m == nil {
			*m = map[string]time.Time{}
		}
	}
	if d.ctl.Retries == nil {
		d.ctl.Retries = map[string]string{}
	}
	if d.ctl.Canary == nil {
		d.ctl.Canary = map[string]*CanaryRecord{}
	}
	if d.ctl.Campaigns == nil {
		d.ctl.Campaigns = map[string]*CampaignState{}
	}
	if d.ctl.Capture == nil {
		d.ctl.Capture = map[string]bool{}
	}
	if d.ctl.Heldout == nil {
		d.ctl.Heldout = map[string]bool{}
	}
	if d.ctl.Limits == nil {
		d.ctl.Limits = map[string]*run.Override{}
	}
	if d.ctl.Cells == nil {
		d.ctl.Cells = map[string]*CellSpec{}
	}
	if d.ctl.Quarantined == nil {
		d.ctl.Quarantined = map[string]Quarantine{}
	}
	if d.ctl.LaneHeld == nil {
		d.ctl.LaneHeld = map[string]time.Time{}
	}
	if d.ctl.InfraStreak == nil {
		d.ctl.InfraStreak = map[string][]InfraEnd{}
	}
	if d.ctl.Priority == nil {
		d.ctl.Priority = map[string]int{}
	}
	if d.ctl.StartedBy == nil {
		d.ctl.StartedBy = map[string]string{}
	}
	if d.ctl.Cohorts == nil {
		d.ctl.Cohorts = map[string]*Cohort{}
	}
	if d.ctl.Rescores == nil {
		d.ctl.Rescores = map[string]*RescoreJob{}
	}
	if d.ctl.Reserved == nil {
		d.ctl.Reserved = map[string]Reservation{}
	}
	if d.ctl.Idem == nil {
		d.ctl.Idem = map[string]*IdemEntry{}
	}
	if d.ctl.CellBatches == nil {
		d.ctl.CellBatches = map[string]string{}
	}
	for batch, c := range d.ctl.Cells {
		if err := d.addCell(batch, c); err != nil {
			d.Log.Error("a cell's endpoint cannot be registered; its runs will be skipped", "batch", batch, "error", err)
		}
	}
}

// saveControl writes the control file whole. It is called with d.mu held.
func (d *Daemon) saveControl() error {
	if d.ControlFile == "" {
		return nil
	}
	b, err := json.MarshalIndent(d.ctl, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.ControlFile + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.ControlFile)
}

// stopped says why a queued run may not start now, or "" when it may.
// It is called with d.mu held.
func (d *Daemon) stopped(it store.QueueItem) string {
	if q, ok := d.ctl.Quarantined[it.Lane]; ok {
		return "lane quarantined: " + q.Reason
	}
	if _, ok := d.ctl.Drained[it.Lane]; ok {
		return "lane drained"
	}
	if _, ok := d.ctl.Held[it.Batch]; ok && it.Batch != "" {
		return "batch paused"
	}
	if r, ok := d.ctl.Reserved[it.Lane]; ok && time.Now().Before(r.Until) && it.Batch != r.For && d.campaignOf(it.Batch) != r.For {
		return fmt.Sprintf("lane reserved for %s until %s", r.For, r.Until.Format(time.RFC3339))
	}
	return ""
}

// releaseHeld lets the runs in flight on the lanes (every lane when none
// is named) go on if they are held by `run pause`, `batch pause` or `lane
// pause`, so that a drain ends with their verdicts and never waits on a
// hold (D119). A lane paused is drained only from then on. It is called
// without d.mu held, and gives the runs that were held or asked to hold.
func (d *Daemon) releaseHeld(lanes ...string) []string {
	h, ok := d.Runner.(holder)
	if !ok {
		return nil
	}
	d.mu.Lock()
	if len(lanes) == 0 {
		for name := range d.flights {
			lanes = append(lanes, name)
		}
	}
	type target struct {
		f      *flight
		asked  bool
		laneOf string
	}
	var ts []target
	for _, name := range lanes {
		f := d.flights[name]
		if f == nil {
			continue
		}
		_, lanePaused := d.ctl.LaneHeld[name]
		_, batchPaused := d.ctl.Held[f.item.Batch]
		ts = append(ts, target{f, lanePaused || batchPaused && f.item.Batch != "", name})
		delete(d.ctl.LaneHeld, name)
	}
	if err := d.saveControl(); err != nil {
		d.Log.Error("the control file", "error", err)
	}
	d.mu.Unlock()
	var out []string
	for _, t := range ts {
		held, _ := t.f.holding()
		// Release is a no-op on a run that is not held, and also takes
		// back a hold asked for that the runner has not reached yet.
		if err := h.Release(t.f.runID); err != nil {
			d.Log.Error("a drain could not release a held run", "run", t.f.runID, "error", err)
			continue
		}
		if held || t.asked {
			d.Log.Info("a drain released a held run; it finishes", "run", t.f.runID, "lane", t.laneOf)
			out = append(out, t.f.runID)
		}
	}
	return out
}

// asked is what a run was asked to be, kept in the record of a run that
// never started, so that it can be retried.
type asked struct {
	Arm  string `json:"arm"`
	Task string `json:"task"`
	Lane string `json:"lane"`
}

func askedProvenance(it store.QueueItem) json.RawMessage {
	b, _ := json.Marshal(map[string]asked{"asked": {Arm: it.Arm, Task: it.Task, Lane: it.Lane}})
	return b
}

// LaneControl is the answer to a lane's control.
type LaneControl struct {
	Lane    string `json:"lane"`
	State   string `json:"state"` // drained, paused, quarantined, resumed, aborting
	Drained bool   `json:"drained"`
	// InFlight is the run in flight on the lane, and Action what was, or
	// would be, done to it.
	InFlight string `json:"in_flight,omitempty"`
	Action   string `json:"action,omitempty"`
	Reason   string `json:"reason,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
}

// handleLane controls a lane (050 §4):
//   - drain: the run in flight finishes, then nothing starts;
//   - pause: drained, and the run in flight held before its next model
//     request with the endpoint freed of its model (unless keep_model);
//   - quarantine: drained with a reason, shown as quarantined (050 §5);
//   - abort: the run in flight is aborted; the lane is otherwise left as it
//     is;
//   - resume: drained, paused or quarantined no more, a held run released.
func (d *Daemon) handleLane(verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("lane")
		if _, ok := d.Local.Lanes[name]; !ok {
			fail(w, http.StatusNotFound, "there is no lane %s", name)
			return
		}
		var req struct {
			KeepModel bool   `json:"keep_model"`
			Reason    string `json:"reason"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		req.Reason = strings.TrimSpace(req.Reason)
		if verb == "quarantine" && req.Reason == "" {
			fail(w, http.StatusBadRequest, "a quarantine needs a reason")
			return
		}
		d.mu.Lock()
		f := d.flights[name]
		_, lanePaused := d.ctl.LaneHeld[name]
		batchPaused := false
		if f != nil && f.item.Batch != "" {
			_, batchPaused = d.ctl.Held[f.item.Batch]
		}
		d.mu.Unlock()
		out := LaneControl{Lane: name, DryRun: dryRun(r), Reason: req.Reason}
		if f != nil {
			out.InFlight = f.runID
		}
		h, canHold := d.Runner.(holder)
		switch verb {
		case "drain", "quarantine":
			out.State, out.Drained = "drained", true
			if verb == "quarantine" {
				out.State = "quarantined"
			}
			if f != nil {
				out.Action = "finishes, then nothing else starts"
				if held, _ := f.holding(); (held || lanePaused || batchPaused) && canHold {
					out.Action = "released from its hold; " + out.Action
				}
			}
		case "pause":
			out.State, out.Drained = "paused", true
			switch {
			case f == nil:
			case !canHold:
				out.Action = "this daemon cannot hold a run; it finishes"
			default:
				if held, _ := f.holding(); held {
					out.Action = "already held"
				} else {
					out.Action = "held before its next model request"
					if !req.KeepModel {
						out.Action += ", then the endpoint freed of its model"
					}
				}
			}
		case "abort":
			if f == nil {
				fail(w, http.StatusNotFound, "lane %s has no run in flight", name)
				return
			}
			out.State, out.Action = "aborting", "aborted; the lane is otherwise left as it is"
			d.mu.Lock()
			_, out.Drained = d.ctl.Drained[name]
			d.mu.Unlock()
		case "resume":
			out.State = "resumed"
			if f != nil && lanePaused && canHold {
				out.Action = "released; its next request loads the model again"
			}
		}
		if out.DryRun {
			reply(w, http.StatusOK, out)
			return
		}
		if verb == "abort" {
			d.Abort(f.runID)
			reply(w, http.StatusAccepted, out)
			return
		}
		if verb == "pause" && f != nil && canHold && out.Action != "already held" {
			if err := h.Hold(f.runID); err != nil {
				out.Action = "not held: " + err.Error()
			} else {
				go d.afterHeld(f, !req.KeepModel)
			}
		}
		if verb == "resume" && f != nil && lanePaused && canHold {
			if err := h.Release(f.runID); err != nil {
				out.Action = "not released: " + err.Error()
			}
		}
		if verb == "drain" || verb == "quarantine" {
			if released := d.releaseHeld(name); len(released) > 0 && !strings.HasPrefix(out.Action, "released") {
				out.Action = "released from its hold; " + out.Action
			}
		}
		now := time.Now().UTC()
		d.mu.Lock()
		switch verb {
		case "drain", "pause", "quarantine":
			if _, ok := d.ctl.Drained[name]; !ok {
				d.ctl.Drained[name] = now
			}
			if verb == "pause" {
				d.ctl.LaneHeld[name] = now
			}
			if verb == "quarantine" {
				d.ctl.Quarantined[name] = Quarantine{At: now, Reason: req.Reason}
			}
		case "resume":
			delete(d.ctl.Drained, name)
			delete(d.ctl.LaneHeld, name)
			delete(d.ctl.Quarantined, name)
			delete(d.ctl.InfraStreak, name)
		}
		err := d.saveControl()
		d.mu.Unlock()
		if err != nil {
			fail(w, http.StatusInternalServerError, "the control file: %v", err)
			return
		}
		d.Log.Info("lane control", "lane", name, "verb", verb, "reason", req.Reason)
		d.poke()
		reply(w, http.StatusOK, out)
	}
}

// BatchControl is the answer to pausing, resuming or stopping a batch.
type BatchControl struct {
	Batch string `json:"batch"`
	State string `json:"state"` // paused, resumed, stopped, limits set
	// Queued are the batch's runs still waiting; Stopped those taken off
	// the queue.
	Queued  []string `json:"queued,omitempty"`
	Stopped []string `json:"stopped,omitempty"`
	// InFlight is the batch's run in flight and what was done to it.
	InFlight string `json:"in_flight,omitempty"`
	Action   string `json:"action,omitempty"`
	// Limits are the batch's own limits after `batch limits`.
	Limits *run.Override `json:"limits,omitempty"`
	DryRun bool          `json:"dry_run,omitempty"`
}

// batchFlight is the batch's run in flight, if it has one.
func (d *Daemon) batchFlight(batch string) *flight {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, f := range d.flights {
		if f.item.Batch == batch {
			return f
		}
	}
	return nil
}

// batchQueued lists the batch's queued runs that are not in flight.
func (d *Daemon) batchQueued(r *http.Request, batch string, inFlight *flight) ([]store.QueueItem, error) {
	queue, err := d.State.Queue(r.Context())
	if err != nil {
		return nil, err
	}
	var out []store.QueueItem
	for _, it := range queue {
		if it.Batch == batch && (inFlight == nil || it.RunID != inFlight.runID) {
			out = append(out, it)
		}
	}
	return out, nil
}

// handleBatchPause holds a batch: none of its queued runs starts, and its
// run in flight is paused as `run pause` does, the endpoint freed of its
// model unless keep_model is asked (D83).
func (d *Daemon) handleBatchPause(w http.ResponseWriter, r *http.Request) {
	batch := r.PathValue("batch")
	var req struct {
		KeepModel bool `json:"keep_model"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	f := d.batchFlight(batch)
	queued, err := d.batchQueued(r, batch, f)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if f == nil && len(queued) == 0 {
		fail(w, http.StatusNotFound, "batch %s has no run queued or in flight", batch)
		return
	}
	out := BatchControl{Batch: batch, State: "paused", Queued: ids(queued), DryRun: dryRun(r)}
	if out.DryRun {
		if f != nil {
			out.InFlight, out.Action = f.runID, "held before its next model request"
			if held, _ := f.holding(); held {
				out.Action = "already held"
			} else if !req.KeepModel {
				out.Action += ", then the endpoint freed of its model"
			}
		}
		reply(w, http.StatusOK, out)
		return
	}
	if f != nil {
		out.InFlight = f.runID
		h, ok := d.Runner.(holder)
		if held, _ := f.holding(); held {
			out.Action = "already held"
		} else if !ok {
			out.Action = "this daemon cannot hold a run; it finishes"
		} else if err := h.Hold(f.runID); err != nil {
			out.Action = "not held: " + err.Error()
		} else {
			out.Action = "held before its next model request"
			if !req.KeepModel {
				out.Action += ", then the endpoint freed of its model"
			}
			go d.afterHeld(f, !req.KeepModel)
		}
	}
	d.mu.Lock()
	if _, ok := d.ctl.Held[batch]; !ok {
		d.ctl.Held[batch] = time.Now().UTC()
	}
	err = d.saveControl()
	d.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the control file: %v", err)
		return
	}
	d.Log.Info("batch paused", "batch", batch, "queued", len(queued), "in_flight", out.InFlight)
	reply(w, http.StatusAccepted, out)
}

// handleBatchLimits sets a batch's own limits (D107) for its runs that have
// not started; the run in flight keeps the limits it started with. A field
// left out keeps what was set before.
func (d *Daemon) handleBatchLimits(w http.ResponseWriter, r *http.Request) {
	batch := r.PathValue("batch")
	var req run.Override
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the request is not understood: %v", err)
		return
	}
	if err := req.Check(); err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	if req.Empty() {
		fail(w, http.StatusBadRequest, "give a wall limit, an idle limit or both")
		return
	}
	f := d.batchFlight(batch)
	queued, err := d.batchQueued(r, batch, f)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if len(queued) == 0 {
		fail(w, http.StatusNotFound, "batch %s has no queued run to set limits for", batch)
		return
	}
	d.mu.Lock()
	o := d.ctl.Limits[batch]
	if o == nil {
		o = &run.Override{}
	}
	merged := *o
	if req.WallS != nil {
		merged.WallS = req.WallS
	}
	if req.IdleS != nil {
		merged.IdleS = req.IdleS
	}
	if dryRun(r) {
		d.mu.Unlock()
		out := BatchControl{Batch: batch, State: "limits set", Queued: ids(queued), Limits: &merged, DryRun: true}
		if f != nil {
			out.InFlight, out.Action = f.runID, "keeps the limits it started with"
		}
		reply(w, http.StatusOK, out)
		return
	}
	d.ctl.Limits[batch] = &merged
	err = d.saveControl()
	d.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the control file: %v", err)
		return
	}
	out := BatchControl{Batch: batch, State: "limits set", Queued: ids(queued), Limits: &merged}
	if f != nil {
		out.InFlight = f.runID
		out.Action = "keeps the limits it started with"
	}
	d.Log.Info("batch limits set", "batch", batch, "queued", len(queued))
	reply(w, http.StatusAccepted, out)
}

// handleBatchResume lets a paused batch go on: its held run in flight is
// released and its queued runs may start again.
func (d *Daemon) handleBatchResume(w http.ResponseWriter, r *http.Request) {
	batch := r.PathValue("batch")
	f := d.batchFlight(batch)
	queued, err := d.batchQueued(r, batch, f)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	d.mu.Lock()
	_, wasHeld := d.ctl.Held[batch]
	if dryRun(r) {
		d.mu.Unlock()
		if !wasHeld && f == nil {
			fail(w, http.StatusNotFound, "batch %s is not paused", batch)
			return
		}
		out := BatchControl{Batch: batch, State: "resumed", Queued: ids(queued), DryRun: true}
		if f != nil {
			out.InFlight, out.Action = f.runID, "released; its next request loads the model again"
		}
		reply(w, http.StatusOK, out)
		return
	}
	delete(d.ctl.Held, batch)
	err = d.saveControl()
	d.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the control file: %v", err)
		return
	}
	out := BatchControl{Batch: batch, State: "resumed", Queued: ids(queued)}
	if f != nil {
		out.InFlight = f.runID
		if h, ok := d.Runner.(holder); ok {
			if err := h.Release(f.runID); err != nil {
				out.Action = "not released: " + err.Error()
			} else {
				out.Action = "released; its next request loads the model again"
			}
		}
	}
	if !wasHeld && f == nil {
		fail(w, http.StatusNotFound, "batch %s is not paused", batch)
		return
	}
	d.poke()
	reply(w, http.StatusAccepted, out)
}

// handleBatchStop takes every queued run of a batch off the queue, each
// recorded SKIPPED with that reason. Stopping is graceful: the run in
// flight finishes, unless abort is asked.
func (d *Daemon) handleBatchStop(w http.ResponseWriter, r *http.Request) {
	batch := r.PathValue("batch")
	var req struct {
		Abort bool `json:"abort"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	var cause error
	if req.Abort || strings.HasSuffix(r.URL.Path, "/abort") {
		cause = run.ErrOperatorAbort
	}
	if dryRun(r) {
		f := d.batchFlight(batch)
		queued, err := d.batchQueued(r, batch, f)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		if f == nil && len(queued) == 0 {
			fail(w, http.StatusNotFound, "batch %s has no run queued or in flight", batch)
			return
		}
		out := BatchControl{Batch: batch, State: "stopped", Stopped: ids(queued), DryRun: true}
		if f != nil {
			out.InFlight, out.Action = f.runID, "finishes, then the batch is over"
			if cause != nil {
				out.Action = "aborting"
			}
		}
		reply(w, http.StatusOK, out)
		return
	}
	out, err := d.stopBatch(r.Context(), batch, "batch stopped by the operator before it started", cause)
	switch {
	case errors.Is(err, errNoBatch):
		fail(w, http.StatusNotFound, "batch %s has no run queued or in flight", batch)
	case err != nil:
		fail(w, http.StatusInternalServerError, "%v", err)
	default:
		reply(w, http.StatusOK, out)
	}
}

var errNoBatch = errors.New("no run of the batch is queued or in flight")

// stopBatch records every queued run of a batch SKIPPED with reason and
// takes it off the queue. The run in flight is ended with cause when cause
// is not nil, and otherwise finishes.
func (d *Daemon) stopBatch(ctx context.Context, batch, reason string, cause error) (BatchControl, error) {
	out := BatchControl{Batch: batch, State: "stopped"}
	f := d.batchFlight(batch)
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return out, err
	}
	var queued []store.QueueItem
	for _, it := range queue {
		if it.Batch == batch && (f == nil || it.RunID != f.runID) {
			queued = append(queued, it)
		}
	}
	if f == nil && len(queued) == 0 {
		return out, errNoBatch
	}
	for _, it := range queued {
		now := time.Now().UTC()
		rec := &store.Run{ID: it.RunID, Batch: it.Batch, Lane: it.Lane, Verdict: store.Skipped,
			VerdictReason: reason, StartedAt: now, EndedAt: now, Provenance: askedProvenance(it)}
		if suite, tid, ok := strings.Cut(it.Task, "/"); ok {
			rec.Suite, rec.TaskID = suite, tid
		}
		if err := d.State.UpsertRun(ctx, rec); err != nil {
			return out, err
		}
		if err := d.State.Unqueue(ctx, it.RunID); err != nil {
			return out, err
		}
		out.Stopped = append(out.Stopped, it.RunID)
	}
	if f != nil {
		out.InFlight = f.runID
		switch {
		case cause != nil:
			d.mu.Lock()
			f.cancel(cause)
			d.mu.Unlock()
			out.Action = "aborting"
		default:
			out.Action = "finishes, then the batch is over"
			// A held run would wait for ever for a resume nobody gives.
			if held, _ := f.holding(); held {
				out.Action = "is held: resume it with `automaticum run resume " + f.runID + "` or end it with `automaticum run abort " + f.runID + "`"
			}
		}
	}
	d.mu.Lock()
	delete(d.ctl.Held, batch)
	err = d.saveControl()
	d.mu.Unlock()
	if err != nil {
		return out, fmt.Errorf("the control file: %w", err)
	}
	d.Log.Info("batch stopped", "batch", batch, "stopped", len(out.Stopped), "in_flight", out.InFlight, "reason", reason)
	detail := fmt.Sprintf("%s; %d queued runs taken off the queue", reason, len(out.Stopped))
	if out.InFlight != "" {
		detail += "; the run in flight " + out.InFlight + " " + out.Action
	}
	d.notify(ctx, Notification{Kind: "batch", Status: "stopped", Batch: batch, Run: out.InFlight, Detail: detail})
	d.poke()
	return out, nil
}

// retryable is the set of verdicts a retry may run again: those that are
// not a result of the arm on the task. A counted verdict (PASS, FAIL,
// TIMEOUT) is never run again, since a retry would replace a result.
var retryable = map[store.Verdict]bool{store.Skipped: true, store.Aborted: true, store.Invalid: true}

// RetryView is the answer to a retry: the new runs and what each retries.
type RetryView struct {
	Batch   string            `json:"batch,omitempty"`
	Retries map[string]string `json:"retries"` // new run -> run it retries
	// Refused says, for each run that is not retried, why.
	Refused map[string]string `json:"refused,omitempty"`
	DryRun  bool              `json:"dry_run,omitempty"`
}

// handleRetry queues a new run for one run (POST /v1/runs/{id}/retry) or
// for every retryable run of a batch (POST /v1/batches/{batch}/retry), with
// the same arm, task and lane, in the same batch. The old record stays as
// it is; the new run says which one it retries.
func (d *Daemon) handleRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DryRun bool `json:"dry_run"`
		// Verdicts narrows a batch's retry to these verdicts, each of them
		// retryable; a cell's resume retries ABORTED and SKIPPED only.
		Verdicts []store.Verdict `json:"verdicts"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	out, code, err := d.retry(r.Context(), r.PathValue("id"), r.PathValue("batch"), req.Verdicts, req.DryRun || dryRun(r))
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	reply(w, code, out)
}

// retry queues a new run for one run, or for every retryable run of a
// batch, and says how it went as an HTTP status.
func (d *Daemon) retry(ctx context.Context, id, batch string, verdicts []store.Verdict, dry bool) (RetryView, int, error) {
	var recs []store.Run
	out := RetryView{Retries: map[string]string{}, Refused: map[string]string{}, DryRun: dry}
	if id != "" {
		rec, err := d.State.GetRun(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return out, http.StatusNotFound, fmt.Errorf("there is no finished run %s", id)
		}
		if err != nil {
			return out, http.StatusInternalServerError, err
		}
		recs = []store.Run{*rec}
	} else {
		out.Batch = batch
		list, err := d.State.ListRuns(ctx, store.RunFilter{Batch: out.Batch})
		if err != nil {
			return out, http.StatusInternalServerError, err
		}
		if len(list) == 0 {
			return out, http.StatusNotFound, fmt.Errorf("batch %s has no finished run", out.Batch)
		}
		for _, rec := range list {
			if retryable[rec.Verdict] && (len(verdicts) == 0 || slices.Contains(verdicts, rec.Verdict)) {
				recs = append(recs, rec)
			}
		}
	}
	d.mu.Lock()
	retried := map[string]string{}
	for nu, old := range d.ctl.Retries {
		retried[old] = nu
	}
	d.mu.Unlock()
	now := time.Now().UTC()
	stem := run.NewID(now)
	var items []store.QueueItem
	for _, rec := range recs {
		why := ""
		switch {
		case !retryable[rec.Verdict]:
			why = fmt.Sprintf("%s is a result of the arm on the task; it is not run again", rec.Verdict)
		case rec.Imported:
			why = "the run was imported from the older harness"
		case retried[rec.ID] != "":
			why = "already retried by " + retried[rec.ID]
		}
		var it store.QueueItem
		if why == "" {
			var err error
			if it, err = askedOf(&rec); err != nil {
				why = err.Error()
			}
		}
		if why == "" {
			if _, ok := d.Local.Lanes[it.Lane]; !ok {
				why = "lane " + it.Lane + " is not in this installation"
			}
		}
		if why != "" {
			out.Refused[rec.ID] = why
			continue
		}
		it.RunID = stem
		if len(recs) > 1 {
			it.RunID = fmt.Sprintf("%s-%03d", stem, len(items)+1)
		}
		it.Batch, it.EnqueuedAt = rec.Batch, now
		items = append(items, it)
		out.Retries[it.RunID] = rec.ID
	}
	if len(items) == 0 {
		if len(recs) == 0 {
			return out, http.StatusConflict, fmt.Errorf("batch %s has no run to retry: none is SKIPPED, ABORTED or INVALID", out.Batch)
		}
		var why []string
		for old, reason := range out.Refused {
			why = append(why, old+": "+reason)
		}
		sort.Strings(why)
		return out, http.StatusConflict, fmt.Errorf("nothing to retry; %s", strings.Join(why, "; "))
	}
	if dry {
		return out, http.StatusOK, nil
	}
	d.mu.Lock()
	for nu, old := range out.Retries {
		d.ctl.Retries[nu] = old
	}
	err := d.saveControl()
	d.mu.Unlock()
	if err != nil {
		return out, http.StatusInternalServerError, fmt.Errorf("the control file: %w", err)
	}
	if err := d.State.Enqueue(ctx, items); err != nil {
		return out, http.StatusInternalServerError, err
	}
	d.Log.Info("runs retried", "batch", out.Batch, "runs", len(items))
	d.poke()
	return out, http.StatusAccepted, nil
}

// askedOf says which arm, task and lane a finished run was: from its
// resolved arm when it started, from what it was asked to be when it did
// not.
func askedOf(rec *store.Run) (store.QueueItem, error) {
	it := store.QueueItem{Lane: rec.Lane}
	if rec.Suite != "" {
		it.Task = rec.Suite + "/" + rec.TaskID
	}
	var arm struct {
		Name string `json:"name"`
	}
	if len(rec.Arm) > 0 && json.Unmarshal(rec.Arm, &arm) == nil {
		it.Arm = arm.Name
	}
	var prov struct {
		Asked *asked `json:"asked"`
	}
	if it.Arm == "" && len(rec.Provenance) > 0 && json.Unmarshal(rec.Provenance, &prov) == nil && prov.Asked != nil {
		it.Arm = prov.Asked.Arm
		if it.Task == "" {
			it.Task = prov.Asked.Task
		}
		if it.Lane == "" {
			it.Lane = prov.Asked.Lane
		}
	}
	switch {
	case it.Arm == "":
		return it, errors.New("the record does not say which arm it was")
	case it.Task == "":
		return it, errors.New("the record does not say which task it was")
	case it.Lane == "":
		return it, errors.New("the record does not say which lane it was on")
	}
	return it, nil
}

// retryLinks adds to a run's view the runs it retries and is retried by.
func (d *Daemon) retryLinks(v *RunView) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v.RetryOf = d.ctl.Retries[v.ID]
	for nu, old := range d.ctl.Retries {
		if old == v.ID {
			v.RetriedBy = nu
		}
	}
}

func ids(items []store.QueueItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.RunID)
	}
	sort.Strings(out)
	return out
}

// ControlFile is where a daemon whose data root is root keeps its control
// state.
func ControlFile(root string) string { return filepath.Join(root, "control.json") }
