package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/stats"
	"github.com/mann1x/automaticum/internal/store"
)

// CellSchema is the version of the result file (065 §3). A change that
// removes or re-defines a field is a new version.
const CellSchema = "automaticum.cell/1"

// CellResult is automaticum_result.json: the only file a caller reads for
// the score (065 §3). Fields may be added within a schema version.
type CellResult struct {
	Schema        string `json:"schema"`
	Suite         string `json:"suite"`
	Subset        string `json:"subset"`
	SuiteVersion  string `json:"suite_version"`
	TaskSetSHA256 string `json:"task_set_sha256"`
	NExpected     int    `json:"n_expected"`
	ArmID         string `json:"arm_id"`
	Arm           string `json:"arm"`
	Batch         string `json:"batch"`
	// ModeC is the caller's endpoint, values, facts and lanes, when the
	// cell ran in mode C (065 §3.1).
	ModeC            *CellSpec `json:"mode_c,omitempty"`
	Cohort           string    `json:"cohort"`
	DeclaredVariable string    `json:"declared_variable"`
	Headline         struct {
		Metric         string  `json:"metric"`
		Value          float64 `json:"value"`
		HigherIsBetter bool    `json:"higher_is_better"`
	} `json:"headline"`
	// Counts are of each run's last attempt, and sum to NExpected:
	// "unfinished" holds those still queued or in flight.
	Counts           map[string]int           `json:"counts"`
	Scores           map[string]float64       `json:"scores"`
	Basis            map[string][]string      `json:"basis"`
	BasisSHA256      string                   `json:"basis_sha256"`
	TokenStats       map[string]stats.Summary `json:"token_stats"`
	GenerationCaps   CapAudit                 `json:"generation_caps"`
	Degeneration     map[string]int           `json:"degeneration"`
	ToolCalls        map[string]stats.Summary `json:"tool_calls"`
	DetectorSelfTest struct {
		Passed   bool     `json:"passed"`
		Version  string   `json:"version"`
		Controls int      `json:"controls"`
		Failed   []string `json:"failed,omitempty"`
	} `json:"detector_self_test"`
	Warnings   []string  `json:"warnings"`
	Complete   bool      `json:"complete"`
	Runs       string    `json:"runs"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	DurationS  float64   `json:"duration_s"`
}

// CellRow is one line of runs.jsonl: one run, its verdict, how it is
// counted, and its numbers.
type CellRow struct {
	RunID         string             `json:"run_id"`
	Task          string             `json:"task"`
	Verdict       string             `json:"verdict"`
	VerdictReason string             `json:"verdict_reason,omitempty"`
	CountedAs     string             `json:"counted_as"`
	RetryOf       string             `json:"retry_of,omitempty"`
	SupersededBy  string             `json:"superseded_by,omitempty"`
	Findings      map[string]int     `json:"findings,omitempty"`
	Metrics       map[string]float64 `json:"metrics"`
}

// CellView is the daemon's answer about a cell: the result, its rows, what
// is not finished, and, when every run was refused by a gate, the refusal.
type CellView struct {
	Result     CellResult `json:"result"`
	Rows       []CellRow  `json:"rows"`
	Unfinished []string   `json:"unfinished"`
	Refusal    *Refusal   `json:"refusal,omitempty"`
	// BasisText is the basis in plain text, one fact per line (065 §5).
	BasisText string `json:"basis_text"`
}

// Refusal is refusal.json (065 §2.1): the gate that refused, or the runs
// that are not finished.
type Refusal struct {
	Code       int      `json:"code"`
	Gate       string   `json:"gate,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Unfinished []string `json:"unfinished,omitempty"`
}

// degenerate are the detectors whose firings the result counts as
// degeneration.
var degenerate = []string{detect.ShortUnitRepeat, detect.CharRun, detect.SentenceRepeat, detect.Runaway,
	detect.ReasoningRepetition, detect.ReasoningLoop, detect.RepeatedToolCall}

