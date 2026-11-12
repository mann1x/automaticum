package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/task"
)

// A rescore (D121, 065 §4) runs the task's current primary oracle again on
// each counted run's stored workspace. It is queued work on the lane that
// ran the run, one oracle at a time and never beside a run. Each answer is
// a rescore record stored beside the run, as blobs of kind "rescore" (the
// record) and "rescore_oracle" (the oracle's output), named by the time;
// the run's record and verdict stay as they were.

// RescoreJob is a cell's rescore, kept in the control file so a restart
// goes on with it.
type RescoreJob struct {
	Batch    string        `json:"batch"`
	Queued   time.Time     `json:"queued"`
	Started  time.Time     `json:"started,omitzero"`
	Finished time.Time     `json:"finished,omitzero"`
	Todo     []RescoreItem `json:"todo"`
	Done     []string      `json:"done"`
	// Failed are the runs whose oracle gave no answer, with why; their
	// verdict stands.
	Failed map[string]string `json:"failed,omitempty"`
}

// RescoreItem is one run to rescore, on its own lane.
type RescoreItem struct {
	Run  string `json:"run"`
	Lane string `json:"lane"`
}

// RescoreRecord is one new answer of a run's oracle.
type RescoreRecord struct {
	Run    string    `json:"run"`
	At     time.Time `json:"at"`
	Oracle string    `json:"oracle"`
	// Verdict is the run's verdict decided again with the new answer, and
	// Reason why; empty when the oracle gave no answer.
	Verdict store.Verdict `json:"verdict,omitempty"`
	Reason  string        `json:"reason"`
	// Was is the run's own verdict, which stays in its record.
	Was      store.Verdict `json:"was"`
	Passed   bool          `json:"passed"`
	ExitCode int           `json:"exit_code"`
	TimedOut bool          `json:"timed_out,omitempty"`
	Err      string        `json:"error,omitempty"`
	Tampered []string      `json:"tampered,omitempty"`
	// TaskHash is the hash of the task's files the oracle came from now,
	// RunTaskHash the one the run ran on.
	TaskHash    string `json:"task_hash"`
	RunTaskHash string `json:"run_task_hash"`
	// TaskVersion is the version of the task the oracle came from: a run
	// rescored with a later version's oracle is compared at that version
	// (020 §4). Records made before it was written have none.
	TaskVersion int `json:"task_version,omitempty"`
}

// RescoreStatus is where a cell's rescore is.
type RescoreStatus struct {
	Job     *RescoreJob     `json:"job"`
	Current []string        `json:"current,omitempty"`
	Records []RescoreRecord `json:"records"`
	DryRun  bool            `json:"dry_run,omitempty"`
}

// rescoreName names a rescore's blobs: the time, sortable.
const rescoreName = "20060102T150405.000000000Z"

