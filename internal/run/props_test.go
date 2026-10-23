package run

import (
	"context"
	"encoding/json"
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

// A llama-server endpoint's identity comes from /props: the build, the
// template's hash, whether it can call tools, the default sampler and
// window, and the weights file; a server without /props stays not probed.
func TestProbeLlamaServer(t *testing.T) {
	props := `{"default_generation_settings":{"n_ctx":256000,"params":{"temperature":0.8,"top_k":40,"dry_multiplier":0.0}},
		"total_slots":1,"model_path":"/m/g4-eos106.gguf","model_alias":"g4","chat_template":"{{ messages }}",
		"chat_template_caps":{"supports_tools":true},"eos_token":"<turn|>","build_info":"b228-x"}`
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"data":[{"id":"g4"}]}`)) })
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(props)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e := &config.Endpoint{Name: "g", Kind: "llamacpp", BaseURL: srv.URL + "/v1"}
	id, err := Probe(context.Background(), http.DefaultClient, e, "", "g4")
	if err != nil {
		t.Fatal(err)
	}
	if id.ServerVersion != "b228-x" || id.TemplateBytes != 14 || id.TemplateSHA256 == "" {
		t.Errorf("version %q, template %d %q", id.ServerVersion, id.TemplateBytes, id.TemplateSHA256)
	}
	if strings.Join(id.Capabilities, ",") != "completion,tools" {
		t.Errorf("capabilities %v", id.Capabilities)
	}
	if id.Parameters != "temperature 0.8\ntop_k 40\nnum_ctx 256000" {
		t.Errorf("parameters %q", id.Parameters)
	}
	var d Props
	if json.Unmarshal(id.Details, &d) != nil || d.ModelPath != "/m/g4-eos106.gguf" || d.EOSToken != "<turn|>" || !d.Caps["supports_tools"] {
		t.Errorf("details %s", id.Details)
	}
	if strings.Join(id.NotProbed, ",") != "model digest,residency" {
		t.Errorf("not probed %v", id.NotProbed)
	}
	// Another template after the run makes it uncountable.
	other := id
	other.TemplateSHA256 = "x"
	if len(id.Changed(other)) != 1 {
		t.Error("a changed template is not seen")
	}

	// Without /props (vLLM) the model is still found, and the rest is not
	// probed.
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"data":[{"id":"g4"}]}`)) })
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()
	id2, err := Probe(context.Background(), http.DefaultClient, &config.Endpoint{Name: "v", Kind: "llamacpp", BaseURL: srv2.URL + "/v1"}, "", "g4")
	if err != nil || len(id2.NotProbed) != 5 {
		t.Errorf("without /props: %v, not probed %v", err, id2.NotProbed)
	}

	// A server that does not say what its template can do is not refused
	// for it: that is left unchecked.
	props = strings.Replace(props, `"chat_template_caps":{"supports_tools":true},`, "", 1)
	id3, err := Probe(context.Background(), http.DefaultClient, e, "", "g4")
	if err != nil || len(id3.Capabilities) != 0 || strings.Join(id3.NotProbed, ",") != "model digest,residency,template capabilities" {
		t.Errorf("without caps: %v, capabilities %v, not probed %v", err, id3.Capabilities, id3.NotProbed)
	}
	if _, err := Probe(context.Background(), http.DefaultClient, e, "", "other"); err == nil {
		t.Error("a model the server does not list was accepted")
	}
}
