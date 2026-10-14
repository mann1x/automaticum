package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/task"
)

// The exit codes of `cell run` (065 §2.1), confirmed with omnimergekit.
const (
	cellClean      = 0
	cellWarnings   = 10
	cellRefused    = 20
	cellIncomplete = 30
	cellInternal   = 70
)

// exitCode ends the command with a code of its own.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

// cellRun runs one cell and blocks until it is finished (065): every task of
// a suite, or of a subset, n runs each, for one arm. It is safe to run again
// with the same key: finished runs are not repeated, runs that ended without
// a result are run again, and runs still going are waited for. Stopping the
// command does not stop the cell's runs; the daemon carries on.
func cellRun(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("cell run", flag.ExitOnError)
	p, dial := addClient(fs)
	suite := fs.String("suite", "", "the suite, or suite:subset where the subset is a task id or a comma-separated list of them")
	arm := fs.String("arm", "", "the arm")
	n := fs.Int("n", 1, "runs of the arm on each task")
	lane := fs.String("lane", "", "the lane; the first that may use the arm's endpoint when not given")
	out := fs.String("out", "", "the directory for automaticum_result.json, runs.jsonl, basis.txt and refusal.json")
	key := fs.String("idempotency-key", "", "names the cell: running the command again with it resumes the cell")
	every := fs.Duration("poll", 5*time.Second, "how often the cell's state is read")
	endpointURL := fs.String("endpoint-url", "", "mode C: the endpoint the caller serves; not built yet")
	served := fs.String("served-model", "", "mode C: the model the caller serves; not built yet")
	if rest := parse(fs, args); len(rest) > 0 || *suite == "" || *arm == "" || *out == "" || *key == "" {
		return errors.New("usage: automaticum cell run --suite <suite[:subset]> --arm <arm> --out <dir> --idempotency-key <key> [-n 1] [--lane L] [--json]")
	}
	defer func() {
		// Anything that is not one of the contract's codes is internal.
		var code exitCode
		if err != nil && !errors.As(err, &code) {
			fmt.Fprintln(os.Stderr, "automaticum:", err)
			err = exitCode(cellInternal)
		}
	}()
	if *endpointURL != "" || *served != "" {
		return errors.New("mode C (--endpoint-url, --served-model) is not built yet: the cell runs on the arm's own endpoint")
	}
	suiteName, subset, _ := strings.Cut(*suite, ":")
	all, err := task.LoadAll(p.tasks())
	if err != nil {
		return err
	}
	var tasks []string
	want := map[string]bool{}
	for _, id := range strings.Split(subset, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	for _, t := range all {
		if t.Suite == suiteName && (len(want) == 0 || want[t.ID]) {
			tasks = append(tasks, t.Name())
			delete(want, t.ID)
		}
	}
	if len(want) > 0 {
		var missing []string
		for id := range want {
			missing = append(missing, id)
		}
		slices.Sort(missing)
		return fmt.Errorf("suite %s has no task %s", suiteName, strings.Join(missing, ", "))
	}
	if len(tasks) == 0 {
		return fmt.Errorf("suite %s has no tasks", suiteName)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var start daemon.StartResponse
	req := daemon.StartRequest{Arm: *arm, Tasks: tasks, Count: *n, Lane: *lane, Key: *key}
	if err := c.do(ctx, "POST", "/v1/runs", req, &start); err != nil {
		return err
	}
	if start.Repeated {
		// A resume: what ended without a result runs again.
		var rv daemon.RetryView
		retry := map[string]any{"verdicts": []store.Verdict{store.Aborted, store.Skipped}}
		err := c.do(ctx, "POST", "/v1/batches/"+url.PathEscape(start.Batch)+"/retry", retry, &rv)
		if err != nil && !strings.Contains(err.Error(), "nothing to retry") && !strings.Contains(err.Error(), "has no run to retry") {
			return err
		}
		fmt.Printf("resumed %s: %d runs, %d run again\n", start.Batch, len(start.RunIDs), len(rv.Retries))
	} else {
		fmt.Printf("started %s: %d runs, %d tasks, arm %s\n", start.Batch, len(start.RunIDs), len(tasks), *arm)
	}

	var view daemon.CellView
	last, down := -1, false
	for {
		view = daemon.CellView{}
		if err := c.do(ctx, "GET", "/v1/batches/"+url.PathEscape(start.Batch)+"/cell", nil, &view); err != nil {
			if ctx.Err() != nil {
				break
			}
			if !strings.Contains(err.Error(), "does not answer") {
				return err
			}
			// A daemon that restarts loses no run of the cell: wait for it.
			if !down {
				fmt.Println("the daemon does not answer; waiting for it")
				down = true
			}
			select {
			case <-ctx.Done():
				return exitCode(cellIncomplete)
			case <-time.After(*every):
			}
			continue
		}
		down = false
		res := view.Result
		done := res.NExpected - res.Counts["unfinished"]
		if done != last {
			fmt.Printf("%d/%d\n", done, res.NExpected)
			last = done
		}
		if res.Counts["unfinished"] == 0 {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(*every):
		}
		if ctx.Err() != nil {
			// Read once more for what the files say; the runs go on.
			stopped := context.Background()
			view = daemon.CellView{}
			if err := c.do(stopped, "GET", "/v1/batches/"+url.PathEscape(start.Batch)+"/cell", nil, &view); err != nil {
				return err
			}
			break
		}
	}
	res := view.Result
	res.Suite, res.Subset = suiteName, subset
	code := cellClean
	switch {
	case view.Refusal != nil && view.Refusal.Code == cellRefused:
		code = cellRefused
	case !res.Complete || ctx.Err() != nil:
		code = cellIncomplete
		if view.Refusal == nil {
			view.Refusal = &daemon.Refusal{Code: cellIncomplete, Unfinished: view.Unfinished}
		}
	case len(res.Warnings) > 0:
		code = cellWarnings
	}
	if code == cellIncomplete && len(res.Warnings) == 0 {
		res.Warnings = append(res.Warnings, "incomplete: the command stopped before the cell finished")
		res.Complete = false
	}
	if err := writeCell(*out, res, view, code); err != nil {
		return err
	}
	fmt.Printf("finished %s: %s, exit %d\n", start.Batch, map[int]string{cellClean: "clean", cellWarnings: "with warnings",
		cellRefused: "refused by a gate", cellIncomplete: "incomplete"}[code], code)
	if c.json {
		b, _ := json.Marshal(res)
		fmt.Println(string(b))
	}
	if code != cellClean {
		return exitCode(code)
	}
	return nil
}

// writeCell writes the cell's files, each whole or not at all.
func writeCell(dir string, res daemon.CellResult, view daemon.CellView, code int) error {
	write := func(name string, data []byte) error {
		tmp := filepath.Join(dir, "."+name+".new")
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, filepath.Join(dir, name))
	}
	var rows strings.Builder
	for _, r := range view.Rows {
		b, _ := json.Marshal(r)
		rows.Write(append(b, '\n'))
	}
	if err := write("runs.jsonl", []byte(rows.String())); err != nil {
		return err
	}
	if err := write("basis.txt", []byte(daemon.BasisText(res))); err != nil {
		return err
	}
	refusal := filepath.Join(dir, "refusal.json")
	if code == cellRefused || code == cellIncomplete {
		b, _ := json.MarshalIndent(view.Refusal, "", "  ")
		if err := write("refusal.json", append(b, '\n')); err != nil {
			return err
		}
	} else if err := os.Remove(refusal); err != nil && !os.IsNotExist(err) {
		return err
	}
	// The result is written last: a reader that finds it finds the rest.
	b, _ := json.MarshalIndent(res, "", "  ")
	return write("automaticum_result.json", append(b, '\n'))
}
