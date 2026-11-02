package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"

	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/stats"
	"github.com/mann1x/automaticum/internal/store"
)

// CensusRow is the tool calls of one tool on one model (070 §6): how many
// there were, in how many runs, how many failed and by which error class,
// and how each was read (070 §4).
type CensusRow struct {
	Tool   string         `json:"tool"`
	Model  string         `json:"model"`
	Calls  int            `json:"calls"`
	Runs   int            `json:"runs"`
	Failed int            `json:"failed"`
	Errors map[string]int `json:"errors,omitempty"` // by error class
	// Parse counts the calls by parse path; "not recorded" where ingest
	// did not say.
	Parse map[string]int `json:"parse"`
}

// Census is the answer of GET /v1/report/census.
type Census struct {
	Runs int `json:"runs"`
	// NoCalls are the runs counted that made no tool call.
	NoCalls int         `json:"no_calls"`
	Rows    []CensusRow `json:"rows"`
}

// handleCensus counts the tool calls of the runs a filter selects, by tool,
// model, error class and parse path: ?where= (as reanalyse's) and ?batch=,
// which may be given more than once. A run without a result (SKIPPED) made
// no call and is not counted; INVALID runs are, since their calls were made.
func (d *Daemon) handleCensus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	recs, code, err := d.reportRuns(ctx, r.URL.Query())
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	out := Census{Rows: []CensusRow{}}
	by := map[[2]string]*CensusRow{}
	seen := map[[2]string]map[string]bool{}
	for i := range recs {
		rec := &recs[i]
		if rec.Verdict == store.Skipped {
			continue
		}
		calls, err := d.State.ToolCalls(ctx, rec.ID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		out.Runs++
		if len(calls) == 0 {
			out.NoCalls++
		}
		for _, c := range calls {
			k := [2]string{c.Tool, rec.Model}
			row := by[k]
			if row == nil {
				row = &CensusRow{Tool: c.Tool, Model: rec.Model, Errors: map[string]int{}, Parse: map[string]int{}}
				by[k], seen[k] = row, map[string]bool{}
			}
			row.Calls++
			if !seen[k][rec.ID] {
				seen[k][rec.ID] = true
				row.Runs++
			}
			if !c.OK {
				row.Failed++
				class := c.ErrorClass
				if class == "" {
					class = "not recorded"
				}
				row.Errors[class]++
			}
			path := c.ParsePath
			if path == "" {
				path = "not recorded"
			}
			row.Parse[path]++
		}
	}
	for _, row := range by {
		out.Rows = append(out.Rows, *row)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.Failed != b.Failed {
			return a.Failed > b.Failed
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Tool < b.Tool
	})
	reply(w, http.StatusOK, out)
}

// reportRuns are the runs a report's ?where= and ?batch= (given any number
// of times) select.
func (d *Daemon) reportRuns(ctx context.Context, q url.Values) ([]store.Run, int, error) {
	f, err := whereFilter(q.Get("where"))
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	batches := q["batch"]
	if len(batches) == 0 {
		recs, err := d.State.ListRuns(ctx, f)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		return recs, 0, nil
	}
	var recs []store.Run
	for _, b := range batches {
		f.Batch = b
		got, err := d.State.ListRuns(ctx, f)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		recs = append(recs, got...)
	}
	return recs, 0, nil
}

// StruggleRow is how one struggle signal (D126) did on a set of runs:
// how many runs that did not pass and how many that passed it held on,
// its recall (the share of the runs that did not pass it held on), its
// false-alarm rate (the share of the runs that passed it held on), its
// precision (the share of the runs it held on that did not pass) and the
// turn it first held, over the runs that did not pass. A share with
// nothing to divide by is left out.
type StruggleRow struct {
	Trigger        string         `json:"trigger"`
	HeldNotPassed  int            `json:"held_not_passed"`
	HeldPassed     int            `json:"held_passed"`
	Recall         *float64       `json:"recall,omitempty"`
	FalseAlarmRate *float64       `json:"false_alarm_rate,omitempty"`
	Precision      *float64       `json:"precision,omitempty"`
	FirstTurn      *stats.Summary `json:"first_turn,omitempty"`
}

// StruggleReport is the answer of GET /v1/report/struggle. Runs that
// passed and runs that did not (FAIL, TIMEOUT) are measured; ABORTED,
// INVALID and SKIPPED runs say nothing about the agent and are left out;
// runs analysed before analysis version 10 carry no signals and are
// counted apart until `automaticum reanalyse` is run on them.
type StruggleReport struct {
	Passed      int           `json:"passed"`
	NotPassed   int           `json:"not_passed"`
	LeftOut     int           `json:"left_out"`
	NotAnalysed int           `json:"not_analysed"`
	Rows        []StruggleRow `json:"rows"`
}

