package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/stats"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/task"
)

// CompareView puts the runs of some batches side by side, one group per
// arm (070 §6). It gives the reader every number and draws no conclusion
// (D78): with small sets the swing from run to run is large, and whether a
// difference is real is the reader's call.
type CompareView struct {
	Batches []string `json:"batches"`
	// Split is how the runs are grouped when not by arm (070 §6): "build"
	// (the regression view), "lane" (lane skew), or a group-by axis:
	// "arm.<field>", "label.<name>", "finding.<detector>".
	Split  string         `json:"split,omitempty"`
	Groups []CompareGroup `json:"groups"`
	Pairs  []ComparePair  `json:"pairs"`
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
	// Build is the harness build fact of the group's runs, in the
	// regression view only.
	Build string `json:"build,omitempty"`
	// Value is the group's lane host under split "lane", or its value of
	// the axis under a group-by split.
	Value string `json:"value,omitempty"`
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
	// Basis is every fact that must be equal for two groups to be
	// compared (020 §5).
	Basis Basis `json:"basis"`

	values map[string][]float64
	bases  []map[string]string
	passed map[string]bool // by run, for the streaks
	// Streaks are the longest runs of PASS and of anything else, in the
	// order the runs started (070 §6).
	Streaks Streaks `json:"streaks"`
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
	PassP float64 `json:"pass_p"`
	// Paired is McNemar's test over the runs of the two groups paired in
	// the order they ran (070 §6): the i-th counted run of one with the
	// i-th of the other.
	Paired  Paired                `json:"paired"`
	Metrics map[string]stats.Rank `json:"metrics"`
	// Cap is the cap asymmetry between the two arms (070 §5).
	Cap CapAsymmetry `json:"cap"`
	// Basis lists exactly the facts of the basis in which the two differ.
	Basis []BasisDiff `json:"basis"`
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

// dropQuietUser leaves out the scripted user's metrics (user.*), and the
// agent's mode switches with them, that are zero in every run of every
// group: runs without a script or a plan always have them at zero, and
// they would only fill the comparison (owner, 2026-10-16, D88). One that
// is not zero somewhere stays, zeros included.
func dropQuietUser(groups []CompareGroup) {
	quiet := map[string]bool{}
	for _, g := range groups {
		for name, vals := range g.values {
			if !quietWhenZero(name) {
				continue
			}
			if _, seen := quiet[name]; !seen {
				quiet[name] = true
			}
			for _, x := range vals {
				if x != 0 {
					quiet[name] = false
				}
			}
		}
	}
	for name, q := range quiet {
		if q {
			for _, g := range groups {
				delete(g.values, name)
			}
		}
	}
}

// quietWhenZero says whether a metric is left out where it is zero (D88).
func quietWhenZero(name string) bool {
	return strings.HasPrefix(name, "user.") || strings.HasPrefix(name, "mode_switches.") || strings.HasPrefix(name, "guard_trips") || strings.HasPrefix(name, "guard_stop.")
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

// Paired is the paired comparison of two groups' pass counts: how many
// pairs, the discordant ones each way, McNemar's exact p, and the runs left
// without a partner.
type Paired struct {
	Pairs    int     `json:"pairs"`
	OnlyA    int     `json:"only_a"`
	OnlyB    int     `json:"only_b"`
	P        float64 `json:"p"`
	Unpaired int     `json:"unpaired"`
}

func paired(a, b *CompareGroup) Paired {
	ra, rb := append([]string(nil), a.Runs...), append([]string(nil), b.Runs...)
	sort.Strings(ra)
	sort.Strings(rb)
	n := min(len(ra), len(rb))
	p := Paired{Pairs: n, Unpaired: len(ra) + len(rb) - 2*n}
	for i := 0; i < n; i++ {
		switch pa, pb := a.passed[ra[i]], b.passed[rb[i]]; {
		case pa && !pb:
			p.OnlyA++
		case pb && !pa:
			p.OnlyB++
		}
	}
	p.P = stats.McNemar(p.OnlyA, p.OnlyB)
	return p
}

// Streaks are a group's longest runs of passes and of failures (any
// verdict counted that is not PASS), in the order the runs started.
type Streaks struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
}

// streaks reads the runs in the order of their ids, which start with the
// time they were made.
func streaks(runs []string, passed map[string]bool) Streaks {
	ids := append([]string(nil), runs...)
	sort.Strings(ids)
	var s Streaks
	cur, last := 0, false
	for i, id := range ids {
		p := passed[id]
		if i == 0 || p != last {
			cur = 0
		}
		cur++
		last = p
		if p && cur > s.Pass {
			s.Pass = cur
		}
		if !p && cur > s.Fail {
			s.Fail = cur
		}
	}
	return s
}

// validSplit says whether split names a way of grouping compare knows.
func validSplit(split string) bool {
	switch split {
	case "", "build", "lane":
		return true
	}
	return groupAxis(split) != ""
}

// groupAxis is the field, label or detector of a group-by split, or "".
func groupAxis(split string) string {
	for _, prefix := range []string{"arm.", "label.", "finding."} {
		if rest, ok := strings.CutPrefix(split, prefix); ok && rest != "" {
			return rest
		}
	}
	return ""
}

// laneHost is the host a run's agent ran on, as its record says, else its
// lane's name.
func laneHost(run *store.Run) string {
	var p struct {
		Inference struct {
			LaneHost string `json:"lane_host"`
		} `json:"inference"`
	}
	json.Unmarshal(run.Provenance, &p)
	if p.Inference.LaneHost != "" {
		return p.Inference.LaneHost
	}
	if run.Lane != "" {
		return "lane " + run.Lane
	}
	return "not recorded"
}

// groupValue is a run's value of a group-by axis: an arm field as the run
// recorded its arm (a dotted path, "thinking.level"), a label of its task
// as the task's files say now, or whether it has a finding of a detector.
func groupValue(split string, run *store.Run, labels map[string]string, findings []store.Finding) string {
	axis := groupAxis(split)
	switch {
	case strings.HasPrefix(split, "arm."):
		var v any
		if json.Unmarshal(run.Arm, &v) != nil {
			return "not recorded"
		}
		for _, k := range strings.Split(axis, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				return "not stated"
			}
			if v, ok = m[k]; !ok {
				return "not stated"
			}
		}
		switch t := v.(type) {
		case nil:
			return "not stated"
		case string:
			return t
		case float64, bool:
			return fmt.Sprint(t)
		}
		b, _ := json.Marshal(v)
		return string(b)
	case strings.HasPrefix(split, "label."):
		if l, ok := labels[axis]; ok {
			return l
		}
		return "not labelled"
	default:
		for _, f := range findings {
			if f.Detector == axis {
				return "with " + axis
			}
		}
		return "without " + axis
	}
}