// handleRescore queues a cell's rescore (POST, `?dry_run=1` to only say
// what it would rescore) or says where it is (GET).
func (d *Daemon) handleRescore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	batch := r.PathValue("batch")
	if r.Method == http.MethodGet {
		st, err := d.rescoreStatus(ctx, batch)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		if st.Job == nil {
			fail(w, http.StatusNotFound, "batch %s has no rescore", batch)
			return
		}
		reply(w, http.StatusOK, st)
		return
	}
	if d.Oracle == nil || d.Store == nil {
		fail(w, http.StatusServiceUnavailable, "this daemon cannot run an oracle on stored material")
		return
	}
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: batch})
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if len(recs) == 0 {
		fail(w, http.StatusNotFound, "there is no batch %s", batch)
		return
	}
	for _, it := range queue {
		if it.Batch == batch {
			fail(w, http.StatusConflict, "batch %s is not finished: run %s is queued", batch, it.RunID)
			return
		}
	}
	d.mu.Lock()
	for _, f := range d.flights {
		if f.item.Batch == batch {
			d.mu.Unlock()
			fail(w, http.StatusConflict, "batch %s is not finished: run %s is in flight", batch, f.runID)
			return
		}
	}
	if j := d.ctl.Rescores[batch]; j != nil && j.Finished.IsZero() {
		d.mu.Unlock()
		fail(w, http.StatusConflict, "batch %s is being rescored since %s", batch, j.Queued.Format(time.RFC3339))
		return
	}
	superseded := map[string]bool{}
	for _, old := range d.ctl.Retries {
		superseded[old] = true
	}
	d.mu.Unlock()
	job := &RescoreJob{Batch: batch, Queued: time.Now().UTC(), Todo: []RescoreItem{}, Done: []string{}}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	for _, rec := range recs {
		if superseded[rec.ID] {
			continue
		}
		if _, _, ok := run.Rescore(&rec, false); !ok {
			continue
		}
		if d.Local.Lane(rec.Lane) == nil {
			fail(w, http.StatusConflict, "run %s ran on lane %s, which this installation no longer has", rec.ID, rec.Lane)
			return
		}
		job.Todo = append(job.Todo, RescoreItem{Run: rec.ID, Lane: rec.Lane})
	}
	if len(job.Todo) == 0 {
		fail(w, http.StatusConflict, "batch %s has no run with a verdict of the oracle (PASS, FAIL or TIMEOUT) to rescore", batch)
		return
	}
	if dryRun(r) {
		reply(w, http.StatusOK, RescoreStatus{Job: job, Records: []RescoreRecord{}, DryRun: true})
		return
	}
	d.mu.Lock()
	d.ctl.Rescores[batch] = job
	d.saveControl()
	d.mu.Unlock()
	d.Log.Info("rescore queued", "batch", batch, "runs", len(job.Todo))
	d.poke()
	d.bump()
	st, _ := d.rescoreStatus(ctx, batch)
	reply(w, http.StatusAccepted, st)
}

// rescoreStatus is a cell's rescore with the latest record of each run.
func (d *Daemon) rescoreStatus(ctx context.Context, batch string) (RescoreStatus, error) {
	d.mu.Lock()
	var job *RescoreJob
	if j := d.ctl.Rescores[batch]; j != nil {
		c := *j
		c.Todo, c.Done = append([]RescoreItem{}, j.Todo...), append([]string{}, j.Done...)
		job = &c
	}
	st := RescoreStatus{Job: job, Records: []RescoreRecord{}}
	for _, it := range d.rescoring {
		if it.batch == batch {
			st.Current = append(st.Current, it.run)
		}
	}
	d.mu.Unlock()
	sort.Strings(st.Current)
	if job == nil {
		return st, nil
	}
	for _, id := range job.Done {
		rec, err := d.latestRescore(ctx, id)
		if err != nil {
			return st, err
		}
		if rec != nil {
			st.Records = append(st.Records, *rec)
		}
	}
	return st, nil
}

// latestRescore reads a run's latest rescore record; nil when it has none.
func (d *Daemon) latestRescore(ctx context.Context, runID string) (*RescoreRecord, error) {
	refs, err := d.State.BlobRefs(ctx, runID)
	if err != nil {
		return nil, err
	}
	var last *store.BlobRef
	for i := range refs {
		if refs[i].Kind == "rescore" && (last == nil || refs[i].Name > last.Name) {
			last = &refs[i]
		}
	}
	if last == nil || d.Blobs == nil {
		return nil, nil
	}
	rd, _, err := d.Blobs.Reader(ctx, last.Hash)
	if err != nil {
		return nil, fmt.Errorf("reading run %s's rescore %s: %w", runID, last.Name, err)
	}
	defer rd.Close()
	var rec RescoreRecord
	if err := json.NewDecoder(rd).Decode(&rec); err != nil {
		return nil, fmt.Errorf("reading run %s's rescore %s: %w", runID, last.Name, err)
	}
	return &rec, nil
}

// rescoreSlot is the oracle a lane runs for a rescore.
type rescoreSlot struct {
	batch, run string
}

