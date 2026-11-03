package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/task"
)

// `automaticum detect` over runs (070 §3.3): every detector of this build,
// or the ones named, run again now on the stored material of any set of
// runs (and on what a run in flight has printed so far), without writing
// anything unless asked. GET /v1/detect reads; POST /v1/detect also records
// the new analysis on the finished runs, as `automaticum reanalyse` would.
//
// The runs are the union of what is named (?run=, ?batch=, ?cohort=,
// ?campaign=; batches, cohorts and campaigns as globs), narrowed by every
// other field given (?lane=, ?arm= (name or id), ?task=, ?suite=, ?model=,
// ?verdict=, all globs, any number of each, and ?since=, ?until=). With
// nothing named, every run the narrowing leaves; with nothing at all,
// ?all=1 is needed.
//
// The answer is a stream of JSON lines: a header, one line per run in the
// order of their ids (runs in flight last), and an end line.

// DetectLine is one line of the answer: exactly one field is set.
type DetectLine struct {
	Header *DetectHeader `json:"header,omitempty"`
	Run    *DetectRow    `json:"run,omitempty"`
	End    *DetectEnd    `json:"end,omitempty"`
	// Error is a failure that ended the answer early.
	Error string `json:"error,omitempty"`
}

// DetectHeader says what the runs are read with.
type DetectHeader struct {
	Library   string   `json:"detector_library_version"`
	Analysis  int      `json:"analysis_version"`
	Versions  string   `json:"detector_versions"`
	Detectors []string `json:"detectors"`
	Runs      int      `json:"runs"`
	InFlight  int      `json:"in_flight"`
	Group     string   `json:"group,omitempty"`
	Record    bool     `json:"record,omitempty"`
}

// DetectRow is one run read again.
type DetectRow struct {
	Run      string `json:"run"`
	Batch    string `json:"batch,omitempty"`
	Campaign string `json:"campaign,omitempty"`
	Cohort   string `json:"cohort,omitempty"`
	Arm      string `json:"arm,omitempty"`
	ArmID    string `json:"arm_id,omitempty"`
	Task     string `json:"task"`
	Lane     string `json:"lane,omitempty"`
	Model    string `json:"model,omitempty"`
	// State is "finished" or "in flight".
	State string `json:"state"`
	// Partial says why the result is not the whole run's: a run in flight
	// is read from what it has printed so far, without its wire capture.
	Partial string    `json:"partial,omitempty"`
	Verdict string    `json:"verdict,omitempty"`
	Started time.Time `json:"started,omitzero"`
	Blocks  int       `json:"blocks"`
	Calls   int       `json:"tool_calls"`
	// Firings are each detector's firings now (a finding counts its Count,
	// at least one); Stored what the run's stored findings say, for a
	// finished run, so a changed rule shows what it changes.
	Firings map[string]int `json:"firings"`
	Stored  map[string]int `json:"stored,omitempty"`
	// ByTool are the metrics counted per tool (a name with ".by_tool."),
	// by the metric's name before it, then by tool.
	ByTool   map[string]map[string]float64 `json:"by_tool,omitempty"`
	Metrics  map[string]float64            `json:"metrics,omitempty"`
	Outcomes []ingest.ToolOutcome          `json:"tool_outcomes"`
	// Group is the run's value of an arm., label. or finding. axis.
	Group    string          `json:"group,omitempty"`
	Findings []store.Finding `json:"findings,omitempty"`
	// Swallowed are all the calls with a swallowed argument; the finding
	// quotes the first few.
	Swallowed []detect.SwallowFiring `json:"swallowed,omitempty"`
	// Definitions are all the string arguments with repeated definitions.
	Definitions []detect.DefinitionFiring `json:"definitions,omitempty"`
	// Guard are every output the repetition guard would have tripped on
	// (D135), the first first.
	Guard    []ingest.GuardFire `json:"repetition_guard,omitempty"`
	Recorded bool               `json:"recorded,omitempty"`
	Error    string             `json:"error,omitempty"`
}

// DetectEnd closes the answer.
type DetectEnd struct {
	Runs   int    `json:"runs"`
	Failed int    `json:"failed"`
	Took   string `json:"took"`
}

