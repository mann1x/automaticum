package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mann1x/automaticum/internal/config"
)

// Props is what llama-server says of itself on GET /props: the context of
// one slot, how many slots it has, the weights file it loaded, the chat
// template it serves and its build.
type Props struct {
	NCtx               int    `json:"n_ctx"`
	TotalSlots         int    `json:"total_slots"`
	ModelPath          string `json:"model_path"`
	ChatTemplateSHA256 string `json:"chat_template_sha256"`
	ChatTemplateBytes  int    `json:"chat_template_bytes"`
	BuildInfo          string `json:"build_info,omitempty"`
}

// ReadProps asks an OpenAI-compatible endpoint for llama-server's /props,
// which is at the server's root, not under /v1. It returns nil and no error
// when the server has no /props (vLLM): then the caller's facts cannot be
// checked and are recorded as the caller's word.
func ReadProps(ctx context.Context, hc *http.Client, e *config.Endpoint, apiKey string) (*Props, error) {
	if Ollama(e) {
		return nil, nil
	}
	base := strings.TrimSuffix(strings.TrimRight(e.BaseURL, "/"), "/v1")
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/props", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s does not answer /props", e.Name)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("endpoint %s: /props gave status %d", e.Name, resp.StatusCode)
	}
	var raw struct {
		Settings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		TotalSlots   int    `json:"total_slots"`
		ModelPath    string `json:"model_path"`
		ChatTemplate string `json:"chat_template"`
		BuildInfo    string `json:"build_info"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("endpoint %s: /props is not understood: %v", e.Name, err)
	}
	sum := sha256.Sum256([]byte(raw.ChatTemplate))
	return &Props{NCtx: raw.Settings.NCtx, TotalSlots: raw.TotalSlots, ModelPath: raw.ModelPath,
		ChatTemplateSHA256: hex.EncodeToString(sum[:]), ChatTemplateBytes: len(raw.ChatTemplate), BuildInfo: raw.BuildInfo}, nil
}

// Contradictions lists every way the server's /props contradicts the
// caller's basis facts, or the context window the arm asks for. A fact the
// caller left out, or one /props does not report, is not compared.
func Contradictions(p *Props, f *config.BasisFacts, contextWindow int) []string {
	if p == nil {
		return nil
	}
	var out []string
	if f != nil {
		if f.NParallel > 0 && p.TotalSlots > 0 && f.NParallel != p.TotalSlots {
			slots := fmt.Sprintf("%d slots", p.TotalSlots)
			if p.TotalSlots == 1 {
				slots = "1 slot"
			}
			out = append(out, fmt.Sprintf("n_parallel is %d, the server has %s", f.NParallel, slots))
		}
		if f.CtxPerSlot > 0 && p.NCtx > 0 && f.CtxPerSlot != p.NCtx {
			out = append(out, fmt.Sprintf("ctx_per_slot is %d, a slot of the server holds %d", f.CtxPerSlot, p.NCtx))
		}
		if f.ChatTemplateSHA256 != "" && p.ChatTemplateBytes > 0 && !strings.EqualFold(f.ChatTemplateSHA256, p.ChatTemplateSHA256) {
			out = append(out, fmt.Sprintf("chat_template_sha256 is %s, the server serves a template with sha256 %s", f.ChatTemplateSHA256, p.ChatTemplateSHA256))
		}
		// build_info is "b<number>-<commit>"; either commit may be short.
		if _, commit, ok := strings.Cut(p.BuildInfo, "-"); ok && f.LlamaCppCommit != "" && commit != "" {
			a, b := strings.ToLower(f.LlamaCppCommit), strings.ToLower(commit)
			if !strings.HasPrefix(a, b) && !strings.HasPrefix(b, a) {
				out = append(out, fmt.Sprintf("llamacpp_commit is %s, the server was built from %s", f.LlamaCppCommit, commit))
			}
		}
	}
	if contextWindow > 0 && p.NCtx > 0 && contextWindow > p.NCtx {
		out = append(out, fmt.Sprintf("the arm's context window is %d tokens, a slot of the server holds %d", contextWindow, p.NCtx))
	}
	return out
}
