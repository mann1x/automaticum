package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/gate"
	"github.com/mann1x/automaticum/internal/ingest"
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
	// Waiting is why the lane's next run has not started.
	Waiting string `json:"waiting,omitempty"`
	// Drained says the lane starts no run after the one in flight.
	Drained bool `json:"drained,omitempty"`
}

// DoctorEntry is every gate's answer for one arm on one task on one lane.
type DoctorEntry struct {
	Lane  string        `json:"lane"`
	Arm   string        `json:"arm"`
	Task  string        `json:"task"`
	Gates []gate.Result `json:"gates"`
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
	// Stopped is why it does not start now: its lane is drained or its
	// batch paused.
	Stopped string `json:"stopped,omitempty"`
	// RetryOf is the run it retries.
	RetryOf string `json:"retry_of,omitempty"`
}

// StartRequest is the body of POST /v1/runs.
type StartRequest struct {
	Arm string `json:"arm,omitempty"`
	// Arms starts a batch of several arms, Count runs of each. Order says
	// how they are queued: "interleaved" (the default: one run of each arm
	// in turn, so that drift of the host does not land on one arm) or
	// "blocks" (all runs of one arm, then the next).
	Arms  []string `json:"arms,omitempty"`
	Order string   `json:"order,omitempty"`
	Task  string   `json:"task"`
	Lane  string   `json:"lane,omitempty"`
	Count int      `json:"count,omitempty"`
	// Key makes the command safe to repeat: a second request with the
	// same key starts nothing and answers with what the first one made.
	Key    string `json:"idempotency_key,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// StartResponse says what was, or would be, queued.
type StartResponse struct {
	Batch  string   `json:"batch"`
	RunIDs []string `json:"run_ids"`
	Lane   string   `json:"lane"`
	ArmID  string   `json:"arm_id"`
	// ArmIDs holds every arm of the batch by name, when there are several.
	ArmIDs map[string]string `json:"arm_ids,omitempty"`
	// Order is the arm of each run, in the order they are queued.
	Order    []string `json:"order,omitempty"`
	Task     string   `json:"task"`
	Image    string   `json:"image"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Repeated bool     `json:"repeated,omitempty"` // the key had been used
}

