package daemon

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/task"
)

// The API's types. Every interface is a client of these.

// Status is the answer to GET /v1/status.
type Status struct {
	Version  string       `json:"version"`
	Started  time.Time    `json:"started"`
	Draining bool         `json:"draining"`
	Lanes    []LaneStatus `json:"lanes"`
	Queue    []QueueEntry `json:"queue"`
}

// LaneStatus is one lane in a Status.
type LaneStatus struct {
	Lane      string    `json:"lane"`
	Host      string    `json:"host"`
	State     string    `json:"state"`
	RunID     string    `json:"run_id,omitempty"`
	Arm       string    `json:"arm,omitempty"`
	Task      string    `json:"task,omitempty"`
	Since     time.Time `json:"since,omitzero"`
	Endpoints []string  `json:"endpoints"`
}

// QueueEntry is a queued run.
type QueueEntry struct {
	RunID      string    `json:"run_id"`
	Batch      string    `json:"batch,omitempty"`
	Arm        string    `json:"arm"`
	Task       string    `json:"task"`
	Lane       string    `json:"lane"`
	EnqueuedAt time.Time `json:"enqueued_at"`
	Running    bool      `json:"running"`
}

// StartRequest is the body of POST /v1/runs.
type StartRequest struct {
	Arm   string `json:"arm"`
	Task  string `json:"task"`
	Lane  string `json:"lane,omitempty"`
	Count int    `json:"count,omitempty"`
	// Key makes the command safe to repeat: a second request with the
	// same key starts nothing and answers with what the first one made.
	Key    string `json:"idempotency_key,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// StartResponse says what was, or would be, queued.
type StartResponse struct {
	Batch    string   `json:"batch"`
	RunIDs   []string `json:"run_ids"`
	Lane     string   `json:"lane"`
	ArmID    string   `json:"arm_id"`
	Task     string   `json:"task"`
	Image    string   `json:"image"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Repeated bool     `json:"repeated,omitempty"` // the key had been used
}

