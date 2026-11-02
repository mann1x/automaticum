// Package daemon is automaticumd without its main: the queue, one worker
// per lane, recovery after a restart, and the HTTP API every interface is a
// client of.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/gate"
	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/task"
	"github.com/mann1x/automaticum/internal/wait"
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
	Local *config.Local
	State store.State
	Blobs BlobReader
	// Archiver moves old blobs to the archive tier (080 §5); nil when the
	// installation archives nothing.
	Archiver   Archiver
	Runner     Runner
	Containers Containers
	// Repo is the checkout with tasks/ and images/; arms and models are the
	// configuration's (D118).
	Repo string
	// RunsDir holds the directories of runs in flight.
	RunsDir string
	Version string
	Token   string
	Log     *slog.Logger
	// Gates asks every gate about one arm on one task on one lane. Nil
	// means no gate is asked.
	Gates func(ctx context.Context, lane string, arm *config.Arm, tk *task.Task, image string) []gate.Result
	// Static asks the static gates (gate.Static) about one arm on one
	// task, so that a start that would only queue refused runs is itself
	// refused. Nil asks none.
	Static func(ctx context.Context, arm *config.Arm, tk *task.Task, image string) []gate.Result
	// GateRetry is how long a waiting run waits before the gates are asked
	// again; zero means half a minute.
	GateRetry time.Duration
	// HTTP sends notifications; nil means a client with a short timeout.
	HTTP *http.Client
	// ControlFile keeps what the operator asked of lanes and batches across
	// a restart; empty keeps it in memory only.
	ControlFile string
	// CanaryEvery is how often the canary reads the runs in flight; zero
	// means every five seconds. ControlTokens, when set, replaces asking
	// the endpoint for a model's control tokens.
	CanaryEvery time.Duration
	// ArchiveEvery is how often old blobs are looked for; zero means once
	// a day.
	ArchiveEvery time.Duration
	// HeartbeatEvery is how often the heartbeat is logged; zero means ten
	// minutes (050 §6).
	HeartbeatEvery time.Duration
	// CampaignEvery is how often the campaigns are moved on; zero means
	// every five seconds.
	CampaignEvery time.Duration
	ControlTokens func(ctx context.Context, arm string) ([]string, error)
	// Oracle runs a task's oracle on a workspace, for a rescore (D121);
	// *task.Env's RunOracle is one. Store keeps what a rescore makes;
	// *blob.Store's Put is one. Without both no rescore runs.
	Oracle func(ctx context.Context, t *task.Task, o *task.Oracle, workspace string, collected map[string]string) (task.OracleResult, error)
	Store  func(ctx context.Context, r io.Reader) (blob.Hash, int64, error)

	mu      sync.Mutex
	flights map[string]*flight // by lane
	// rescoring is the rescore oracle each lane runs, by lane (D121).
	rescoring map[string]rescoreSlot
	draining  bool
	wake      chan struct{}
	// changed is closed, and replaced, whenever the queue or a run in
	// flight changes, so a wait reads the state again (090 §5).
	changed chan struct{}
	started time.Time
	// reanalysis is the one running, or the last that ran.
	reanalysis *ReanalyseStatus
	// holds says until when a lane's waiting run is left alone, and
	// notified which waiting runs were already told about.
	holds    map[string]time.Time
	notified map[string]bool
	// lastBeat is the last heartbeat.
	lastBeat *Heartbeat
	// atCapacity says, by run, that it waits because its endpoint serves
	// as many runs as its capacity (050 §1).
	atCapacity map[string]string
	// batchTold is how many finished runs a batch had when its end was
	// last notified.
	batchTold map[string]int
	ctl       control
	// campaignMu makes the campaigns move one step at a time.
	campaignMu sync.Mutex
	// idemMu serves requests with an idempotency key one at a time.
	idemMu sync.Mutex
}

