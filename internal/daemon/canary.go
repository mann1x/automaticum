package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// CanaryRecord is the first-generations canary's verdict on a batch (050
// §3.3), kept in the control file.
type CanaryRecord struct {
	At time.Time `json:"at"`
	ingest.CanaryVerdict
	// Stopped says the canary stopped the batch.
	Stopped bool `json:"stopped,omitempty"`
}

// canaryWatch is what the canary has collected of a batch so far.
type canaryWatch struct {
	done     []ingest.CanaryRequest // from the batch's runs that ended
	run      string                 // the run being read
	cur      []ingest.CanaryRequest
	controls []string
	asked    bool // the model's control tokens were asked for
}

// canaryStop ends a run in flight because the canary stopped its batch. It
// is an abort, recorded with the canary's words.
type canaryStop string

func (c canaryStop) Error() string        { return "the canary stopped the batch: " + string(c) }
func (c canaryStop) Is(target error) bool { return target == run.ErrOperatorAbort }

// canaryLoop reads the batch of each run in flight until the canary has
// judged it.
func (d *Daemon) canaryLoop(ctx context.Context) {
	every := d.CanaryEvery
	if every == 0 {
		every = 5 * time.Second
	}
	watches := map[string]*canaryWatch{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
		d.canaryTick(ctx, watches)
	}
}

func (d *Daemon) canaryTick(ctx context.Context, watches map[string]*canaryWatch) {
	d.mu.Lock()
	var flying []*flight
	for _, f := range d.flights {
		if f.item.Batch != "" && d.ctl.Canary[f.item.Batch] == nil {
			flying = append(flying, f)
		}
	}
	d.mu.Unlock()
	for _, f := range flying {
		batch := f.item.Batch
		w := watches[batch]
		if w == nil {
			w = &canaryWatch{}
			watches[batch] = w
		}
		if w.run != f.runID {
			w.done, w.cur, w.run = append(w.done, w.cur...), nil, f.runID
		}
		if !w.asked {
			w.asked = true
			controls, err := d.controlTokens(ctx, batch, f.item.Arm)
			if err != nil {
				d.Log.Info("canary: the control tokens are not checked", "batch", batch, "reason", err)
			}
			w.controls = controls
		}
		if d.RunsDir == "" {
			continue
		}
		file, err := os.Open(filepath.Join(d.RunsDir, f.runID, "out", "wire.jsonl"))
		if err != nil {
			continue // not written yet
		}
		reqs, _ := ingest.ReadWire(file)
		file.Close()
		w.cur = ingest.Finished(f.runID, reqs)
		v := ingest.Canary(append(append([]ingest.CanaryRequest(nil), w.done...), w.cur...), w.controls)
		if v == nil {
			continue
		}
		delete(watches, batch)
		rec := &CanaryRecord{At: time.Now().UTC(), CanaryVerdict: *v, Stopped: len(v.Problems) > 0}
		d.mu.Lock()
		d.ctl.Canary[batch] = rec
		err = d.saveControl()
		d.mu.Unlock()
		if err != nil {
			d.Log.Error("canary: the control file", "error", err)
		}
		if !rec.Stopped {
			d.Log.Info("canary: the batch's first requests are sound", "batch", batch, "requests", strings.Join(v.Requests, " "))
			continue
		}
		why := strings.Join(v.Problems, "; ")
		if _, err := d.stopBatch(ctx, batch, "batch stopped by the canary before it started: "+why, canaryStop(why)); err != nil && !errors.Is(err, errNoBatch) {
			d.Log.Error("canary: stopping the batch", "batch", batch, "error", err)
		}
		d.notify(ctx, Notification{Kind: "canary", Status: "stop", Gate: "canary", Detail: why, Lane: f.item.Lane, Run: f.runID, Arm: f.item.Arm, Task: f.item.Task})
	}
}

// controlTokens are the control tokens of the model an arm names, read
// from its endpoint; an endpoint that cannot say is an error.
func (d *Daemon) controlTokens(ctx context.Context, batch, armName string) ([]string, error) {
	if d.ControlTokens != nil {
		return d.ControlTokens(ctx, armName)
	}
	arm, _, err := d.armFor(batch, armName)
	if err != nil {
		return nil, err
	}
	ep := d.Local.Endpoint(arm.Endpoint)
	if !run.Ollama(ep) {
		return nil, errors.New("this kind of endpoint does not list a model's tokens")
	}
	key, err := d.Local.APIKey(ep)
	if err != nil {
		return nil, err
	}
	hc := d.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	return run.ControlTokens(ctx, hc, ep, key, arm.Model.Model)
}

// BatchView is one batch: its runs by state, whether it is paused, and the
// canary's verdict.
type BatchView struct {
	Batch    string         `json:"batch"`
	Queued   []string       `json:"queued,omitempty"`
	InFlight string         `json:"in_flight,omitempty"`
	Finished map[string]int `json:"finished"` // by verdict
	Paused   bool           `json:"paused,omitempty"`
	Canary   *CanaryRecord  `json:"canary,omitempty"`
}

func (d *Daemon) handleBatch(w http.ResponseWriter, r *http.Request) {
	batch := r.PathValue("batch")
	f := d.batchFlight(batch)
	queued, err := d.batchQueued(r, batch, f)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	runs, err := d.State.ListRuns(r.Context(), store.RunFilter{Batch: batch})
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	v := BatchView{Batch: batch, Queued: ids(queued), Finished: map[string]int{}}
	if f != nil {
		v.InFlight = f.runID
	}
	for _, rec := range runs {
		v.Finished[string(rec.Verdict)]++
	}
	d.mu.Lock()
	_, v.Paused = d.ctl.Held[batch]
	v.Canary = d.ctl.Canary[batch]
	d.mu.Unlock()
	if f == nil && len(queued) == 0 && len(runs) == 0 && v.Canary == nil {
		fail(w, http.StatusNotFound, "there is no batch %s", batch)
		return
	}
	reply(w, http.StatusOK, v)
}
