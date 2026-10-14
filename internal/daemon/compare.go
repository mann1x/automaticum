package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/stats"
	"github.com/mann1x/automaticum/internal/run"
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

	values map[string][]float64
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
			for _, m := range metrics {
				if !strings.HasSuffix(m.Name, ".available") {
					g.values[m.Name] = append(g.values[m.Name], m.Value)
				}
			}
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
				PassP: stats.Fisher(a.Pass, len(a.Runs)-a.Pass, b.Pass, len(b.Runs)-b.Pass)}
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
