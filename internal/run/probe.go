package run

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
)

// Identity is what an endpoint says about itself and about one model. It is
// read without loading the model and without generating anything.
type Identity struct {
	Kind          string    `json:"kind"`
	ProbedAt      time.Time `json:"probed_at"`
	ServerVersion string    `json:"server_version"`
	Model         string    `json:"model"`
	Digest        string    `json:"digest"`
	// The template as served: its hash and size, not its text.
	TemplateSHA256 string   `json:"template_sha256"`
	TemplateBytes  int      `json:"template_bytes"`
	Parameters     string   `json:"parameters"`
	Capabilities   []string `json:"capabilities"`
	// Renderer and Parser are the server's own prompt builder and answer
	// parser for the tag, when it uses them in place of the template.
	Renderer string          `json:"renderer,omitempty"`
	Parser   string          `json:"parser,omitempty"`
	Details  json.RawMessage `json:"details,omitempty"`
	// Loaded is how the model sits in memory, when it is loaded.
	Loaded *Loaded `json:"loaded"`
	// Weights is what the weights file says of itself: its "general"
	// metadata (base model, size label, who quantized it) and the
	// architecture's context length.
	Weights map[string]any `json:"weights,omitempty"`
	// Engine is the server's runner for the model, when it is loaded.
	Engine string `json:"engine,omitempty"`
	// OtherModels lists what else the server holds in memory. Loading
	// another model there would push these out.
	OtherModels []string `json:"other_models_loaded,omitempty"`
	// NotProbed lists what this kind of endpoint cannot be asked.
	NotProbed []string `json:"not_probed,omitempty"`
}

// Loaded is a model's residency as the server reports it.
type Loaded struct {
	ContextLength int   `json:"context_length"`
	Size          int64 `json:"size"`
	SizeVRAM      int64 `json:"size_vram"`
}

// Changed lists what differs between two probes of the same endpoint in a
// way that makes a run uncountable. Residency is not compared: a model is
// usually loaded by the run itself.
func (a Identity) Changed(b Identity) []string {
	var out []string
	cmp := func(what, x, y string) {
		if x != y {
			out = append(out, fmt.Sprintf("endpoint %s changed during the run: %s -> %s", what, x, y))
		}
	}
	cmp("server version", a.ServerVersion, b.ServerVersion)
	cmp("model digest", a.Digest, b.Digest)
	cmp("template", a.TemplateSHA256, b.TemplateSHA256)
	cmp("parameters", a.Parameters, b.Parameters)
	cmp("renderer", a.Renderer, b.Renderer)
	cmp("parser", a.Parser, b.Parser)
	return out
}