// flight is a run in flight on a lane.
type flight struct {
	runID   string
	item    store.QueueItem
	started time.Time
	// endpoint is the name of the arm's endpoint, for its capacity.
	endpoint string
	cancel   context.CancelCauseFunc

	mu     sync.Mutex
	lines  [][]byte // what the runner printed, bounded
	bytes  int
	first  int // number of the first line still held
	subs   map[chan []byte]struct{}
	closed bool
	// held is true while the runner says the run is held between two
	// requests.
	held bool
}

// holding says whether the run is held, and whether it is over.
func (f *flight) holding() (held, over bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held, f.closed
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
	if bytes.Contains(line, []byte(`"k":"runner"`)) {
		switch {
		case bytes.Contains(line, []byte(`"note":"held"`)):
			f.held = true
		case bytes.Contains(line, []byte(`"note":"released"`)):
			f.held = false
		}
	}
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
	// Waiting is why the lane's next run has not started: a gate's words.
	Waiting     string          `json:"waiting,omitempty"`
	ContainerID string          `json:"container_id,omitempty"`
	Run         *store.Run      `json:"run,omitempty"`
	Item        store.QueueItem `json:"item"`
}

func (d *Daemon) init() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.flights == nil {
		d.flights = map[string]*flight{}
		d.rescoring = map[string]rescoreSlot{}
		d.holds = map[string]time.Time{}
		d.notified = map[string]bool{}
		d.atCapacity = map[string]string{}
		d.batchTold = map[string]int{}
		d.wake = make(chan struct{}, 1)
		d.changed = make(chan struct{})
		d.started = time.Now()
		d.loadControl()
	}
}

func (d *Daemon) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
	d.bump()
}

// bump tells every wait that something changed.
func (d *Daemon) bump() {
	d.mu.Lock()
	if d.changed != nil {
		close(d.changed)
		d.changed = make(chan struct{})
	}
	d.mu.Unlock()
}

