package config

import (
	"strings"
	"testing"
)

// A cell's arm runs on the cell's endpoint and takes the caller's values,
// each with the caller as its origin, under an identity of its own; values
// that leave the arm incomplete are refused like an incomplete arm.
func TestArmForCell(t *testing.T) {
	a, err := LoadArm("../../examples/config/arms", "stub")
	if err != nil {
		t.Fatal(err)
	}
	id := a.ID
	cv, err := ParseCallerValues("v.toml", []byte("output_tokens = 4096\ncontext_window = 16384\n[sampler]\nowner = \"arm\"\nname = \"c\"\ntemperature = 0.6\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ForCell("cell-x", "llamacpp", "served", cv); err != nil {
		t.Fatal(err)
	}
	if a.Endpoint != "cell-x" || a.Model.Provider != "openai-compatible" || a.Model.Model != "served" || *a.Model.OutputTokens != 4096 || *a.Model.ContextWindow != 16384 || *a.Sampler.Temperature != 0.6 {
		t.Errorf("arm: %+v", a)
	}
	for _, k := range []string{"endpoint", "model.model", "model.output_tokens", "sampler.owner", "sampler.temperature"} {
		if a.Origins[k] != "caller" {
			t.Errorf("origin of %s: %q", k, a.Origins[k])
		}
	}
	if a.Origins["thinking.owner"] == "caller" || a.ID == id || a.ID == "" {
		t.Errorf("thinking origin %q, id %s", a.Origins["thinking.owner"], a.ID)
	}

	b, _ := LoadArm("../../examples/config/arms", "stub")
	if err := b.ForCell("cell-x", "openai-compatible", "served", &CallerValues{Sampler: &Sampler{Name: "no owner"}}); err == nil || !strings.Contains(err.Error(), "sampler") {
		t.Errorf("a sampler without an owner: %v", err)
	}
	if _, err := ParseCallerValues("v.json", []byte(`{"sampler":{"owner":"tag"},"top_k":5}`)); err == nil {
		t.Error("an unknown field in JSON was taken")
	}
	if _, err := ParseCallerValues("v.toml", []byte("seed = 1\n")); err == nil {
		t.Error("an unknown field in TOML was taken")
	}
	if _, err := ParseBasisFacts([]byte(`{"n_parallel":2,"ctx_per_slot":32768,"kv_cache":"q8"}`)); err == nil {
		t.Error("an unknown basis fact was taken")
	}
}

// A cell's endpoint is found by name and usable from its lanes only; a
// configured endpoint cannot be replaced by one.
func TestCellEndpoint(t *testing.T) {
	l := &Local{Lanes: map[string]*Lane{"a": {Endpoints: []string{"e"}}, "b": {}}, Endpoints: map[string]*Endpoint{"e": {Name: "e"}}}
	if err := l.AddCellEndpoint(&Endpoint{Name: "e", Kind: "llamacpp"}, []string{"b"}); err == nil {
		t.Error("a cell replaced a configured endpoint")
	}
	if err := l.AddCellEndpoint(&Endpoint{Name: "cell-k", Kind: "llamacpp"}, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if ep := l.Endpoint("cell-k"); ep == nil || !ep.Cell || !l.MayUse("b", "cell-k") || l.MayUse("a", "cell-k") || !l.MayUse("a", "e") || l.MayUse("b", "e") {
		t.Errorf("lookups: %+v", ep)
	}
}
