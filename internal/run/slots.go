package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
)

// SlotSample is one slot of llama-server's GET /slots, read while a run
// was in flight: which task the slot served and how many tokens it had
// decoded for it. A server that holds back what the model writes (its
// parser holding a malformed tool call) sends nothing on the wire while the
// count still grows, and this is the only record of it (held_stream).
type SlotSample struct {
	At         time.Time `json:"at"`
	Slot       int       `json:"slot"`
	Task       int64     `json:"task"`
	Processing bool      `json:"processing"`
	Decoded    int       `json:"n_decoded"`
}

// ReadSlots reads llama-server's /slots, at the server's root, not under
// /v1. It returns nil and no error for a server without it (Ollama, vLLM,
// a llama-server started without --slots).
func ReadSlots(ctx context.Context, hc *http.Client, e *config.Endpoint, apiKey string) ([]SlotSample, error) {
	if Ollama(e) {
		return nil, nil
	}
	base := strings.TrimSuffix(strings.TrimRight(e.BaseURL, "/"), "/v1")
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/slots", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s does not answer /slots", e.Name)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusNotImplemented:
		return nil, nil
	default:
		return nil, fmt.Errorf("endpoint %s: /slots gave status %d", e.Name, resp.StatusCode)
	}
	var raw []struct {
		ID         int             `json:"id"`
		Task       int64           `json:"id_task"`
		Processing bool            `json:"is_processing"`
		Decoded    *int            `json:"n_decoded"`
		Next       json.RawMessage `json:"next_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("endpoint %s: /slots is not understood: %v", e.Name, err)
	}
	now := time.Now().UTC()
	out := make([]SlotSample, 0, len(raw))
	for _, s := range raw {
		x := SlotSample{At: now, Slot: s.ID, Task: s.Task, Processing: s.Processing}
		if s.Decoded != nil {
			x.Decoded = *s.Decoded
		} else {
			x.Decoded = nextDecoded(s.Next)
		}
		out = append(out, x)
	}
	return out, nil
}

// nextDecoded is n_decoded where llama-server keeps it: in next_token, an
// object in older builds and a list of one per sequence in newer ones.
func nextDecoded(raw json.RawMessage) int {
	type next struct {
		Decoded int `json:"n_decoded"`
	}
	var list []next
	if json.Unmarshal(raw, &list) == nil {
		n := 0
		for _, x := range list {
			n = max(n, x.Decoded)
		}
		return n
	}
	var one next
	if json.Unmarshal(raw, &one) == nil {
		return one.Decoded
	}
	return 0
}

// slotsEvery is how often /slots is read during a run, the SDK's own
// keepalive interval (sse_ping_interval 10 s).
const slotsEvery = 10 * time.Second

// watchSlots reads the endpoint's /slots until the run ends and keeps each
// slot that is processing, and each that stopped since the last reading.
// It stops at once for a server without /slots; a reading that fails is
// skipped.
func (x *Executor) watchSlots(ctx context.Context, e *config.Endpoint, apiKey string, done chan<- []SlotSample) {
	tick := x.SlotsTick
	if tick == 0 {
		tick = slotsEvery
	}
	var out []SlotSample
	defer func() { done <- out }()
	busy := map[int]bool{}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		rctx, cancel := context.WithTimeout(ctx, tick)
		ss, err := ReadSlots(rctx, x.http(), e, apiKey)
		cancel()
		if err == nil && ss == nil {
			return
		}
		for _, s := range ss {
			if s.Processing || busy[s.Slot] {
				out = append(out, s)
			}
			busy[s.Slot] = s.Processing
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