// change is the channel closed at the next change of the queue or of a run
// in flight.
func (d *Daemon) change() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.changed == nil {
		d.changed = make(chan struct{})
	}
	return d.changed
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
	canary, stopCanary := context.WithCancel(ctx)
	defer stopCanary()
	go d.canaryLoop(canary)
	go d.campaignLoop(canary)
	go d.heartbeatLoop(canary)
	go d.archiveLoop(canary)
	for {
		d.mu.Lock()
		draining := d.draining
		busy := len(d.flights) + len(d.rescoring)
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
// run is started after it. A run held by a pause is released first, so the
// drain ends with its verdict (D119).
func (d *Daemon) Drain() {
	d.init()
	d.mu.Lock()
	d.draining = true
	d.mu.Unlock()
	d.releaseHeld()
	d.poke()
}

// startReady starts the oldest queued run of every idle lane.
func (d *Daemon) startReady(ctx context.Context, wg *sync.WaitGroup) {
	queue, err := d.State.Queue(ctx)
	if err != nil {
		d.Log.Error("reading the queue", "error", err)
		return
	}
	// Higher priority first; the queue's own order otherwise, which keeps
	// a batch's A/B interleaving (D125).
	d.mu.Lock()
	prio := d.ctl.Priority
	sort.SliceStable(queue, func(i, j int) bool { return prio[queue[i].Batch] > prio[queue[j].Batch] })
	d.mu.Unlock()
	// A lane whose waiting run was taken off the queue is idle again.
	queued, waiting := map[string]bool{}, map[string]bool{}
	for _, it := range queue {
		queued[it.Lane], waiting[it.RunID] = true, true
	}
	d.mu.Lock()
	for id := range d.atCapacity {
		if !waiting[id] {
			delete(d.atCapacity, id)
		}
	}
	var freed []string
	for lane := range d.holds {
		if !queued[lane] {
			delete(d.holds, lane)
			freed = append(freed, lane)
		}
	}
	d.mu.Unlock()
	for _, lane := range freed {
		if err := d.State.SetLaneState(ctx, store.LaneState{Lane: lane, State: "idle"}); err != nil {
			d.Log.Error("writing the lane's state", "error", err)
		}
	}
	d.startRescores(ctx, wg)
	for _, it := range queue {
		d.mu.Lock()
		_, busy := d.flights[it.Lane]
		if _, r := d.rescoring[it.Lane]; r {
			busy = true
		}
		if until, held := d.holds[it.Lane]; held && time.Now().Before(until) {
			busy = true
		}
		// A drained lane or a paused batch starts nothing; the next run of
		// another batch on the lane may.
		if d.stopped(it) != "" {
			busy = true
		}
		d.mu.Unlock()
		if busy {
			continue
		}
		// The endpoint serves at most its capacity of runs at once (050
		// §1); the arm is read again when the run starts.
		endpoint, full := d.endpointFull(it)
		d.mu.Lock()
		if _, taken := d.flights[it.Lane]; taken {
			busy = true
		}
		if _, r := d.rescoring[it.Lane]; r {
			busy = true
		}
		if full != "" {
			busy = true
			d.atCapacity[it.RunID] = full
		} else {
			delete(d.atCapacity, it.RunID)
		}
		var f *flight
		var fctx context.Context
		if !busy {
			var cancel context.CancelCauseFunc
			fctx, cancel = context.WithCancelCause(context.WithoutCancel(ctx))
			f = &flight{runID: it.RunID, item: it, started: time.Now(), cancel: cancel, endpoint: endpoint, subs: map[chan []byte]struct{}{}}
			d.flights[it.Lane] = f
		}
		d.mu.Unlock()
		if busy {
			continue
		}
		d.bump()
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The daemon's own end aborts the run, with that cause.
			stop := context.AfterFunc(ctx, func() { f.cancel(run.ErrDaemonStop) })
			defer stop()
			waiting := d.fly(fctx, f)
			if !waiting {
				// Before the lane is free: a quarantine this end sets
				// must hold for the next run (D124).
				if rec, err := d.State.GetRun(context.WithoutCancel(ctx), f.runID); err == nil {
					d.countInfra(context.WithoutCancel(ctx), rec)
				}
			}
			d.mu.Lock()
			delete(d.flights, f.item.Lane)
			if waiting {
				retry := d.GateRetry
				if retry == 0 {
					retry = 30 * time.Second
				}
				d.holds[f.item.Lane] = time.Now().Add(retry)
				time.AfterFunc(retry, d.poke)
			} else {
				delete(d.holds, f.item.Lane)
				delete(d.notified, f.runID)
			}
			d.mu.Unlock()
			if !waiting {
				d.poke()
				d.notifyEnd(context.WithoutCancel(ctx), f.item)
			}
		}()
	}
}

