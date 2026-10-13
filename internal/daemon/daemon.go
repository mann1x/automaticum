// Package daemon is automaticumd without its main: the queue, one worker
// per lane, recovery after a restart, and the HTTP API every interface is a
// client of.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/task"
)

// Runner executes one run. *run.Executor is one.
type Runner interface {
	Execute(ctx context.Context, req run.Request) (*store.Run, error)
}

// Containers is what recovery needs of the container engine.
type Containers interface {
	Kill(ctx context.Context, id, signal string) error
	Remove(ctx context.Context, id string) error
}

// BlobReader reads stored raw material. *blob.Store is one.
type BlobReader interface {
	Reader(ctx context.Context, h blob.Hash) (io.ReadCloser, blob.Info, error)
}

// Daemon holds the queue and the lanes of one installation.
type Daemon struct {
	Local      *config.Local
	State      store.State
	Blobs      BlobReader
	Runner     Runner
	Containers Containers
	// Repo is the checkout with arms/ and tasks/.
	Repo string
	// RunsDir holds the directories of runs in flight.
	RunsDir string
	Version string
	Token   string
	Log     *slog.Logger

	mu       sync.Mutex
	flights  map[string]*flight // by lane
	draining bool
	wake     chan struct{}
	started  time.Time
}

// flight is a run in flight on a lane.
type flight struct {
	runID   string
	item    store.QueueItem
	started time.Time
	cancel  context.CancelCauseFunc

	mu     sync.Mutex
	lines  [][]byte // what the runner printed, bounded
	bytes  int
	first  int // number of the first line still held
	subs   map[chan []byte]struct{}
	closed bool
}

// What a flight keeps for clients that attach late.
const (
	maxFlightLines = 5000
	maxFlightBytes = 32 << 20
)

func (f *flight) add(line []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	f.bytes += len(line)
	for len(f.lines) > maxFlightLines || (f.bytes > maxFlightBytes && len(f.lines) > 1) {
		f.bytes -= len(f.lines[0])
		f.lines = f.lines[1:]
		f.first++
	}
	for ch := range f.subs {
		select {
		case ch <- line:
		default: // a client that does not keep up loses lines, not the run
		}
	}
}

// subscribe returns the lines held so far and a channel of those to come.
// The channel is closed when the run ends.
func (f *flight) subscribe() ([][]byte, chan []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan []byte, 1024)
	if f.closed {
		close(ch)
	} else {
		f.subs[ch] = struct{}{}
	}
	return append([][]byte(nil), f.lines...), ch
}

func (f *flight) unsubscribe(ch chan []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.subs[ch]; ok {
		delete(f.subs, ch)
		close(ch)
	}
}

func (f *flight) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for ch := range f.subs {
		close(ch)
		delete(f.subs, ch)
	}
}

// laneDetail is what the lane's state holds while a run is in flight: enough
// for a restarted daemon to clean up and to write the run's record.
type laneDetail struct {
	ContainerID string          `json:"container_id,omitempty"`
	Run         *store.Run      `json:"run,omitempty"`
	Item        store.QueueItem `json:"item"`
}

func (d *Daemon) init() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.flights == nil {
		d.flights = map[string]*flight{}
		d.wake = make(chan struct{}, 1)
		d.started = time.Now()
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
}

func (d *Daemon) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// LaneImage names the image a task runs in: the base image, or the one that
// adds the task's language layers.
func LaneImage(layers []string) string {
	if len(layers) == 0 {
		return "automaticum-lane:base"
	}
	sorted := append([]string(nil), layers...)
	sort.Strings(sorted)
	return "automaticum-lane:" + strings.Join(sorted, "-")
}