// handleCell gives the result of a batch as a cell (065 §3).
func (d *Daemon) handleCell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	batch := r.PathValue("batch")
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: batch})
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	var queued []store.QueueItem
	for _, it := range queue {
		if it.Batch == batch {
			queued = append(queued, it)
		}
	}
	if len(recs) == 0 && len(queued) == 0 {
		fail(w, http.StatusNotFound, "there is no batch %s", batch)
		return
	}
	d.mu.Lock()
	supersededBy := map[string]string{}
	retryOf := map[string]string{}
	for nu, old := range d.ctl.Retries {
		supersededBy[old], retryOf[nu] = nu, old
	}
	d.mu.Unlock()

	v := CellView{Rows: []CellRow{}, Unfinished: []string{}}
	res := &v.Result
	res.Schema, res.Batch, res.Runs = CellSchema, batch, "runs.jsonl"
	d.mu.Lock()
	res.ModeC = d.ctl.Cells[batch]
	d.mu.Unlock()
	res.Counts = map[string]int{"pass": 0, "fail": 0, "timeout": 0, "aborted": 0, "invalid": 0, "skipped": 0, "unfinished": 0}
	res.Scores, res.Degeneration, res.Warnings = map[string]float64{}, map[string]int{}, []string{}
	leave := map[string]bool{}
	var refusedBy []string
	tasks := map[string]string{}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	for _, rec := range recs {
		row := CellRow{RunID: rec.ID, Task: rec.Suite + "/" + rec.TaskID, Verdict: string(rec.Verdict), VerdictReason: rec.VerdictReason,
			RetryOf: retryOf[rec.ID], SupersededBy: supersededBy[rec.ID], Metrics: map[string]float64{}}
		ms, err := d.State.Metrics(ctx, rec.ID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for _, m := range ms {
			row.Metrics[m.Name] = m.Value
		}
		fs, err := d.State.Findings(ctx, rec.ID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		for _, f := range fs {
			if row.Findings == nil {
				row.Findings = map[string]int{}
			}
			row.Findings[f.Detector]++
		}
		if res.StartedAt.IsZero() || (!rec.StartedAt.IsZero() && rec.StartedAt.Before(res.StartedAt)) {
			res.StartedAt = rec.StartedAt
		}
		if rec.EndedAt.After(res.FinishedAt) {
			res.FinishedAt = rec.EndedAt
		}
		if row.SupersededBy != "" {
			// A retry took its place: it counts as nothing.
			row.CountedAs = "superseded by " + row.SupersededBy
			leave[rec.ID] = true
			v.Rows = append(v.Rows, row)
			continue
		}
		var prov struct {
			Task struct {
				Hash string `json:"hash"`
			} `json:"task"`
		}
		json.Unmarshal(rec.Provenance, &prov)
		if prov.Task.Hash != "" {
			tasks[fmt.Sprintf("%s version %d %s", row.Task, rec.TaskVersion, prov.Task.Hash)] = row.Task
		}
		kind := strings.ToLower(string(rec.Verdict))
		res.Counts[kind]++
		row.CountedAs = kind
		switch rec.Verdict {
		case store.Pass, store.Fail, store.Timeout:
			for _, det := range degenerate {
				if row.Findings[det] > 0 {
					res.Degeneration[det]++
				}
			}
		case store.Aborted:
			v.Unfinished = append(v.Unfinished, rec.ID)
			row.CountedAs = "aborted: not a result of the arm; a resume runs it again"
		case store.Skipped:
			v.Unfinished = append(v.Unfinished, rec.ID)
			row.CountedAs = "skipped: never ran; a resume runs it again"
			if strings.HasPrefix(rec.VerdictReason, "gate ") {
				refusedBy = append(refusedBy, rec.VerdictReason)
			}
		case store.Invalid:
			row.CountedAs = "invalid: not a result of the arm, and not run again"
		}
		v.Rows = append(v.Rows, row)
	}
	for _, it := range queued {
		res.Counts["unfinished"]++
		v.Unfinished = append(v.Unfinished, it.RunID)
	}
	n := 0
	for _, c := range res.Counts {
		n += c
	}
	res.NExpected = n
	res.Headline.Metric, res.Headline.HigherIsBetter = "pass_rate", true
	if n > 0 {
		res.Headline.Value = float64(res.Counts["pass"]) / float64(n)
	}
	res.Scores["pass_rate"] = res.Headline.Value
	if counted := res.Counts["pass"] + res.Counts["fail"] + res.Counts["timeout"]; counted > 0 {
		looped := 0
		for _, row := range v.Rows {
			if row.SupersededBy == "" && (row.Verdict == "PASS" || row.Verdict == "FAIL" || row.Verdict == "TIMEOUT") &&
				(row.Findings[detect.ReasoningLoop] > 0 || row.Findings[detect.ReasoningRepetition] > 0) {
				looped++
			}
		}
		res.Scores["loop_rate"] = float64(looped) / float64(counted)
	}
	if !res.StartedAt.IsZero() && !res.FinishedAt.IsZero() {
		res.DurationS = res.FinishedAt.Sub(res.StartedAt).Seconds()
	}

	// The task set: every task with its version and the hash of its files.
	keys := make([]string, 0, len(tasks))
	names := map[string]int{}
	for k, name := range tasks {
		keys = append(keys, k)
		names[name]++
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	res.TaskSetSHA256 = hex.EncodeToString(sum[:])
	for name, c := range names {
		if c > 1 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("task %s changed during the cell: its runs ran on %d versions of its files", name, c))
		}
	}

	// The numbers, over the runs that count.
	if len(recs) > 0 {
		cv, _, err := d.compareView(ctx, []string{batch}, leave)
		if err == nil {
			d.cellNumbers(res, cv)
		}
	}
	results, ok := detect.SelfTest()
	res.DetectorSelfTest.Passed, res.DetectorSelfTest.Version, res.DetectorSelfTest.Controls = ok, detect.Version, len(results)
	for _, c := range results {
		if !c.OK {
			res.DetectorSelfTest.Failed = append(res.DetectorSelfTest.Failed, c.Control)
		}
	}
	if !ok {
		res.Warnings = append(res.Warnings, "the detector self-test failed: the degeneration numbers are not available")
	}

	// What makes the cell not clean, each said in the file.
	if c := res.Counts["invalid"]; c > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d runs are INVALID: counted as invalid, not as failures", c))
	}
	if c := res.Counts["aborted"] + res.Counts["skipped"]; c > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d runs ended without a result (ABORTED or SKIPPED): counted as such, not as failures; a resume with the same key runs them again", c))
	}
	if c := res.Counts["unfinished"]; c > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d runs are not finished", c))
	}
	res.Complete = len(v.Unfinished) == 0
	if !res.Complete {
		res.Warnings = append(res.Warnings, fmt.Sprintf("incomplete: %d of %d runs have no result", len(v.Unfinished), n))
	}
	sort.Strings(v.Unfinished)
	if len(refusedBy) > 0 && len(refusedBy) == n {
		gateName, _, _ := strings.Cut(strings.TrimPrefix(refusedBy[0], "gate "), ":")
		v.Refusal = &Refusal{Code: 20, Gate: gateName, Reason: refusedBy[0]}
	} else if !res.Complete {
		v.Refusal = &Refusal{Code: 30, Unfinished: v.Unfinished}
	}
	reply(w, http.StatusOK, v)
}

