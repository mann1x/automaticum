package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// rescore scores a finished cell again with its tasks' current oracles
// (D121, 065 §4): queued on the lanes that ran its runs, one oracle at a
// time. It waits until every run has its new answer; with --out it then
// writes the cell's files again, keeping the old result file as
// automaticum_result.<time>.json.
func rescore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rescore", flag.ExitOnError)
	_, dial := addClient(fs)
	m := addMutation(fs)
	out := fs.String("out", "", "the cell's directory: its files are written again, the old result file kept beside them")
	noWait := fs.Bool("no-wait", false, "queue the rescore and return")
	every := fs.Duration("poll", 2*time.Second, "how often the rescore's state is read")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum rescore <batch> [--out D] [--no-wait] [--dry-run]")
	}
	batch := rest[0]
	if *out != "" {
		if st, err := os.Stat(*out); err != nil || !st.IsDir() {
			return fmt.Errorf("usage: --out %s is not a directory", *out)
		}
	}
	c, err := dial()
	if err != nil {
		return err
	}
	path := "/v1/batches/" + url.PathEscape(batch) + "/rescore"
	var st daemon.RescoreStatus
	if err := c.do(ctx, "POST", m.on(c, path), nil, &st); err != nil {
		return err
	}
	if st.DryRun || *noWait {
		if c.json {
			return printJSON(st)
		}
		fmt.Printf("%s%s: %d runs to rescore with their tasks' current oracles, one at a time on their lanes\n", m.dryNote(), batch, len(st.Job.Todo))
		return nil
	}
	told := map[string]bool{}
	for {
		for _, r := range st.Records {
			if told[r.Run] || c.json {
				continue
			}
			told[r.Run] = true
			now := string(r.Verdict)
			if now == "" {
				now = "no answer"
			}
			fmt.Printf("%s: %s -> %s: %s\n", r.Run, r.Was, now, r.Reason)
		}
		if st.Job != nil && !st.Job.Finished.IsZero() {
			break
		}
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "the rescore of %s goes on in the daemon; `automaticum rescore` again only once it has finished\n", batch)
			return ctx.Err()
		case <-time.After(*every):
		}
		st = daemon.RescoreStatus{}
		if err := c.do(ctx, "GET", path, nil, &st); err != nil {
			return err
		}
	}
	j := st.Job
	if !c.json {
		fmt.Printf("rescored %s in %s: %d runs, %d without an answer\n", batch, human.Duration(j.Finished.Sub(j.Queued)), len(j.Done), len(j.Failed))
		for run, why := range j.Failed {
			fmt.Printf("  %s: %s\n", run, why)
		}
	}
	if *out == "" {
		if c.json {
			return printJSON(st)
		}
		return nil
	}
	old := filepath.Join(*out, "automaticum_result.json")
	if _, err := os.Stat(old); err == nil {
		kept := filepath.Join(*out, "automaticum_result."+j.Finished.UTC().Format("20060102T150405Z")+".json")
		if err := os.Rename(old, kept); err != nil {
			return err
		}
		if !c.json {
			fmt.Printf("the old result file is kept as %s\n", kept)
		}
	}
	code, err := writeBatchCell(ctx, c, batch, *out, "jsonl")
	if err != nil {
		return err
	}
	if c.json {
		return printJSON(map[string]any{"batch": batch, "out": *out, "code": code, "rescore": st})
	}
	fmt.Printf("wrote the cell's files to %s (cell code %d)\n", *out, code)
	return nil
}
