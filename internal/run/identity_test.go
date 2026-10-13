package run

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
)

// What the catalogue leaves empty is read from the weights file; what it
// states is kept; the variant is never guessed (D80).
func TestFillIdentity(t *testing.T) {
	weights := map[string]any{"general.base_model.0.name": "Qwen3.6 27B", "general.size_label": "27B", "general.basename": "ignored"}
	got := FillIdentity(&config.ModelIdentity{Tag: "t", Variant: "OmniMerge v4"}, "t", weights)
	if got.Family != "Qwen3.6" || got.Size != "27B" || got.Variant != "OmniMerge v4" || strings.Join(got.Queried, ",") != "family,size" || len(got.Missing()) != 0 {
		t.Errorf("an entry with only the variant: %+v", got)
	}
	stated := &config.ModelIdentity{Tag: "t", Family: "Qwen 3.6", Size: "27B", Variant: "base"}
	if got := FillIdentity(stated, "t", weights); got.Family != "Qwen 3.6" || len(got.Queried) != 0 {
		t.Errorf("a whole entry was changed: %+v", got)
	}
	got = FillIdentity(nil, "t", map[string]any{"general.basename": "Qwen3.6-27B", "general.size_label": "27B"})
	if got == nil || got.Family != "Qwen3.6" || strings.Join(got.Missing(), ",") != "variant" {
		t.Errorf("no entry: %+v", got)
	}
	if got := FillIdentity(nil, "t", nil); got != nil {
		t.Errorf("no entry and a file that says nothing: %+v", got)
	}
}

// Unload drops every model but the one the run uses, and waits until the
// server no longer lists them.
func TestUnload(t *testing.T) {
	var mu sync.Mutex
	held := []string{"other:1", "mine:1"}
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/ps":
			type m struct {
				Name string `json:"name"`
			}
			out := struct {
				Models []m `json:"models"`
			}{}
			for _, h := range held {
				out.Models = append(out.Models, m{h})
			}
			json.NewEncoder(w).Encode(out)
		case "/api/generate":
			var in struct {
				Model     string `json:"model"`
				KeepAlive *int   `json:"keep_alive"`
				Prompt    string `json:"prompt"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.KeepAlive == nil || *in.KeepAlive != 0 || in.Prompt != "" {
				t.Errorf("not an unload: %+v", in)
			}
			asked = append(asked, in.Model)
			for i, h := range held {
				if h == in.Model {
					held = append(held[:i], held[i+1:]...)
					break
				}
			}
			w.Write([]byte(`{"done":true,"done_reason":"unload"}`))
		}
	}))
	defer srv.Close()
	e := &config.Endpoint{Name: "e", Kind: "xollama", BaseURL: srv.URL}
	got, err := Unload(context.Background(), srv.Client(), e, "", "mine:1")
	if err != nil || strings.Join(got, ",") != "other:1" || strings.Join(asked, ",") != "other:1" || strings.Join(held, ",") != "mine:1" {
		t.Errorf("unloaded %v, asked %v, still held %v, %v", got, asked, held, err)
	}
	// Nothing else held: nothing is asked.
	if got, err := Unload(context.Background(), srv.Client(), e, "", "mine:1"); err != nil || got != nil || len(asked) != 1 {
		t.Errorf("a second time: %v %v, asked %v", got, err, asked)
	}
}