// Probe asks an endpoint for its identity and the model's. An error means
// the endpoint cannot serve the model as things stand: it does not answer,
// or it does not have the model.
func Probe(ctx context.Context, hc *http.Client, e *config.Endpoint, apiKey, model string) (Identity, error) {
	id := Identity{Kind: e.Kind, ProbedAt: time.Now().UTC(), Model: model}
	base := strings.TrimRight(e.BaseURL, "/")
	call := func(method, path string, in, out any) error {
		var body io.Reader
		if in != nil {
			b, _ := json.Marshal(in)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, body)
		if err != nil {
			return err
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := hc.Do(req)
		if err != nil {
			// The address is private; say what failed, not where.
			return fmt.Errorf("endpoint %s does not answer %s", e.Name, path)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("endpoint %s: %s gave status %d", e.Name, path, resp.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
	}
	if e.Kind == "openai-compatible" {
		var models struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := call("GET", "/models", nil, &models); err != nil {
			return id, err
		}
		id.NotProbed = []string{"server version", "model digest", "template", "parameters", "residency"}
		for _, m := range models.Data {
			if m.ID == model {
				return id, nil
			}
		}
		return id, fmt.Errorf("endpoint %s does not list model %s", e.Name, model)
	}

	var version struct {
		Version string `json:"version"`
	}
	if err := call("GET", "/api/version", nil, &version); err != nil {
		return id, err
	}
	id.ServerVersion = version.Version
	type entry struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		Digest        string `json:"digest"`
		Size          int64  `json:"size"`
		SizeVRAM      int64  `json:"size_vram"`
		ContextLength int    `json:"context_length"`
		Runner        string `json:"runner"`
	}
	var tags struct {
		Models []entry `json:"models"`
	}
	if err := call("GET", "/api/tags", nil, &tags); err != nil {
		return id, err
	}
	found := false
	for _, m := range tags.Models {
		if m.Name == model || m.Model == model {
			id.Digest, found = m.Digest, true
		}
	}
	if !found {
		return id, fmt.Errorf("endpoint %s does not have model %s", e.Name, model)
	}
	var show struct {
		Template     string          `json:"template"`
		Renderer     string          `json:"renderer"`
		Parser       string          `json:"parser"`
		Parameters   string          `json:"parameters"`
		Capabilities []string        `json:"capabilities"`
		Details      json.RawMessage `json:"details"`
		ModelInfo    map[string]any  `json:"model_info"`
	}
	if err := call("POST", "/api/show", map[string]string{"model": model}, &show); err != nil {
		return id, err
	}
	sum := sha256.Sum256([]byte(show.Template))
	id.TemplateSHA256, id.TemplateBytes = hex.EncodeToString(sum[:]), len(show.Template)
	id.Parameters, id.Capabilities, id.Details = show.Parameters, show.Capabilities, show.Details
	id.Renderer, id.Parser = show.Renderer, show.Parser
	for k, v := range show.ModelInfo {
		// Scalars only: a file's token list is not its identity.
		switch v.(type) {
		case string, float64, bool:
		default:
			continue
		}
		if strings.HasPrefix(k, "general.") || strings.HasSuffix(k, ".context_length") || strings.HasSuffix(k, ".block_count") {
			if id.Weights == nil {
				id.Weights = map[string]any{}
			}
			id.Weights[k] = v
		}
	}
	var ps struct {
		Models []entry `json:"models"`
	}
	if err := call("GET", "/api/ps", nil, &ps); err != nil {
		return id, err
	}
	for _, m := range ps.Models {
		if m.Name == model || m.Model == model {
			id.Loaded = &Loaded{ContextLength: m.ContextLength, Size: m.Size, SizeVRAM: m.SizeVRAM}
			id.Engine = m.Runner
		} else if m.Name != "" {
			id.OtherModels = append(id.OtherModels, m.Name)
		}
	}
	return id, nil
}

// Ollama says whether an endpoint is of a kind that answers the Ollama
// API: it tells what a model's weights file says of itself and can be asked
// to drop a model from memory.
func Ollama(e *config.Endpoint) bool { return e != nil && (e.Kind == "ollama" || e.Kind == "xollama") }

// FillIdentity completes what the catalogue states of a model with what
// the weights file says of itself, for the facts the entry leaves empty:
// the family from the base model's name, the size from the size label
// (D80). The catalogue's own words are never replaced, and the variant is
// never guessed: a file cannot say what a merge is called. The result
// lists what was read this way.
func FillIdentity(stated *config.ModelIdentity, tag string, weights map[string]any) *config.ModelIdentity {
	out := config.ModelIdentity{Tag: tag}
	if stated != nil {
		out = *stated
	}
	out.Queried = nil
	text := func(key string) string {
		s, _ := weights[key].(string)
		return strings.TrimSpace(s)
	}
	size := text("general.size_label")
	if strings.TrimSpace(out.Size) == "" && size != "" {
		out.Size, out.Queried = size, append(out.Queried, "size")
	}
	if strings.TrimSpace(out.Family) == "" {
		family := text("general.base_model.0.name")
		if family == "" {
			family = text("general.basename")
		}
		// "Qwen3.6 27B" and "Qwen3.6-27B" name the size too.
		if size != "" {
			family = strings.TrimRight(strings.TrimSuffix(family, size), " -")
		}
		if family != "" {
			out.Family, out.Queried = family, append(out.Queried, "family")
		}
	}
	if stated == nil && len(out.Queried) == 0 {
		return nil
	}
	sort.Strings(out.Queried)
	return &out
}

// Unload asks an Ollama-kind server to drop every model it holds in memory
// other than keep, and waits until it has. It returns the models dropped.
// The model a run is about to use is never dropped.
func Unload(ctx context.Context, hc *http.Client, e *config.Endpoint, apiKey, keep string) ([]string, error) {
	base := strings.TrimRight(e.BaseURL, "/")
	call := func(method, path string, in, out any) error {
		var body io.Reader
		if in != nil {
			b, _ := json.Marshal(in)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, body)
		if err != nil {
			return err
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return fmt.Errorf("endpoint %s does not answer %s", e.Name, path)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("endpoint %s: %s gave status %d", e.Name, path, resp.StatusCode)
		}
		if out == nil {
			_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			return err
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
	}
	held := func() ([]string, error) {
		var ps struct {
			Models []struct {
				Name  string `json:"name"`
				Model string `json:"model"`
			} `json:"models"`
		}
		if err := call("GET", "/api/ps", nil, &ps); err != nil {
			return nil, err
		}
		var others []string
		for _, m := range ps.Models {
			if m.Name != keep && m.Model != keep {
				others = append(others, m.Name)
			}
		}
		return others, nil
	}
	others, err := held()
	if err != nil || len(others) == 0 {
		return nil, err
	}
	for _, name := range others {
		// A generate request without a prompt and with no time to live is
		// the server's way of dropping a model: it generates nothing.
		if err := call("POST", "/api/generate", map[string]any{"model": name, "keep_alive": 0, "stream": false}, nil); err != nil {
			return nil, err
		}
	}
	for range 60 {
		left, err := held()
		if err != nil {
			return others, err
		}
		if len(left) == 0 {
			return others, nil
		}
		select {
		case <-ctx.Done():
			return others, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return others, fmt.Errorf("endpoint %s still holds another model a minute after it was asked to drop it", e.Name)
}

// ControlTokens reads the model's control tokens from an Ollama-kind
// server: the tokens its tokenizer marks as control (type 3 in the weights
// file's token types), such as the markers of a turn. A model writes them
// only when its template leaks into what it says. Nothing is loaded.
func ControlTokens(ctx context.Context, hc *http.Client, e *config.Endpoint, apiKey, model string) ([]string, error) {
	b, _ := json.Marshal(map[string]any{"model": model, "verbose": true})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(e.BaseURL, "/")+"/api/show", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s does not answer /api/show", e.Name)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoint %s: /api/show gave status %d", e.Name, resp.StatusCode)
	}
	var show struct {
		ModelInfo struct {
			Tokens []string `json:"tokenizer.ggml.tokens"`
			Types  []int    `json:"tokenizer.ggml.token_type"`
		} `json:"model_info"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(&show); err != nil {
		return nil, fmt.Errorf("endpoint %s: /api/show: %w", e.Name, err)
	}
	mi := show.ModelInfo
	if len(mi.Tokens) == 0 || len(mi.Tokens) != len(mi.Types) {
		return nil, fmt.Errorf("endpoint %s does not list the tokens of %s and their types", e.Name, model)
	}
	out := []string{}
	for i, t := range mi.Tokens {
		if mi.Types[i] == 3 {
			out = append(out, t)
		}
	}
	return out, nil
}
