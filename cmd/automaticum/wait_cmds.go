package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/wait"
)

// The exit codes of `wait` (090 §5): the condition holds, the time given
// ran out, or what is followed ended without it, so it never will.
const (
	waitMet     = 0
	waitTimeout = 4
	waitOver    = 5
)

const waitUsage = `usage: automaticum wait <kind> <id> [--until "<condition>"] [--timeout D] [--json]

  wait run <run> [<run>...] [--any]   until the run has finished (every one named; --any: the first)
  wait batch <batch>                  until every run of the batch has finished
  wait batch <batch> --next [--seen N]  until the next run of the batch finishes; prints it and the --seen for the next wait
  wait batch <batch> --each [--seen N]  one line per run as it finishes, until the batch has finished
  wait cell <batch>                   until the cell has finished, then its counts
  wait campaign <name>                until the campaign is finished or stopped
  wait lane <lane>                    until the lane has no run in flight and none queued
  wait idle                           until no lane has a run in flight and nothing is queued

  --until "<condition>"  counts compared with a number and facts, joined by and/or:
                         run, batch, cell: pass fail timeout aborted invalid skipped finished queued running total; started done
                         campaign: batches batches_finished batches_stopped batches_running batches_waiting; running paused stopped finished done
                         lane: running queued; idle free drained paused     idle: running queued; idle free
                         e.g. --until "pass>=5 or fail>=3"
  --timeout D            give up after D (e.g. 6h); 0 waits as long as it takes

Exit codes: 0 the condition holds; 4 the timeout ran out; 5 what is followed ended
without the condition, so it never will; 1 an error. A daemon that restarts
during the wait is waited for.`

// waitCmd is `automaticum wait <kind> ...`.
func waitCmd(kind string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("wait "+kind, flag.ExitOnError)
		fs.Usage = func() { fmt.Fprintln(os.Stderr, waitUsage) }
		_, dial := addClient(fs)
		until := fs.String("until", "", "the condition; the kind's own when not given (run, batch, cell, campaign: done; lane, idle: idle)")
		timeout := fs.Duration("timeout", 0, "give up after this long; 0 waits as long as it takes")
		next := fs.Bool("next", false, "batch: until the next run finishes")
		each := fs.Bool("each", false, "batch: one line per run as it finishes, until the batch has finished")
		seen := fs.Int("seen", -1, "batch, with --next or --each: the runs already seen finished, as the last wait printed it")
		anyRun := fs.Bool("any", false, "run: until the first of the runs named has finished")
		poll := fs.Duration("poll", 5*time.Second, "how often the state is read when the daemon does not wait itself")
		rest := parse(fs, args)
		switch {
		case kind == wait.KindIdle && len(rest) != 0,
			kind == wait.KindRun && len(rest) == 0,
			kind != wait.KindIdle && kind != wait.KindRun && len(rest) != 1:
			return errors.New(waitUsage)
		case (*next || *each) && kind != wait.KindBatch:
			return errors.New("--next and --each follow a batch")
		case *next && *each:
			return errors.New("give --next or --each, not both")
		case (*next || *each) && *until != "":
			return errors.New("--next and --each wait for runs to finish; --until is for a wait that ends once")
		case *anyRun && kind != wait.KindRun:
			return errors.New("--any is for runs")
		}
		if _, err := wait.Parse(kind, *until); err != nil {
			return err
		}
		c, err := dial()
		if err != nil {
			return err
		}
		if *timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, *timeout)
			defer cancel()
		}
		wt := &waiter{c: c, poll: *poll}
		id := ""
		if len(rest) > 0 {
			id = rest[0]
		}
		switch {
		case kind == wait.KindRun:
			return wt.runs(ctx, rest, *until, *anyRun)
		case *next:
			return wt.next(ctx, id, *seen)
		case *each:
			return wt.each(ctx, id, *seen)
		}
		v, err := wt.until(ctx, kind, id, *until, -1)
		if err != nil {
			return err
		}
		if c.json {
			if err := printJSON(v); err != nil {
				return err
			}
		} else {
			fmt.Println(summary(v))
			if kind == wait.KindCell && v.Facts["done"] {
				wt.cellLine(ctx, id)
			}
		}
		return ended(v)
	}
}

// ended is the exit of a wait that has an answer.
func ended(v daemon.WaitView) error {
	if v.Met {
		return nil
	}
	if v.Over {
		return exitCode(waitOver)
	}
	return exitCode(waitTimeout)
}

// waiter asks the daemon to wait, or, when the daemon is older than the
// route, reads its state every poll and decides here.
type waiter struct {
	c        *client
	poll     time.Duration
	fallback bool
	told     bool
}