// Recover deals with what a daemon that stopped without warning left: a run
// in flight becomes ABORTED, its container is removed by its recorded id,
// and it leaves the queue. Nothing is lost and nothing runs twice.
func (d *Daemon) Recover(ctx context.Context) error {
	d.init()
	states, err := d.State.LaneStates(ctx)
	if err != nil {
		return err
	}
	for _, ls := range states {
		if ls.State != "running" {
			continue
		}
		var det laneDetail
		json.Unmarshal(ls.Detail, &det)
		if det.ContainerID != "" && d.Containers != nil {
			if err := d.Containers.Kill(ctx, det.ContainerID, ""); err != nil {
				d.Log.Info("recover: kill", "container", det.ContainerID, "result", err)
			}
			if err := d.Containers.Remove(ctx, det.ContainerID); err != nil {
				d.Log.Info("recover: remove", "container", det.ContainerID, "result", err)
			}
		}
		if ls.RunID != "" {
			if _, err := d.State.GetRun(ctx, ls.RunID); errors.Is(err, store.ErrNotFound) {
				r := det.Run
				if r == nil {
					r = &store.Run{ID: ls.RunID, Lane: ls.Lane, StartedAt: ls.UpdatedAt}
				}
				r.Verdict, r.VerdictReason = store.Aborted, "daemon restart"
				r.EndedAt = time.Now().UTC()
				if !r.StartedAt.IsZero() {
					r.WallMS = r.EndedAt.Sub(r.StartedAt).Milliseconds()
				}
				if err := d.State.UpsertRun(ctx, r); err != nil {
					return fmt.Errorf("recover: run %s: %w", ls.RunID, err)
				}
				d.Log.Warn("recover: run in flight at restart is ABORTED", "run", ls.RunID, "lane", ls.Lane)
			} else if err != nil {
				return err
			}
			if err := d.State.Unqueue(ctx, ls.RunID); err != nil {
				return err
			}
			if d.RunsDir != "" {
				os.RemoveAll(filepath.Join(d.RunsDir, ls.RunID))
			}
		}
		if err := d.State.SetLaneState(ctx, store.LaneState{Lane: ls.Lane, State: "idle"}); err != nil {
			return err
		}
	}
	// A queued run that already has a record finished just before the stop.
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return err
	}
	for _, it := range queue {
		if _, err := d.State.GetRun(ctx, it.RunID); err == nil {
			if err := d.State.Unqueue(ctx, it.RunID); err != nil {
				return err
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	for name := range d.Local.Lanes {
		if err := d.State.SetLaneState(ctx, store.LaneState{Lane: name, State: "idle"}); err != nil {
			return err
		}
	}
	return nil
}

// Serve works the queue until ctx ends or the daemon has drained. Ending
// ctx aborts runs in flight; Drain lets them finish first.
func (d *Daemon) Serve(ctx context.Context) {
	d.init()
	var wg sync.WaitGroup
	for {
		d.mu.Lock()
		draining := d.draining
		busy := len(d.flights)
		d.mu.Unlock()
		if ctx.Err() != nil || (draining && busy == 0) {
			break
		}
		if !draining {
			d.startReady(ctx, &wg)
		}
		select {
		case <-ctx.Done():
		case <-d.wake:
		case <-time.After(2 * time.Second):
		}
	}
	wg.Wait()
}

// Drain makes Serve return once the runs in flight have finished. No queued
// run is started after it.
func (d *Daemon) Drain() {
	d.init()
	d.mu.Lock()
	d.draining = true
	d.mu.Unlock()
	d.poke()
}

// startReady starts the oldest queued run of every idle lane.
func (d *Daemon) startReady(ctx context.Context, wg *sync.WaitGroup) {
	queue, err := d.State.Queue(ctx)
	if err != nil {
		d.Log.Error("reading the queue", "error", err)
		return
	}
	for _, it := range queue {
		d.mu.Lock()
		_, busy := d.flights[it.Lane]
		var f *flight
		var fctx context.Context
		if !busy {
			var cancel context.CancelCauseFunc
			fctx, cancel = context.WithCancelCause(context.WithoutCancel(ctx))
			f = &flight{runID: it.RunID, item: it, started: time.Now(), cancel: cancel, subs: map[chan []byte]struct{}{}}
			d.flights[it.Lane] = f
		}
		d.mu.Unlock()
		if busy {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The daemon's own end aborts the run, with that cause.
			stop := context.AfterFunc(ctx, func() { f.cancel(run.ErrDaemonStop) })
			defer stop()
			d.fly(fctx, f)
			d.mu.Lock()
			delete(d.flights, f.item.Lane)
			d.mu.Unlock()
			d.poke()
		}()
	}
}

// fly runs one queued run to its end.
func (d *Daemon) fly(ctx context.Context, f *flight) {
	it := f.item
	bg := context.WithoutCancel(ctx)
	defer f.close()
	defer f.cancel(nil)
	log := d.Log.With("run", it.RunID, "lane", it.Lane)
	setLane := func(state string, det *laneDetail) {
		ls := store.LaneState{Lane: it.Lane, State: state}
		if det != nil {
			ls.RunID = it.RunID
			ls.Detail, _ = json.Marshal(det)
		}
		if err := d.State.SetLaneState(bg, ls); err != nil {
			log.Error("writing the lane's state", "error", err)
		}
	}
	done := func() {
		if err := d.State.Unqueue(bg, it.RunID); err != nil {
			log.Error("taking the run off the queue", "error", err)
		}
		setLane("idle", nil)
	}
	skip := func(reason string) {
		r := &store.Run{ID: it.RunID, Batch: it.Batch, Lane: it.Lane, Verdict: store.Skipped, VerdictReason: reason,
			StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC()}
		if suite, id, ok := strings.Cut(it.Task, "/"); ok {
			r.Suite, r.TaskID = suite, id
		}
		if err := d.State.UpsertRun(bg, r); err != nil {
			log.Error("writing the record", "error", err)
			return
		}
		log.Warn("run skipped", "reason", reason)
		done()
	}
	// The arm and the task are read now and fixed from here on.
	arm, err := config.LoadArm(filepath.Join(d.Repo, "arms"), it.Arm)
	if err != nil {
		skip("the arm cannot be used: " + err.Error())
		return
	}
	tk, err := task.Find(filepath.Join(d.Repo, "tasks"), it.Task)
	if err != nil {
		skip("the task cannot be used: " + err.Error())
		return
	}
	det := &laneDetail{Item: it}
	setLane("running", det)
	log.Info("run starts", "arm", it.Arm, "task", it.Task)
	r, err := d.Runner.Execute(ctx, run.Request{
		ID: it.RunID, Arm: arm, Task: tk, Lane: it.Lane, Image: LaneImage(tk.Layers), Batch: it.Batch,
		OnStart: func(partial *store.Run, containerID string) {
			copy := *partial
			det.ContainerID, det.Run = containerID, &copy
			setLane("running", det)
		},
		OnLine: f.add,
	})
	if err != nil {
		// The record could not be written. The run stays queued and the
		// lane stays marked, so a restart makes it ABORTED: it is not
		// run a second time.
		log.Error("run ended without a record", "error", err)
		d.mu.Lock()
		d.draining = true
		d.mu.Unlock()
		return
	}
	log.Info("run ended", "verdict", r.Verdict, "reason", r.VerdictReason)
	if r.Verdict != store.Skipped {
		// A run that failed to be analysed is still a run: its material is
		// stored and `reanalyse` can be run again.
		if res, err := ingest.Run(context.WithoutCancel(ctx), d.State, d.Blobs, r.ID); err != nil {
			log.Error("the run could not be analysed", "error", err)
		} else {
			log.Info("run analysed", "metrics", len(res.Metrics), "tool_calls", len(res.ToolCalls), "findings", len(res.Findings))
		}
	}
	done()
}

// Abort ends a run in flight; the cause is recorded as its reason.
func (d *Daemon) Abort(runID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, f := range d.flights {
		if f.runID == runID {
			f.cancel(run.ErrOperatorAbort)
			return true
		}
	}
	return false
}

func (d *Daemon) flight(runID string) *flight {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, f := range d.flights {
		if f.runID == runID {
			return f
		}
	}
	return nil
}

// laneFor picks a lane for an arm: the one asked for, or the first, by
// name, that may use the arm's endpoint.
func (d *Daemon) laneFor(arm *config.Arm, asked string) (string, error) {
	may := func(l *config.Lane) bool {
		for _, e := range l.Endpoints {
			if e == arm.Endpoint {
				return true
			}
		}
		return false
	}
	if asked != "" {
		l := d.Local.Lanes[asked]
		switch {
		case l == nil:
			return "", fmt.Errorf("this installation has no lane %q", asked)
		case !may(l):
			return "", fmt.Errorf("lane %s may not use endpoint %s", asked, arm.Endpoint)
		}
		return asked, nil
	}
	var names []string
	for n, l := range d.Local.Lanes {
		if may(l) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no lane may use endpoint %s", arm.Endpoint)
	}
	sort.Strings(names)
	return names[0], nil
}
