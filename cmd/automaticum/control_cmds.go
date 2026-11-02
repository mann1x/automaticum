package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// laneControl drains, pauses, quarantines or resumes a lane, or aborts its
// run in flight (050 §4); --all does it to every lane, the daemon's level.
func laneControl(verb string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("lane "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		m := addMutation(fs)
		all := fs.Bool("all", false, "every lane of this installation")
		reason := fs.String("reason", "", "quarantine: why the lane is taken out of use")
		keep := fs.Bool("keep-model", false, "pause: hold the run in flight but leave its model loaded")
		rest := parse(fs, args)
		if *all == (len(rest) == 1) || len(rest) > 1 {
			return fmt.Errorf("usage: automaticum lane %s <lane> | --all [--dry-run] [--idempotency-key K]", verb)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		lanes := rest
		if *all {
			var st daemon.Status
			if err := c.do(ctx, "GET", "/v1/status", nil, &st); err != nil {
				return err
			}
			lanes = nil
			for _, l := range st.Lanes {
				lanes = append(lanes, l.Lane)
			}
		}
		key := *m.key
		var outs []daemon.LaneControl
		for _, lane := range lanes {
			// One key per lane: the same command again repeats nothing.
			if key != "" && *all {
				*m.key = key + "/" + lane
			}
			var out daemon.LaneControl
			err := c.do(ctx, "POST", m.on(c, "/v1/lanes/"+url.PathEscape(lane)+"/"+verb), map[string]any{"reason": *reason, "keep_model": *keep}, &out)
			if err != nil && *all && verb == "abort" && strings.Contains(err.Error(), "no run in flight") {
				continue
			}
			if err != nil {
				return err
			}
			outs = append(outs, out)
		}
		if c.json {
			if *all {
				return printJSON(outs)
			}
			return printJSON(outs[0])
		}
		for _, out := range outs {
			line := out.Lane + ": " + out.State
			switch out.State {
			case "drained", "paused":
				line += "; nothing starts until `automaticum lane resume " + out.Lane + "`"
			case "quarantined":
				line += " (" + out.Reason + "); nothing starts until `automaticum lane resume " + out.Lane + "`"
			case "resumed":
				line += "; starts queued runs again"
			}
			fmt.Println(m.dryNote() + line)
			if out.InFlight != "" && out.Action != "" {
				fmt.Printf("%s%s: %s\n", m.dryNote(), out.InFlight, out.Action)
			}
		}
		if *all && len(outs) == 0 {
			fmt.Println("no lane had a run in flight")
		}
		return nil
	}
}

// batchControl pauses, resumes or stops a batch.
func batchControl(verb string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("batch "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		m := addMutation(fs)
		keep := fs.Bool("keep-model", false, "pause: hold the run in flight but leave its model loaded")
		abort := fs.Bool("abort", false, "stop: end the run in flight too, instead of letting it finish (as batch abort)")
		rest := parse(fs, args)
		if len(rest) != 1 {
			return fmt.Errorf("usage: automaticum batch %s <batch> [--dry-run] [--idempotency-key K]", verb)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		var in any
		switch verb {
		case "pause":
			in = map[string]bool{"keep_model": *keep}
		case "stop":
			in = map[string]bool{"abort": *abort}
		}
		var out daemon.BatchControl
		if err := c.do(ctx, "POST", m.on(c, "/v1/batches/"+url.PathEscape(rest[0])+"/"+verb), in, &out); err != nil {
			return err
		}
		if c.json {
			return printJSON(out)
		}
		note := m.dryNote()
		switch verb {
		case "pause":
			fmt.Printf("%s%s: paused; %d queued runs wait until `automaticum batch resume %s`\n", note, out.Batch, len(out.Queued), out.Batch)
		case "resume":
			fmt.Printf("%s%s: resumed; %d queued runs may start\n", note, out.Batch, len(out.Queued))
		case "stop", "abort":
			fmt.Printf("%s%s: stopped; %d queued runs taken off the queue and recorded SKIPPED\n", note, out.Batch, len(out.Stopped))
		}
		if out.InFlight != "" {
			fmt.Printf("%s%s: %s\n", note, out.InFlight, out.Action)
		}
		return nil
	}
}

// retry queues the runs of a batch, or one run, that ended without a result
// of the arm on the task: SKIPPED, ABORTED or INVALID.
func retry(noun string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet(noun+" retry", flag.ExitOnError)
		_, dial := addClient(fs)
		dry := fs.Bool("dry-run", false, "say what would be queued, queue nothing")
		m := keyOnly(fs, dry)
		rest := parse(fs, args)
		if len(rest) != 1 {
			return fmt.Errorf("usage: automaticum %s retry <%s> [--dry-run]", noun, noun)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		path := "/v1/runs/" + url.PathEscape(rest[0]) + "/retry"
		if noun == "batch" {
			path = "/v1/batches/" + url.PathEscape(rest[0]) + "/retry"
		}
		var out daemon.RetryView
		if err := c.do(ctx, "POST", m.on(c, path), map[string]bool{"dry_run": *dry}, &out); err != nil {
			return err
		}
		if c.json {
			return printJSON(out)
		}
		news := make([]string, 0, len(out.Retries))
		for nu := range out.Retries {
			news = append(news, nu)
		}
		sort.Strings(news)
		what := "queued"
		if out.DryRun {
			what = "would be queued"
		}
		for _, nu := range news {
			fmt.Printf("%s %s, retries %s\n", nu, what, out.Retries[nu])
		}
		olds := make([]string, 0, len(out.Refused))
		for old := range out.Refused {
			olds = append(olds, old)
		}
		sort.Strings(olds)
		for _, old := range olds {
			fmt.Printf("%s not retried: %s\n", old, out.Refused[old])
		}
		return nil
	}
}

// batchShow prints one batch: its runs by state, whether it is paused, and
// what the canary said of its first requests.
func batchShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("batch show", flag.ExitOnError)
	_, dial := addClient(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return fmt.Errorf("usage: automaticum batch show <batch>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.BatchView
	if err := c.do(ctx, "GET", "/v1/batches/"+url.PathEscape(rest[0]), nil, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	fmt.Printf("batch            %s\n", v.Batch)
	if v.StartedBy != "" {
		fmt.Printf("started by       %s\n", v.StartedBy)
	}
	if v.Paused {
		fmt.Printf("state            paused\n")
	}
	if v.InFlight != "" {
		fmt.Printf("in flight        %s\n", v.InFlight)
	}
	fmt.Printf("queued           %d\n", len(v.Queued))
	verdicts := make([]string, 0, len(v.Finished))
	for k := range v.Finished {
		verdicts = append(verdicts, k)
	}
	sort.Strings(verdicts)
	for _, k := range verdicts {
		fmt.Printf("%-16s %d\n", k, v.Finished[k])
	}
	switch {
	case v.Canary == nil:
		fmt.Printf("canary           not judged yet\n")
	case v.Canary.Stopped:
		fmt.Printf("canary           stopped the batch at %s\n", human.Time(v.Canary.At))
	default:
		fmt.Printf("canary           sound at %s\n", human.Time(v.Canary.At))
	}
	if v.Canary != nil {
		fmt.Printf("  judged         %s\n", strings.Join(v.Canary.Requests, " "))
		for _, p := range v.Canary.Problems {
			fmt.Printf("  problem        %s\n", p)
		}
		for _, u := range v.Canary.Unchecked {
			fmt.Printf("  unchecked      %s\n", u)
		}
	}
	return nil
}

// batchLimits sets a batch's own limits for its queued runs (D107).
func batchLimits(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("batch limits", flag.ExitOnError)
	_, dial := addClient(fs)
	limits := addLimits(fs)
	m := addMutation(fs)
	rest := parse(fs, args)
	o := limits()
	if len(rest) != 1 || o == nil {
		return fmt.Errorf("usage: automaticum batch limits [--wall-s S] [--idle-s S] <batch>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var out daemon.BatchControl
	if err := c.do(ctx, "POST", m.on(c, "/v1/batches/"+url.PathEscape(rest[0])+"/limits"), o, &out); err != nil {
		return err
	}
	if c.json {
		return printJSON(out)
	}
	var set []string
	if l := out.Limits; l != nil && l.WallS != nil {
		set = append(set, "wall "+human.Seconds(float64(*l.WallS)))
	}
	if l := out.Limits; l != nil && l.IdleS != nil {
		if *l.IdleS == 0 {
			set = append(set, "no idle limit")
		} else {
			set = append(set, "idle "+human.Seconds(float64(*l.IdleS)))
		}
	}
	fmt.Printf("%s%s: %s for its %d queued runs, in place of the arm's and the task's\n", m.dryNote(), out.Batch, strings.Join(set, ", "), len(out.Queued))
	if out.InFlight != "" {
		fmt.Printf("%s: %s\n", out.InFlight, out.Action)
	}
	return nil
}

// laneReserve reserves a lane for one batch or campaign until a time, or
// ends its reservation (D125).
func laneReserve(unreserve bool) func(context.Context, []string) error {
	verb := "reserve"
	if unreserve {
		verb = "unreserve"
	}
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("lane "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		m := addMutation(fs)
		until := fs.String("until", "", "reserve: when it ends, as 2026-11-02T08:00:00+01:00, or a length of time from now such as 8h")
		forWhat := fs.String("for", "", "reserve: the batch or campaign the lane is kept for")
		rest := parse(fs, args)
		if len(rest) != 1 || !unreserve && (*until == "" || *forWhat == "") {
			return fmt.Errorf("usage: automaticum lane %s <lane>%s [--dry-run] [--idempotency-key K]", verb,
				map[bool]string{false: " --until T --for <batch|campaign>", true: ""}[unreserve])
		}
		body := daemon.ReserveRequest{For: *forWhat, Until: *until}
		if d, err := time.ParseDuration(*until); err == nil && !unreserve {
			body.Until = time.Now().Add(d).Format(time.RFC3339)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		var out map[string]any
		if err := c.do(ctx, "POST", m.on(c, "/v1/lanes/"+url.PathEscape(rest[0])+"/"+verb), body, &out); err != nil {
			return err
		}
		if c.json {
			return printJSON(out)
		}
		if unreserve {
			fmt.Printf("%slane %s: no longer reserved for %v\n", m.dryNote(), rest[0], out["was_for"])
			return nil
		}
		fmt.Printf("%slane %s: reserved for %s until %s; only its runs start there\n", m.dryNote(), rest[0], *forWhat, body.Until)
		return nil
	}
}