// detectQuery is a detect request, read from the query.
type detectQuery struct {
	runs, batches, cohorts, campaigns            []string
	lanes, arms, tasks, suites, models, verdicts []string
	detectors, metrics                           []string
	since, until                                 time.Time
	group                                        string
	firings, imported, finished, all, record     bool
}

// list is a parameter given any number of times, or comma-separated.
func list(q url.Values, k string) []string {
	var out []string
	for _, v := range q[k] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func readDetectQuery(r *http.Request) (detectQuery, error) {
	q := r.URL.Query()
	dq := detectQuery{
		runs: list(q, "run"), batches: list(q, "batch"), cohorts: list(q, "cohort"), campaigns: list(q, "campaign"),
		lanes: list(q, "lane"), arms: list(q, "arm"), tasks: list(q, "task"), suites: list(q, "suite"), models: list(q, "model"),
		detectors: list(q, "detector"), metrics: list(q, "metric"), group: q.Get("group"),
		firings: q.Get("firings") == "1", imported: q.Get("imported") == "1", finished: q.Get("finished") == "1",
		all: q.Get("all") == "1", record: r.Method == http.MethodPost,
	}
	for _, v := range list(q, "verdict") {
		v = strings.ToUpper(v)
		if !strings.ContainsAny(v, "*?[") && !store.Verdict(v).Valid() {
			return dq, fmt.Errorf("%q is not a verdict", v)
		}
		dq.verdicts = append(dq.verdicts, v)
	}
	for _, p := range slices.Concat(dq.runs, dq.batches, dq.cohorts, dq.campaigns, dq.lanes, dq.arms, dq.tasks, dq.suites, dq.models, dq.verdicts) {
		if _, err := path.Match(p, ""); err != nil {
			return dq, fmt.Errorf("%q is not a pattern: %v", p, err)
		}
	}
	known := ingest.Detectors()
	for _, d := range dq.detectors {
		if !slices.Contains(known, d) {
			return dq, fmt.Errorf("there is no detector %q; there are %s", d, strings.Join(known, ", "))
		}
	}
	if dq.group != "" && groupAxis(dq.group) == "" {
		return dq, fmt.Errorf("--group %q: the daemon groups by arm.<field>, label.<label> or finding.<detector>; batch, arm, cohort, campaign, lane, task, model, state and verdict are grouped by the command", dq.group)
	}
	for _, k := range []string{"since", "until"} {
		v := q.Get(k)
		if v == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			if t, err = time.Parse(time.RFC3339, v); err != nil {
				return dq, fmt.Errorf("%s is a date (2026-11-01) or a time (RFC 3339), not %q", k, v)
			}
		}
		if k == "since" {
			dq.since = t
		} else {
			dq.until = t
		}
	}
	named := len(dq.runs)+len(dq.batches)+len(dq.cohorts)+len(dq.campaigns) > 0
	narrowed := len(dq.lanes)+len(dq.arms)+len(dq.tasks)+len(dq.suites)+len(dq.models)+len(dq.verdicts) > 0 || !dq.since.IsZero() || !dq.until.IsZero()
	if !named && !narrowed && !dq.all {
		return dq, errors.New("name runs, batches, cohorts or campaigns, or narrow by lane, arm, task, suite, model, verdict or time; every run takes --all")
	}
	return dq, nil
}

// match says whether v matches any of the patterns; none matches all.
func match(pats []string, vs ...string) bool {
	if len(pats) == 0 {
		return true
	}
	for _, p := range pats {
		for _, v := range vs {
			if ok, _ := path.Match(p, v); ok || p == v {
				return true
			}
		}
	}
	return false
}

// detectTarget is one run to read: a finished one, or one in flight.
type detectTarget struct {
	rec    *store.Run
	flight *flight
	item   store.QueueItem
}

