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

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/stats"
	"github.com/mann1x/automaticum/internal/store"
)

// CompareView puts the runs of some batches side by side, one group per
// arm (070 §6). It gives the reader every number and draws no conclusion
// (D78): with small sets the swing from run to run is large, and whether a
// difference is real is the reader's call.
type CompareView struct {
	Batches []string       `json:"batches"`
	Groups  []CompareGroup `json:"groups"`
	Pairs   []ComparePair  `json:"pairs"`
	// NeverStarted are the runs of these batches that were taken off the
	// queue or refused before they started, with the reason: they belong
	// to no arm's numbers.
	NeverStarted map[string]string `json:"never_started,omitempty"`
	// Level is the level of every interval in the view.
	Level float64 `json:"interval_level"`
}

// CompareGroup is the runs of one arm on one task.
type CompareGroup struct {
	Arm   string `json:"arm"`
	ArmID string `json:"arm_id"`
	Task  string `json:"task"`
	Model string `json:"model"`
	SDK   string `json:"sdk_release"`
	// Runs are the runs counted; Excluded those left out, with the reason:
	// a run that never started or was not valid says nothing of the arm.
	Runs     []string          `json:"runs"`
	Excluded map[string]string `json:"excluded,omitempty"`
	Verdicts map[string]int    `json:"verdicts"`
	// Pass is the number of PASS among Runs, with its Wilson interval.
	Pass     int                      `json:"pass"`
	PassLow  float64                  `json:"pass_low"`
	PassHigh float64                  `json:"pass_high"`
	Metrics  map[string]stats.Summary `json:"metrics"`
	// Findings counts the runs with at least one finding of a detector.
	Findings map[string]int `json:"findings"`
	// Served holds what the server said of the model at the start of each
	// run, every distinct answer per fact (D82). More than one answer for
	// a fact means it changed between the runs of this group: a cloud model
	// moved to another quantization, a tag was rebuilt, a server upgraded.
	Served map[string][]string `json:"served"`
	// TokensPerSolved is the group's completion tokens over its PASS runs.
	TokensPerSolved float64 `json:"tokens_per_solved,omitempty"`
	// Cap is the generation-cap audit of the group (070 §5).
	Cap CapAudit `json:"cap"`

	values map[string][]float64
}

// CapAudit is the generation-cap audit of one arm on one task (070 §5): a
// cap hit at any turn of a run counts.
type CapAudit struct {
	// Verdict is CLEAN, CAPPED or UNVERIFIED, and Why says what made it.
	// A CAPPED group is not comparable with one at another budget.
	Verdict string `json:"verdict"`
	Why     string `json:"why"`
	// Requests are the agent's requests audited; Unreported those for
	// which the server gave no finish reason.
	Requests   int `json:"requests"`
	Unreported int `json:"unreported"`
	// CappedRequests, CappedRuns, and of these runs CappedPassed the ones
	// that still passed.
	CappedRequests int `json:"capped_requests"`
	CappedRuns     int `json:"capped_runs"`
	CappedPassed   int `json:"capped_passed"`
	// NotAudited are runs analysed before the audit existed.
	NotAudited []string `json:"not_audited,omitempty"`
	// Declared are the output limits the requests sent, and ThinkBudget the
	// thinking budgets the server applied: the reasoning-budget regime.
	Declared    []float64 `json:"declared_tokens,omitempty"`
	ThinkBudget []float64 `json:"think_budget_tokens,omitempty"`
}

// audit adds one run's cap numbers to the group's.
func (c *CapAudit) audit(id string, passed bool, metrics map[string]float64) {
	hits, audited := metrics["cap.hits"]
	if !audited {
		c.NotAudited = append(c.NotAudited, id)
		return
	}
	for name, v := range metrics {
		if strings.HasPrefix(name, "finish.") {
			c.Requests += int(v)
		}
	}
	c.Unreported += int(metrics["finish.none"])
	c.CappedRequests += int(hits)
	if hits > 0 {
		c.CappedRuns++
		if passed {
			c.CappedPassed++
		}
	}
	for _, x := range []struct {
		name string
		into *[]float64
	}{{"cap.declared_tokens", &c.Declared}, {"thinking.budget_tokens.served", &c.ThinkBudget}} {
		if v, ok := metrics[x.name]; ok && !slices.Contains(*x.into, v) {
			*x.into = append(*x.into, v)
		}
	}
}

