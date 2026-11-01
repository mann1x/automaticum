package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

// A cohort (D120, 020 §5) is a set of cells that differ in one declared
// variable: a basis fact, or an arm field. It is declared by a campaign's
// `[cohort]` (every batch of the campaign is a cell of it) or by `cell run
// --cohort N --declared-variable V`. A cohort in which any basis fact
// other than its variable's differs between the cells is confounded.

// Cohort is a declared cohort and its cells, kept in the control file.
type Cohort struct {
	Name     string    `json:"name"`
	Variable string    `json:"variable"`
	Batches  []string  `json:"batches"`
	Created  time.Time `json:"created"`
}

// armFacts are the basis facts each section of an arm feeds (basisOfRun).
var armFacts = map[string][]string{
	"model":      {"weights", "quantization", "imatrix", "template", "parser", "tag parameters", "context", "output limit"},
	"endpoint":   {"weights", "quantization", "imatrix", "template", "parser", "tag parameters", "context", "server", "GPU"},
	"sampler":    {"sampler", "seed"},
	"thinking":   {"thinking"},
	"agent":      {"agent settings"},
	"guards":     {"agent settings"},
	"compaction": {"agent settings"},
	"limits":     {"agent settings"},
	"protocol":   {"agent settings"},
	"harness":    {"harness build"},
}

var armFieldRE = regexp.MustCompile(`^[a-z_]+(\.[a-z0-9_]+)*$`)

// cohortFacts are the basis facts a cohort's variable stands for: the fact
// itself, or the facts the arm field's section feeds.
func cohortFacts(variable string) ([]string, error) {
	if slices.Contains(basisFields, variable) {
		return []string{variable}, nil
	}
	section, _, _ := strings.Cut(variable, ".")
	if facts, ok := armFacts[section]; ok && armFieldRE.MatchString(variable) {
		return facts, nil
	}
	sections := make([]string, 0, len(armFacts))
	for s := range armFacts {
		sections = append(sections, s)
	}
	sort.Strings(sections)
	return nil, fmt.Errorf("the declared variable %q is neither a basis fact (%s) nor an arm field under %s",
		variable, strings.Join(basisFields, ", "), strings.Join(sections, ", "))
}

// checkCohort says whether a cohort may be declared with this variable: a
// name and a variable together, a variable the basis knows, and the same
// variable as the cohort's earlier cells. It is called with d.mu held.
func (d *Daemon) checkCohort(name, variable string) error {
	switch {
	case name == "" && variable == "":
		return nil
	case name == "" || variable == "":
		return fmt.Errorf("a cohort needs its name and its declared variable together")
	}
	if _, err := cohortFacts(variable); err != nil {
		return err
	}
	if c := d.ctl.Cohorts[name]; c != nil && c.Variable != variable {
		return fmt.Errorf("cohort %s declares the variable %q, not %q", name, c.Variable, variable)
	}
	return nil
}

// addToCohort makes a batch a cell of a cohort. It is called with d.mu
// held.
func (d *Daemon) addToCohort(name, variable, batch string) {
	c := d.ctl.Cohorts[name]
	if c == nil {
		c = &Cohort{Name: name, Variable: variable, Created: time.Now().UTC()}
		d.ctl.Cohorts[name] = c
	}
	if !slices.Contains(c.Batches, batch) {
		c.Batches = append(c.Batches, batch)
	}
}

// cohortOf is the cohort a batch is a cell of, if any. It is called with
// d.mu held.
func (d *Daemon) cohortOf(batch string) *Cohort {
	for _, c := range d.ctl.Cohorts {
		if slices.Contains(c.Batches, batch) {
			return c
		}
	}
	return nil
}

// CohortSummary is one line of `cohort ls`.
type CohortSummary struct {
	Name     string    `json:"name"`
	Variable string    `json:"variable"`
	Cells    int       `json:"cells"`
	Created  time.Time `json:"created"`
}

// CohortView is a cohort's cells side by side with the facts in which they
// differ (`cohort show`).
type CohortView struct {
	Name     string `json:"name"`
	Variable string `json:"variable"`
	// VariableFacts are the basis facts the variable stands for.
	VariableFacts []string     `json:"variable_facts"`
	Cells         []CohortCell `json:"cells"`
	// Differs are the basis facts in which the cells differ, with each
	// cell's values.
	Differs []CohortFact `json:"differs"`
	// Confounded is true when a fact other than the variable's differs;
	// ConfoundedBy names those facts.
	Confounded   bool     `json:"confounded"`
	ConfoundedBy []string `json:"confounded_by,omitempty"`
	Warnings     []string `json:"warnings"`
}