// until waits for one condition; the view says how it ended. A wait whose
// time runs out ends with the last state read, not an error.
func (w *waiter) until(ctx context.Context, kind, id, until string, seen int) (daemon.WaitView, error) {
	var last daemon.WaitView
	for {
		v, err := w.once(ctx, kind, id, until, seen)
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				if last.Kind == "" {
					last.Kind, last.ID, last.Until = kind, id, until
				}
				return last, nil
			}
			return last, ctx.Err()
		}
		if err != nil {
			return last, err
		}
		last = v
		if v.Met || v.Over {
			return v, nil
		}
	}
}

// errNoRoute says that the daemon has no wait route: it is older.
var errNoRoute = errors.New("the daemon has no wait route")

// once asks once: the daemon holds the request until the condition holds
// or a minute passes. A daemon that does not answer is asked again, so a
// restart does not end the wait.
func (w *waiter) once(ctx context.Context, kind, id, until string, seen int) (daemon.WaitView, error) {
	if !w.fallback {
		v, err := w.ask(ctx, kind, id, until, seen)
		if !errors.Is(err, errNoRoute) {
			return v, err
		}
		w.fallback = true
	}
	return w.read(ctx, kind, id, until, seen)
}

func (w *waiter) ask(ctx context.Context, kind, id, until string, seen int) (daemon.WaitView, error) {
	q := url.Values{"until": {until}, "hold": {"55s"}}
	if id != "" {
		q.Set("id", id)
	}
	if seen >= 0 {
		q.Set("seen", fmt.Sprint(seen))
	}
	var v daemon.WaitView
	for {
		req, err := http.NewRequestWithContext(ctx, "GET", w.c.base+"/v1/wait/"+kind+"?"+q.Encode(), nil)
		if err != nil {
			return v, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return v, ctx.Err()
			}
			w.away(ctx)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			// The daemon went away in the middle of the answer.
			w.away(ctx)
			continue
		}
		var e struct {
			Error string `json:"error"`
		}
		if resp.StatusCode >= 400 {
			if json.Unmarshal(body, &e) != nil || e.Error == "" {
				if resp.StatusCode == http.StatusNotFound {
					return v, errNoRoute
				}
				return v, errors.New(resp.Status)
			}
			return v, errors.New(e.Error)
		}
		return v, json.Unmarshal(body, &v)
	}
}

// away waits a little for a daemon that does not answer, saying so once.
func (w *waiter) away(ctx context.Context) {
	if !w.told {
		fmt.Fprintln(os.Stderr, "automaticum: the daemon does not answer; waiting for it")
		w.told = true
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
}

// read is the wait of a daemon without the route: read the state, decide
// here, and read again after the poll, for up to a minute.
func (w *waiter) read(ctx context.Context, kind, id, until string, seen int) (daemon.WaitView, error) {
	cond, err := wait.Parse(kind, until)
	if err != nil {
		return daemon.WaitView{}, err
	}
	end := time.Now().Add(time.Minute)
	for {
		s, err := w.snapshot(ctx, kind, id)
		if ctx.Err() != nil {
			return daemon.WaitView{}, ctx.Err()
		}
		if err != nil {
			if strings.Contains(err.Error(), "does not answer") {
				w.away(ctx)
				continue
			}
			return daemon.WaitView{}, err
		}
		v := daemon.WaitView{Snapshot: s, Until: cond.String(), Met: cond.Eval(s)}
		v.Over = !v.Met && s.Over()
		if seen >= 0 {
			v.Seen = seen
			if seen < len(s.Finished) {
				v.New = s.Finished[seen:]
			}
		}
		if v.Met || v.Over || time.Now().After(end) {
			return v, nil
		}
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		case <-time.After(w.poll):
		}
	}
}

