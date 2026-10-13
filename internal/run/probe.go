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
	}
	if err := call("POST", "/api/show", map[string]string{"model": model}, &show); err != nil {
		return id, err
	}
	sum := sha256.Sum256([]byte(show.Template))
	id.TemplateSHA256, id.TemplateBytes = hex.EncodeToString(sum[:]), len(show.Template)
	id.Parameters, id.Capabilities, id.Details = show.Parameters, show.Capabilities, show.Details
	id.Renderer, id.Parser = show.Renderer, show.Parser
	var ps struct {
		Models []entry `json:"models"`
	}
	if err := call("GET", "/api/ps", nil, &ps); err != nil {
		return id, err
	}
	for _, m := range ps.Models {
		if m.Name == model || m.Model == model {
			id.Loaded = &Loaded{ContextLength: m.ContextLength, Size: m.Size, SizeVRAM: m.SizeVRAM}
		} else if m.Name != "" {
			id.OtherModels = append(id.OtherModels, m.Name)
		}
	}
	return id, nil
}
