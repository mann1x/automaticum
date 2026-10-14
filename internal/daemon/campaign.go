package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
)

// CampaignState is a campaign as the daemon drives it (050 §1): its
// definition as read when it started, and where each batch is. It is kept
// in the control file, so a restart forgets none of it.
type CampaignState struct {
	Def     *config.Campaign     `json:"definition"`
	State   string               `json:"state"` // running, paused, stopped, finished
	Reason  string               `json:"reason,omitempty"`
	Started time.Time            `json:"started"`
	Ended   time.Time            `json:"ended,omitzero"`
	Batches []CampaignBatchState `json:"batches"`
}

// CampaignBatchState is one batch of a campaign.
type CampaignBatchState struct {
	Name    string    `json:"name"`
	Batch   string    `json:"batch,omitempty"` // the batch id once started
	State   string    `json:"state"`           // waiting, running, stopped, finished
	Reason  string    `json:"reason,omitempty"`
	Started time.Time `json:"started,omitzero"`
	Ended   time.Time `json:"ended,omitzero"`
}

// snapshot copies a campaign's state, so it can be read after the lock is
// let go. It is called with d.mu held.
func (c *CampaignState) snapshot() CampaignState {
	out := *c
	out.Batches = append([]CampaignBatchState(nil), c.Batches...)
	return out
}

// campaignKey names a campaign's batch: the idempotency key of its start,
// so a batch is never queued twice.
func campaignKey(campaign, batch string) string { return campaign + "-" + batch }

// campaignOf is the campaign a batch belongs to. It is called with d.mu
// held.
func (d *Daemon) campaignOf(batch string) string {
	for name, c := range d.ctl.Campaigns {
		for _, b := range c.Batches {
			if b.Batch == batch {
				return name
			}
		}
	}
	return ""
}

// campaignLoop drives every running campaign.
func (d *Daemon) campaignLoop(ctx context.Context) {
	every := d.CampaignEvery
	if every == 0 {
		every = 5 * time.Second
	}
	for {
		d.campaignTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// campaignTick moves every running campaign one step: it applies the stop
// conditions, notes a batch that has ended, and starts the next batch.
func (d *Daemon) campaignTick(ctx context.Context) {
	// One step at a time: the loop and a poke may come together.
	d.campaignMu.Lock()
	defer d.campaignMu.Unlock()
	d.mu.Lock()
	var names []string
	for name, c := range d.ctl.Campaigns {
		if c.State == "running" {
			names = append(names, name)
		}
	}
	d.mu.Unlock()
	sort.Strings(names)
	for _, name := range names {
		if err := d.campaignStep(ctx, name); err != nil {
			d.Log.Error("campaign", "campaign", name, "error", err)
		}
	}
}

func (d *Daemon) campaignStep(ctx context.Context, name string) error {
	d.mu.Lock()
	c := d.ctl.Campaigns[name]
	if c == nil || c.State != "running" {
		d.mu.Unlock()
		return nil
	}
	cur := -1
	for i, b := range c.Batches {
		if b.State == "waiting" || b.State == "running" {
			cur = i
			break
		}
	}
	var b CampaignBatchState
	if cur >= 0 {
		b = c.Batches[cur]
	}
	d.mu.Unlock()
	now := time.Now()
	if cur < 0 {
		return d.endCampaign(ctx, name, "finished", "every batch has ended")
	}
	def := c.Def.Batches[cur]
	if at := c.Def.Stop.At; !at.IsZero() && now.After(at) {
		if b.State == "running" {
			if _, err := d.stopBatch(ctx, b.Batch, "campaign "+name+" stopped at "+at.Format(time.RFC3339)+" before it started", nil); err != nil && !errors.Is(err, errNoBatch) {
				return err
			}
		}
		return d.endCampaign(ctx, name, "stopped", "its stop time "+at.Format(time.RFC3339)+" has come")
	}
	if b.State == "waiting" {
		if at := def.Stop.At; !at.IsZero() && now.After(at) {
			return d.setBatch(name, cur, "stopped", "its stop time "+at.Format(time.RFC3339)+" came before it started")
		}
		resp, _, err := d.start(ctx, StartRequest{Arms: def.Arms, Tasks: def.Tasks, Count: def.Runs, Order: def.Order, Lane: def.Lane,
			Key: campaignKey(name, def.Name)})
		if err != nil {
			d.setBatch(name, cur, "stopped", "it could not start: "+err.Error())
			return d.endCampaign(ctx, name, "stopped", "batch "+def.Name+" could not start: "+err.Error())
		}
		d.mu.Lock()
		c.Batches[cur].Batch, c.Batches[cur].State, c.Batches[cur].Started = resp.Batch, "running", now.UTC()
		err = d.saveControl()
		d.mu.Unlock()
		d.Log.Info("campaign: batch started", "campaign", name, "batch", resp.Batch, "runs", len(resp.RunIDs))
		return err
	}

	// A running batch: has a stop condition come, or has it ended?
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: b.Batch})
	if err != nil {
		return err
	}
	why, whole := stopReason(def.Stop, recs, now)
	if why == "" {
		if why, _ = stopReason(c.Def.Stop, recs, now); why != "" {
			whole = true
		}
	}
	if why != "" {
		if _, err := d.stopBatch(ctx, b.Batch, "campaign "+name+": "+why+"; stopped before it started", nil); err != nil && !errors.Is(err, errNoBatch) {
			return err
		}
		if err := d.setBatch(name, cur, "stopped", why); err != nil {
			return err
		}
		if whole {
			return d.endCampaign(ctx, name, "stopped", "batch "+def.Name+": "+why)
		}
		return nil
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return err
	}
	for _, it := range queue {
		if it.Batch == b.Batch {
			return nil // still going
		}
	}
	if d.batchFlight(b.Batch) != nil {
		return nil
	}
	return d.setBatch(name, cur, "finished", "")
}