// targets are the runs a query names, finished ones by id, then those in
// flight by id.
func (d *Daemon) targets(ctx context.Context, dq detectQuery) ([]detectTarget, error) {
	d.mu.Lock()
	batchSet := map[string]bool{}
	for name, c := range d.ctl.Cohorts {
		if len(dq.cohorts) > 0 && match(dq.cohorts, name) {
			for _, b := range c.Batches {
				batchSet[b] = true
			}
		}
	}
	campaignOf := map[string]string{}
	for name, c := range d.ctl.Campaigns {
		for _, b := range c.Batches {
			if b.Batch == "" {
				continue
			}
			campaignOf[b.Batch] = name
			if len(dq.campaigns) > 0 && match(dq.campaigns, name) {
				batchSet[b.Batch] = true
			}
		}
	}
	d.mu.Unlock()
	named := len(dq.runs)+len(dq.batches)+len(dq.cohorts)+len(dq.campaigns) > 0
	member := func(id, batch, campaign string) bool {
		if !named {
			return true
		}
		return slices.Contains(dq.runs, id) || len(dq.batches) > 0 && match(dq.batches, batch) || batchSet[batch] ||
			len(dq.campaigns) > 0 && campaign != "" && match(dq.campaigns, campaign)
	}
	narrow := func(lane, arm, armID, tk, model, verdict string, started time.Time) bool {
		suite, id, _ := strings.Cut(tk, "/")
		return match(dq.lanes, lane) && match(dq.arms, arm, armID) && match(dq.tasks, tk, id) && match(dq.suites, suite) &&
			match(dq.models, model) && match(dq.verdicts, verdict) &&
			(dq.since.IsZero() || !started.Before(dq.since)) && (dq.until.IsZero() || started.Before(dq.until))
	}
	var recs []store.Run
	onlyRuns := len(dq.runs) > 0 && len(dq.batches)+len(dq.cohorts)+len(dq.campaigns) == 0
	if onlyRuns {
		for _, id := range dq.runs {
			rec, err := d.State.GetRun(ctx, id)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			recs = append(recs, *rec)
		}
	} else {
		var err error
		if recs, err = d.State.ListRuns(ctx, store.RunFilter{Since: dq.since, Until: dq.until}); err != nil {
			return nil, err
		}
	}
	var out []detectTarget
	written := map[string]bool{}
	for i := range recs {
		rec := &recs[i]
		written[rec.ID] = true
		if rec.Verdict == store.Skipped || rec.Imported && !dq.imported {
			continue
		}
		campaign := rec.Campaign
		if campaign == "" {
			campaign = campaignOf[rec.Batch]
		}
		it, _ := askedOf(rec)
		if member(rec.ID, rec.Batch, campaign) && narrow(rec.Lane, it.Arm, rec.ArmID, rec.Suite+"/"+rec.TaskID, rec.Model, string(rec.Verdict), rec.StartedAt) {
			out = append(out, detectTarget{rec: rec})
		}
	}
	slices.SortFunc(out, func(a, b detectTarget) int { return strings.Compare(a.rec.ID, b.rec.ID) })
	if dq.finished || len(dq.models) > 0 || len(dq.verdicts) > 0 {
		return out, nil
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return nil, err
	}
	var flying []detectTarget
	for _, it := range queue {
		f := d.flight(it.RunID)
		if f == nil || written[it.RunID] {
			continue
		}
		if member(it.RunID, it.Batch, campaignOf[it.Batch]) && narrow(it.Lane, it.Arm, "", it.Task, "", "", f.started) {
			flying = append(flying, detectTarget{flight: f, item: it})
		}
	}
	slices.SortFunc(flying, func(a, b detectTarget) int { return strings.Compare(a.item.RunID, b.item.RunID) })
	return append(out, flying...), nil
}

