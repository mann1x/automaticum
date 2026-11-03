package ingest

import (
	"encoding/json"
	"sort"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// swallowExamples is how many calls the finding quotes.
const swallowExamples = 5

// swallowed finds the calls with a swallowed argument (D131) with the
// tools' schemas the run's requests offered: tool_calls.swallowed_argument
// and its .by_tool.<tool> on every run with calls, and the finding when
// there was one.
func swallowed(runID string, ev *Events, reqs []Request, m func(string, float64), res *Result) {
	if len(ev.Calls) == 0 || !detect.Available(detect.SwallowedArgument) {
		return
	}
	schemas := Schemas(reqs)
	fs := detect.SwallowedArgs(ev.Calls, schemas)
	m("tool_calls.swallowed_argument", float64(len(fs)))
	if len(fs) == 0 {
		return
	}
	type tool struct {
		Tool      string         `json:"tool"`
		Calls     int            `json:"calls"`
		Swallowed map[string]int `json:"swallowed"`
		Turns     []int          `json:"turns"`
	}
	by := map[string]*tool{}
	for _, f := range fs {
		t := by[f.Tool]
		if t == nil {
			t = &tool{Tool: f.Tool, Swallowed: map[string]int{}}
			by[f.Tool] = t
		}
		t.Calls++
		t.Swallowed[f.Swallowed]++
		if len(t.Turns) < maxTurns {
			t.Turns = append(t.Turns, f.Turn)
		}
	}
	tools := make([]tool, 0, len(by))
	for name, t := range by {
		m("tool_calls.swallowed_argument.by_tool."+name, float64(t.Calls))
		tools = append(tools, *t)
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Calls > tools[j].Calls || tools[i].Calls == tools[j].Calls && tools[i].Tool < tools[j].Tool
	})
	examples := fs
	if len(examples) > swallowExamples {
		examples = examples[:swallowExamples]
	}
	raw, _ := json.Marshal(struct {
		Tools    []tool                 `json:"tools"`
		Schemas  bool                   `json:"schemas"`
		Examples []detect.SwallowFiring `json:"examples"`
	}{tools, len(schemas) > 0, examples})
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: detect.SwallowedArgument, DetectorVersion: detect.Version,
		Severity: "warning", Turn: fs[0].Turn, Count: len(fs), Evidence: raw})
}

// Schemas are the argument names of each tool, as the run's requests
// offered them; nil when none were captured.
func Schemas(reqs []Request) map[string][]string {
	var schemas map[string][]string
	for _, q := range reqs {
		for tool, names := range q.ToolArgs {
			if schemas == nil {
				schemas = map[string][]string{}
			}
			seen := map[string]bool{}
			for _, n := range schemas[tool] {
				seen[n] = true
			}
			for _, n := range names {
				if !seen[n] {
					schemas[tool] = append(schemas[tool], n)
				}
			}
		}
	}
	return schemas
}

// SwallowFirings are every call of a run with a swallowed argument; the
// finding quotes only the first few.
func SwallowFirings(ev *Events, reqs []Request) []detect.SwallowFiring {
	if len(ev.Calls) == 0 || !detect.Available(detect.SwallowedArgument) {
		return nil
	}
	return detect.SwallowedArgs(ev.Calls, Schemas(reqs))
}
