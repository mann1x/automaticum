package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/run"
)

// TestWatchGuard: a run whose reply says one paragraph again and again is
// cancelled as stopped by the repetition guard; a healthy one is not (D135).
func TestWatchGuard(t *testing.T) {
	g := newRig(t, false)
	guardWatchEvery = 10 * time.Millisecond
	t.Cleanup(func() { guardWatchEvery = 10 * time.Second })
	para := `Now let me fix each syntax error in the file properly, one at a time:\n\n`
	loop := [][]byte{[]byte(agentEventLine(0, `{"type":"iteration_start","iteration":7}`))}
	for range 200 {
		loop = append(loop, []byte(agentEventLine(1, `{"type":"content_start","contentType":"text","text":"`+para+`"}`)))
	}
	healthy := [][]byte{[]byte(agentEventLine(0, `{"type":"iteration_start","iteration":1}`)),
		[]byte(agentEventLine(1, `{"type":"content_end","contentType":"text","text":"`+strings.Repeat("a distinct sentence. ", 3)+`"}`))}
	for _, c := range []struct {
		name  string
		lines [][]byte
		stop  bool
	}{{"loop", loop, true}, {"healthy", healthy, false}} {
		ctx, cancel := context.WithCancelCause(context.Background())
		f := &flight{runID: "r-" + c.name, cancel: cancel, lines: c.lines}
		done := make(chan struct{})
		go func() { g.d.watchGuard(ctx, f); close(done) }()
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
		var gs *run.GuardStop
		stopped := errors.As(context.Cause(ctx), &gs)
		if stopped != c.stop || stopped && (gs.Guard != "repetition_guard" || !strings.Contains(gs.Reason, "turn 7, text, paragraph")) {
			t.Errorf("%s: stopped %v, cause %v", c.name, stopped, context.Cause(ctx))
		}
		cancel(nil)
		<-done
	}
}
