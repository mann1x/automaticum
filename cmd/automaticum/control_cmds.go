package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// laneControl drains a lane or lets it start runs again.
func laneControl(drain bool) func(context.Context, []string) error {
	verb := "resume"
	if drain {
		verb = "drain"
	}
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("lane "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		rest := parse(fs, args)
		if len(rest) != 1 {
			return fmt.Errorf("usage: automaticum lane %s <lane>", verb)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		var out daemon.LaneControl
		if err := c.do(ctx, "POST", "/v1/lanes/"+url.PathEscape(rest[0])+"/"+verb, nil, &out); err != nil {
			return err
		}
		if c.json {
			return printJSON(out)
		}
		switch {
		case drain && out.InFlight != "":
			fmt.Printf("%s: drained; %s finishes, then nothing else starts until `automaticum lane resume %s`\n", out.Lane, out.InFlight, out.Lane)
		case drain:
			fmt.Printf("%s: drained; nothing starts until `automaticum lane resume %s`\n", out.Lane, out.Lane)
		default:
			fmt.Printf("%s: starts queued runs again\n", out.Lane)
		}
		return nil
	}
}

// batchControl pauses, resumes or stops a batch.
func batchControl(verb string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("batch "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		keep := fs.Bool("keep-model", false, "pause: hold the run in flight but leave its model loaded")
		abort := fs.Bool("abort", false, "stop: end the run in flight too, instead of letting it finish")
		rest := parse(fs, args)
		if len(rest) != 1 {
			return fmt.Errorf("usage: automaticum batch %s <batch>", verb)
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
		if err := c.do(ctx, "POST", "/v1/batches/"+url.PathEscape(rest[0])+"/"+verb, in, &out); err != nil {
			return err
		}
		if c.json {
			return printJSON(out)
		}
		switch verb {
		case "pause":
			fmt.Printf("%s: paused; %d queued runs wait until `automaticum batch resume %s`\n", out.Batch, len(out.Queued), out.Batch)
		case "resume":
			fmt.Printf("%s: resumed; %d queued runs may start\n", out.Batch, len(out.Queued))
		case "stop":
			fmt.Printf("%s: stopped; %d queued runs taken off the queue and recorded SKIPPED\n", out.Batch, len(out.Stopped))
		}
		if out.InFlight != "" {
			fmt.Printf("%s: %s\n", out.InFlight, out.Action)
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
		if err := c.do(ctx, "POST", path, map[string]bool{"dry_run": *dry}, &out); err != nil {
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