// decide gives the group's verdict once every run is in.
func (c *CapAudit) decide() {
	slices.Sort(c.Declared)
	slices.Sort(c.ThinkBudget)
	switch {
	case c.CappedRequests > 0:
		c.Verdict = "CAPPED"
		c.Why = fmt.Sprintf("%d requests in %d runs ended at their output limit", c.CappedRequests, c.CappedRuns)
	case len(c.NotAudited) > 0:
		c.Verdict = "UNVERIFIED"
		c.Why = fmt.Sprintf("%d runs were analysed before the cap audit existed: `automaticum reanalyse` audits them", len(c.NotAudited))
	case c.Unreported > 0:
		// The pile-up test for such servers is not built yet.
		c.Verdict = "UNVERIFIED"
		c.Why = fmt.Sprintf("the server gave no finish reason for %d of %d requests", c.Unreported, c.Requests)
	case c.Requests == 0:
		c.Verdict = "UNVERIFIED"
		c.Why = "no request of the agent's loop was recorded"
	default:
		c.Verdict = "CLEAN"
		c.Why = fmt.Sprintf("none of %d requests ended at its output limit, and the server gave a finish reason for each", c.Requests)
	}
}

// ComparePair is two groups against each other.
type ComparePair struct {
	A, B int // indexes into Groups
	// Differs lists what differs between the two arms: above any number,
	// what else changed besides what is being compared.
	Differs []string `json:"differs"`
	// PassP is Fisher's exact test of the two pass counts, two-sided.
	PassP   float64               `json:"pass_p"`
	Metrics map[string]stats.Rank `json:"metrics"`
	// Cap is the cap asymmetry between the two arms (070 §5).
	Cap CapAsymmetry `json:"cap"`
}

// CapAsymmetry compares the capped requests of two arms over the tasks
// they share, as omnimergekit's agent summary does (summarize_arms, CAP
// GUARD, commit 01f5ed3): Hi and Lo are sums of capped requests, the floor
// is in tasks. When it is Material and Lo is 0 or Hi over Lo is 1.5 or
// more, the difference in scores is Confounded: the bench has measured
// length.
type CapAsymmetry struct {
	Hi, Lo     int  `json:"-"`
	A          int  `json:"a_capped_requests"`
	B          int  `json:"b_capped_requests"`
	Shared     int  `json:"shared_tasks"`
	Material   bool `json:"material"`
	Confounded bool `json:"confounded"`
}

func capAsymmetry(a, b, shared int) CapAsymmetry {
	c := CapAsymmetry{A: a, B: b, Hi: max(a, b), Lo: min(a, b), Shared: shared}
	c.Material = float64(c.Hi) >= max(5, 0.01*float64(shared))
	c.Confounded = c.Material && (c.Lo == 0 || float64(c.Hi)/float64(max(c.Lo, 1)) >= 1.5)
	return c
}

// served reads, from a run's record, what the server said of the model
// when the run started.
func served(provenance json.RawMessage) map[string]string {
	var p struct {
		Before *struct {
			ServerVersion  string `json:"server_version"`
			Digest         string `json:"digest"`
			TemplateSHA256 string `json:"template_sha256"`
			Parameters     string `json:"parameters"`
			Renderer       string `json:"renderer"`
			Parser         string `json:"parser"`
			Details        struct {
				Format        string `json:"format"`
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				Quantization  string `json:"quantization_level"`
			} `json:"details"`
		} `json:"endpoint_before"`
	}
	out := map[string]string{}
	if json.Unmarshal(provenance, &p) != nil || p.Before == nil {
		return out
	}
	b := p.Before
	cut := func(s string) string {
		if len(s) > 12 {
			return s[:12]
		}
		return s
	}
	params := ""
	if b.Parameters != "" {
		sum := sha256.Sum256([]byte(run.ParametersKey(b.Parameters)))
		params = hex.EncodeToString(sum[:6])
	}
	for fact, value := range map[string]string{"server version": b.ServerVersion, "model digest": cut(b.Digest), "quantization": b.Details.Quantization,
		"parameter size": b.Details.ParameterSize, "format": b.Details.Format, "family, by the server": b.Details.Family,
		"template": cut(b.TemplateSHA256), "tag parameters": params, "renderer": b.Renderer, "parser": b.Parser} {
		if value != "" {
			out[fact] = value
		}
	}
	return out
}

