package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/detect"
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
	// Invalid lists what makes the run INVALID by the endpoint's conduct
	// (D73); empty when nothing does or the arm accepts it.
	Invalid []string
}

// GPUResidency is the finding that the model was only partly on the GPU
// when the run ended: its speed is then not the arm's.
const GPUResidency = "gpu_residency"

// ModelReloaded is the finding that the server loaded the model again
// after the run's first request.
const ModelReloaded = "model_reloaded"

// SessionFork is the finding that a restore of messages started a new
// session inside the run (D86): every counter and guard of the SDK started
// again from nothing at that time, so the run's guard and counter metrics
// are not those of one session. It records each fork; it judges nothing.
const SessionFork = "session_fork"

// AnalysisVersion numbers the set of detector versions an analysis
// applies. It goes up whenever a version in Versions changes, and every
// analysis records it as the metric analysis.version: the basis (020 §5)
// says from it which detectors a run was analysed with, whether or not
// they found anything.
const AnalysisVersion = 7

// Versions are the versions of the detectors this analysis applies:
// ingest's own and the detector library's.
var Versions = map[string]string{
	"library": detect.Version, WireArm: WireArmVersion, CapHit: "2", GPUResidency: "1", ModelIdentity: "1", ModelReloaded: "2", SessionFork: "1",
}

// VersionsText is Versions as one line, in order.
func VersionsText() string {
	names := make([]string, 0, len(Versions))
	for n := range Versions {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		names[i] = n + " " + Versions[n]
	}
	return strings.Join(names, ", ")
}

// reloadSeconds is the load time from which a request counts as a reload. A
// model in memory answers "load" in milliseconds; a load takes seconds.
const reloadSeconds = 1.0

// owned names the detectors whose findings ingest writes. Findings of any
// other detector are kept as they are.
var owned = map[string]bool{WireArm: true, CapHit: true, GPUResidency: true, ModelReloaded: true, ModelIdentity: true, detect.ShortUnitRepeat: true, detect.CharRun: true,
	detect.SentenceRepeat: true, detect.Runaway: true, detect.RepeatedToolCall: true,
	detect.ReasoningRepetition: true, detect.ReasoningLoop: true}

