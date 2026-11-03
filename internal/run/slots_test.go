package run

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/config"
)

func TestReadSlots(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slots" {
			http.NotFound(w, r)
			return
		}
		k := n.Add(1)
		switch {
		case k <= 3:
			// The server a client runs: n_decoded in a list under next_token.
			w.Write([]byte(`[{"id":0,"id_task":308924,"is_processing":true,"next_token":[{"has_next_token":true,"n_decoded":` + string(rune('0'+k)) + `000}]}]`))
		default:
			// An older build: next_token an object; the slot is done.
			w.Write([]byte(`[{"id":0,"id_task":308924,"is_processing":false,"next_token":{"n_decoded":0}}]`))
		}
	}))
	defer srv.Close()
	e := &config.Endpoint{Name: "e", Kind: "llamacpp", BaseURL: srv.URL + "/v1"}
	ss, err := ReadSlots(context.Background(), srv.Client(), e, "")
	if err != nil || len(ss) != 1 || ss[0].Decoded != 1000 || ss[0].Task != 308924 || !ss[0].Processing {
		t.Fatalf("ReadSlots: %+v, %v", ss, err)
	}

	x := &Executor{HTTP: srv.Client(), SlotsTick: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	done := make(chan []SlotSample, 1)
	x.watchSlots(ctx, e, "", done)
	got := <-done
	// Readings 2 and 3 processing, then the one that stopped; none after.
	if len(got) != 3 || got[1].Decoded != 3000 || got[2].Processing {
		t.Fatalf("watchSlots kept %+v", got)
	}

	// A server without /slots: nothing is read again.
	none := httptest.NewServer(http.NotFoundHandler())
	defer none.Close()
	done = make(chan []SlotSample, 1)
	x.watchSlots(context.Background(), &config.Endpoint{Name: "n", Kind: "llamacpp", BaseURL: none.URL + "/v1"}, "", done)
	if got := <-done; got != nil {
		t.Fatalf("a server without /slots gave %+v", got)
	}
	// Ollama has no /slots and is not asked.
	if ss, err := ReadSlots(context.Background(), srv.Client(), &config.Endpoint{Kind: "ollama", BaseURL: srv.URL}, ""); ss != nil || err != nil {
		t.Fatalf("ollama: %+v, %v", ss, err)
	}
}