// handleCompare gives compare's view of the batches named (?batch=, given
// any number of times), grouped as ?split= says.
func (d *Daemon) handleCompare(w http.ResponseWriter, r *http.Request) {
	split := r.URL.Query().Get("split")
	if !validSplit(split) {
		fail(w, http.StatusBadRequest, "split %q: build, lane, arm.<field>, label.<name> or finding.<detector>", split)
		return
	}
	v, code, err := d.compareView(r.Context(), r.URL.Query()["batch"], nil, split)
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	reply(w, http.StatusOK, v)
}

// compareView groups the counted runs of some batches by arm and task, with
// every number of each group and of each pair, and each group's basis. An
// error comes with the HTTP status it means.
//
// leave names runs that are not counted at all, such as a run a retry
// replaced.
//
// split "build" is the regression view (070 §6): the runs are grouped by
// arm name, task and harness build (SDK release and package, runner
// protocol, lane image, automaticum version), so an arm whose id changed
// with its SDK release is still one arm, and only the groups of one arm
// are paired.
func (d *Daemon) compareView(ctx context.Context, batches []string, leave map[string]bool, split string) (CompareView, int, error) {
	if len(batches) == 0 {
		return CompareView{}, http.StatusBadRequest, errors.New("name at least one batch")
	}
	v := CompareView{Batches: batches, Split: split, Level: 0.95, Groups: []CompareGroup{}, Pairs: []ComparePair{}}
	index := map[string]int{}
	arms := map[int]*config.Arm{}
	axis := groupAxis(split)
	var labels map[string]map[string]string
	if strings.HasPrefix(split, "label.") {
		tasks, err := task.LoadAll(d.taskRoots())
		if err != nil {
			return v, http.StatusInternalServerError, fmt.Errorf("the tasks cannot be read: %v", err)
		}
		labels = map[string]map[string]string{}
		for _, t := range tasks {
			labels[t.Suite+"/"+t.ID] = t.Labels
		}
	}
	for _, b := range batches {
		runs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: b})
		if err != nil {
			return v, http.StatusInternalServerError, err
		}
		if len(runs) == 0 {
			return v, http.StatusNotFound, fmt.Errorf("batch %s has no finished run", b)
		}
		for _, run := range runs {
			if leave[run.ID] {
				continue
			}
			if run.Verdict == store.Skipped {
				if v.NeverStarted == nil {
					v.NeverStarted = map[string]string{}
				}
				v.NeverStarted[run.ID] = run.VerdictReason
				continue
			}
			task := run.Suite + "/" + run.TaskID
			var a config.Arm
			named := json.Unmarshal(run.Arm, &a) == nil
			findings, err := d.State.Findings(ctx, run.ID)
			if err != nil {
				return v, http.StatusInternalServerError, err
			}
			key, build, value := run.ArmID+" "+task, "", ""
			switch {
			case split == "build":
				build = basisOfRun(&run, nil)["harness build"]
				if build == "" {
					build = "not recorded"
				}
				key = a.Name + " " + task + " " + build
			case split == "lane":
				value = laneHost(&run)
				key = run.ArmID + " " + task + " " + value
			case axis != "":
				value = groupValue(split, &run, labels[task], findings)
				key = task + " " + value
			}
			i, ok := index[key]
			if !ok {
				i = len(v.Groups)
				index[key] = i
				g := CompareGroup{ArmID: run.ArmID, Task: task, Model: run.Model, SDK: run.Harness, Build: build, Value: value, Runs: []string{},
					Verdicts: map[string]int{}, Metrics: map[string]stats.Summary{}, Findings: map[string]int{}, Served: map[string][]string{}, values: map[string][]float64{}}
				if named {
					g.Arm = a.Name
					// A group-by group may hold several arms: no arm
					// stands for it in the pairs' arm diff.
					if axis == "" {
						arms[i] = &a
					}
				}
				v.Groups = append(v.Groups, g)
			} else if g := &v.Groups[i]; !slices.Contains(strings.Split(g.ArmID, ","), run.ArmID) {
				g.ArmID += "," + run.ArmID
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
			if g.passed == nil {
				g.passed = map[string]bool{}
			}
			g.passed[run.ID] = run.Verdict == store.Pass
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
				return v, http.StatusInternalServerError, err
			}
			byName := map[string]float64{}
			for _, m := range metrics {
				byName[m.Name] = m.Value
				if !strings.HasSuffix(m.Name, ".available") {
					g.values[m.Name] = append(g.values[m.Name], m.Value)
				}
			}
			g.Cap.audit(run.ID, run.Verdict == store.Pass, byName)
			seen := map[string]bool{}
			for _, f := range findings {
				if !seen[f.Detector] {
					seen[f.Detector] = true
					g.Findings[f.Detector]++
				}
			}
			g.bases = append(g.bases, basisOfRun(&run, byName))
		}
	}
	dropQuietUser(v.Groups)
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
		g.Streaks = streaks(g.Runs, g.passed)
		g.Basis = basisOf(g.bases)
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
			if a.Task != b.Task || split == "build" && a.Arm != b.Arm || split == "lane" && a.ArmID != b.ArmID {
				continue
			}
			p := ComparePair{A: i, B: j, Differs: []string{}, Metrics: map[string]stats.Rank{},
				PassP: stats.Fisher(a.Pass, len(a.Runs)-a.Pass, b.Pass, len(b.Runs)-b.Pass),
				// One task per group: the two share it.
				Cap:   capAsymmetry(a.Cap.CappedRequests, b.Cap.CappedRequests, 1),
				Basis: DiffBasis(a.Basis, b.Basis), Paired: paired(a, b)}
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
	return v, 0, nil
}