// startRescores gives every free lane the next oracle of the oldest
// rescore with a run of that lane. It is called before queued runs are
// started, so a rescore and a batch on the same lane take turns.
func (d *Daemon) startRescores(ctx context.Context, wg *sync.WaitGroup) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.ctl.Rescores) == 0 {
		return
	}
	jobs := make([]*RescoreJob, 0, len(d.ctl.Rescores))
	for _, j := range d.ctl.Rescores {
		if j.Finished.IsZero() && len(j.Todo) > 0 {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(a, b int) bool { return jobs[a].Queued.Before(jobs[b].Queued) })
	started := false
	for _, j := range jobs {
		// An item stays in Todo until its oracle has answered, so a daemon
		// that dies meanwhile rescores it after the restart.
		for _, it := range j.Todo {
			if _, busy := d.flights[it.Lane]; busy {
				continue
			}
			if _, busy := d.rescoring[it.Lane]; busy {
				continue
			}
			if until, held := d.holds[it.Lane]; held && time.Now().Before(until) {
				continue
			}
			if d.stopped(store.QueueItem{Lane: it.Lane, Batch: j.Batch}) != "" {
				continue
			}
			if j.Started.IsZero() {
				j.Started = time.Now().UTC()
			}
			d.rescoring[it.Lane] = rescoreSlot{batch: j.Batch, run: it.Run}
			started = true
			wg.Add(1)
			go func(j *RescoreJob, it RescoreItem) {
				defer wg.Done()
				err := d.rescoreOne(ctx, it.Run)
				d.mu.Lock()
				delete(d.rescoring, it.Lane)
				if err == nil || ctx.Err() == nil {
					// A daemon that stops rescores the run after the restart.
					for i := range j.Todo {
						if j.Todo[i].Run == it.Run {
							j.Todo = append(j.Todo[:i:i], j.Todo[i+1:]...)
							break
						}
					}
					j.Done = append(j.Done, it.Run)
					if err != nil {
						if j.Failed == nil {
							j.Failed = map[string]string{}
						}
						j.Failed[it.Run] = err.Error()
					}
				}
				if len(j.Todo) == 0 {
					j.Finished = time.Now().UTC()
				}
				d.saveControl()
				d.mu.Unlock()
				if err != nil {
					d.Log.Error("rescore", "batch", j.Batch, "run", it.Run, "error", err)
				}
				d.poke()
				d.bump()
			}(j, it)
		}
	}
	if started {
		d.saveControl()
	}
}