// stopReason says whether a stop condition holds for a batch's runs, and
// whether it is a time (which ends the whole campaign when it is the
// campaign's).
func stopReason(s config.CampaignStop, recs []store.Run, now time.Time) (string, bool) {
	if !s.At.IsZero() && now.After(s.At) {
		return "its stop time " + s.At.Format(time.RFC3339) + " has come", true
	}
	if s.OnInvalid {
		for _, r := range recs {
			if r.Verdict == store.Invalid {
				return "run " + r.ID + " is INVALID", false
			}
		}
	}
	if n := s.AfterConsecutivePass; n > 0 {
		byArm := map[string][]store.Run{}
		for _, r := range recs {
			if r.Verdict != store.Skipped {
				byArm[r.ArmID] = append(byArm[r.ArmID], r)
			}
		}
		if len(byArm) == 0 {
			return "", false
		}
		for _, runs := range byArm {
			sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
			if len(runs) < n {
				return "", false
			}
			for _, r := range runs[len(runs)-n:] {
				if r.Verdict != store.Pass {
					return "", false
				}
			}
		}
		return fmt.Sprintf("the last %d runs of every arm passed", n), false
	}
	return "", false
}

func (d *Daemon) setBatch(campaign string, i int, state, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.ctl.Campaigns[campaign]
	c.Batches[i].State, c.Batches[i].Reason, c.Batches[i].Ended = state, reason, time.Now().UTC()
	d.Log.Info("campaign: batch "+state, "campaign", campaign, "batch", c.Batches[i].Batch, "reason", reason)
	return d.saveControl()
}

// endCampaign marks a campaign over: every batch not yet ended is stopped,
// and the end is notified.
func (d *Daemon) endCampaign(ctx context.Context, name, state, reason string) error {
	d.mu.Lock()
	c := d.ctl.Campaigns[name]
	c.State, c.Reason, c.Ended = state, reason, time.Now().UTC()
	for i := range c.Batches {
		if c.Batches[i].State == "waiting" || c.Batches[i].State == "running" {
			c.Batches[i].State, c.Batches[i].Reason = "stopped", "the campaign "+state
		}
	}
	err := d.saveControl()
	d.mu.Unlock()
	d.notify(ctx, Notification{Kind: "campaign", Status: state, Gate: name, Detail: reason})
	return err
}