// snapshot reads the state of what is followed from the daemon's other
// routes.
func (w *waiter) snapshot(ctx context.Context, kind, id string) (wait.Snapshot, error) {
	switch kind {
	case wait.KindRun:
		var v daemon.RunView
		if err := w.c.do(ctx, "GET", "/v1/runs/"+url.PathEscape(id), nil, &v); err != nil {
			return wait.Snapshot{}, err
		}
		return wait.Runs(kind, id, []wait.Run{daemon.WaitRun(v)}), nil
	case wait.KindBatch, wait.KindCell:
		var views []daemon.RunView
		if err := w.c.do(ctx, "GET", "/v1/runs?"+url.Values{"batch": {id}, "limit": {"10000"}}.Encode(), nil, &views); err != nil {
			return wait.Snapshot{}, err
		}
		// An older daemon may list a run both queued and finished while
		// it is being written: it is finished.
		done := map[string]bool{}
		for _, v := range views {
			if v.State == "finished" {
				done[v.ID] = true
			}
		}
		var runs []wait.Run
		for _, v := range views {
			if v.State != "finished" && done[v.ID] {
				continue
			}
			runs = append(runs, daemon.WaitRun(v))
		}
		if len(runs) == 0 {
			return wait.Snapshot{}, fmt.Errorf("there is no batch %s", id)
		}
		return wait.Runs(kind, id, runs), nil
	case wait.KindCampaign:
		var v daemon.CampaignView
		if err := w.c.do(ctx, "GET", "/v1/campaigns/"+url.PathEscape(id), nil, &v); err != nil {
			return wait.Snapshot{}, err
		}
		var states []string
		for _, b := range v.Batches {
			states = append(states, b.State)
		}
		return wait.Campaign(id, v.State, v.Reason, states), nil
	default:
		var st daemon.Status
		if err := w.c.do(ctx, "GET", "/v1/status", nil, &st); err != nil {
			return wait.Snapshot{}, err
		}
		s, ok := daemon.StatusSnapshot(st, kind, id)
		if !ok {
			return s, fmt.Errorf("there is no lane %s", id)
		}
		return s, nil
	}
}

// runs waits for every run named, or for the first with any.
func (w *waiter) runs(ctx context.Context, ids []string, until string, anyRun bool) error {
	type result struct {
		v   daemon.WaitView
		err error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, len(ids))
	for _, id := range ids {
		go func() {
			// Each run has a waiter of its own: one may fall back alone.
			own := &waiter{c: w.c, poll: w.poll}
			v, err := own.until(ctx, wait.KindRun, id, until, -1)
			results <- result{v, err}
		}()
	}
	var views []daemon.WaitView
	code := waitMet
	for range ids {
		r := <-results
		if r.err != nil && !errors.Is(r.err, context.Canceled) {
			return r.err
		}
		if r.err != nil {
			continue
		}
		views = append(views, r.v)
		if e := ended(r.v); e != nil {
			code = max(code, int(e.(exitCode)))
		}
		if anyRun && r.v.Met {
			code = waitMet
			cancel()
			break
		}
	}
	if w.c.json {
		if err := printJSON(views); err != nil {
			return err
		}
	} else {
		for _, v := range views {
			w.runLine(ctx, v)
		}
	}
	if code != waitMet {
		return exitCode(code)
	}
	return nil
}

// runLine prints one run as `run ls` does, read again in full when it has
// finished.
func (w *waiter) runLine(ctx context.Context, v daemon.WaitView) {
	var rv daemon.RunView
	if v.State == "finished" && w.c.do(context.WithoutCancel(ctx), "GET", "/v1/runs/"+url.PathEscape(v.ID), nil, &rv) == nil {
		fmt.Println(runRow(daemon.WaitRun(rv)))
		return
	}
	fmt.Printf("%s  %s  (%s)\n", v.ID, v.State, waitEnd(v))
}

// next waits for the next run of a batch to finish.
func (w *waiter) next(ctx context.Context, batch string, seen int) error {
	if seen < 0 {
		s, err := w.now(ctx, batch)
		if err != nil {
			return err
		}
		seen = s.Counts["finished"]
	}
	v, err := w.until(ctx, wait.KindBatch, batch, fmt.Sprintf("finished>%d", seen), seen)
	if err != nil {
		return err
	}
	if w.c.json {
		if err := printJSON(v); err != nil {
			return err
		}
		return ended(v)
	}
	for _, r := range v.New {
		fmt.Println(runRow(r))
	}
	fmt.Println(summary(v))
	if v.Met && !v.Facts["done"] {
		fmt.Printf("next: automaticum wait batch %s --next --seen %d\n", batch, v.Counts["finished"])
	}
	return ended(v)
}

// each prints every run of a batch as it finishes, until the batch has
// finished: one command for the whole batch, nothing to arm again.
func (w *waiter) each(ctx context.Context, batch string, seen int) error {
	if seen < 0 {
		s, err := w.now(ctx, batch)
		if err != nil {
			return err
		}
		seen = s.Counts["finished"]
	}
	enc := json.NewEncoder(os.Stdout)
	for {
		v, err := w.until(ctx, wait.KindBatch, batch, fmt.Sprintf("finished>%d or done", seen), seen)
		if err != nil {
			return err
		}
		for _, r := range v.New {
			if w.c.json {
				enc.Encode(r)
			} else {
				fmt.Println(runRow(r))
			}
		}
		seen = max(seen, v.Counts["finished"])
		if !v.Met {
			if w.c.json {
				enc.Encode(v)
			} else {
				fmt.Println(summary(v))
			}
			return ended(v)
		}
		if v.Facts["done"] {
			if w.c.json {
				enc.Encode(v)
			} else {
				fmt.Println(summary(v))
			}
			return nil
		}
	}
}

