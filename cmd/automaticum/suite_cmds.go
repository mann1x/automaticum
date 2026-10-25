package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/suite/tbench2"
)

// suiteImport imports tasks of an external suite from a checkout of its
// source at a pinned commit (066). The only suite so far is tbench2. The
// tasks are the ones named, or with none named every task of the import
// config (tasks/tbench2/import.toml), which declares each.
func suiteImport(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != tbench2.Suite {
		return errors.New("usage: automaticum suite import tbench2 -source DIR -commit SHA [<task>...]")
	}
	fs := flag.NewFlagSet("suite import", flag.ExitOnError)
	p := addPlace(fs)
	source := fs.String("source", "", "a checkout of terminal-bench-2")
	commit := fs.String("commit", "", "the commit the checkout must be at")
	repeats := fs.Int("repeats", 3, "how many times the oracle judges each fixture before a task is kept")
	names := parse(fs, args[1:])
	if *source == "" || *commit == "" {
		return errors.New("usage: automaticum suite import tbench2 -source DIR -commit SHA [<task>...]")
	}
	cfg, err := tbench2.ReadConfig(p.root())
	if err != nil {
		return err
	}
	if len(names) == 0 {
		names = cfg.Names()
	}
	head, err := exec.CommandContext(ctx, "git", "-C", *source, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("%s: %w", *source, err)
	}
	if got := strings.TrimSpace(string(head)); got != *commit {
		return fmt.Errorf("%s is at %s, not %s", *source, got, *commit)
	}
	if dirty, err := exec.CommandContext(ctx, "git", "-C", *source, "status", "--porcelain").Output(); err != nil || len(dirty) > 0 {
		return fmt.Errorf("%s has changes of its own: import from a clean checkout", *source)
	}
	l, err := p.local()
	if err != nil {
		return err
	}
	env, err := taskEnv(p)
	if err != nil {
		return err
	}
	im := &tbench2.Importer{
		Engine: docker.New(l.Daemon.DockerSocket), Env: env, Source: *source, Commit: *commit,
		Repo: p.root(), Config: cfg, Repeats: *repeats, Log: os.Stderr,
	}
	failed := 0
	for _, name := range names {
		r, err := im.Import(ctx, name)
		if err != nil {
			failed++
			fmt.Fprintln(os.Stderr, "automaticum:", err)
			if r != nil {
				for _, v := range r.Verify {
					for _, run := range v.Runs {
						fmt.Fprintf(os.Stderr, "  %s (expect %s): passed %v, %s, exit %d %s\n", v.Fixture, v.Expect, run.Passed, human.Duration(run.Duration), run.ExitCode, run.Err)
						if !v.OK {
							lines := strings.Split(strings.TrimRight(run.Output, "\n"), "\n")
							if len(lines) > 15 {
								lines = lines[len(lines)-15:]
							}
							fmt.Fprintf(os.Stderr, "    %s\n", strings.Join(lines, "\n    "))
						}
					}
				}
			}
			continue
		}
		fmt.Printf("%s/%s  %s  %s\n", tbench2.Suite, name, r.Hash, r.Dir)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d tasks not imported", failed, len(names))
	}
	fmt.Printf("build the lane layers next: automaticum image build %s\n", layers(names))
	return nil
}

func layers(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = tbench2.Layer(n)
	}
	return strings.Join(out, " ")
}
