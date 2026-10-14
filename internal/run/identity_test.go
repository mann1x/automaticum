package run

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// The stream reader keeps the intervals a run was held, counts a held run
// as not idle, and gives the time held for the outer limit to move by.
func TestStreamHolds(t *testing.T) {
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	line := func(at time.Time, note string) string {
		return `{"t":"` + at.Format(time.RFC3339Nano) + `","k":"runner","note":"` + note + `"}` + "\n"
	}
	s := newStream(t0)
	s.Write([]byte(line(t0.Add(time.Minute), "prepared")))
	s.Write([]byte(line(t0.Add(2*time.Minute), "held")))
	s.Write([]byte(line(t0.Add(3*time.Minute), "holding")))
	if total, holding := s.heldFor(t0.Add(10 * time.Minute)); !holding || total != 8*time.Minute {
		t.Errorf("in a hold: %v %v", total, holding)
	}
	if s.idle(time.Now().Add(time.Hour)) != 0 {
		t.Error("a held run counts as idle")
	}
	s.Write([]byte(line(t0.Add(12*time.Minute), "released")))
	s.Write([]byte(line(t0.Add(20*time.Minute), "held")))
	s.Write([]byte(line(t0.Add(21*time.Minute), "released")))
	total, holding := s.heldFor(t0.Add(time.Hour))
	if holding || total != 11*time.Minute || len(s.heldIntervals()) != 2 {
		t.Errorf("after two holds: %v %v %v", total, holding, s.heldIntervals())
	}
	if s.idle(time.Now().Add(time.Hour)) == 0 {
		t.Error("a released run is never idle")
	}
}

// Hold and Release are a file next to the job; a run without a job cannot
// be held.
func TestHoldFile(t *testing.T) {
	x := &Executor{RunsDir: t.TempDir()}
	if err := x.Hold("r1"); err == nil {
		t.Error("a run without a job was held")
	}
	dir := filepath.Join(x.RunsDir, "r1", "job")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, jobFile), []byte("{}"), 0o644)
	if err := x.Hold("r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, HoldFile)); err != nil {
		t.Error("no hold file")
	}
	if err := x.Release("r1"); err != nil {
		t.Fatal(err)
	}
	if err := x.Release("r1"); err != nil {
		t.Errorf("releasing a run that is not held: %v", err)
	}
}

func TestControlTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/api/show" || in["verbose"] != true {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"model_info":{"tokenizer.ggml.tokens":["a","<|im_start|>","<think>","<|im_end|>"],"tokenizer.ggml.token_type":[1,3,4,3]}}`))
	}))
	defer srv.Close()
	got, err := ControlTokens(context.Background(), srv.Client(), &config.Endpoint{Name: "e", Kind: "ollama", BaseURL: srv.URL}, "", "m")
	if err != nil || strings.Join(got, " ") != "<|im_start|> <|im_end|>" {
		t.Errorf("%v %v", got, err)
	}
}

// A server that lists the same parameters in another order has not changed
// them; another value has.
func TestParametersKey(t *testing.T) {
	a := "num_ctx                        131072\ntemperature                    1\nstop                           \"<|im_end|>\""
	b := "temperature  1\nstop \"<|im_end|>\"\nnum_ctx 131072\n"
	if ParametersKey(a) != ParametersKey(b) {
		t.Errorf("%q != %q", ParametersKey(a), ParametersKey(b))
	}
	if got := (Identity{Parameters: a}).Changed(Identity{Parameters: b}); len(got) != 0 {
		t.Errorf("a reordering is a change: %v", got)
	}
	if got := (Identity{Parameters: a}).Changed(Identity{Parameters: strings.Replace(b, "1\n", "0.8\n", 1)}); len(got) != 1 {
		t.Errorf("another value is not a change: %v", got)
	}
}