// RunView is a run as the API shows it.
type RunView struct {
	ID            string    `json:"id"`
	State         string    `json:"state"` // queued, running, finished
	Batch         string    `json:"batch,omitempty"`
	Task          string    `json:"task"`
	TaskVersion   int       `json:"task_version,omitempty"`
	Arm           string    `json:"arm,omitempty"`
	ArmID         string    `json:"arm_id,omitempty"`
	Lane          string    `json:"lane"`
	Host          string    `json:"host,omitempty"`
	Verdict       string    `json:"verdict,omitempty"`
	VerdictReason string    `json:"verdict_reason,omitempty"`
	PriorVerdict  string    `json:"prior_verdict,omitempty"`
	AgentEnd      string    `json:"agent_end,omitempty"`
	Claimed       *bool     `json:"claimed_success,omitempty"`
	StartedAt     time.Time `json:"started_at,omitzero"`
	EndedAt       time.Time `json:"ended_at,omitzero"`
	WallMS        int64     `json:"wall_ms,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	Model         string    `json:"model,omitempty"`
	Endpoint      string    `json:"endpoint,omitempty"`
	Thinking      string    `json:"thinking,omitempty"`
	SDKRelease    string    `json:"sdk_release,omitempty"`
	Imported      bool      `json:"imported,omitempty"`
	// Paused says that the run was held at least once; the time held is
	// not in its wall time (050 §4).
	Paused bool `json:"paused,omitempty"`
	// RetryOf is the run this one retries; RetriedBy the run that retries
	// this one.
	RetryOf     string          `json:"retry_of,omitempty"`
	RetriedBy   string          `json:"retried_by,omitempty"`
	ResolvedArm json.RawMessage `json:"resolved_arm,omitempty"`
	Provenance  json.RawMessage `json:"provenance,omitempty"`
	Blobs       []BlobView      `json:"blobs,omitempty"`
	Metrics     []MetricView    `json:"metrics,omitempty"`
	Findings    []FindingView   `json:"findings,omitempty"`
	// Settings are the run's sampling and thinking parameters as they
	// were in effect (070 §4.1).
	Settings []ingest.Setting `json:"settings,omitempty"`
}

// MetricView is one number measured on a run.
type MetricView struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// FindingView is something a detector found in a run.
type FindingView struct {
	Detector string          `json:"detector"`
	Version  string          `json:"detector_version,omitempty"`
	Severity string          `json:"severity,omitempty"`
	Turn     int             `json:"turn"`
	Count    int             `json:"count"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

// ReanalyseRequest names the runs to derive again: one run, one batch, or
// with neither every run this harness made.
type ReanalyseRequest struct {
	Run   string `json:"run,omitempty"`
	Batch string `json:"batch,omitempty"`
	// Imported also takes the runs of the older harness, which get the
	// detectors' findings only.
	Imported bool `json:"imported,omitempty"`
}

// DetectView is what the detector library says about one run, computed now
// from stored material; nothing is written.
type DetectView struct {
	Run      string                `json:"run"`
	Version  string                `json:"detector_version"`
	Blocks   int                   `json:"blocks"`
	Calls    int                   `json:"tool_calls"`
	Hits     []BlockHit            `json:"hits"`
	Repeated []detect.RepeatedCall `json:"repeated_tool_calls"`
}

// BlockHit is one detector firing on one block of a run.
type BlockHit struct {
	Turn    int    `json:"turn"`
	Channel string `json:"channel"`
	Chars   int    `json:"chars"`
	detect.Hit
}

// ReanalyseStatus is the state of the daemon's one reanalysis: the one
// running, or the last one that ran.
type ReanalyseStatus struct {
	Running bool              `json:"running"`
	Request ReanalyseRequest  `json:"request"`
	Total   int               `json:"total"`
	Done    int               `json:"done"`
	Failed  map[string]string `json:"failed,omitempty"` // by run, why
	Started time.Time         `json:"started,omitzero"`
	Ended   time.Time         `json:"ended,omitzero"`
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
		Provider: r.Provider, Model: r.Model, Endpoint: r.Endpoint, Thinking: r.Thinking, SDKRelease: r.Harness, Imported: r.Imported, Paused: r.Paused,
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
	mux.HandleFunc("POST /v1/runs/{id}/pause", d.handlePause)
	mux.HandleFunc("POST /v1/runs/{id}/resume", d.handleResume)
	mux.HandleFunc("GET /v1/compare", d.handleCompare)
	mux.HandleFunc("POST /v1/runs/{id}/invalidate", d.handleInvalidate)
	mux.HandleFunc("POST /v1/runs/{id}/identify", d.handleIdentify)
	mux.HandleFunc("GET /v1/runs/{id}/detect", d.handleDetect)
	mux.HandleFunc("GET /v1/doctor", d.handleDoctor)
	mux.HandleFunc("POST /v1/reanalyse", d.handleReanalyse)
	mux.HandleFunc("GET /v1/reanalyse", func(w http.ResponseWriter, r *http.Request) { reply(w, http.StatusOK, d.reanalyseStatus()) })
	mux.HandleFunc("POST /v1/runs/{id}/retry", d.handleRetry)
	mux.HandleFunc("POST /v1/lanes/{lane}/drain", d.handleLane(true))
	mux.HandleFunc("POST /v1/lanes/{lane}/resume", d.handleLane(false))
	mux.HandleFunc("POST /v1/batches/{batch}/pause", d.handleBatchPause)
	mux.HandleFunc("POST /v1/batches/{batch}/resume", d.handleBatchResume)
	mux.HandleFunc("POST /v1/batches/{batch}/stop", d.handleBatchStop)
	mux.HandleFunc("POST /v1/batches/{batch}/retry", d.handleRetry)
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
			var det laneDetail
			if s.State == "waiting" && json.Unmarshal(s.Detail, &det) == nil {
				ls.RunID, ls.Arm, ls.Task, ls.Waiting = s.RunID, det.Item.Arm, det.Item.Task, det.Waiting
			}
		}
		if f := flying[n]; f != nil {
			ls.State, ls.RunID, ls.Arm, ls.Task, ls.Since = "running", f.runID, f.item.Arm, f.item.Task, f.started
			if held, _ := f.holding(); held {
				ls.State = "paused"
			}
		}
		d.mu.Lock()
		_, ls.Drained = d.ctl.Drained[n]
		d.mu.Unlock()
		if ls.Drained && flying[n] == nil {
			ls.State, ls.RunID, ls.Arm, ls.Task, ls.Waiting = "drained", "", "", "", ""
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
		e := QueueEntry{RunID: it.RunID, Batch: it.Batch, Arm: it.Arm, Task: it.Task, Lane: it.Lane,
			EnqueuedAt: it.EnqueuedAt, Running: f != nil && f.runID == it.RunID}
		d.mu.Lock()
		if !e.Running {
			e.Stopped = d.stopped(it)
		}
		e.RetryOf = d.ctl.Retries[it.RunID]
		d.mu.Unlock()
		st.Queue = append(st.Queue, e)
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
	names := req.Arms
	if req.Arm != "" {
		names = append([]string{req.Arm}, names...)
	}
	if req.Order == "" {
		req.Order = "interleaved"
	}
	if len(names) == 0 || req.Task == "" || req.Count < 1 || req.Count*len(names) > 1000 {
		fail(w, http.StatusBadRequest, "a run needs an arm, a task and at most 1000 runs in all")
		return
	}
	if req.Order != "interleaved" && req.Order != "blocks" {
		fail(w, http.StatusBadRequest, "the order is \"interleaved\" or \"blocks\", not %q", req.Order)
		return
	}
	d.mu.Lock()
	draining := d.draining
	d.mu.Unlock()
	if draining && !req.DryRun {
		fail(w, http.StatusConflict, "the daemon is draining and takes no new runs")
		return
	}
	tk, err := task.Find(filepath.Join(d.Repo, "tasks"), req.Task)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "%v", err)
		return
	}
	var arm *config.Arm
	lane, ids := "", map[string]string{}
	for _, name := range names {
		a, err := config.LoadArm(filepath.Join(d.Repo, "arms"), name)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity, "%v", err)
			return
		}
		if a.Template {
			fail(w, http.StatusUnprocessableEntity, "%s is a template, not an arm", name)
			return
		}
		if d.Local.Endpoints[a.Endpoint] == nil {
			fail(w, http.StatusUnprocessableEntity, "arm %s names endpoint %q, which this installation does not have", name, a.Endpoint)
			return
		}
		l, err := d.laneFor(a, req.Lane)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity, "%v", err)
			return
		}
		// The arms of one batch alternate on one lane: on two lanes the
		// difference between the lanes would land on the arms.
		if lane != "" && l != lane {
			fail(w, http.StatusUnprocessableEntity, "arm %s would run on lane %s and %s on lane %s; name one lane both may use", names[0], lane, name, l)
			return
		}
		if _, twice := ids[name]; twice {
			fail(w, http.StatusBadRequest, "arm %s is named twice", name)
			return
		}
		if arm == nil {
			arm = a
		}
		lane, ids[name] = l, a.ID
	}
	resp := StartResponse{Lane: lane, ArmID: arm.ID, Task: tk.Name(), Image: LaneImage(tk.Layers), DryRun: req.DryRun}
	if len(names) > 1 {
		resp.ArmIDs = ids
	}
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
	// The arm of each run, in the order they are queued.
	var order []string
	if req.Order == "blocks" {
		for _, name := range names {
			for range req.Count {
				order = append(order, name)
			}
		}
	} else {
		for range req.Count {
			order = append(order, names...)
		}
	}
	if len(names) > 1 {
		resp.Order = order
	}
	items := make([]store.QueueItem, len(order))
	// Ids sort by time; the runs of one command share a stem, and the
	// suffix keeps them in the order they were queued.
	stem := run.NewID(now)
	for i := range items {
		id := stem
		if len(items) > 1 {
			id = fmt.Sprintf("%s-%03d", stem, i+1)
		}
		items[i] = store.QueueItem{RunID: id, Batch: resp.Batch, Arm: order[i], Task: tk.Name(), Lane: lane, EnqueuedAt: now}
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
				v := d.queuedView(it)
				d.retryLinks(&v)
				reply(w, http.StatusOK, v)
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
	d.retryLinks(&v)
	refs, err := d.State.BlobRefs(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for _, ref := range refs {
		v.Blobs = append(v.Blobs, BlobView{Kind: ref.Kind, Name: ref.Name, Hash: string(ref.Hash), Size: ref.Size})
	}
	metrics, err := d.State.Metrics(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for _, m := range metrics {
		v.Metrics = append(v.Metrics, MetricView{Name: m.Name, Value: m.Value})
	}
	findings, err := d.State.Findings(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	for _, f := range findings {
		v.Findings = append(v.Findings, FindingView{Detector: f.Detector, Version: f.DetectorVersion, Severity: f.Severity,
			Turn: f.Turn, Count: f.Count, Evidence: f.Evidence})
	}
	// What was in effect, for a run of this harness with an arm on record.
	var arm config.Arm
	if !rec.Imported && len(rec.Arm) > 0 && json.Unmarshal(rec.Arm, &arm) == nil && arm.Model.Model != "" {
		var prov struct {
			EndpointBefore *struct {
				Parameters string `json:"parameters"`
			} `json:"endpoint_before"`
		}
		json.Unmarshal(rec.Provenance, &prov)
		tag, served := "", 0.0
		if prov.EndpointBefore != nil {
			tag = prov.EndpointBefore.Parameters
		}
		for _, m := range metrics {
			if m.Name == "thinking.budget_tokens.served" {
				served = m.Value
			}
		}
		if prov.EndpointBefore != nil {
			v.Settings = ingest.InEffect(&arm, tag, findings, served)
		}
	}
	reply(w, http.StatusOK, v)
}

// handleReanalyse starts deriving metrics, tool calls and findings again
// from what is stored. Verdicts and raw material are not touched. The work
// goes on in the daemon whether or not the caller waits: an old corpus takes
// minutes. One reanalysis runs at a time.
func (d *Daemon) handleReanalyse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req ReanalyseRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil && err != io.EOF {
		fail(w, http.StatusBadRequest, "the request is not readable: %v", err)
		return
	}
	var ids []string
	if req.Run != "" {
		if _, err := d.State.GetRun(ctx, req.Run); errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "there is no finished run %s", req.Run)
			return
		}
		ids = []string{req.Run}
	} else {
		f := store.RunFilter{Batch: req.Batch}
		if !req.Imported {
			f.Imported = new(bool)
		}
		runs, err := d.State.ListRuns(ctx, f)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for _, rec := range runs {
			if rec.Verdict != store.Skipped {
				ids = append(ids, rec.ID)
			}
		}
	}
	d.mu.Lock()
	if d.reanalysis != nil && d.reanalysis.Running {
		st := *d.reanalysis
		d.mu.Unlock()
		fail(w, http.StatusConflict, "a reanalysis is running: %d of %d runs done", st.Done, st.Total)
		return
	}
	job := &ReanalyseStatus{Running: true, Request: req, Total: len(ids), Started: time.Now().UTC()}
	d.reanalysis = job
	d.mu.Unlock()
	go d.reanalyse(job, ids)
	reply(w, http.StatusAccepted, d.reanalyseStatus())
}

