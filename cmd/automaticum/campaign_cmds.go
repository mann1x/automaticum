package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// campaignStart starts campaigns/<name>.toml. Starting it again starts
// nothing and shows where it is.
func campaignStart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("campaign start", flag.ExitOnError)
	_, dial := addClient(fs)
	dry := fs.Bool("dry-run", false, "check every batch and start nothing")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum campaign start <name> [--dry-run]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.CampaignView
	if err := c.do(ctx, "POST", "/v1/campaigns", daemon.CampaignRequest{Name: rest[0], DryRun: *dry}, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	switch {
	case v.Repeated:
		fmt.Printf("campaign %s was already started; nothing new starts\n\n", v.Name)
	case *dry:
		fmt.Printf("campaign %s would start; every batch can be queued\n\n", v.Name)
	default:
		fmt.Printf("campaign %s started\n\n", v.Name)
	}
	printCampaign(v)
	return nil
}

// campaignLs lists the campaigns, newest first.
func campaignLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("campaign ls", flag.ExitOnError)
	_, dial := addClient(fs)
	parse(fs, args)
	c, err := dial()
	if err != nil {
		return err
	}
	var list []daemon.CampaignView
	if err := c.do(ctx, "GET", "/v1/campaigns", nil, &list); err != nil {
		return err
	}
	if c.json {
		return printJSON(list)
	}
	w := table()
	fmt.Fprintln(w, "CAMPAIGN\tSTATE\tBATCHES\tSTARTED\tREASON")
	for _, v := range list {
		ended := 0
		for _, b := range v.Batches {
			if b.State == "finished" || b.State == "stopped" {
				ended++
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%d of %d ended\t%s\t%s\n", v.Name, v.State, ended, len(v.Batches), human.Time(v.Started), v.Reason)
	}
	return w.Flush()
}

// campaignShow prints a campaign, and below it each batch that has runs,
// compared as `automaticum compare` does: the report of the campaign.
func campaignShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("campaign show", flag.ExitOnError)
	_, dial := addClient(fs)
	all := fs.Bool("all", false, "every metric in the comparisons")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum campaign show <name> [--all]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.CampaignView
	if err := c.do(ctx, "GET", "/v1/campaigns/"+url.PathEscape(rest[0]), nil, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	printCampaign(v)
	for _, b := range v.Batches {
		if b.Batch == "" {
			continue
		}
		fmt.Printf("\n=== batch %s (%s) ===\n\n", b.Name, b.Batch)
		cargs := []string{b.Batch}
		if *all {
			cargs = append(cargs, "--all")
		}
		if err := compare(ctx, cargs); err != nil {
			fmt.Printf("no comparison: %v\n", err)
		}
	}
	return nil
}

func printCampaign(v daemon.CampaignView) {
	fmt.Printf("campaign  %s, %s", v.Name, v.State)
	if v.Reason != "" {
		fmt.Printf(": %s", v.Reason)
	}
	fmt.Println()
	if v.Def != nil && v.Def.Description != "" {
		fmt.Printf("          %s\n", v.Def.Description)
	}
	if !v.Started.IsZero() {
		fmt.Printf("started   %s\n", human.Time(v.Started))
	}
	if !v.Ended.IsZero() {
		fmt.Printf("ended     %s, after %s\n", human.Time(v.Ended), human.Millis(float64(v.Ended.Sub(v.Started).Milliseconds())))
	}
	w := table()
	fmt.Fprintln(w, "\nBATCH\tSTATE\tID\tARMS\tTASKS\tRUNS EACH\tSTOP\tREASON")
	for i, b := range v.Batches {
		var arms, tasks, stop []string
		runs := 0
		if v.Def != nil && i < len(v.Def.Batches) {
			d := v.Def.Batches[i]
			arms, tasks, runs = d.Arms, d.Tasks, d.Runs
			if d.Stop.AfterConsecutivePass > 0 {
				stop = append(stop, fmt.Sprintf("after %d passes in a row", d.Stop.AfterConsecutivePass))
			}
			if d.Stop.OnInvalid {
				stop = append(stop, "on INVALID")
			}
			if !d.Stop.At.IsZero() {
				stop = append(stop, "at "+human.Time(d.Stop.At))
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", b.Name, b.State, b.Batch, strings.Join(arms, ", "), strings.Join(tasks, ", "), runs, strings.Join(stop, ", "), b.Reason)
	}
	w.Flush()
}

// campaignControl pauses, resumes or stops a campaign.
func campaignControl(verb string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("campaign "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		keep := fs.Bool("keep-model", false, "pause: hold the run in flight but leave its model loaded")
		rest := parse(fs, args)
		if len(rest) != 1 {
			return fmt.Errorf("usage: automaticum campaign %s <name>", verb)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		var v daemon.CampaignView
		if err := c.do(ctx, "POST", "/v1/campaigns/"+url.PathEscape(rest[0])+"/"+verb, map[string]bool{"keep_model": *keep}, &v); err != nil {
			return err
		}
		if c.json {
			return printJSON(v)
		}
		printCampaign(v)
		return nil
	}
}