// rescoreOne runs a run's task's current primary oracle on the run's stored
// workspace and collected paths, and stores the answer beside the run. An
// error is an oracle that gave no answer; it is stored too.
func (d *Daemon) rescoreOne(ctx context.Context, runID string) error {
	rec, err := d.State.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	tk, err := task.Find(d.taskRoots(), rec.Suite+"/"+rec.TaskID)
	if err != nil {
		return err
	}
	o := tk.PrimaryOracle()
	if o == nil {
		return fmt.Errorf("task %s has no primary oracle", tk.Name())
	}
	refs, err := d.State.BlobRefs(ctx, runID)
	if err != nil {
		return err
	}
	dir := filepath.Join(d.RunsDir, "rescore-"+runID)
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)
	ws := filepath.Join(dir, "workspace")
	before := ""
	collected := map[string]string{}
	found := false
	for i, ref := range refs {
		var into string
		switch ref.Kind {
		case "workspace_after":
			into, found = ws, true
		case "workspace_before":
			before = filepath.Join(dir, "before")
			into = before
		case "collected":
			into = filepath.Join(dir, fmt.Sprintf("collected-%d", i))
			collected[ref.Name] = into
		default:
			continue
		}
		if err := d.untarBlob(ctx, ref, into); err != nil {
			return fmt.Errorf("restoring the stored %s %s: %w", ref.Kind, ref.Name, err)
		}
	}
	if !found {
		return fmt.Errorf("run %s has no stored workspace", runID)
	}
	for _, c := range tk.Workspace.Collect {
		if collected[c] == "" {
			return fmt.Errorf("run %s has no stored copy of %s, which the task collects", runID, c)
		}
	}
	or, oerr := d.Oracle(ctx, tk, o, ws, collected)
	if oerr != nil && ctx.Err() != nil {
		return oerr
	}
	if before != "" {
		or.Tampered = changedInstalls(tk, before, ws, or.Tampered)
	}
	var prov struct {
		Task struct {
			Hash string `json:"hash"`
		} `json:"task"`
	}
	json.Unmarshal(rec.Provenance, &prov)
	at := time.Now().UTC()
	out := RescoreRecord{Run: runID, At: at, Oracle: o.Name, Was: rec.Verdict, Passed: or.Passed, ExitCode: or.ExitCode,
		TimedOut: or.TimedOut, Err: or.Err, Tampered: or.Tampered, TaskHash: tk.Hash, RunTaskHash: prov.Task.Hash, TaskVersion: tk.Version}
	var failed error
	switch {
	case oerr != nil:
		out.Err, failed = oerr.Error(), fmt.Errorf("the oracle could not be run: %w", oerr)
	case len(or.Tampered) > 0:
		failed = fmt.Errorf("the oracle's copy in the workspace was changed: %s", strings.Join(or.Tampered, ", "))
	case or.Err != "":
		failed = fmt.Errorf("the oracle gave no answer: %s", or.Err)
	}
	if failed != nil {
		out.Reason = failed.Error() + "; the run's verdict stands"
	} else {
		out.Verdict, out.Reason, _ = run.Rescore(rec, or.Passed)
	}
	name := at.Format(rescoreName)
	var stored []store.BlobRef
	h, n, err := d.Store(ctx, strings.NewReader(or.Output))
	if err != nil {
		return fmt.Errorf("storing the oracle's output: %w", err)
	}
	stored = append(stored, store.BlobRef{RunID: runID, Kind: "rescore_oracle", Name: name, Hash: h, Size: n})
	b, _ := json.MarshalIndent(out, "", "  ")
	if h, n, err = d.Store(ctx, bytes.NewReader(b)); err != nil {
		return fmt.Errorf("storing the rescore: %w", err)
	}
	stored = append(stored, store.BlobRef{RunID: runID, Kind: "rescore", Name: name, Hash: h, Size: n})
	if err := d.State.AddBlobRefs(ctx, stored); err != nil {
		return err
	}
	d.Log.Info("rescored", "run", runID, "was", rec.Verdict, "now", out.Verdict, "reason", out.Reason)
	return failed
}

// untarBlob writes a stored tree into dir.
func (d *Daemon) untarBlob(ctx context.Context, ref store.BlobRef, dir string) error {
	if d.Blobs == nil {
		return fmt.Errorf("this daemon reads no blobs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rd, _, err := d.Blobs.Reader(ctx, ref.Hash)
	if err != nil {
		return err
	}
	defer rd.Close()
	return run.Untar(rd, dir)
}

// changedInstalls is which installed artefacts the agent changed: its copy
// in the workspace after the run against the copy the run was given, not
// against the task's pin now, which a later task version changes (a run
// of version 4 has version 4's oracle in its workspace). An install the
// stored workspace before does not hold is judged as the oracle judged it,
// against the pin (byPin).
func changedInstalls(tk *task.Task, before, after string, byPin []string) []string {
	var out []string
	for _, in := range tk.Workspace.Install {
		if in.Seed {
			continue
		}
		given, err := task.HashFile(filepath.Join(before, in.Path))
		if err != nil {
			if slices.Contains(byPin, in.Path) {
				out = append(out, in.Path)
			}
			continue
		}
		if left, err := task.HashFile(filepath.Join(after, in.Path)); err != nil || left != given {
			out = append(out, in.Path)
		}
	}
	return out
}