// now is a batch's state at once.
func (w *waiter) now(ctx context.Context, batch string) (wait.Snapshot, error) {
	if !w.fallback {
		v, err := w.askNow(ctx, batch)
		if err == nil {
			return v.Snapshot, nil
		}
		if !errors.Is(err, errNoRoute) {
			return wait.Snapshot{}, err
		}
		w.fallback = true
	}
	return w.snapshot(ctx, wait.KindBatch, batch)
}

func (w *waiter) askNow(ctx context.Context, batch string) (daemon.WaitView, error) {
	q := url.Values{"id": {batch}, "until": {"done"}, "hold": {"0s"}}
	req, err := http.NewRequestWithContext(ctx, "GET", w.c.base+"/v1/wait/batch?"+q.Encode(), nil)
	if err != nil {
		return daemon.WaitView{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return daemon.WaitView{}, errors.New("the daemon does not answer; is automaticumd running?")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var e struct {
		Error string `json:"error"`
	}
	if resp.StatusCode >= 400 {
		if json.Unmarshal(body, &e) != nil || e.Error == "" {
			if resp.StatusCode == http.StatusNotFound {
				return daemon.WaitView{}, errNoRoute
			}
			return daemon.WaitView{}, errors.New(resp.Status)
		}
		return daemon.WaitView{}, errors.New(e.Error)
	}
	var v daemon.WaitView
	return v, json.Unmarshal(body, &v)
}

// cellLine prints a finished cell's counts and headline.
func (w *waiter) cellLine(ctx context.Context, batch string) {
	var cv daemon.CellView
	if err := w.c.do(context.WithoutCancel(ctx), "GET", "/v1/batches/"+url.PathEscape(batch)+"/cell", nil, &cv); err != nil {
		fmt.Fprintln(os.Stderr, "automaticum: the cell's result:", err)
		return
	}
	r := cv.Result
	fmt.Printf("cell %s: %s %s; complete %v; %s\n", batch, r.Headline.Metric, human.Number(r.Headline.Value), r.Complete, countsText(r.Counts))
}

// runRow is one run on one line, as `run ls` prints it.
func runRow(r wait.Run) string {
	wall := ""
	if r.WallMS > 0 {
		wall = human.Millis(float64(r.WallMS))
	}
	reason := r.Reason
	if len(reason) > 60 {
		reason = reason[:57] + "..."
	}
	return strings.TrimRight(fmt.Sprintf("%s  %s  %s  %s  %s  %s  %s", r.ID, r.State, r.Verdict, r.Task, r.Arm, wall, reason), " ")
}

// summary is a wait's end on one line.
func summary(v daemon.WaitView) string {
	what := v.Kind
	if v.ID != "" {
		what += " " + v.ID
	}
	switch v.Kind {
	case wait.KindRun, wait.KindBatch, wait.KindCell:
		c := v.Counts
		return fmt.Sprintf("%s %s: %s; %d finished, %d running, %d queued of %d (%s)", what, v.State, verdicts(c),
			c["finished"], c["running"], c["queued"], c["total"], waitEnd(v))
	case wait.KindCampaign:
		reason := ""
		if v.Reason != "" {
			reason = " (" + v.Reason + ")"
		}
		return fmt.Sprintf("%s %s%s; %d of %d batches finished (%s)", what, v.State, reason, v.Counts["batches_finished"], v.Counts["batches"], waitEnd(v))
	}
	return fmt.Sprintf("%s %s: %d running, %d queued (%s)", what, v.State, v.Counts["running"], v.Counts["queued"], waitEnd(v))
}

// waitEnd says how a wait ended.
func waitEnd(v daemon.WaitView) string {
	switch {
	case v.Met:
		return "holds: " + v.Until
	case v.Over:
		return "ended without: " + v.Until
	}
	return "timed out before: " + v.Until
}

// verdicts are the counts of the verdicts that occur, in a fixed order.
func verdicts(c map[string]int) string {
	var parts []string
	for _, k := range []string{"pass", "fail", "timeout", "aborted", "invalid", "skipped"} {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], strings.ToUpper(k)))
		}
	}
	if len(parts) == 0 {
		return "no verdict yet"
	}
	return strings.Join(parts, ", ")
}

func countsText(c map[string]int) string {
	var parts []string
	for _, k := range []string{"PASS", "FAIL", "TIMEOUT", "ABORTED", "INVALID", "SKIPPED", "unfinished"} {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], k))
		}
	}
	return strings.Join(parts, ", ")
}