// reanalyse works through the runs of one reanalysis, a few at a time.
func (d *Daemon) reanalyse(job *ReanalyseStatus, ids []string) {
	ctx := context.Background()
	next := make(chan string)
	var wg sync.WaitGroup
	for range min(4, max(1, len(ids))) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range next {
				_, err := ingest.Run(ctx, d.State, d.Blobs, id)
				d.mu.Lock()
				job.Done++
				if err != nil {
					if job.Failed == nil {
						job.Failed = map[string]string{}
					}
					job.Failed[id] = err.Error()
				}
				d.mu.Unlock()
			}
		}()
	}
	for _, id := range ids {
		next <- id
	}
	close(next)
	wg.Wait()
	d.mu.Lock()
	job.Running, job.Ended = false, time.Now().UTC()
	failed := len(job.Failed)
	d.mu.Unlock()
	if d.Log != nil {
		d.Log.Info("reanalysis ended", "runs", len(ids), "failed", failed, "took", job.Ended.Sub(job.Started).Round(time.Second).String())
	}
}

// reanalyseStatus is a copy of the reanalysis's state, safe to send.
func (d *Daemon) reanalyseStatus() ReanalyseStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reanalysis == nil {
		return ReanalyseStatus{}
	}
	st := *d.reanalysis
	st.Failed = maps.Clone(st.Failed)
	return st
}