// handleStruggle evaluates every struggle signal of D126 on the runs a
// filter selects, against "did not pass" (070 §4); the last row, "any", is
// any signal holding.
func (d *Daemon) handleStruggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	recs, code, err := d.reportRuns(ctx, r.URL.Query())
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	out := StruggleReport{Rows: []StruggleRow{}}
	triggers := append(append([]string(nil), ingest.StruggleTriggers...), "any")
	counts := map[string]*struggleCount{}
	for _, t := range triggers {
		counts[t] = &struggleCount{}
	}
	for i := range recs {
		rec := &recs[i]
		if rec.Verdict != store.Pass && rec.Verdict != store.Fail && rec.Verdict != store.Timeout {
			out.LeftOut++
			continue
		}
		ms, err := d.State.Metrics(ctx, rec.ID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		m := map[string]float64{}
		for _, x := range ms {
			m[x.Name] = x.Value
		}
		if m["analysis.version"] < 10 {
			out.NotAnalysed++
			continue
		}
		passed := rec.Verdict == store.Pass
		if passed {
			out.Passed++
		} else {
			out.NotPassed++
		}
		first, held := 0.0, false
		for _, t := range ingest.StruggleTriggers {
			if m["struggle."+t] == 0 {
				continue
			}
			turn := m["struggle."+t+".turn"]
			if !held || turn < first {
				first = turn
			}
			held = true
			counts[t].add(passed, turn)
		}
		if held {
			counts["any"].add(passed, first)
		}
	}
	share := func(n, of int) *float64 {
		if of == 0 {
			return nil
		}
		v := float64(n) / float64(of)
		return &v
	}
	for _, t := range triggers {
		c := counts[t]
		row := StruggleRow{Trigger: t, HeldNotPassed: c.notPassed, HeldPassed: c.passed,
			Recall: share(c.notPassed, out.NotPassed), FalseAlarmRate: share(c.passed, out.Passed), Precision: share(c.notPassed, c.notPassed+c.passed)}
		if len(c.turns) > 0 {
			s := stats.Summarise(c.turns)
			row.FirstTurn = &s
		}
		out.Rows = append(out.Rows, row)
	}
	reply(w, http.StatusOK, out)
}

type struggleCount struct {
	notPassed, passed int
	turns             []float64 // the first turn, on runs that did not pass
}

func (c *struggleCount) add(passed bool, turn float64) {
	if passed {
		c.passed++
		return
	}
	c.notPassed++
	c.turns = append(c.turns, turn)
}

// GuardReviewRow is one turn to review (070 §4): a guard's trip that its
// rule, replayed, does not confirm ("unconfirmed", a possible false
// alarm), or a turn where the rule holds and the guard did not trip
// ("missed").
type GuardReviewRow struct {
	Run   string `json:"run"`
	Batch string `json:"batch"`
	Arm   string `json:"arm_id"`
	Model string `json:"model"`
	Guard string `json:"guard"`
	Phase string `json:"phase,omitempty"`
	Kind  string `json:"kind"`
	Turn  int    `json:"turn"`
}

// GuardReview is the answer of GET /v1/report/guards: the review queue,
// unconfirmed trips first, and how many runs were read.
type GuardReview struct {
	Runs int              `json:"runs"`
	Rows []GuardReviewRow `json:"rows"`
}

// handleGuards gathers the guard_replay findings of the runs a filter
// selects into one queue; ?kind=unconfirmed or ?kind=missed keeps one.
func (d *Daemon) handleGuards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "unconfirmed" && kind != "missed" {
		fail(w, http.StatusBadRequest, "kind is unconfirmed or missed, not %q", kind)
		return
	}
	recs, code, err := d.reportRuns(ctx, r.URL.Query())
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	out := GuardReview{Rows: []GuardReviewRow{}}
	for i := range recs {
		rec := &recs[i]
		if rec.Verdict == store.Skipped {
			continue
		}
		out.Runs++
		fs, err := d.State.Findings(ctx, rec.ID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for _, f := range fs {
			if f.Detector != ingest.GuardReplay {
				continue
			}
			var e struct {
				Guards []struct {
					Guard       string `json:"guard"`
					Phase       string `json:"phase"`
					Missed      []int  `json:"missed"`
					Unconfirmed []int  `json:"unconfirmed"`
				} `json:"guards"`
			}
			if json.Unmarshal(f.Evidence, &e) != nil {
				continue
			}
			for _, g := range e.Guards {
				for k, turns := range map[string][]int{"unconfirmed": g.Unconfirmed, "missed": g.Missed} {
					if kind != "" && k != kind {
						continue
					}
					for _, t := range turns {
						out.Rows = append(out.Rows, GuardReviewRow{Run: rec.ID, Batch: rec.Batch, Arm: rec.ArmID, Model: rec.Model, Guard: g.Guard, Phase: g.Phase, Kind: k, Turn: t})
					}
				}
			}
		}
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.Kind != b.Kind {
			return a.Kind == "unconfirmed"
		}
		if a.Run != b.Run {
			return a.Run < b.Run
		}
		if a.Guard+a.Phase != b.Guard+b.Phase {
			return a.Guard+a.Phase < b.Guard+b.Phase
		}
		return a.Turn < b.Turn
	})
	reply(w, http.StatusOK, out)
}