// cellNumbers fills the result's numbers from the compare view of the
// cell's counted runs: its arm, basis, token and tool-call figures, and the
// cap audit.
func (d *Daemon) cellNumbers(res *CellResult, cv CompareView) {
	values := map[string][]float64{}
	var bases [][]BasisField
	caps := CapAudit{}
	arms := map[string]string{}
	for _, g := range cv.Groups {
		arms[g.ArmID] = g.Arm
		for name, vals := range g.values {
			values[name] = append(values[name], vals...)
		}
		bases = append(bases, g.Basis.Fields)
		c := g.Cap
		caps.Requests += c.Requests
		caps.Unreported += c.Unreported
		caps.CappedRequests += c.CappedRequests
		caps.CappedRuns += c.CappedRuns
		caps.CappedPassed += c.CappedPassed
		caps.NotAudited = append(caps.NotAudited, c.NotAudited...)
		for _, x := range c.Declared {
			if !slices.Contains(caps.Declared, x) {
				caps.Declared = append(caps.Declared, x)
			}
		}
		for _, x := range c.ThinkBudget {
			if !slices.Contains(caps.ThinkBudget, x) {
				caps.ThinkBudget = append(caps.ThinkBudget, x)
			}
		}
	}
	caps.decide()
	res.GenerationCaps = caps
	switch caps.Verdict {
	case "CAPPED":
		res.Warnings = append(res.Warnings, "generation cap: "+caps.Why+"; the cell is not comparable with one at another output budget")
	case "UNVERIFIED":
		res.Warnings = append(res.Warnings, "generation cap unverified: "+caps.Why)
	}
	if len(arms) == 1 {
		for id, name := range arms {
			res.ArmID, res.Arm = id, name
		}
	} else if len(arms) > 1 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("the cell's runs are of %d arms: a cell is one arm", len(arms)))
	}

	// The basis over every counted run: each fact with every value.
	res.Basis = map[string][]string{}
	var canon strings.Builder
	for i, name := range basisFields {
		var vals []string
		for _, b := range bases {
			if i < len(b) {
				for _, x := range b[i].Values {
					if !slices.Contains(vals, x) {
						vals = append(vals, x)
					}
				}
			}
		}
		sort.Strings(vals)
		res.Basis[name] = vals
		fmt.Fprintf(&canon, "%s=%s\n", name, strings.Join(vals, " | "))
		if len(vals) > 1 && name != "task" {
			res.Warnings = append(res.Warnings, fmt.Sprintf("basis mixed within the cell: %s is %s", name, strings.Join(vals, " | ")))
		}
	}
	sum := sha256.Sum256([]byte(canon.String()))
	res.BasisSHA256 = hex.EncodeToString(sum[:])

	res.TokenStats, res.ToolCalls = map[string]stats.Summary{}, map[string]stats.Summary{}
	for name, vals := range values {
		switch {
		case strings.HasPrefix(name, "tokens."), strings.HasPrefix(name, "thinking."), strings.HasPrefix(name, "rate."):
			res.TokenStats[name] = stats.Summarise(vals)
		case strings.HasPrefix(name, "tool_calls"):
			res.ToolCalls[name] = stats.Summarise(vals)
		}
	}
}

// BasisText writes a cell's basis in plain text, one fact per line, so the
// diff of two cells' files is the audit of a cohort (065 §5).
func BasisText(r CellResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# basis of cell %s, arm %s, sha256 %s\n", r.Batch, r.Arm, r.BasisSHA256)
	for _, name := range basisFields {
		fmt.Fprintf(&b, "%s: %s\n", name, strings.Join(r.Basis[name], " | "))
	}
	return b.String()
}
