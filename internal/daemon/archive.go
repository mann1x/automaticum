package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// Archiver is the store's archive tier: blob.Store.ArchiveOlderThan.
type Archiver interface {
	ArchiveOlderThan(ctx context.Context, age time.Duration, now time.Time) (blob.ArchiveReport, error)
}

// archiveEvery is how often old blobs are looked for unless set.
const archiveEvery = 24 * time.Hour

// archiveLoop moves the raw blobs older than archive_after_days to the
// archive tier when the daemon starts and once a day after (080 §5, D31).
// Each blob is copied, verified at the archive and only then freed; an
// archive that cannot be reached moves and frees nothing and is notified.
// An installation without archive_after_days archives nothing.
func (d *Daemon) archiveLoop(ctx context.Context) {
	days := d.Local.Daemon.ArchiveAfterDays
	if d.Archiver == nil || days <= 0 {
		return
	}
	every := d.ArchiveEvery
	if every == 0 {
		every = archiveEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		d.archivePass(ctx, time.Duration(days)*24*time.Hour)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// archivePass is one pass: logged, and notified when it moved nothing
// because the archive was unreachable or when blobs failed to move.
func (d *Daemon) archivePass(ctx context.Context, age time.Duration) {
	start := time.Now()
	rep, err := d.Archiver.ArchiveOlderThan(ctx, age, start)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		d.notify(ctx, Notification{Kind: "archive", Status: "unavailable", Detail: err.Error()})
		return
	}
	d.Log.Info("archive", "scanned", rep.Scanned, "older", rep.Eligible, "moved", rep.Moved, "already_archived", rep.AlreadyArchived,
		"freed", human.Bytes(rep.BytesFreed), "failed", len(rep.Failed), "took", human.Millis(float64(time.Since(start).Milliseconds())))
	if n := len(rep.Failed); n > 0 {
		f := rep.Failed[0]
		d.notify(ctx, Notification{Kind: "archive", Status: "failed",
			Detail: fmt.Sprintf("%d of %d blobs older than %d days were not moved and stay on the live tier; the first, %s: %v", n, rep.Eligible, int(age.Hours()/24), f.Hash, f.Err)})
	}
}
