package ingest

import (
	"encoding/json"
	"sort"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// DefinitionFirings are every string argument of a run's calls with
// repeated definitions: from the wire's calls when the requests were
// captured, as the server streamed them (a call the output limit cut has
// only "{}" in the event stream), else from the event stream's.
func DefinitionFirings(ev *Events, reqs []Request) ([]detect.DefinitionFiring, string) {
	if !detect.Available(detect.DuplicateDefinitions) {
		return nil, ""
	}
	var calls []detect.Call
	var seqs []int
	for _, q := range reqs {
		if !q.Agent {
			continue
		}
		for _, c := range q.Calls {
			calls = append(calls, detect.Call{Tool: c.Name, Input: c.Arguments})
			seqs = append(seqs, q.Seq)
		}
	}
	if len(calls) == 0 {
		return detect.DuplicateDefinitionCalls(ev.Calls), "events"
	}
	fs := detect.DuplicateDefinitionCalls(calls)
	for i := range fs {
		fs[i].Request = seqs[fs[i].Index]
	}
	return fs, "wire"
}

// duplicateDefinitions writes tool_calls.duplicate_definitions and its
// .by_tool.<tool> on every run with calls, and the finding when there was
// one.
func duplicateDefinitions(runID string, ev *Events, reqs []Request, m func(string, float64), res *Result) {
	if len(ev.Calls) == 0 && len(reqs) == 0 {
		return
	}
	fs, source := DefinitionFirings(ev, reqs)
	if source == "" {
		return
	}
	m("tool_calls.duplicate_definitions", float64(len(fs)))
	if len(fs) == 0 {
		return
	}
	by := map[string]int{}
	for _, f := range fs {
		by[f.Tool]++
	}
	tools := make([]string, 0, len(by))
	for t, n := range by {
		m("tool_calls.duplicate_definitions.by_tool."+t, float64(n))
		tools = append(tools, t)
	}
	sort.Strings(tools)
	examples := fs
	if len(examples) > swallowExamples {
		examples = examples[:swallowExamples]
	}
	raw, _ := json.Marshal(struct {
		Source   string                    `json:"source"`
		Max      int                       `json:"max"`
		ByTool   map[string]int            `json:"by_tool"`
		Examples []detect.DefinitionFiring `json:"examples"`
	}{source, detect.DuplicateDefinitionsMax, by, examples})
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: detect.DuplicateDefinitions, DetectorVersion: detect.Version,
		Severity: "warning", Turn: fs[0].Turn, Count: len(fs), Evidence: raw})
}
