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
}

// loadControl reads the control file once. A missing file is an empty one.
// It is called with d.mu held.
func (d *Daemon) loadControl() {
	d.ctl = control{Drained: map[string]time.Time{}, Held: map[string]time.Time{}, Retries: map[string]string{}, Canary: map[string]*CanaryRecord{}, Campaigns: map[string]*CampaignState{}}
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
	if _, ok := d.ctl.Drained[it.Lane]; ok {
		return "lane drained"
	}
	if _, ok := d.ctl.Held[it.Batch]; ok && it.Batch != "" {
		return "batch paused"
	}
	return ""
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

// LaneControl is the answer to draining or resuming a lane.
type LaneControl struct {
	Lane    string `json:"lane"`
	Drained bool   `json:"drained"`
	// InFlight is the run that finishes before a drained lane stops.
	InFlight string `json:"in_flight,omitempty"`
}

func (d *Daemon) handleLane(drain bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("lane")
		if _, ok := d.Local.Lanes[name]; !ok {
			fail(w, http.StatusNotFound, "there is no lane %s", name)
			return
		}
		d.mu.Lock()
		if drain {
			if _, ok := d.ctl.Drained[name]; !ok {
				d.ctl.Drained[name] = time.Now().UTC()
			}
		} else {
			delete(d.ctl.Drained, name)
		}
		err := d.saveControl()
		out := LaneControl{Lane: name, Drained: drain}
		if f := d.flights[name]; f != nil {
			out.InFlight = f.runID
		}
		d.mu.Unlock()
		if err != nil {
			fail(w, http.StatusInternalServerError, "the control file: %v", err)
			return
		}
		d.Log.Info("lane control", "lane", name, "drained", drain)
		d.poke()
		reply(w, http.StatusOK, out)
	}
}

// BatchControl is the answer to pausing, resuming or stopping a batch.
type BatchControl struct {
	Batch string `json:"batch"`
	State string `json:"state"` // paused, resumed, stopped
	// Queued are the batch's runs still waiting; Stopped those taken off
	// the queue.
	Queued  []string `json:"queued,omitempty"`
	Stopped []string `json:"stopped,omitempty"`
	// InFlight is the batch's run in flight and what was done to it.
	InFlight string `json:"in_flight,omitempty"`
	Action   string `json:"action,omitempty"`
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
	out := BatchControl{Batch: batch, State: "paused", Queued: ids(queued)}
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
	if req.Abort {
		cause = run.ErrOperatorAbort
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
	ctx := r.Context()
	var req struct {
		DryRun bool `json:"dry_run"`
		// Verdicts narrows a batch's retry to these verdicts, each of them
		// retryable; a cell's resume retries ABORTED and SKIPPED only.
		Verdicts []store.Verdict `json:"verdicts"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	var recs []store.Run
	out := RetryView{Retries: map[string]string{}, Refused: map[string]string{}, DryRun: req.DryRun}
	if id := r.PathValue("id"); id != "" {
		rec, err := d.State.GetRun(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "there is no finished run %s", id)
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		recs = []store.Run{*rec}
	} else {
		out.Batch = r.PathValue("batch")
		list, err := d.State.ListRuns(ctx, store.RunFilter{Batch: out.Batch})
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		if len(list) == 0 {
			fail(w, http.StatusNotFound, "batch %s has no finished run", out.Batch)
			return
		}
		for _, rec := range list {
			if retryable[rec.Verdict] && (len(req.Verdicts) == 0 || slices.Contains(req.Verdicts, rec.Verdict)) {
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
			fail(w, http.StatusConflict, "batch %s has no run to retry: none is SKIPPED, ABORTED or INVALID", out.Batch)
			return
		}
		var why []string
		for old, reason := range out.Refused {
			why = append(why, old+": "+reason)
		}
		sort.Strings(why)
		fail(w, http.StatusConflict, "nothing to retry; %s", strings.Join(why, "; "))
		return
	}
	if req.DryRun {
		reply(w, http.StatusOK, out)
		return
	}
	d.mu.Lock()
	for nu, old := range out.Retries {
		d.ctl.Retries[nu] = old
	}
	err := d.saveControl()
	d.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the control file: %v", err)
		return
	}
	if err := d.State.Enqueue(ctx, items); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	d.Log.Info("runs retried", "batch", out.Batch, "runs", len(items))
	d.poke()
	reply(w, http.StatusAccepted, out)
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