// handleDoctor asks every gate now, without starting a run: for every lane,
// every arm whose endpoint the lane may use, on every task. The query may
// narrow it to one lane, arm or task.
func (d *Daemon) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if d.Gates == nil {
		fail(w, http.StatusServiceUnavailable, "this daemon asks no gates")
		return
	}
	q := r.URL.Query()
	arms, err := config.LoadArms(filepath.Join(d.Repo, "arms"))
	if err != nil {
		fail(w, http.StatusInternalServerError, "the arms cannot be read: %v", err)
		return
	}
	tasks, err := task.LoadAll(filepath.Join(d.Repo, "tasks"))
	if err != nil {
		fail(w, http.StatusInternalServerError, "the tasks cannot be read: %v", err)
		return
	}
	var lanes, names []string
	for n := range d.Local.Lanes {
		lanes = append(lanes, n)
	}
	for n := range arms {
		names = append(names, n)
	}
	sort.Strings(lanes)
	sort.Strings(names)
	out := []DoctorEntry{}
	for _, lane := range lanes {
		if v := q.Get("lane"); v != "" && v != lane {
			continue
		}
		for _, name := range names {
			a := arms[name]
			// Without a question about one arm, a lane is asked about the
			// arms it could run.
			if v := q.Get("arm"); v != "" && v != name || v == "" && !slices.Contains(d.Local.Lanes[lane].Endpoints, a.Endpoint) {
				continue
			}
			for _, tk := range tasks {
				if v := q.Get("task"); v != "" && v != tk.Name() && v != tk.ID {
					continue
				}
				out = append(out, DoctorEntry{Lane: lane, Arm: name, Task: tk.Name(),
					Gates: d.Gates(r.Context(), lane, a, tk, LaneImage(tk.Layers))})
			}
		}
	}
	reply(w, http.StatusOK, out)
}