// CampaignRequest starts a campaign from campaigns/<name>.toml.
type CampaignRequest struct {
	Name   string `json:"name"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// CampaignView is a campaign's state; Repeated says that a start found it
// already there.
type CampaignView struct {
	CampaignState
	Name     string `json:"name"`
	Repeated bool   `json:"repeated,omitempty"`
}

func (d *Daemon) handleCampaignStart(w http.ResponseWriter, r *http.Request) {
	var req CampaignRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.Name == "" {
		fail(w, http.StatusBadRequest, "name the campaign")
		return
	}
	d.mu.Lock()
	if c := d.ctl.Campaigns[req.Name]; c != nil {
		v := CampaignView{CampaignState: c.snapshot(), Name: req.Name, Repeated: true}
		d.mu.Unlock()
		// A repeated start starts nothing (050 §4, idempotency).
		reply(w, http.StatusOK, v)
		return
	}
	d.mu.Unlock()
	def, err := config.LoadCampaign(filepath.Join(d.Repo, "campaigns"), req.Name)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	// Every batch must be one the daemon would queue.
	for _, b := range def.Batches {
		if _, code, err := d.start(r.Context(), StartRequest{Arms: b.Arms, Tasks: b.Tasks, Count: b.Runs, Order: b.Order, Lane: b.Lane, DryRun: true}); err != nil {
			fail(w, code, "campaign %s, batch %s: %v", req.Name, b.Name, err)
			return
		}
	}
	c := &CampaignState{Def: def, State: "running", Started: time.Now().UTC()}
	for _, b := range def.Batches {
		c.Batches = append(c.Batches, CampaignBatchState{Name: b.Name, State: "waiting"})
	}
	if req.DryRun {
		reply(w, http.StatusOK, CampaignView{CampaignState: c.snapshot(), Name: req.Name})
		return
	}
	d.mu.Lock()
	d.ctl.Campaigns[req.Name] = c
	err = d.saveControl()
	v := CampaignView{CampaignState: c.snapshot(), Name: req.Name}
	d.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the control file: %v", err)
		return
	}
	d.Log.Info("campaign started", "campaign", req.Name, "batches", len(def.Batches))
	d.pokeCampaigns()
	reply(w, http.StatusAccepted, v)
}

func (d *Daemon) handleCampaigns(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	out := []CampaignView{}
	for name, c := range d.ctl.Campaigns {
		out = append(out, CampaignView{CampaignState: c.snapshot(), Name: name})
	}
	d.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	reply(w, http.StatusOK, out)
}

func (d *Daemon) handleCampaign(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	d.mu.Lock()
	c := d.ctl.Campaigns[name]
	var v CampaignView
	if c != nil {
		v = CampaignView{CampaignState: c.snapshot(), Name: name}
	}
	d.mu.Unlock()
	if c == nil {
		fail(w, http.StatusNotFound, "there is no campaign %s", name)
		return
	}
	reply(w, http.StatusOK, v)
}

// handleCampaignControl pauses, resumes or stops a campaign. Pausing holds
// its current batch as `batch pause` does (its run in flight held and the
// endpoint freed, unless keep_model); stopping is graceful: the run in
// flight finishes and nothing else of the campaign starts.
func (d *Daemon) handleCampaignControl(verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		name := r.PathValue("name")
		var req struct {
			KeepModel bool `json:"keep_model"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		d.mu.Lock()
		c := d.ctl.Campaigns[name]
		if c == nil {
			d.mu.Unlock()
			fail(w, http.StatusNotFound, "there is no campaign %s", name)
			return
		}
		state := c.State
		current := ""
		for _, b := range c.Batches {
			if b.State == "running" {
				current = b.Batch
			}
		}
		d.mu.Unlock()
		var err error
		switch {
		case verb == "pause" && state == "running":
			d.mu.Lock()
			c.State = "paused"
			if current != "" {
				d.ctl.Held[current] = time.Now().UTC()
			}
			err = d.saveControl()
			d.mu.Unlock()
			if f := d.batchFlight(current); current != "" && f != nil {
				if h, ok := d.Runner.(holder); ok && h.Hold(f.runID) == nil {
					go d.afterHeld(f, !req.KeepModel)
				}
			}
		case verb == "resume" && state == "paused":
			d.mu.Lock()
			c.State = "running"
			delete(d.ctl.Held, current)
			err = d.saveControl()
			d.mu.Unlock()
			if f := d.batchFlight(current); current != "" && f != nil {
				if h, ok := d.Runner.(holder); ok {
					h.Release(f.runID)
				}
			}
			d.poke()
			d.pokeCampaigns()
		case verb == "stop" && (state == "running" || state == "paused"):
			if current != "" {
				if _, err := d.stopBatch(ctx, current, "campaign "+name+" stopped by the operator before it started", nil); err != nil && !errors.Is(err, errNoBatch) {
					fail(w, http.StatusInternalServerError, "%v", err)
					return
				}
			}
			err = d.endCampaign(ctx, name, "stopped", "stopped by the operator")
		default:
			fail(w, http.StatusConflict, "campaign %s is %s: it cannot be %s", name, state, map[string]string{"pause": "paused", "resume": "resumed", "stop": "stopped"}[verb])
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		d.mu.Lock()
		v := CampaignView{CampaignState: c.snapshot(), Name: name}
		d.mu.Unlock()
		reply(w, http.StatusOK, v)
	}
}

// pokeCampaigns moves the campaigns at once instead of at the next tick.
func (d *Daemon) pokeCampaigns() {
	go d.campaignTick(context.Background())
}
