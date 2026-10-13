package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// Blobs is where a run's raw material is read from.
type Blobs interface {
	Reader(ctx context.Context, h blob.Hash) (io.ReadCloser, blob.Info, error)
}

// Result is what ingest derives from one run.
type Result struct {
	Metrics   []store.Metric
	ToolCalls []store.ToolCall
	Findings  []store.Finding
	Blocks    []Block
}

// owned names the detectors whose findings ingest writes. Findings of any
// other detector are kept as they are.
var owned = map[string]bool{WireArm: true}

// Run derives metrics, tool calls and findings from a run's stored material
// and writes them, replacing what an earlier ingest wrote. It is safe to run
// again: nothing but derived rows is touched.
func Run(ctx context.Context, st store.State, blobs Blobs, runID string) (*Result, error) {
	r, err := st.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if r.Imported {
		// An older harness's stream has another shape and no wire capture.
		return nil, fmt.Errorf("run %s is imported: its stream is not in the runner's format", runID)
	}
	refs, err := st.BlobRefs(ctx, runID)
	if err != nil {
		return nil, err
	}
	ev, reqs := &Events{Notices: map[string]int{}}, []Request(nil)
	for _, ref := range refs {
		if ref.Kind != "events" && ref.Kind != "wire" {
			continue
		}
		rd, _, err := blobs.Reader(ctx, ref.Hash)
		if err != nil {
			return nil, fmt.Errorf("%s of run %s: %w", ref.Kind, runID, err)
		}
		if ref.Kind == "events" {
			ev, err = ReadEvents(rd)
		} else {
			var more []Request
			more, err = ReadWire(rd)
			reqs = append(reqs, more...)
		}
		rd.Close()
		if err != nil {
			return nil, fmt.Errorf("%s of run %s: %w", ref.Kind, runID, err)
		}
	}
	res := Analyse(r, ev, reqs)
	old, err := st.Findings(ctx, runID)
	if err != nil {
		return nil, err
	}
	keep := res.Findings
	for _, f := range old {
		if !owned[f.Detector] {
			keep = append(keep, f)
		}
	}
	if err := st.ReplaceMetrics(ctx, runID, res.Metrics); err != nil {
		return nil, err
	}
	if err := st.ReplaceToolCalls(ctx, runID, res.ToolCalls); err != nil {
		return nil, err
	}
	if err := st.ReplaceFindings(ctx, runID, keep); err != nil {
		return nil, err
	}
	return res, nil
}