// flightMaterial is what a run in flight has printed so far, with a record
// made from what it was started with: its arm as the configuration says it
// now, its task's prompt.
func (d *Daemon) flightMaterial(t detectTarget, arms map[string]*config.Arm, tasks map[string]*task.Task) (*ingest.Material, string, error) {
	t.flight.mu.Lock()
	lines, first := slices.Clone(t.flight.lines), t.flight.first
	t.flight.mu.Unlock()
	suite, id, _ := strings.Cut(t.item.Task, "/")
	r := &store.Run{ID: t.item.RunID, Batch: t.item.Batch, Lane: t.item.Lane, Suite: suite, TaskID: id, StartedAt: t.flight.started}
	if a := arms[t.item.Arm]; a != nil {
		r.Arm, r.Harness = a.Resolved(), a.Harness.SDKRelease
	}
	if tk := tasks[t.item.Task]; tk != nil {
		r.Provenance, _ = json.Marshal(map[string]any{"task": map[string]string{"prompt": tk.Prompt}})
	}
	m := &ingest.Material{Run: r, Events: &ingest.Events{}, Oracles: map[string]string{}}
	if err := m.Events.Read(bytes.NewReader(bytes.Join(lines, []byte("\n")))); err != nil {
		return nil, "", err
	}
	partial := "in flight: what it has printed so far, without its wire capture"
	if first > 0 {
		partial = fmt.Sprintf("in flight: its first %d lines are no longer held, and without its wire capture", first)
	}
	return m, partial, nil
}

// handleDetectRuns answers GET and POST /v1/detect.
func (d *Daemon) handleDetectRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dq, err := readDetectQuery(r)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	targets, err := d.targets(ctx, dq)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	detectors := dq.detectors
	if len(detectors) == 0 {
		detectors = ingest.Detectors()
	}
	if dryRun(r) {
		dq.record = false
	}
	var arms map[string]*config.Arm
	var tasks map[string]*task.Task
	inFlight := 0
	for _, t := range targets {
		if t.flight != nil {
			inFlight++
		}
	}
	if inFlight > 0 || strings.HasPrefix(dq.group, "label.") {
		all, err := task.LoadAll(d.taskRoots())
		if err != nil {
			fail(w, http.StatusInternalServerError, "the tasks cannot be read: %v", err)
			return
		}
		tasks = map[string]*task.Task{}
		for _, t := range all {
			tasks[t.Suite+"/"+t.ID] = t
		}
	}
	if inFlight > 0 {
		if arms, err = config.LoadArms(d.Local.ArmsDir()); err != nil {
			fail(w, http.StatusInternalServerError, "the arms cannot be read: %v", err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	send := func(l DetectLine) {
		enc.Encode(l)
		if fl != nil {
			fl.Flush()
		}
	}
	start := time.Now()
	send(DetectLine{Header: &DetectHeader{Library: detect.Version, Analysis: ingest.AnalysisVersion, Versions: ingest.VersionsText(),
		Detectors: detectors, Runs: len(targets), InFlight: inFlight, Group: dq.group, Record: dq.record}})

	// A few runs at a time, sent in order as each is ready.
	rows := make([]*DetectRow, len(targets))
	ready := make([]chan struct{}, len(targets))
	for i := range ready {
		ready[i] = make(chan struct{})
	}
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(4, max(1, len(targets))) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				rows[i] = d.detectOne(ctx, targets[i], dq, detectors, arms, tasks)
				close(ready[i])
			}
		}()
	}
	go func() {
		for i := range targets {
			select {
			case next <- i:
			case <-ctx.Done():
			}
		}
		close(next)
	}()
	failed := 0
	for i := range targets {
		select {
		case <-ready[i]:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		if rows[i].Error != "" {
			failed++
		}
		send(DetectLine{Run: rows[i]})
		rows[i] = nil
	}
	wg.Wait()
	send(DetectLine{End: &DetectEnd{Runs: len(targets), Failed: failed, Took: time.Since(start).Round(time.Second).String()}})
	if dq.record && d.Log != nil {
		d.Log.Info("detect recorded", "runs", len(targets)-inFlight, "failed", failed)
	}
}