// RunView is a run as the API shows it.
type RunView struct {
	ID            string          `json:"id"`
	State         string          `json:"state"` // queued, running, finished
	Batch         string          `json:"batch,omitempty"`
	Task          string          `json:"task"`
	TaskVersion   int             `json:"task_version,omitempty"`
	Arm           string          `json:"arm,omitempty"`
	ArmID         string          `json:"arm_id,omitempty"`
	Lane          string          `json:"lane"`
	Host          string          `json:"host,omitempty"`
	Verdict       string          `json:"verdict,omitempty"`
	VerdictReason string          `json:"verdict_reason,omitempty"`
	PriorVerdict  string          `json:"prior_verdict,omitempty"`
	AgentEnd      string          `json:"agent_end,omitempty"`
	Claimed       *bool           `json:"claimed_success,omitempty"`
	StartedAt     time.Time       `json:"started_at,omitzero"`
	EndedAt       time.Time       `json:"ended_at,omitzero"`
	WallMS        int64           `json:"wall_ms,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	Model         string          `json:"model,omitempty"`
	Endpoint      string          `json:"endpoint,omitempty"`
	Thinking      string          `json:"thinking,omitempty"`
	SDKRelease    string          `json:"sdk_release,omitempty"`
	Imported      bool            `json:"imported,omitempty"`
	ResolvedArm   json.RawMessage `json:"resolved_arm,omitempty"`
	Provenance    json.RawMessage `json:"provenance,omitempty"`
	Blobs         []BlobView      `json:"blobs,omitempty"`
}

// BlobView is one piece of a run's raw material.
type BlobView struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func view(r *store.Run, full bool) RunView {
	v := RunView{
		ID: r.ID, State: "finished", Batch: r.Batch, Task: r.Suite + "/" + r.TaskID, TaskVersion: r.TaskVersion, ArmID: r.ArmID,
		Lane: r.Lane, Host: r.Host, Verdict: string(r.Verdict), VerdictReason: r.VerdictReason, PriorVerdict: string(r.PriorVerdict),
		AgentEnd: r.AgentEnd, Claimed: r.ClaimedSuccess, StartedAt: r.StartedAt, EndedAt: r.EndedAt, WallMS: r.WallMS,
		Provider: r.Provider, Model: r.Model, Endpoint: r.Endpoint, Thinking: r.Thinking, SDKRelease: r.Harness, Imported: r.Imported,
	}
	if full {
		v.ResolvedArm, v.Provenance = r.Arm, r.Provenance
	}
	return v
}

type apiError struct {
	Error string `json:"error"`
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, format string, args ...any) {
	reply(w, status, apiError{fmt.Sprintf(format, args...)})
}

// Handler returns the API.
//
// Reading needs no token when the request comes from this host; everything
// that changes something needs it always.
func (d *Daemon) Handler() http.Handler {
	d.init()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", d.handleStatus)
	mux.HandleFunc("GET /v1/runs", d.handleRuns)
	mux.HandleFunc("POST /v1/runs", d.handleStart)
	mux.HandleFunc("GET /v1/runs/{id}", d.handleRun)
	mux.HandleFunc("GET /v1/runs/{id}/stream", d.handleStream)
	mux.HandleFunc("GET /v1/runs/{id}/blobs/{kind}", d.handleBlob)
	mux.HandleFunc("POST /v1/runs/{id}/stop", d.handleStop)
	mux.HandleFunc("POST /v1/runs/{id}/abort", d.handleAbort)
	mux.HandleFunc("POST /v1/runs/{id}/invalidate", d.handleInvalidate)
	mux.HandleFunc("POST /v1/drain", func(w http.ResponseWriter, r *http.Request) {
		d.Drain()
		reply(w, http.StatusOK, map[string]bool{"draining": true})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !d.allowed(r) {
			fail(w, http.StatusUnauthorized, "this request needs the API token")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (d *Daemon) allowed(r *http.Request) bool {
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if d.Token != "" && subtle.ConstantTimeCompare([]byte(given), []byte(d.Token)) == 1 {
		return true
	}
	if r.Method != http.MethodGet {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (d *Daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d.mu.Lock()
	st := Status{Version: d.Version, Started: d.started, Draining: d.draining}
	flying := map[string]*flight{}
	for lane, f := range d.flights {
		flying[lane] = f
	}
	d.mu.Unlock()
	states, err := d.State.LaneStates(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	stored := map[string]store.LaneState{}
	for _, ls := range states {
		stored[ls.Lane] = ls
	}
	var names []string
	for n := range d.Local.Lanes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		l := d.Local.Lanes[n]
		ls := LaneStatus{Lane: n, Host: l.Host, State: "idle", Endpoints: l.Endpoints}
		if s, ok := stored[n]; ok && s.State != "" {
			ls.State = s.State
		}
		if f := flying[n]; f != nil {
			ls.State, ls.RunID, ls.Arm, ls.Task, ls.Since = "running", f.runID, f.item.Arm, f.item.Task, f.started
		}
		st.Lanes = append(st.Lanes, ls)
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	st.Queue = []QueueEntry{}
	for _, it := range queue {
		f := flying[it.Lane]
		st.Queue = append(st.Queue, QueueEntry{RunID: it.RunID, Batch: it.Batch, Arm: it.Arm, Task: it.Task, Lane: it.Lane,
			EnqueuedAt: it.EnqueuedAt, Running: f != nil && f.runID == it.RunID})
	}
	reply(w, http.StatusOK, st)
}

func (d *Daemon) handleStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req StartRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the request is not understood: %v", err)
		return
	}
	if req.Count == 0 {
		req.Count = 1
	}
	if req.Arm == "" || req.Task == "" || req.Count < 1 || req.Count > 1000 {
		fail(w, http.StatusBadRequest, "a run needs an arm, a task and a count between 1 and 1000")
		return
	}
	d.mu.Lock()
	draining := d.draining
	d.mu.Unlock()
	if draining && !req.DryRun {
		fail(w, http.StatusConflict, "the daemon is draining and takes no new runs")
		return
	}
	arm, err := config.LoadArm(filepath.Join(d.Repo, "arms"), req.Arm)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	if arm.Template {
		fail(w, http.StatusUnprocessableEntity, "%s is a template, not an arm", req.Arm)
		return
	}
	if d.Local.Endpoints[arm.Endpoint] == nil {
		fail(w, http.StatusUnprocessableEntity, "arm %s names endpoint %q, which this installation does not have", req.Arm, arm.Endpoint)
		return
	}
	tk, err := task.Find(filepath.Join(d.Repo, "tasks"), req.Task)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	lane, err := d.laneFor(arm, req.Lane)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	resp := StartResponse{Lane: lane, ArmID: arm.ID, Task: tk.Name(), Image: LaneImage(tk.Layers), DryRun: req.DryRun}
	now := time.Now()
	resp.Batch = "b" + run.NewID(now)
	if req.Key != "" {
		// The key names the batch, so a repeated command finds it.
		resp.Batch = "k-" + req.Key
		var ids []string
		queue, err := d.State.Queue(ctx)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for _, it := range queue {
			if it.Batch == resp.Batch {
				ids = append(ids, it.RunID)
			}
		}
		runs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: resp.Batch})
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for _, r := range runs {
			ids = append(ids, r.ID)
		}
		if len(ids) > 0 {
			sort.Strings(ids)
			resp.RunIDs, resp.Repeated = dedupe(ids), true
			reply(w, http.StatusOK, resp)
			return
		}
	}
	items := make([]store.QueueItem, req.Count)
	// Ids sort by time; the runs of one command share a stem, and the
	// suffix keeps them in the order they were queued.
	stem := run.NewID(now)
	for i := range items {
		id := stem
		if req.Count > 1 {
			id = fmt.Sprintf("%s-%03d", stem, i+1)
		}
		items[i] = store.QueueItem{RunID: id, Batch: resp.Batch, Arm: req.Arm, Task: tk.Name(), Lane: lane, EnqueuedAt: now}
		resp.RunIDs = append(resp.RunIDs, id)
	}
	if req.DryRun {
		reply(w, http.StatusOK, resp)
		return
	}
	if err := d.State.Enqueue(ctx, items); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	d.poke()
	reply(w, http.StatusAccepted, resp)
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func (d *Daemon) handleRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.RunFilter{Suite: q.Get("suite"), TaskID: q.Get("task"), Model: q.Get("model"), Batch: q.Get("batch"), Verdict: store.Verdict(strings.ToUpper(q.Get("verdict")))}
	if f.Verdict != "" && !f.Verdict.Valid() {
		fail(w, http.StatusBadRequest, "%q is not a verdict", q.Get("verdict"))
		return
	}
	if v := q.Get("imported"); v != "" {
		b := v == "true"
		f.Imported = &b
	}
	f.Newest = true
	f.Limit = 50
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 10000 {
		f.Limit = v
	}
	out := []RunView{}
	// What has not finished comes first; it has no record yet.
	if f.Verdict == "" && f.Imported == nil {
		queue, err := d.State.Queue(ctx)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for i := len(queue) - 1; i >= 0; i-- {
			it := queue[i]
			if f.Batch != "" && it.Batch != f.Batch {
				continue
			}
			out = append(out, d.queuedView(it))
		}
	}
	runs, err := d.State.ListRuns(ctx, f)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for i := range runs {
		out = append(out, view(&runs[i], false))
	}
	reply(w, http.StatusOK, out)
}

func (d *Daemon) queuedView(it store.QueueItem) RunView {
	v := RunView{ID: it.RunID, State: "queued", Batch: it.Batch, Task: it.Task, Arm: it.Arm, Lane: it.Lane}
	if f := d.flight(it.RunID); f != nil {
		v.State, v.StartedAt = "running", f.started
	}
	return v
}

func (d *Daemon) handleRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	rec, err := d.State.GetRun(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		queue, qerr := d.State.Queue(ctx)
		if qerr != nil {
			fail(w, http.StatusInternalServerError, "%v", qerr)
			return
		}
		for _, it := range queue {
			if it.RunID == id {
				reply(w, http.StatusOK, d.queuedView(it))
				return
			}
		}
		fail(w, http.StatusNotFound, "there is no run %s", id)
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	v := view(rec, true)
	refs, err := d.State.BlobRefs(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for _, ref := range refs {
		v.Blobs = append(v.Blobs, BlobView{Kind: ref.Kind, Name: ref.Name, Hash: string(ref.Hash), Size: ref.Size})
	}
	reply(w, http.StatusOK, v)
}

// handleStream sends what a run in flight prints, as server-sent events:
// first what is held, then every line as it arrives, then an "end" event.
func (d *Daemon) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f := d.flight(id)
	if f == nil {
		if _, err := d.State.GetRun(r.Context(), id); err == nil {
			fail(w, http.StatusGone, "run %s has finished; read its events with `automaticum logs %s`", id, id)
			return
		}
		fail(w, http.StatusNotFound, "run %s is not in flight", id)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "this connection cannot stream")
		return
	}
	held, ch := f.subscribe()
	defer f.unsubscribe(ch)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	send := func(line []byte) {
		// A line is one JSON object and holds no newline.
		w.Write([]byte("data: "))
		w.Write(line)
		w.Write([]byte("\n\n"))
	}
	for _, line := range held {
		send(line)
	}
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case line, open := <-ch:
			if !open {
				io.WriteString(w, "event: end\ndata: {}\n\n")
				fl.Flush()
				return
			}
			send(line)
			fl.Flush()
		}
	}
}

func (d *Daemon) handleBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, kind, name := r.PathValue("id"), r.PathValue("kind"), r.URL.Query().Get("name")
	refs, err := d.State.BlobRefs(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for _, ref := range refs {
		if ref.Kind != kind || (name != "" && ref.Name != name) {
			continue
		}
		rd, _, err := d.Blobs.Reader(ctx, ref.Hash)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		defer rd.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Automaticum-Blob", string(ref.Hash))
		io.Copy(w, rd)
		return
	}
	fail(w, http.StatusNotFound, "run %s has no %s", id, kind)
}

// handleStop takes a queued run off the queue. A run in flight is not
// stopped this way: it is let finish, or aborted.
func (d *Daemon) handleStop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if d.flight(id) != nil {
		fail(w, http.StatusConflict, "run %s is in flight; it finishes by itself, or `automaticum run abort %s` ends it", id, id)
		return
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for _, it := range queue {
		if it.RunID != id {
			continue
		}
		rec := &store.Run{ID: id, Batch: it.Batch, Lane: it.Lane, Verdict: store.Skipped, VerdictReason: "stopped by the operator before it started",
			StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC()}
		if suite, tid, ok := strings.Cut(it.Task, "/"); ok {
			rec.Suite, rec.TaskID = suite, tid
		}
		if err := d.State.UpsertRun(ctx, rec); err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		if err := d.State.Unqueue(ctx, id); err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		reply(w, http.StatusOK, view(rec, false))
		return
	}
	fail(w, http.StatusNotFound, "run %s is not queued", id)
}

func (d *Daemon) handleAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !d.Abort(id) {
		fail(w, http.StatusNotFound, "run %s is not in flight", id)
		return
	}
	reply(w, http.StatusAccepted, map[string]string{"run_id": id, "state": "aborting"})
}

// handleInvalidate marks a finished run INVALID. What the verdict was is
// kept beside it.
func (d *Daemon) handleInvalidate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		fail(w, http.StatusBadRequest, "marking a run invalid needs a reason")
		return
	}
	rec, err := d.State.GetRun(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "there is no finished run %s", id)
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if rec.PriorVerdict == "" {
		rec.PriorVerdict = rec.Verdict
	}
	rec.Verdict, rec.VerdictReason = store.Invalid, "operator: "+strings.TrimSpace(req.Reason)
	if err := d.State.UpsertRun(ctx, rec); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	reply(w, http.StatusOK, view(rec, false))
}