// handleDetect runs the detector library over one run's stored stream.
func (d *Daemon) handleDetect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, err := d.State.GetRun(ctx, id); errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "there is no finished run %s", id)
		return
	}
	refs, err := d.State.BlobRefs(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	ev := &ingest.Events{Notices: map[string]int{}}
	for _, ref := range refs {
		if ref.Kind != "events" {
			continue
		}
		rd, _, err := d.Blobs.Reader(ctx, ref.Hash)
		if err == nil {
			err = ev.Read(rd)
			rd.Close()
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "the event stream of %s: %v", id, err)
			return
		}
	}
	v := DetectView{Run: id, Version: detect.Version, Blocks: len(ev.Blocks), Calls: len(ev.Calls),
		Hits: []BlockHit{}, Repeated: detect.RepeatedCalls(ev.Calls, 0)}
	for _, b := range ev.Blocks {
		for _, h := range detect.Block(b.Text, detect.Options{}) {
			v.Hits = append(v.Hits, BlockHit{Turn: b.Turn, Channel: b.Kind, Chars: len([]rune(b.Text)), Hit: h})
		}
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
			StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC(), Provenance: askedProvenance(it)}
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

// holder is what pausing needs of the runner of runs; *run.Executor is one.
type holder interface {
	Hold(runID string) error
	Release(runID string) error
}