// endpointFull gives the name of a queued run's endpoint and, when that
// endpoint already serves as many runs as its capacity, why the run waits.
// A mode C cell's endpoint is the caller's, with its own lanes (065 §3.1),
// and an arm that cannot be read is left to fail when the run starts.
func (d *Daemon) endpointFull(it store.QueueItem) (string, string) {
	arm, cell, err := d.armFor(it.Batch, it.Arm)
	if err != nil || cell != nil {
		return "", ""
	}
	ep := d.Local.Endpoint(arm.Endpoint)
	if ep == nil || ep.Cell || ep.Capacity < 1 {
		return arm.Endpoint, ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, f := range d.flights {
		if f.endpoint == arm.Endpoint {
			n++
		}
	}
	if n >= ep.Capacity {
		return arm.Endpoint, fmt.Sprintf("endpoint %s serves its capacity of %d", arm.Endpoint, ep.Capacity)
	}
	return arm.Endpoint, ""
}

// fly runs one queued run to its end. It returns true when the run did not
// start because a gate says to wait: the run stays queued.
func (d *Daemon) fly(ctx context.Context, f *flight) (waiting bool) {
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
	// A run a gate skips still names its arm by id, once the arm is read, so
	// that run ls --arm finds it.
	var armID string
	skip := func(reason string) {
		r := &store.Run{ID: it.RunID, Batch: it.Batch, Lane: it.Lane, ArmID: armID, Verdict: store.Skipped, VerdictReason: reason,
			StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC(), Provenance: askedProvenance(it)}
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
	d.mu.Lock()
	campaign, cohort := d.campaignOf(it.Batch), ""
	if c := d.cohortOf(it.Batch); c != nil {
		cohort = c.Name
	}
	d.mu.Unlock()
	// The arm and the task are read now and fixed from here on.
	arm, cell, err := d.armFor(it.Batch, it.Arm)
	if err != nil {
		skip("the arm cannot be used: " + err.Error())
		return false
	}
	armID = arm.ID
	tk, err := task.Find(d.taskRoots(), it.Task)
	if err != nil {
		skip("the task cannot be used: " + err.Error())
		return false
	}
	// A mode C cell's server is asked what it is before every run, and a
	// contradiction of the caller's facts refuses the run as a gate does
	// (065 §3.1).
	var cellRec *CellRecord
	if cell != nil {
		var why string
		if cellRec, why = d.checkCell(ctx, it.Batch, cell, arm); why != "" && ctx.Err() == nil {
			d.notify(bg, Notification{Kind: "gate", Status: "refuse", Gate: "basis_facts", Detail: why,
				Lane: it.Lane, Run: it.RunID, Arm: it.Arm, Task: it.Task})
			skip("gate basis_facts: " + why)
			return false
		}
	}
	// Something else uses the lane's devices (D123): the run waits as for
	// the free gate, once notified, and nothing is unloaded.
	if busy, why := d.laneBusy(ctx, it.Lane); busy && ctx.Err() == nil {
		setLane("waiting", &laneDetail{Item: it, Waiting: "busy: " + why})
		d.mu.Lock()
		told := d.notified[it.RunID]
		d.notified[it.RunID] = true
		d.mu.Unlock()
		if !told {
			log.Warn("run waits", "gate", "busy", "detail", why)
			d.notify(bg, Notification{Kind: "gate", Status: string(gate.Wait), Gate: "busy", Detail: why,
				Lane: it.Lane, Run: it.RunID, Arm: it.Arm, Task: it.Task})
		}
		return true
	}
	// An endpoint that allows it is asked to drop any other model before
	// the gates look (D79): arms on different models can then follow each
	// other on one device. When it fails the gate "free" waits, as before.
	var unloaded []string
	if ep := d.Local.Endpoint(arm.Endpoint); run.Ollama(ep) && ep.UnloadOthers && !ep.Cell {
		key, err := d.Local.APIKey(ep)
		if err == nil {
			hc := d.HTTP
			if hc == nil {
				hc = &http.Client{Timeout: 90 * time.Second}
			}
			unloaded, err = run.Unload(ctx, hc, ep, key, arm.Model.Model)
		}
		if err != nil {
			log.Warn("the endpoint did not drop its other model", "error", err)
		} else if len(unloaded) > 0 {
			log.Info("the endpoint dropped another model for this run", "models", strings.Join(unloaded, ", "))
		}
	}
	// The gates (050 §3). A refusal skips the run with the gate's name; a
	// wait leaves it queued (D64). Both are notified, a wait once.
	var answers []gate.Result
	if d.Gates != nil {
		answers = d.Gates(ctx, it.Lane, arm, tk, LaneImage(tk.Layers))
		if w := gate.Worst(answers); w != nil && ctx.Err() == nil {
			note := Notification{Kind: "gate", Status: string(w.Status), Gate: w.Gate, Detail: w.Detail,
				Lane: it.Lane, Run: it.RunID, Arm: it.Arm, Task: it.Task}
			if w.Status == gate.Wait {
				setLane("waiting", &laneDetail{Item: it, Waiting: w.Gate + ": " + w.Detail})
				d.mu.Lock()
				told := d.notified[it.RunID]
				d.notified[it.RunID] = true
				d.mu.Unlock()
				if !told {
					log.Warn("run waits", "gate", w.Gate, "detail", w.Detail)
					d.notify(bg, note)
				}
				return true
			}
			d.notify(bg, note)
			skip("gate " + w.Gate + ": " + w.Detail)
			return false
		}
	}
	// What the model is, in the owner's words (D70). A tag without an entry
	// runs all the same and its run says so (D71).
	models, err := config.LoadModels(d.Local.ModelsDir())
	if err != nil {
		skip("the model catalogue cannot be read: " + err.Error())
		return false
	}
	d.mu.Lock()
	capture, limits := d.ctl.Capture[it.Batch], d.ctl.Limits[it.Batch]
	d.mu.Unlock()
	det := &laneDetail{Item: it}
	setLane("running", det)
	log.Info("run starts", "arm", it.Arm, "task", it.Task)
	r, err := d.Runner.Execute(ctx, run.Request{
		ID: it.RunID, Arm: arm, Task: tk, Lane: it.Lane, Image: LaneImage(tk.Layers), Batch: it.Batch, Campaign: campaign, Cohort: cohort, Gates: answers, Identity: models[arm.Model.Model], Unloaded: unloaded, Cell: cellOrNil(cellRec), CaptureRequests: capture, Limits: limits,
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
		return false
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
	return false
}

// cellOrNil keeps a run that is not a cell's from recording a typed nil.
func cellOrNil(c *CellRecord) any {
	if c == nil {
		return nil
	}
	return c
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

// laneFor picks a lane for an arm and the labels its runs require (the
// arm's and the tasks', D122): the one asked for, or the first, by name,
// that may use the arm's endpoint and has every label.
func (d *Daemon) laneFor(arm *config.Arm, asked string, requires []string) (string, error) {
	may := func(lane string) bool { return d.Local.MayUse(lane, arm.Endpoint) }
	missing := func(lane string) []string {
		var out []string
		have := d.Local.Lanes[lane].Labels
		for _, r := range requires {
			k, v, _ := strings.Cut(r, "=")
			if got, ok := have[strings.TrimSpace(k)]; !ok || got != strings.TrimSpace(v) {
				out = append(out, r)
			}
		}
		return out
	}
	if asked != "" {
		l := d.Local.Lanes[asked]
		switch {
		case l == nil:
			return "", fmt.Errorf("this installation has no lane %q", asked)
		case !may(asked):
			return "", fmt.Errorf("lane %s may not use endpoint %s", asked, arm.Endpoint)
		}
		if m := missing(asked); len(m) > 0 {
			return "", fmt.Errorf("lane %s lacks the labels the runs require: %s", asked, strings.Join(m, ", "))
		}
		return asked, nil
	}
	var names []string
	for n := range d.Local.Lanes {
		if may(n) && len(missing(n)) == 0 {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		if len(requires) > 0 {
			return "", fmt.Errorf("no lane may use endpoint %s and has the labels %s", arm.Endpoint, strings.Join(requires, ", "))
		}
		return "", fmt.Errorf("no lane may use endpoint %s", arm.Endpoint)
	}
	sort.Strings(names)
	return names[0], nil
}

// Notification is what the daemon tells the installation (050 §2, 090 §5):
// a refusing or waiting gate, a canary stop, a campaign's end, a batch that
// finished or was stopped, a run's end.
type Notification struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`   // gate, canary, campaign, batch, run or lane
	Status string `json:"status"` // gate: refuse or wait; canary: stop; campaign: finished or stopped; batch: finished or stopped; run: its verdict
	Gate   string `json:"gate"`   // the gate; the campaign's name for a campaign
	Detail string `json:"detail"`
	Lane   string `json:"lane"`
	Run    string `json:"run"`
	Arm    string `json:"arm"`
	Task   string `json:"task"`
	Batch  string `json:"batch,omitempty"`
	// Counts are a batch's runs by verdict, and by state while unfinished.
	Counts map[string]int `json:"counts,omitempty"`
	Time   time.Time      `json:"time"`
}

// notify writes the notification in the daemon's log and, for the kinds
// [notify] events sends, posts it to the webhook and gives it to the
// command. Neither stops anything when it fails; the failure is logged.
func (d *Daemon) notify(ctx context.Context, n Notification) {
	n.Source, n.Time = "automaticum", time.Now().UTC()
	d.Log.Warn(n.Kind+" "+n.Status, "gate", n.Gate, "run", n.Run, "batch", n.Batch, "lane", n.Lane, "detail", n.Detail)
	cfg := d.Local.Daemon.Notify
	if !cfg.Sends(n.Kind) {
		return
	}
	body, _ := json.Marshal(n)
	if len(cfg.Command) > 0 {
		d.notifyCommand(ctx, cfg.Command, n, body)
	}
	hook := cfg.Webhook
	if hook == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", hook, bytes.NewReader(body))
	if err != nil {
		d.Log.Error("the webhook's address is not usable")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	hc := d.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		// The address is private; say that it failed, not where.
		d.Log.Error("the webhook did not take the notification")
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		d.Log.Error("the webhook refused the notification", "status", resp.StatusCode)
	}
}

// notifyCommand runs the [notify] command with the notification on its
// standard input and its main fields in the environment, for at most half
// a minute.
func (d *Daemon) notifyCommand(ctx context.Context, argv []string, n Notification, body []byte) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	cmd.Env = append(os.Environ(),
		"AUTOMATICUM_NOTIFY_KIND="+n.Kind, "AUTOMATICUM_NOTIFY_STATUS="+n.Status,
		"AUTOMATICUM_NOTIFY_RUN="+n.Run, "AUTOMATICUM_NOTIFY_BATCH="+n.Batch,
		"AUTOMATICUM_NOTIFY_LANE="+n.Lane, "AUTOMATICUM_NOTIFY_GATE="+n.Gate)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		d.Log.Error("the notify command failed", "error", err, "output", msg)
	}
}

// notifyEnd tells about a run that ended, and about its batch when that was
// its last run. A batch is told about once for each number of finished
// runs, so two lanes ending together tell once.
func (d *Daemon) notifyEnd(ctx context.Context, it store.QueueItem) {
	rec, err := d.State.GetRun(ctx, it.RunID)
	if err != nil {
		return
	}
	d.notify(ctx, Notification{Kind: "run", Status: string(rec.Verdict), Detail: rec.VerdictReason, Lane: it.Lane, Run: it.RunID,
		Arm: it.Arm, Task: it.Task, Batch: it.Batch})
	if it.Batch == "" {
		return
	}
	runs, err := d.batchRuns(ctx, it.Batch)
	if err != nil {
		return
	}
	s := wait.Runs(wait.KindBatch, it.Batch, runs)
	if !s.Over() || d.batchFlight(it.Batch) != nil {
		return
	}
	d.mu.Lock()
	told := d.batchTold[it.Batch] == s.Counts["finished"]
	d.batchTold[it.Batch] = s.Counts["finished"]
	d.mu.Unlock()
	if told {
		return
	}
	d.notify(ctx, Notification{Kind: "batch", Status: "finished", Batch: it.Batch, Lane: it.Lane, Arm: it.Arm, Task: it.Task,
		Detail: verdictLine(s.Counts), Counts: verdictCounts(s.Counts)})
}

// verdictCounts are a snapshot's counts of the verdicts that occur.
func verdictCounts(c map[string]int) map[string]int {
	out := map[string]int{}
	for _, k := range []string{"pass", "fail", "timeout", "aborted", "invalid", "skipped", "queued", "running"} {
		if c[k] > 0 {
			out[strings.ToUpper(k)] = c[k]
		}
	}
	return out
}

func verdictLine(c map[string]int) string {
	var parts []string
	for _, k := range []string{"pass", "fail", "timeout", "aborted", "invalid", "skipped"} {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], strings.ToUpper(k)))
		}
	}
	return fmt.Sprintf("%s of %d runs", strings.Join(parts, ", "), c["total"])
}

// taskRoots are the repository's tasks/ and the private root the local
// configuration names, if any (D105).
func (d *Daemon) taskRoots() task.Roots {
	r := task.Roots{Public: filepath.Join(d.Repo, "tasks")}
	if d.Local != nil {
		r.Private = d.Local.Daemon.PrivateTasks
	}
	return r
}