// detectOne reads one run again.
func (d *Daemon) detectOne(ctx context.Context, t detectTarget, dq detectQuery, detectors []string, arms map[string]*config.Arm, tasks map[string]*task.Task) *DetectRow {
	row := &DetectRow{Firings: map[string]int{}, Outcomes: []ingest.ToolOutcome{}}
	for _, det := range detectors {
		row.Firings[det] = 0
	}
	var m *ingest.Material
	var err error
	if t.flight != nil {
		it := t.item
		row.Run, row.Batch, row.Arm, row.Task, row.Lane, row.State, row.Started = it.RunID, it.Batch, it.Arm, it.Task, it.Lane, "in flight", t.flight.started
		m, row.Partial, err = d.flightMaterial(t, arms, tasks)
	} else {
		rec := t.rec
		it, _ := askedOf(rec)
		row.Run, row.Batch, row.Campaign, row.Arm, row.ArmID, row.Task, row.Lane, row.Model = rec.ID, rec.Batch, rec.Campaign, it.Arm, rec.ArmID, rec.Suite+"/"+rec.TaskID, rec.Lane, rec.Model
		row.State, row.Verdict, row.Started = "finished", string(rec.Verdict), rec.StartedAt
		m, err = ingest.Load(ctx, d.State, d.Blobs, rec.ID)
	}
	d.mu.Lock()
	if c := d.cohortOf(row.Batch); c != nil {
		row.Cohort = c.Name
	}
	if row.Campaign == "" {
		for name, c := range d.ctl.Campaigns {
			for _, b := range c.Batches {
				if b.Batch != "" && b.Batch == row.Batch {
					row.Campaign = name
				}
			}
		}
	}
	d.mu.Unlock()
	if err != nil {
		row.Error = err.Error()
		return row
	}
	res := ingest.Derive(m)
	row.Blocks, row.Calls = len(m.Events.Blocks), len(m.Events.Calls)
	for _, f := range res.Findings {
		if _, ok := row.Firings[f.Detector]; ok {
			row.Firings[f.Detector] += max(1, f.Count)
			if dq.firings {
				row.Findings = append(row.Findings, f)
			}
		}
	}
	for _, mt := range res.Metrics {
		if base, tool, ok := strings.Cut(mt.Name, ".by_tool."); ok && (len(dq.detectors) == 0 || slices.ContainsFunc(dq.detectors, func(d string) bool { return strings.Contains(base, d) })) {
			if row.ByTool == nil {
				row.ByTool = map[string]map[string]float64{}
			}
			if row.ByTool[base] == nil {
				row.ByTool[base] = map[string]float64{}
			}
			row.ByTool[base][tool] = mt.Value
		}
		if slices.ContainsFunc(dq.metrics, func(p string) bool {
			return mt.Name == p || strings.HasPrefix(mt.Name, p+".") || match([]string{p}, mt.Name)
		}) {
			if row.Metrics == nil {
				row.Metrics = map[string]float64{}
			}
			row.Metrics[mt.Name] = mt.Value
		}
	}
	row.Outcomes = ingest.Outcomes(m.Events, m.Requests)
	if dq.firings && slices.Contains(detectors, detect.SwallowedArgument) {
		row.Swallowed = ingest.SwallowFirings(m.Events, m.Requests)
	}
	if dq.firings && slices.Contains(detectors, detect.DuplicateDefinitions) {
		row.Definitions, _ = ingest.DefinitionFirings(m.Events, m.Requests)
	}
	if dq.firings && slices.Contains(detectors, detect.RepetitionGuard) {
		row.Guard = ingest.GuardFires(m.Events, m.Requests, ingest.DetectOptions(m.Run))
	}
	if dq.group != "" {
		var labels map[string]string
		if tk := tasks[row.Task]; tk != nil {
			labels = tk.Labels
		}
		row.Group = groupValue(dq.group, m.Run, labels, res.Findings)
	}
	if t.rec != nil {
		if stored, err := d.State.Findings(ctx, t.rec.ID); err == nil {
			row.Stored = map[string]int{}
			for _, det := range detectors {
				row.Stored[det] = 0
			}
			for _, f := range stored {
				if _, ok := row.Stored[f.Detector]; ok {
					row.Stored[f.Detector] += max(1, f.Count)
				}
			}
		}
		if dq.record {
			if _, err := ingest.Run(ctx, d.State, d.Blobs, t.rec.ID); err != nil {
				row.Error = "the analysis was not recorded: " + err.Error()
			} else {
				row.Recorded = true
			}
		}
	}
	return row
}
