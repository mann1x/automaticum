package daemon

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/run"
)

// guardWatchEvery is how often a run whose arm stops on a guard of the
// harness is read again: how late after the trip the run can stop.
var guardWatchEvery = 10 * time.Second

// stopsOnHarness says whether an arm's guards.stop_on names a guard of the
// harness (D135).
func stopsOnHarness(a *config.Arm, guard string) bool {
	return a != nil && a.Guards.StopOn != nil && slices.Contains(*a.Guards.StopOn, guard)
}

// watchGuard stops a run at the repetition guard's first trip, for an arm
// whose stop_on names it (D135): what the runner printed so far is read as
// detect reads a run in flight, and the run is cancelled as stopped by a
// guard, a failed run whose oracle still reads what it left (D99). Every
// other arm only observes: the guard is a finding of the analysis.
func (d *Daemon) watchGuard(ctx context.Context, f *flight) {
	t := time.NewTicker(guardWatchEvery)
	defer t.Stop()
	read := -1
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		f.mu.Lock()
		lines, n := slices.Clone(f.lines), f.first+len(f.lines)
		f.mu.Unlock()
		if n == read {
			continue
		}
		read = n
		ev := &ingest.Events{}
		if ev.Read(bytes.NewReader(bytes.Join(lines, []byte("\n")))) != nil {
			continue
		}
		if fs := ingest.GuardFires(ev, nil); len(fs) > 0 {
			g := fs[0]
			why := fmt.Sprintf("turn %d, %s, %s at character %d (%dx): %q", g.Turn, g.Channel, g.Rule, g.Offset, g.Count, g.Unit)
			d.Log.Info("the repetition guard stops the run", "run", f.runID, "lane", f.item.Lane, "at", why)
			f.cancel(&run.GuardStop{Guard: detect.RepetitionGuard, Reason: why})
			return
		}
	}
}
