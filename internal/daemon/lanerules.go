package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/human"

	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// InfraEnd is one run of a lane's streak of infrastructure ends.
type InfraEnd struct {
	Run    string `json:"run"`
	Reason string `json:"reason"`
}

// countInfra keeps, per lane, the runs in a row that ended ABORTED for the
// harness's or the server's reason (run.Infrastructure). When a lane's
// streak reaches its number (config.Local.QuarantineAfter, 10 unless set,
// 0 never), the daemon quarantines the lane, says why with each run and its
// reason, and notifies; only `lane resume` lifts it (D124). Any other end
// resets the streak.
func (d *Daemon) countInfra(ctx context.Context, rec *store.Run) {
	lane := rec.Lane
	if lane == "" {
		return
	}
	after := d.Local.QuarantineAfter(lane)
	d.mu.Lock()
	if !run.Infrastructure(rec.Verdict, rec.VerdictReason) {
		changed := len(d.ctl.InfraStreak[lane]) > 0
		delete(d.ctl.InfraStreak, lane)
		if changed {
			d.saveControl()
		}
		d.mu.Unlock()
		return
	}
	streak := append(d.ctl.InfraStreak[lane], InfraEnd{Run: rec.ID, Reason: rec.VerdictReason})
	d.ctl.InfraStreak[lane] = streak
	_, already := d.ctl.Quarantined[lane]
	var reason string
	if after > 0 && len(streak) >= after && !already {
		reason = fmt.Sprintf("the daemon: %d runs in a row ended for the harness's or the server's reason, the last: %s", len(streak), rec.VerdictReason)
		d.ctl.Quarantined[lane] = Quarantine{At: time.Now().UTC(), Reason: reason}
		if _, ok := d.ctl.Drained[lane]; !ok {
			d.ctl.Drained[lane] = time.Now().UTC()
		}
	}
	if err := d.saveControl(); err != nil {
		d.Log.Error("the control file", "error", err)
	}
	d.mu.Unlock()
	if reason == "" {
		return
	}
	var lines []string
	for _, e := range streak {
		lines = append(lines, e.Run+": "+e.Reason)
	}
	d.notify(ctx, Notification{Kind: "lane", Status: "quarantined", Lane: lane, Run: rec.ID, Batch: rec.Batch,
		Detail: reason + "\n" + strings.Join(lines, "\n") + "\n`automaticum lane resume " + lane + "` lifts it"})
	d.bump()
}

// laneBusy runs the lane's busy command (D123), if it has one: exit 0, no
// answer within its time limit (10 s unless set), or a command that cannot
// be run means busy, with the command's output or what went wrong as the
// reason; any other exit means free.
func (d *Daemon) laneBusy(ctx context.Context, lane string) (bool, string) {
	ln := d.Local.Lanes[lane]
	if ln == nil || len(ln.BusyCommand) == 0 {
		return false, ""
	}
	limit := time.Duration(ln.BusyTimeoutS) * time.Second
	if limit == 0 {
		limit = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	out, err := exec.CommandContext(cctx, ln.BusyCommand[0], ln.BusyCommand[1:]...).CombinedOutput()
	why := strings.TrimSpace(string(out))
	if len(why) > 500 {
		why = why[:500] + " …"
	}
	switch {
	case cctx.Err() != nil && ctx.Err() == nil:
		return true, fmt.Sprintf("the busy command did not answer within %s", human.Duration(limit))
	case err == nil:
		if why == "" {
			why = "the busy command says the lane is in use"
		}
		return true, why
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		// A command that cannot be run cannot say the lane is free: the
		// run waits, and the reason says why, rather than start beside
		// someone else's work.
		return true, "the busy command cannot be run: " + err.Error()
	}
	return false, ""
}

// ReserveRequest reserves a lane (D125): For is a batch or a campaign,
// Until when the reservation ends (RFC 3339).
type ReserveRequest struct {
	For   string `json:"for"`
	Until string `json:"until"`
}

// handleReserve reserves a lane for one batch or campaign until a time,
// or ends its reservation (POST /v1/lanes/{lane}/reserve|unreserve). Only
// that work starts on the lane while it lasts; a run in flight finishes.
func (d *Daemon) handleReserve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("lane")
	if _, ok := d.Local.Lanes[name]; !ok {
		fail(w, http.StatusNotFound, "there is no lane %s", name)
		return
	}
	out := map[string]any{"lane": name, "dry_run": dryRun(r)}
	if strings.HasSuffix(r.URL.Path, "/unreserve") {
		d.mu.Lock()
		old, had := d.ctl.Reserved[name]
		d.mu.Unlock()
		if !had {
			fail(w, http.StatusNotFound, "lane %s is not reserved", name)
			return
		}
		out["state"], out["was_for"] = "unreserved", old.For
		if !dryRun(r) {
			d.mu.Lock()
			delete(d.ctl.Reserved, name)
			err := d.saveControl()
			d.mu.Unlock()
			if err != nil {
				fail(w, http.StatusInternalServerError, "the control file: %v", err)
				return
			}
			d.poke()
		}
		reply(w, http.StatusOK, out)
		return
	}
	var req ReserveRequest
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	until, err := time.Parse(time.RFC3339, req.Until)
	switch {
	case strings.TrimSpace(req.For) == "":
		fail(w, http.StatusBadRequest, "name the batch or campaign the lane is for")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, "until %q: a time such as 2026-11-02T08:00:00+01:00", req.Until)
		return
	case !until.After(time.Now()):
		fail(w, http.StatusBadRequest, "until %s is not in the future", req.Until)
		return
	}
	res := Reservation{For: req.For, Until: until.UTC(), At: time.Now().UTC()}
	out["state"], out["for"], out["until"] = "reserved", res.For, res.Until
	if !dryRun(r) {
		d.mu.Lock()
		d.ctl.Reserved[name] = res
		err := d.saveControl()
		d.mu.Unlock()
		if err != nil {
			fail(w, http.StatusInternalServerError, "the control file: %v", err)
			return
		}
		d.Log.Info("lane reserved", "lane", name, "for", res.For, "until", res.Until)
		d.poke()
	}
	reply(w, http.StatusOK, out)
}
