package run

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/stubmodel"
)

// /props is read at the server's root; a server without it (vLLM) leaves
// the facts unchecked, and every contradiction is listed.
func TestProps(t *testing.T) {
	stub := httptest.NewServer(stubmodel.New(stubmodel.Script{ContextWindow: 8192, Slots: 4}))
	defer stub.Close()
	p, err := ReadProps(context.Background(), http.DefaultClient, &config.Endpoint{Name: "c", Kind: "llamacpp", BaseURL: stub.URL + "/v1"}, "")
	if err != nil || p == nil || p.NCtx != 8192 || p.TotalSlots != 4 || p.ChatTemplateBytes == 0 {
		t.Fatalf("%v %+v", err, p)
	}
	vllm := httptest.NewServer(http.NotFoundHandler())
	defer vllm.Close()
	if q, err := ReadProps(context.Background(), http.DefaultClient, &config.Endpoint{Name: "v", Kind: "openai-compatible", BaseURL: vllm.URL + "/v1"}, ""); q != nil || err != nil {
		t.Errorf("no /props: %v %+v", err, q)
	}
	if q, err := ReadProps(context.Background(), http.DefaultClient, &config.Endpoint{Name: "o", Kind: "ollama", BaseURL: vllm.URL}, ""); q != nil || err != nil {
		t.Errorf("ollama: %v %+v", err, q)
	}

	if bad := Contradictions(p, &config.BasisFacts{NParallel: 4, CtxPerSlot: 8192, ChatTemplateSHA256: strings.ToUpper(p.ChatTemplateSHA256)}, 8192); len(bad) != 0 {
		t.Errorf("agreeing facts: %v", bad)
	}
	p.BuildInfo = "b7000-ebe30d93f"
	bad := Contradictions(p, &config.BasisFacts{NParallel: 2, CtxPerSlot: 16384, ChatTemplateSHA256: "00", LlamaCppCommit: "abc1234"}, 16384)
	if len(bad) != 5 {
		t.Errorf("contradictions: %q", bad)
	}
	if bad := Contradictions(p, &config.BasisFacts{LlamaCppCommit: "ebe30d93f0123456789"}, 0); len(bad) != 0 {
		t.Errorf("a long commit of the same build: %v", bad)
	}
	if Contradictions(nil, &config.BasisFacts{NParallel: 9}, 0) != nil {
		t.Error("no /props, yet a contradiction")
	}
}
