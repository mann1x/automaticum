package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// CallerValues are the arm values a mode C caller resolves itself (065 §2,
// §3.1): each replaces the arm's, is recorded with source "caller", and is
// checked on the wire like any arm value. A section it leaves out stays the
// arm's.
type CallerValues struct {
	Sampler       *Sampler  `toml:"sampler" json:"sampler,omitempty"`
	Thinking      *Thinking `toml:"thinking" json:"thinking,omitempty"`
	OutputTokens  *int      `toml:"output_tokens" json:"output_tokens,omitempty"`
	ContextWindow *int      `toml:"context_window" json:"context_window,omitempty"`
}

// ParseCallerValues reads a caller-values file: TOML when its name ends in
// .toml, JSON otherwise. Unknown fields are errors.
func ParseCallerValues(name string, data []byte) (*CallerValues, error) {
	var cv CallerValues
	if strings.EqualFold(filepath.Ext(name), ".toml") {
		md, err := toml.Decode(string(data), &cv)
		if err != nil {
			return nil, fmt.Errorf("caller values: %w", err)
		}
		if u := md.Undecoded(); len(u) > 0 {
			keys := make([]string, len(u))
			for i, k := range u {
				keys[i] = k.String()
			}
			return nil, fmt.Errorf("caller values: unknown fields: %s", strings.Join(keys, ", "))
		}
		return &cv, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cv); err != nil {
		return nil, fmt.Errorf("caller values: %w", err)
	}
	return &cv, nil
}

// BasisFacts are what only a mode C caller knows of the server (065 §3.1).
// automaticum records them; on llama-server it checks those /props can
// contradict, and refuses the cell when one is contradicted.
type BasisFacts struct {
	WeightsSHA256      string `json:"weights_sha256,omitempty"`
	Quantization       string `json:"quantization,omitempty"`
	ChatTemplateSHA256 string `json:"chat_template_sha256,omitempty"`
	ImatrixSHA256      string `json:"imatrix_sha256,omitempty"`
	LlamaCppCommit     string `json:"llamacpp_commit,omitempty"`
	LibllamaBuildTime  string `json:"libllama_build_time,omitempty"`
	// NParallel is llama-server's -np.
	NParallel int `json:"n_parallel,omitempty"`
	// CtxTotal is llama-server's -c; CtxPerSlot what one slot holds.
	CtxTotal    int    `json:"ctx_total,omitempty"`
	CtxPerSlot  int    `json:"ctx_per_slot,omitempty"`
	KVCacheType string `json:"kv_cache_type,omitempty"`
	GPU         string `json:"gpu,omitempty"`
	Driver      string `json:"driver,omitempty"`
}

// ParseBasisFacts reads basis facts as JSON. Unknown fields are errors.
func ParseBasisFacts(data []byte) (*BasisFacts, error) {
	var f BasisFacts
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("basis facts: %w", err)
	}
	if f.NParallel < 0 || f.CtxTotal < 0 || f.CtxPerSlot < 0 {
		return nil, fmt.Errorf("basis facts: n_parallel, ctx_total and ctx_per_slot must not be negative")
	}
	return &f, nil
}

// caller is the origin of a value a mode C caller gave.
const caller = "caller"

// ProviderFor is the SDK provider that speaks to an endpoint of a kind.
// llamacpp is an OpenAI-compatible llama-server (030 §3): the SDK has no
// provider of that name, and asking for one ends the session before its
// first request ("Unknown or disabled provider").
func ProviderFor(kind string) string {
	if kind == "llamacpp" {
		return "openai-compatible"
	}
	return kind
}

// ForCell turns the arm into the arm of a mode C cell: it runs on the
// cell's endpoint, of kind kind, asks for model (the server's alias), and
// takes the caller's values. The result is validated like any arm and gets
// its own identity: the same arm file in two cells with other values is two
// arms.
func (a *Arm) ForCell(endpoint, kind, model string, cv *CallerValues) error {
	if a.Template {
		return fmt.Errorf("%s is a template, not an arm", a.Name)
	}
	if a.Origins == nil {
		a.Origins = map[string]string{}
	}
	a.Origins = maps.Clone(a.Origins)
	a.Endpoint, a.Model.Provider, a.Model.Model = endpoint, ProviderFor(kind), model
	// A pinned digest was the arm's model's; the caller serves another.
	a.Model.Digest = ""
	delete(a.Origins, "model.digest")
	for _, k := range []string{"endpoint", "model.provider", "model.model"} {
		a.Origins[k] = caller
	}
	reorigin := func(prefix string, section any) {
		maps.DeleteFunc(a.Origins, func(k, _ string) bool { return k == prefix || strings.HasPrefix(k, prefix+".") })
		b, _ := json.Marshal(section)
		var m map[string]any
		json.Unmarshal(b, &m)
		flat := map[string]string{}
		flatten(m, prefix, flat)
		for k := range flat {
			a.Origins[k] = caller
		}
	}
	if cv != nil {
		if cv.Sampler != nil {
			a.Sampler = *cv.Sampler
			reorigin("sampler", a.Sampler)
		}
		if cv.Thinking != nil {
			a.Thinking = *cv.Thinking
			reorigin("thinking", a.Thinking)
		}
		if cv.OutputTokens != nil {
			v := *cv.OutputTokens
			a.Model.OutputTokens = &v
			a.Origins["model.output_tokens"] = caller
		}
		if cv.ContextWindow != nil {
			v := *cv.ContextWindow
			a.Model.ContextWindow = &v
			a.Origins["model.context_window"] = caller
		}
	}
	if problems := a.Validate(); len(problems) > 0 {
		return &ArmError{Arm: a.Name + " with the caller's values", Problems: problems}
	}
	a.ID = a.hash()
	return nil
}
