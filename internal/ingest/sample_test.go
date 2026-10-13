package ingest

import (
	"encoding/json"
	"os"

	"github.com/mann1x/automaticum/internal/store"
	"testing"
)

// TestSample reads a real run's material when AUTOMATICUM_SAMPLE names a
// directory holding events.jsonl and wire.jsonl. It prints, it asserts little.
func TestSample(t *testing.T) {
	dir := os.Getenv("AUTOMATICUM_SAMPLE")
	if dir == "" {
		t.Skip("AUTOMATICUM_SAMPLE not set")
	}
	f, err := os.Open(dir + "/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ev, err := ReadEvents(f)
	if err != nil {
		t.Fatal(err)
	}
	fails := 0
	byTool := map[string]int{}
	for _, c := range ev.ToolCalls {
		byTool[c.Tool]++
		if !c.OK {
			fails++
		}
	}
	t.Logf("lines %d bad %d iterations %d end %q span %s", ev.Lines, ev.BadLines, ev.Iterations, ev.AgentEnd, ev.Last.Sub(ev.First))
	t.Logf("tool calls %d failed %d by tool %v", len(ev.ToolCalls), fails, byTool)
	t.Logf("blocks %d notices %v", len(ev.Blocks), ev.Notices)
	t.Logf("first token %.0f ms; split r %d t %d ti %d (%s); derived %v; questions %d",
		ev.FirstTokenMS, ev.ReasoningTokens, ev.TextTokens, ev.ToolInputTokens, ev.SplitMethod, ev.Derived, ev.Questions)
	w, err := os.Open(dir + "/wire.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	reqs, err := ReadWire(w)
	if err != nil {
		t.Fatal(err)
	}
	agent, in, out, empty := 0, 0, 0, 0
	paths := map[string]int{}
	done := map[string]int{}
	for _, q := range reqs {
		paths[q.Path]++
		if !q.Agent {
			continue
		}
		agent++
		in += q.PromptTokens
		out += q.EvalTokens
		done[q.DoneReason]++
		if q.Empty {
			empty++
		}
	}
	t.Logf("requests %d agent %d paths %v", len(reqs), agent, paths)
	t.Logf("in %d out %d empty %d done %v", in, out, empty, done)
	for _, q := range reqs {
		if q.Agent {
			t.Logf("first agent request: options %v think %s budget %s/%d", q.Options, q.Body["think"], q.ThinkBudget, q.ThinkBudgetTokens)
			break
		}
	}
	raw, err := os.ReadFile(dir + "/run.json")
	if err != nil {
		t.Skip("no run.json: ", err)
	}
	var view struct {
		ID         string          `json:"id"`
		WallMS     int64           `json:"wall_ms"`
		Arm        json.RawMessage `json:"resolved_arm"`
		Provenance json.RawMessage `json:"provenance"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	res := Analyse(&store.Run{ID: view.ID, WallMS: view.WallMS, Arm: view.Arm, Provenance: view.Provenance}, ev, reqs)
	for _, m := range res.Metrics {
		t.Logf("%-44s %v", m.Name, m.Value)
	}
	for _, f := range res.Findings {
		t.Logf("%s %s seq %d x%d %s", f.Detector, f.Severity, f.Turn, f.Count, f.Evidence)
	}
}