// handlePause holds a run in flight before its next model request and, on
// an Ollama or xOllama endpoint, then asks the server to drop the model, so
// that the device is free for something else (050 §4, D83). The request
// being answered is finished first; that can take minutes, so the answer
// comes at once and the lane's state says when the run is held.
func (d *Daemon) handlePause(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h, ok := d.Runner.(holder)
	if !ok {
		fail(w, http.StatusNotImplemented, "this daemon cannot hold a run")
		return
	}
	var req struct {
		// KeepModel leaves the model in memory: the run is held, the
		// device is not freed.
		KeepModel bool `json:"keep_model"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	d.mu.Lock()
	var f *flight
	for _, x := range d.flights {
		if x.runID == id {
			f = x
		}
	}
	d.mu.Unlock()
	if f == nil {
		fail(w, http.StatusNotFound, "run %s is not in flight", id)
		return
	}
	if err := h.Hold(id); err != nil {
		fail(w, http.StatusConflict, "%v", err)
		return
	}
	go d.afterHeld(f, !req.KeepModel)
	reply(w, http.StatusAccepted, map[string]string{"run_id": id, "state": "pausing"})
}

// afterHeld waits until the runner says the run is held, then frees the
// endpoint of every model when asked to.
func (d *Daemon) afterHeld(f *flight, unload bool) {
	log := d.Log.With("run", f.runID)
	for {
		held, over := f.holding()
		if over {
			return
		}
		if held {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Info("run is held between two requests")
	if !unload {
		return
	}
	arm, err := config.LoadArm(filepath.Join(d.Repo, "arms"), f.item.Arm)
	if err != nil {
		log.Warn("the run is held, its model stays loaded", "error", err)
		return
	}
	ep := d.Local.Endpoints[arm.Endpoint]
	if !run.Ollama(ep) {
		log.Info("the run is held; this kind of endpoint cannot be asked to drop a model")
		return
	}
	key, err := d.Local.APIKey(ep)
	if err == nil {
		hc := d.HTTP
		if hc == nil {
			hc = &http.Client{Timeout: 90 * time.Second}
		}
		var dropped []string
		// No model is kept: the point of a pause is a free device.
		dropped, err = run.Unload(context.Background(), hc, ep, key, "")
		if err == nil {
			log.Info("the endpoint dropped its models for the pause", "models", strings.Join(dropped, ", "))
		}
	}
	if err != nil {
		log.Warn("the run is held, the endpoint did not drop its model", "error", err)
	}
}

// handleResume lets a held run go on. Its next request makes the server
// load the model again; nothing else is needed.
func (d *Daemon) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h, ok := d.Runner.(holder)
	if !ok {
		fail(w, http.StatusNotImplemented, "this daemon cannot hold a run")
		return
	}
	if err := h.Release(id); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	reply(w, http.StatusAccepted, map[string]string{"run_id": id, "state": "resuming"})
}

// handleIdentify adds to a finished run's record where and on what it ran,
// when the run did not record it or its model's identity was not stated
// then (D74). The facts come from today's configuration and catalogue, and
// the record says that they were added later. Nothing the run recorded
// itself is changed.
func (d *Daemon) handleIdentify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	rec, err := d.State.GetRun(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "there is no finished run %s", id)
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	var arm config.Arm
	if rec.Imported || json.Unmarshal(rec.Arm, &arm) != nil || arm.Model.Model == "" {
		fail(w, http.StatusConflict, "run %s is not a run of this harness: its record has no arm to identify", id)
		return
	}
	prov := map[string]json.RawMessage{}
	if err := json.Unmarshal(rec.Provenance, &prov); err != nil {
		fail(w, http.StatusInternalServerError, "the record of run %s cannot be read: %v", id, err)
		return
	}
	var inf *run.Inference
	if raw, ok := prov["inference"]; ok {
		json.Unmarshal(raw, &inf)
	}
	if inf != nil && len(inf.Model.NotStated) == 0 {
		fail(w, http.StatusConflict, "run %s already records where and on what it ran", id)
		return
	}
	models, err := config.LoadModels(filepath.Join(d.Repo, "models"))
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	stated := models[arm.Model.Model]
	now := time.Now().UTC().Truncate(time.Second)
	if inf != nil {
		// The run recorded the place; only the model's stated identity
		// was missing.
		if len(stated.Missing()) >= len(inf.Model.NotStated) {
			fail(w, http.StatusConflict, "the catalogue states no more of %s than run %s already records", arm.Model.Model, id)
			return
		}
		inf.Model.Stated, inf.Model.NotStated = stated, stated.Missing()
		inf.AddedLater = &run.AddedLater{At: now, Source: "the model's stated identity, from the catalogue"}
	} else {
		endpoint, lane := d.Local.Endpoints[arm.Endpoint], d.Local.Lanes[rec.Lane]
		if endpoint == nil || lane == nil {
			fail(w, http.StatusConflict, "the configuration no longer has endpoint %q or lane %q of run %s", arm.Endpoint, rec.Lane, id)
			return
		}
		var before, after *run.Identity
		json.Unmarshal(prov["endpoint_before"], &before)
		json.Unmarshal(prov["endpoint_after"], &after)
		if before == nil {
			fail(w, http.StatusConflict, "run %s never asked its endpoint what it was", id)
			return
		}
		inf = &run.Inference{Endpoint: endpoint.Name, Kind: endpoint.Kind, ServerVersion: before.ServerVersion, Engine: before.Engine,
			Host: endpoint.Host, LaneHost: lane.Host}
		inf.GPU.Declared, inf.GPU.VRAMGB, inf.GPU.Notes = endpoint.Hardware.GPU, endpoint.Hardware.VRAMGB, endpoint.Hardware.Notes
		inf.Model.Tag, inf.Model.Digest = arm.Model.Model, before.Digest
		if run.Ollama(endpoint) {
			stated = run.FillIdentity(stated, arm.Model.Model, before.Weights)
		}
		inf.Model.Stated, inf.Model.NotStated = stated, stated.Missing()
		inf.Model.Server, inf.Model.Weights = before.Details, before.Weights
		if after != nil && after.Loaded != nil && after.Loaded.Size > 0 {
			share := float64(after.Loaded.SizeVRAM) / float64(after.Loaded.Size)
			inf.GPU.Share = &share
			if inf.Engine == "" {
				inf.Engine = after.Engine
			}
		}
		inf.AddedLater = &run.AddedLater{At: now, Source: "today's configuration and catalogue; the server's answers are the run's own"}
	}
	prov["inference"], _ = json.Marshal(inf)
	if rec.Provenance, err = json.Marshal(prov); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := d.State.UpsertRun(ctx, rec); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if _, err := ingest.Run(ctx, d.State, d.Blobs, id); err != nil {
		fail(w, http.StatusInternalServerError, "the record is written, the analysis failed: %v", err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"run_id": id, "state": "identified"})
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