// CohortCell is one cell of a cohort: its headline and the value of the
// variable.
type CohortCell struct {
	Batch     string         `json:"batch"`
	Arm       string         `json:"arm"`
	ArmID     string         `json:"arm_id"`
	Value     string         `json:"value"`
	Metric    string         `json:"metric"`
	Headline  float64        `json:"headline"`
	Counts    map[string]int `json:"counts"`
	NExpected int            `json:"n_expected"`
	Complete  bool           `json:"complete"`
	Rescored  bool           `json:"rescored,omitempty"`
}

// CohortFact is one basis fact in which a cohort's cells differ.
type CohortFact struct {
	Name     string              `json:"name"`
	Values   map[string][]string `json:"values"` // by batch
	Variable bool                `json:"variable"`
}

func (d *Daemon) handleCohorts(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	out := []CohortSummary{}
	for _, c := range d.ctl.Cohorts {
		out = append(out, CohortSummary{Name: c.Name, Variable: c.Variable, Cells: len(c.Batches), Created: c.Created})
	}
	d.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	reply(w, http.StatusOK, out)
}

func (d *Daemon) handleCohort(w http.ResponseWriter, r *http.Request) {
	v, code, err := d.cohortView(r.Context(), r.PathValue("name"))
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	reply(w, http.StatusOK, v)
}

// cohortView puts a cohort's cells side by side.
func (d *Daemon) cohortView(ctx context.Context, name string) (CohortView, int, error) {
	d.mu.Lock()
	c := d.ctl.Cohorts[name]
	var batches []string
	if c != nil {
		batches = append(batches, c.Batches...)
	}
	d.mu.Unlock()
	if c == nil {
		return CohortView{}, http.StatusNotFound, fmt.Errorf("there is no cohort %s", name)
	}
	facts, _ := cohortFacts(c.Variable)
	v := CohortView{Name: c.Name, Variable: c.Variable, VariableFacts: facts, Cells: []CohortCell{}, Differs: []CohortFact{}, Warnings: []string{}}
	bases := map[string]map[string][]string{}
	for _, b := range batches {
		cv, code, err := d.cellView(ctx, b)
		if err != nil {
			if code == http.StatusNotFound {
				v.Warnings = append(v.Warnings, fmt.Sprintf("cell %s has no runs", b))
				continue
			}
			return v, code, err
		}
		res := cv.Result
		cell := CohortCell{Batch: b, Arm: res.Arm, ArmID: res.ArmID, Metric: res.Headline.Metric, Headline: res.Headline.Value,
			Counts: res.Counts, NExpected: res.NExpected, Complete: res.Complete, Rescored: res.Rescored != nil}
		if slices.Contains(basisFields, c.Variable) {
			cell.Value = strings.Join(res.Basis[c.Variable], " | ")
		} else {
			cell.Value = d.armValue(ctx, b, c.Variable)
		}
		if !res.Complete {
			v.Warnings = append(v.Warnings, fmt.Sprintf("cell %s is not complete", b))
		}
		v.Cells = append(v.Cells, cell)
		bases[b] = res.Basis
	}
	for _, f := range basisFields {
		var first []string
		differs := false
		values := map[string][]string{}
		for i, cell := range v.Cells {
			vals := bases[cell.Batch][f]
			values[cell.Batch] = vals
			if i == 0 {
				first = vals
			} else if !slices.Equal(first, vals) {
				differs = true
			}
		}
		if !differs {
			continue
		}
		isVar := slices.Contains(facts, f)
		v.Differs = append(v.Differs, CohortFact{Name: f, Values: values, Variable: isVar})
		if !isVar {
			v.ConfoundedBy = append(v.ConfoundedBy, f)
		}
	}
	v.Confounded = len(v.ConfoundedBy) > 0
	if len(v.Cells) > 1 {
		varies := false
		for _, cell := range v.Cells[1:] {
			varies = varies || cell.Value != v.Cells[0].Value
		}
		if !varies {
			v.Warnings = append(v.Warnings, fmt.Sprintf("the variable %s has the same value in every cell", c.Variable))
		}
	}
	return v, http.StatusOK, nil
}

// armValue is the value of an arm field in a cell's arm, as its first run
// recorded the arm; each different value when its runs' arms differ.
func (d *Daemon) armValue(ctx context.Context, batch, field string) string {
	recs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: batch})
	if err != nil {
		return notRecorded + err.Error()
	}
	var seen []string
	for _, rec := range recs {
		var m any
		if json.Unmarshal(rec.Arm, &m) != nil {
			continue
		}
		for _, k := range strings.Split(field, ".") {
			o, ok := m.(map[string]any)
			if !ok {
				m = nil
				break
			}
			m = o[k]
		}
		s := "unset"
		if m != nil {
			b, _ := json.Marshal(m)
			s = string(b)
		}
		if !slices.Contains(seen, s) {
			seen = append(seen, s)
		}
	}
	if len(seen) == 0 {
		return notRecorded + "no run recorded its arm"
	}
	sort.Strings(seen)
	return strings.Join(seen, " | ")
}