// Run derives metrics, tool calls and findings from a run's stored material
// and writes them, replacing what an earlier ingest wrote. It is safe to run
// again: nothing but derived rows is touched.
//
// An imported run gets the detectors' findings and nothing else: its tool
// calls were written by the importer and it has no wire capture, so the
// metrics that need one do not exist for it (D65).
func Run(ctx context.Context, st store.State, blobs Blobs, runID string) (*Result, error) {
	r, err := st.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	refs, err := st.BlobRefs(ctx, runID)
	if err != nil {
		return nil, err
	}
	ev, reqs := &Events{Notices: map[string]int{}, GuardTrips: map[string]int{}, GuardCauses: map[string]int{}}, []Request(nil)
	for _, ref := range refs {
		if ref.Kind != "events" && (ref.Kind != "wire" || r.Imported) {
			continue
		}
		rd, _, err := blobs.Reader(ctx, ref.Hash)
		if err != nil {
			return nil, fmt.Errorf("%s of run %s: %w", ref.Kind, runID, err)
		}
		if ref.Kind == "events" {
			err = ev.Read(rd)
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
	var res *Result
	if r.Imported {
		res = &Result{Blocks: ev.Blocks}
		res.Findings, _ = detectorFindings(r.ID, ev, detect.Options{})
	} else {
		res = Analyse(r, ev, reqs)
	}
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
	if !r.Imported {
		if err := st.ReplaceMetrics(ctx, runID, res.Metrics); err != nil {
			return nil, err
		}
		if err := st.ReplaceToolCalls(ctx, runID, res.ToolCalls); err != nil {
			return nil, err
		}
	}
	if err := st.ReplaceFindings(ctx, runID, keep); err != nil {
		return nil, err
	}
	// A run whose model was loaded again, or sat partly off the GPU, did
	// not run under the arm's conditions: INVALID, unless the arm accepts
	// it (D73). The verdict it had is kept beside it. A verdict this rule
	// set is taken back when the cause is gone on a later analysis.
	const by = "endpoint: "
	switch {
	case len(res.Invalid) > 0 && r.Verdict != store.Invalid && r.Verdict != store.Skipped:
		if r.PriorVerdict == "" {
			r.PriorVerdict = r.Verdict
		}
		r.Verdict, r.VerdictReason = store.Invalid, by+strings.Join(res.Invalid, "; ")
		err = st.UpsertRun(ctx, r)
	case len(res.Invalid) == 0 && r.Verdict == store.Invalid && strings.HasPrefix(r.VerdictReason, by) && r.PriorVerdict != "":
		r.Verdict, r.PriorVerdict, r.VerdictReason = r.PriorVerdict, "", ""
		err = st.UpsertRun(ctx, r)
	}
	return res, err
}

// severity of a condition the arm accepts or does not.
func severity(accepted bool) string {
	if accepted {
		return "note"
	}
	return "error"
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
	m("analysis.version", AnalysisVersion)
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
	m("user.actions", float64(ev.UserActions))
	m("user.denials", float64(ev.UserDenials))
	m("user.failed", float64(ev.UserFailed))
	m("user.forks", float64(len(ev.Forks)))
	m("user.mode_switches", float64(ev.ModeSwitches))
	m("mode_switches.by_agent", float64(ev.AgentModeSwitches))
	trips := 0
	for _, key := range sortedKeys(ev.GuardTrips) {
		m("guard_trips."+key, float64(ev.GuardTrips[key]))
		trips += ev.GuardTrips[key]
	}
	m("guard_trips", float64(trips))
	// By cause too, beside the guard and phase totals, not instead of them.
	for _, key := range sortedKeys(ev.GuardCauses) {
		m("guard_trips."+key, float64(ev.GuardCauses[key]))
	}
	if ev.GuardStopCause != "" {
		m("guard_stop."+ev.GuardStopCause, 1)
	}
	if ev.GuardStop != "" {
		m("guard_stop."+ev.GuardStop, 1)
	}
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
			switch {
			case reason != "":
			case q.Failed:
				// No answer came, so no finish reason could: a request cut
				// by the agent's side (a steer, a cancel) or one that
				// failed. It is not one the server left unexplained.
				reason = "failed"
			default:
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
		// The cap audit, every turn (070 §5).
		hits := CapHits(agent)
		m("cap.hits", float64(len(hits)))
		if v, ok := agent[0].Options["num_predict"].(float64); ok && v > 0 {
			m("cap.declared_tokens", v)
		}
		if len(hits) > 0 {
			raw, _ := json.Marshal(map[string]any{"capped": hits, "tolerance": CapTolerance})
			res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: CapHit, DetectorVersion: Versions[CapHit],
				Severity: "warning", Turn: hits[0].Seq, Count: len(hits), Evidence: raw})
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
	for _, key := range sortedKeys(ev.Notices) {
		m("notices."+key, float64(ev.Notices[key]))
	}
	m("context.compactions", float64(ev.Compactions))

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

	// What the arm accepts of the two conditions below (D73).
	var stated struct {
		Accepts config.Accepts `json:"accepts"`
	}
	json.Unmarshal(r.Arm, &stated)
	accepts := stated.Accepts

	// Where the model sat after the run. The gate before the run can only
	// look when the model is already loaded; this is the look after load.
	var after struct {
		EndpointAfter *struct {
			Loaded *struct {
				Size     int64 `json:"size"`
				SizeVRAM int64 `json:"size_vram"`
			} `json:"loaded"`
		} `json:"endpoint_after"`
	}
	json.Unmarshal(r.Provenance, &after)
	if e := after.EndpointAfter; e != nil && e.Loaded != nil && e.Loaded.Size > 0 {
		share := float64(e.Loaded.SizeVRAM) / float64(e.Loaded.Size)
		m("endpoint.gpu_share", share)
		if e.Loaded.SizeVRAM < e.Loaded.Size {
			raw, _ := json.Marshal(map[string]any{"size": e.Loaded.Size, "size_vram": e.Loaded.SizeVRAM, "share": share})
			res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: GPUResidency, DetectorVersion: Versions[GPUResidency],
				Severity: severity(accepts.PartialGPU), Count: 1, Evidence: raw})
			if !accepts.PartialGPU {
				res.Invalid = append(res.Invalid, fmt.Sprintf("the model was only partly on the GPU (%.0f%%)", 100*share))
			}
		}
	}

	// A run whose model has no stated identity runs all the same, and says
	// so (D71): its family, size or variant is not on record.
	var who struct {
		Inference *struct {
			Model struct {
				Tag       string   `json:"tag"`
				NotStated []string `json:"not_stated"`
			} `json:"model"`
		} `json:"inference"`
	}
	json.Unmarshal(r.Provenance, &who)
	if i := who.Inference; i != nil && len(i.Model.NotStated) > 0 {
		raw, _ := json.Marshal(map[string]any{"tag": i.Model.Tag, "not_stated": i.Model.NotStated})
		res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: ModelIdentity, DetectorVersion: Versions[ModelIdentity],
			Severity: "warning", Count: len(i.Model.NotStated), Evidence: raw})
	}

	// Each fork of the session (D86), with its time and what restarted.
	if len(ev.Forks) > 0 {
		raw, _ := json.Marshal(map[string]any{"forks": ev.Forks})
		res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: SessionFork, DetectorVersion: Versions[SessionFork],
			Severity: "note", Count: len(ev.Forks), Evidence: raw})
	}

	// A model that had to be loaded again in the middle of a run was pushed
	// out of memory by something else: the run's times are not the arm's.
	// The server says how long it spent loading for every chat request.
	// The one exception is the first request after a pause the operator
	// asked for: the pause dropped the model on purpose, and that reload
	// keeps the verdict (D85). It is recorded apart.
	type load struct {
		Seq     int     `json:"seq"`
		Seconds float64 `json:"seconds"`
	}
	var held struct {
		Holds []struct {
			To time.Time `json:"to"`
		} `json:"holds"`
	}
	json.Unmarshal(r.Provenance, &held)
	// The first chat request to start after each release.
	afterRelease := map[int]bool{}
	for _, h := range held.Holds {
		for _, q := range reqs {
			if q.Chat && !q.Failed && !h.To.IsZero() && !q.Start.Before(h.To) {
				afterRelease[q.Seq] = true
				break
			}
		}
	}
	var loads, afterPause []load
	chats := 0
	for _, q := range reqs {
		if !q.Chat || q.Failed {
			continue
		}
		chats++
		if sec := float64(q.LoadNS) / 1e9; chats == 1 {
			m("endpoint.first_load_s", sec)
		} else if sec >= reloadSeconds && afterRelease[q.Seq] {
			afterPause = append(afterPause, load{q.Seq, sec})
		} else if sec >= reloadSeconds {
			loads = append(loads, load{q.Seq, sec})
		}
	}
	if chats > 0 {
		m("endpoint.reloads", float64(len(loads)))
		if len(held.Holds) > 0 {
			m("endpoint.reloads_after_pause", float64(len(afterPause)))
		}
	}
	if len(loads)+len(afterPause) > 0 {
		ev := map[string]any{"reloads": loads, "threshold_s": reloadSeconds}
		if len(afterPause) > 0 {
			ev["after_pause"] = afterPause
		}
		if loads == nil {
			ev["reloads"] = []load{}
		}
		raw, _ := json.Marshal(ev)
		sev, first := severity(accepts.ModelReload), 0
		if len(loads) == 0 {
			sev, first = "note", afterPause[0].Seq
		} else {
			first = loads[0].Seq
		}
		res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: ModelReloaded, DetectorVersion: Versions[ModelReloaded],
			Severity: sev, Turn: first, Count: len(loads) + len(afterPause), Evidence: raw})
		if len(loads) > 0 && !accepts.ModelReload {
			res.Invalid = append(res.Invalid, fmt.Sprintf("the model was loaded again during the run (%d times)", len(loads)))
		}
	}

	// What the model wrote and called, through the detector library. The
	// runaway size is the library's default until suites declare their own.
	found, counts := detectorFindings(r.ID, ev, detect.Options{})
	res.Findings = append(res.Findings, found...)
	res.Metrics = append(res.Metrics, counts...)
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