// Analyse derives everything from a run record and what was read from its
// event stream and wire capture. It reads and writes nothing.
func Analyse(r *store.Run, ev *Events, reqs []Request) *Result {
	res := &Result{Blocks: ev.Blocks}
	m := func(name string, v float64) {
		res.Metrics = append(res.Metrics, store.Metric{RunID: r.ID, Name: name, Value: v})
	}
	for _, c := range ev.ToolCalls {
		c.RunID = r.ID
		res.ToolCalls = append(res.ToolCalls, c)
	}

	// Time.
	m("time.wall_ms", float64(r.WallMS))
	if !ev.First.IsZero() {
		m("time.agent_ms", float64(ev.Last.Sub(ev.First).Milliseconds()))
	}
	if ev.FirstTokenMS > 0 {
		m("time.first_token_ms", ev.FirstTokenMS)
	}
	var toolMS float64
	for _, c := range ev.ToolCalls {
		toolMS += float64(c.DurationMS)
	}
	m("time.tools_ms", toolMS)

	// Volume.
	m("turns", float64(ev.Iterations))
	m("tool_calls", float64(len(ev.ToolCalls)))
	byTool, failed, failedBy := map[string]int{}, 0, map[string]int{}
	for _, c := range ev.ToolCalls {
		byTool[c.Tool]++
		if !c.OK {
			failed++
			failedBy[c.Tool]++
		}
	}
	m("tool_calls.failed", float64(failed))
	for _, t := range sortedKeys(byTool) {
		m("tool_calls.by_tool."+t, float64(byTool[t]))
	}
	for _, t := range sortedKeys(failedBy) {
		m("tool_calls.failed.by_tool."+t, float64(failedBy[t]))
	}
	m("questions", float64(ev.Questions))
	m("events.lines", float64(ev.Lines))
	if ev.BadLines > 0 {
		m("events.unreadable_lines", float64(ev.BadLines))
	}

	// What the server counted, request by request.
	var agent []Request
	other, bad := 0, 0
	for _, q := range reqs {
		switch {
		case q.Agent:
			agent = append(agent, q)
		default:
			other++
		}
		if q.Failed {
			bad++
		}
	}
	m("requests", float64(len(agent)))
	m("requests.other", float64(other))
	m("requests.failed", float64(bad))
	if len(agent) > 0 {
		var prompt, cached, eval, peak, empty int
		var promptNS, evalNS int64
		var modelMS float64
		var perRequest []float64
		done := map[string]int{}
		for _, q := range agent {
			prompt += q.PromptTokens
			cached += q.CachedTokens
			eval += q.EvalTokens
			promptNS += q.PromptNS
			evalNS += q.EvalNS
			modelMS += q.MS
			peak = max(peak, q.PromptTokens+q.EvalTokens)
			perRequest = append(perRequest, float64(q.EvalTokens))
			if q.Empty {
				empty++
			}
			reason := q.DoneReason
			if reason == "" {
				reason = "none"
			}
			done[reason]++
		}
		m("time.model_ms", modelMS)
		m("tokens.prompt", float64(prompt))
		m("tokens.prompt.cached", float64(cached))
		m("tokens.prompt.fresh", float64(prompt-cached))
		m("tokens.completion", float64(eval))
		if evalNS > 0 {
			m("rate.generation_tok_s", float64(eval)/(float64(evalNS)/1e9))
		}
		if promptNS > 0 && prompt > cached {
			m("rate.prompt_tok_s", float64(prompt-cached)/(float64(promptNS)/1e9))
		}
		distribution(m, "tokens.completion.per_request", perRequest)
		for _, reason := range sortedKeys(done) {
			m("finish."+reason, float64(done[reason]))
		}
		m("completions.empty", float64(empty))
		m("context.prompt_first", float64(agent[0].PromptTokens))
		m("context.prompt_last", float64(agent[len(agent)-1].PromptTokens))
		m("context.peak_tokens", float64(peak))
		if w := window(r, agent); w > 0 {
			m("context.window", float64(w))
			m("context.peak_share", float64(peak)/float64(w))
		}
		if prompt > 0 {
			m("context.cached_share", float64(cached)/float64(prompt))
		}
		if t := agent[0].ThinkBudgetTokens; t > 0 {
			m("thinking.budget_tokens.served", float64(t))
		}
	}

	// The SDK's split of the completion, and how it split.
	if ev.SplitMethod != "" {
		m("tokens.completion.reasoning", float64(ev.ReasoningTokens))
		m("tokens.completion.text", float64(ev.TextTokens))
		m("tokens.completion.tool_input", float64(ev.ToolInputTokens))
	}
	for _, name := range sortedKeys(ev.Derived) {
		m("derived."+name, ev.Derived[name])
	}

	// Thinking and visible text, in characters.
	var reasoning, text []float64
	thought, spoke := map[int]bool{}, map[int]bool{}
	for _, b := range ev.Blocks {
		n := float64(len([]rune(b.Text)))
		if b.Kind == "reasoning" {
			reasoning, thought[b.Turn] = append(reasoning, n), true
		} else {
			text, spoke[b.Turn] = append(text, n), true
		}
	}
	m("thinking.blocks", float64(len(reasoning)))
	distribution(m, "thinking.chars", reasoning)
	distribution(m, "text.chars", text)
	if ev.Iterations > 0 {
		m("thinking.turns_without", float64(ev.Iterations-len(thought)))
	}

	// Context management, as the SDK announced it.
	compactions := 0
	for _, key := range sortedKeys(ev.Notices) {
		m("notices."+key, float64(ev.Notices[key]))
		if strings.Contains(key, "compact") {
			compactions += ev.Notices[key]
		}
	}
	m("context.compactions", float64(compactions))

	var arm config.Arm
	if len(r.Arm) > 0 && json.Unmarshal(r.Arm, &arm) == nil {
		var prov struct {
			EndpointBefore *struct {
				Parameters string `json:"parameters"`
			} `json:"endpoint_before"`
		}
		json.Unmarshal(r.Provenance, &prov)
		tag := ""
		if prov.EndpointBefore != nil {
			tag = prov.EndpointBefore.Parameters
		}
		res.Findings = wireFindings(r.ID, CompareWire(&arm, tag, reqs))
	}
	return res
}

// window is the context window of a run: what the requests asked for, else
// what the arm states.
func window(r *store.Run, agent []Request) int {
	if v, ok := agent[0].Options["num_ctx"].(float64); ok && v > 0 {
		return int(v)
	}
	var arm config.Arm
	if json.Unmarshal(r.Arm, &arm) == nil && arm.Model.ContextWindow != nil {
		return *arm.Model.ContextWindow
	}
	return 0
}

// distribution writes n, sum, p10, p50, p90 and max of a list.
func distribution(m func(string, float64), name string, v []float64) {
	if len(v) == 0 {
		return
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	var sum float64
	for _, x := range s {
		sum += x
	}
	// Nearest rank: the smallest value with at least p of the list at or
	// below it.
	rank := func(p float64) float64 {
		i := int(p*float64(len(s))+0.999999) - 1
		return s[min(max(i, 0), len(s)-1)]
	}
	m(name+".n", float64(len(s)))
	m(name+".sum", sum)
	m(name+".p10", rank(0.10))
	m(name+".p50", rank(0.50))
	m(name+".p90", rank(0.90))
	m(name+".max", s[len(s)-1])
}
