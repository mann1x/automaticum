package daemon

import (
	"net/http"
	"sort"

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
	q := r.URL.Query()
	f, err := whereFilter(q.Get("where"))
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	var recs []store.Run
	if batches := q["batch"]; len(batches) > 0 {
		for _, b := range batches {
			f.Batch = b
			got, err := d.State.ListRuns(ctx, f)
			if err != nil {
				fail(w, http.StatusInternalServerError, "%v", err)
				return
			}
			recs = append(recs, got...)
		}
	} else if recs, err = d.State.ListRuns(ctx, f); err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
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
