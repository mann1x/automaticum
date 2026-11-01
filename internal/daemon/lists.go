package daemon

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
)

// BatchSummary is one batch in GET /v1/batches: its runs by state and
// verdict, its times, and how it was started.
type BatchSummary struct {
	Batch    string         `json:"batch"`
	Campaign string         `json:"campaign,omitempty"`
	State    string         `json:"state"` // queued, running, paused, done
	Total    int            `json:"total"`
	Queued   int            `json:"queued"`
	Running  int            `json:"running"`
	Finished map[string]int `json:"finished"` // by verdict
	Arms     []string       `json:"arms,omitempty"`
	Tasks    int            `json:"tasks"`
	Lanes    []string       `json:"lanes,omitempty"`
	// Cell is the suite (and subset) when the batch was started as a cell.
	Cell   string    `json:"cell,omitempty"`
	First  time.Time `json:"first,omitzero"` // the first run's start or enqueue
	Last   time.Time `json:"last,omitzero"`  // the last run's end
	Paused bool      `json:"paused,omitempty"`
	// Priority is the batch's priority, where not 0 (D125).
	Priority int  `json:"priority,omitempty"`
	Heldout  bool `json:"heldout,omitempty"`
}

// handleBatches lists the batches, the one with the latest activity first;
// ?limit= (default 50), ?campaign= and ?cell=1 narrow it.
func (d *Daemon) handleBatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	queue, err := d.State.Queue(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	recs, err := d.State.ListRuns(ctx, store.RunFilter{})
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	by := map[string]*BatchSummary{}
	arms, tasks, lanes := map[string]map[string]bool{}, map[string]map[string]bool{}, map[string]map[string]bool{}
	get := func(b string) *BatchSummary {
		s := by[b]
		if s == nil {
			s = &BatchSummary{Batch: b, Finished: map[string]int{}}
			by[b] = s
			arms[b], tasks[b], lanes[b] = map[string]bool{}, map[string]bool{}, map[string]bool{}
		}
		return s
	}
	written := map[string]bool{}
	for _, rec := range recs {
		if rec.Batch == "" {
			continue
		}
		written[rec.ID] = true
		s := get(rec.Batch)
		s.Total++
		s.Finished[string(rec.Verdict)]++
		if s.Campaign == "" {
			s.Campaign = rec.Campaign
		}
		if !rec.StartedAt.IsZero() && (s.First.IsZero() || rec.StartedAt.Before(s.First)) {
			s.First = rec.StartedAt
		}
		if rec.EndedAt.After(s.Last) {
			s.Last = rec.EndedAt
		}
		tasks[rec.Batch][rec.Suite+"/"+rec.TaskID] = true
		if rec.Lane != "" {
			lanes[rec.Batch][rec.Lane] = true
		}
		if it, err := askedOf(&rec); err == nil && it.Arm != "" {
			arms[rec.Batch][it.Arm] = true
		}
	}
	for _, it := range queue {
		if it.Batch == "" || written[it.RunID] {
			continue
		}
		s := get(it.Batch)
		s.Total++
		if d.flight(it.RunID) != nil {
			s.Running++
		} else {
			s.Queued++
		}
		if s.First.IsZero() || it.EnqueuedAt.Before(s.First) {
			s.First = it.EnqueuedAt
		}
		arms[it.Batch][it.Arm] = true
		tasks[it.Batch][it.Task] = true
		lanes[it.Batch][it.Lane] = true
	}
	d.mu.Lock()
	for b, s := range by {
		_, s.Paused = d.ctl.Held[b]
		s.Heldout = d.ctl.Heldout[b]
		s.Priority = d.ctl.Priority[b]
		if c := d.ctl.CellBatches[b]; c != "" {
			s.Cell = c
		} else if d.ctl.Cells[b] != nil {
			s.Cell = "mode C"
		}
		if s.Campaign == "" {
			s.Campaign = d.campaignOf(b)
		}
	}
	d.mu.Unlock()
	q := r.URL.Query()
	limit := 50
	if n, err := atoiPositive(q.Get("limit")); err == nil {
		limit = n
	}
	out := []BatchSummary{}
	for b, s := range by {
		if q.Get("campaign") != "" && s.Campaign != q.Get("campaign") || q.Get("cell") == "1" && s.Cell == "" {
			continue
		}
		s.Arms, s.Lanes, s.Tasks = sortedKeys(arms[b]), sortedKeys(lanes[b]), len(tasks[b])
		switch {
		case s.Running > 0:
			s.State = "running"
		case s.Queued > 0 && s.Paused:
			s.State = "paused"
		case s.Queued > 0:
			s.State = "queued"
		default:
			s.State = "done"
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := latest(out[i]), latest(out[j])
		if !ai.Equal(aj) {
			return ai.After(aj)
		}
		return out[i].Batch > out[j].Batch
	})
	if len(out) > limit {
		out = out[:limit]
	}
	reply(w, http.StatusOK, out)
}

// latest is a batch's last sign of life: its last end, or its first start
// while nothing has ended; a batch with work left counts as now.
func latest(s BatchSummary) time.Time {
	if s.Running+s.Queued > 0 {
		return time.Now()
	}
	if s.Last.After(s.First) {
		return s.Last
	}
	return s.First
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// LaneView is one lane in GET /v1/lanes/{lane}: its configuration, what it
// is doing, its queue and its last runs.
type LaneView struct {
	LaneStatus
	Config  *config.Lane         `json:"config"`
	Queue   []QueueEntry         `json:"queue"`
	Recent  []RunView            `json:"recent"`
	Control map[string]time.Time `json:"control,omitempty"` // since when it is drained, paused or quarantined
}

func (d *Daemon) handleLaneShow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("lane")
	l, ok := d.Local.Lanes[name]
	if !ok {
		fail(w, http.StatusNotFound, "there is no lane %s", name)
		return
	}
	st, err := d.status(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	v := LaneView{Config: l, Queue: []QueueEntry{}, Control: map[string]time.Time{}}
	for _, ls := range st.Lanes {
		if ls.Lane == name {
			v.LaneStatus = ls
		}
	}
	for _, q := range st.Queue {
		if q.Lane == name {
			v.Queue = append(v.Queue, q)
		}
	}
	n := 10
	if m, err := atoiPositive(r.URL.Query().Get("recent")); err == nil {
		n = m
	}
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Lane: name, Newest: true, Limit: n})
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	v.Recent = []RunView{}
	for i := range recs {
		v.Recent = append(v.Recent, view(&recs[i], false))
	}
	d.mu.Lock()
	if t, ok := d.ctl.Drained[name]; ok {
		v.Control["drained"] = t
	}
	if t, ok := d.ctl.LaneHeld[name]; ok {
		v.Control["paused"] = t
	}
	if q, ok := d.ctl.Quarantined[name]; ok {
		v.Control["quarantined"] = q.At
	}
	d.mu.Unlock()
	reply(w, http.StatusOK, v)
}

// atoiPositive reads a positive count.
func atoiPositive(s string) (int, error) {
	var n int
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, errNoSuch
	}
	for _, c := range s {
		n = n*10 + int(c-'0')
		if n > 100000 {
			return 100000, nil
		}
	}
	if n == 0 {
		return 0, errNoSuch
	}
	return n, nil
}