// counter says whether a metric counts events, so that a run without it
// has none.
func counter(name string) bool {
	for _, p := range []string{"tool_calls.by_tool.", "tool_calls.failed.by_tool.", "notices.", "finish."} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func (d *Daemon) handleCompare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	batches := r.URL.Query()["batch"]
	if len(batches) == 0 {
		fail(w, http.StatusBadRequest, "name at least one batch")
		return
	}
	v := CompareView{Batches: batches, Level: 0.95, Groups: []CompareGroup{}, Pairs: []ComparePair{}}
	index := map[string]int{}
	arms := map[int]*config.Arm{}
	for _, b := range batches {
		runs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: b})
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		if len(runs) == 0 {
			fail(w, http.StatusNotFound, "batch %s has no finished run", b)
			return
		}
		for _, run := range runs {
			if run.Verdict == store.Skipped {
				if v.NeverStarted == nil {
					v.NeverStarted = map[string]string{}
				}
				v.NeverStarted[run.ID] = run.VerdictReason
				continue
			}
			task := run.Suite + "/" + run.TaskID
			key := run.ArmID + " " + task
			i, ok := index[key]
			if !ok {
				i = len(v.Groups)
				index[key] = i
				g := CompareGroup{ArmID: run.ArmID, Task: task, Model: run.Model, SDK: run.Harness, Runs: []string{},
					Verdicts: map[string]int{}, Metrics: map[string]stats.Summary{}, Findings: map[string]int{}, Served: map[string][]string{}, values: map[string][]float64{}}
				var a config.Arm
				if json.Unmarshal(run.Arm, &a) == nil {
					g.Arm, arms[i] = a.Name, &a
				}
				v.Groups = append(v.Groups, g)
			}
			g := &v.Groups[i]
			g.Verdicts[string(run.Verdict)]++
			if run.Verdict == store.Invalid {
				if g.Excluded == nil {
					g.Excluded = map[string]string{}
				}
				g.Excluded[run.ID] = string(run.Verdict) + ": " + run.VerdictReason
				continue
			}
			g.Runs = append(g.Runs, run.ID)
			if run.Verdict == store.Pass {
				g.Pass++
			}
			for fact, value := range served(run.Provenance) {
				if !slices.Contains(g.Served[fact], value) {
					g.Served[fact] = append(g.Served[fact], value)
				}
			}
			metrics, err := d.State.Metrics(ctx, run.ID)
			if err != nil {
				fail(w, http.StatusInternalServerError, "%v", err)
				return
			}
			byName := map[string]float64{}
			for _, m := range metrics {
				byName[m.Name] = m.Value
				if !strings.HasSuffix(m.Name, ".available") {
					g.values[m.Name] = append(g.values[m.Name], m.Value)
				}
			}
			g.Cap.audit(run.ID, run.Verdict == store.Pass, byName)
			findings, err := d.State.Findings(ctx, run.ID)
			if err != nil {
				fail(w, http.StatusInternalServerError, "%v", err)
				return
			}
			seen := map[string]bool{}
			for _, f := range findings {
				if !seen[f.Detector] {
					seen[f.Detector] = true
					g.Findings[f.Detector]++
				}
			}
		}
	}
	for i := range v.Groups {
		g := &v.Groups[i]
		// A count that a run does not have is a zero of that run: a tool
		// it never called, a notice it never got.
		for name, vals := range g.values {
			if counter(name) {
				for len(vals) < len(g.Runs) {
					vals = append(vals, 0)
				}
				g.values[name] = vals
			}
		}
		g.Cap.decide()
		g.PassLow, g.PassHigh = stats.Wilson(g.Pass, len(g.Runs), stats.Z95)
		for name, vals := range g.values {
			g.Metrics[name] = stats.Summarise(vals)
		}
		if g.Pass > 0 {
			total := 0.0
			for _, t := range g.values["tokens.completion"] {
				total += t
			}
			g.TokensPerSolved = total / float64(g.Pass)
		}
	}
	// Every two groups on the same task.
	for i := range v.Groups {
		for j := i + 1; j < len(v.Groups); j++ {
			a, b := &v.Groups[i], &v.Groups[j]
			if a.Task != b.Task {
				continue
			}
			p := ComparePair{A: i, B: j, Differs: []string{}, Metrics: map[string]stats.Rank{},
				PassP: stats.Fisher(a.Pass, len(a.Runs)-a.Pass, b.Pass, len(b.Runs)-b.Pass),
				// One task per group: the two share it.
				Cap: capAsymmetry(a.Cap.CappedRequests, b.Cap.CappedRequests, 1)}
			if arms[i] != nil && arms[j] != nil {
				p.Differs = config.Diff(arms[i], arms[j])
			}
			names := []string{}
			for name := range a.values {
				if _, ok := b.values[name]; ok {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			for _, name := range names {
				p.Metrics[name] = stats.MannWhitney(a.values[name], b.values[name])
			}
			v.Pairs = append(v.Pairs, p)
		}
	}
	reply(w, http.StatusOK, v)
}
