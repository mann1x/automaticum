package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/store/blob"
)

// fakeArchiver answers each pass from a list, and keeps the ages asked.
type fakeArchiver struct {
	mu      sync.Mutex
	ages    []time.Duration
	answers []func() (blob.ArchiveReport, error)
}

func (f *fakeArchiver) ArchiveOlderThan(ctx context.Context, age time.Duration, now time.Time) (blob.ArchiveReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ages = append(f.ages, age)
	if len(f.answers) == 0 {
		return blob.ArchiveReport{}, nil
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a()
}

func (f *fakeArchiver) passes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.ages)
}

// Blobs older than archive_after_days move on start and on every tick
// (080 §5, D31); an unreachable archive and a blob that did not move are
// notified; an installation without the setting archives nothing.
func TestArchiveLoop(t *testing.T) {
	g := newRig(t, false)
	fa := &fakeArchiver{answers: []func() (blob.ArchiveReport, error){
		func() (blob.ArchiveReport, error) { return blob.ArchiveReport{}, blob.ErrArchiveUnavailable },
		func() (blob.ArchiveReport, error) {
			return blob.ArchiveReport{Scanned: 3, Eligible: 2, Moved: 1, Failed: []blob.Failure{{Hash: "h1", Err: errors.New("disk full")}}}, nil
		},
	}}
	g.d.Archiver, g.d.ArchiveEvery = fa, 20*time.Millisecond
	g.d.Local.Daemon.ArchiveAfterDays = 30
	var logs lockedBuffer
	g.d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	deadline := time.Now().Add(5 * time.Second)
	for fa.passes() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fa.passes() < 3 || fa.ages[0] != 30*24*time.Hour {
		t.Fatalf("passes %d, ages %v", fa.passes(), fa.ages)
	}
	out := logs.String()
	for _, want := range []string{`msg="archive unavailable"`, `msg="archive failed"`, `1 of 2 blobs older than 30 days were not moved`, `msg=archive scanned=3 older=2 moved=1`} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}

	off := newRig(t, false)
	none := &fakeArchiver{}
	off.d.Archiver, off.d.ArchiveEvery = none, time.Millisecond
	go func() { off.d.Serve(off.ctx()); close(off.served) }()
	time.Sleep(30 * time.Millisecond)
	if none.passes() != 0 {
		t.Errorf("archived without archive_after_days: %d passes", none.passes())
	}
}
