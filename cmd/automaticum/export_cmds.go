package main

import (
	"archive/tar"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
)

// exportConversations writes runs' captured model requests out as training
// traces (110 §3.5, D102, D104, D106): <out>/<run>/ holds the capture's
// files as the SDK wrote them, meta.json and spans.jsonl. A run the daemon
// will not export (not a training task, INVALID, no capture) is named and
// passed over.
func exportConversations(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export conversations", flag.ExitOnError)
	_, dial := addClient(fs)
	var batches, runs list
	fs.Var(&batches, "batch", "a batch, or a pattern such as 'k-pool-*'; give it several times for several")
	fs.Var(&runs, "run", "a run; give it several times for several")
	out := fs.String("out", "", "the directory to write into; a run's directory in it must not exist yet")
	parse(fs, args)
	if *out == "" || len(batches)+len(runs) == 0 {
		return errors.New("usage: automaticum export conversations --out <dir> [--batch B|GLOB ...] [--run R ...]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	ids, err := exportRuns(ctx, c, batches, runs)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("no finished run matches")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	done, passed := 0, 0
	for _, id := range ids {
		dir := filepath.Join(*out, id)
		if _, err := os.Stat(dir); err == nil {
			return fmt.Errorf("%s exists; export into another directory or move it away", dir)
		}
		files, spans, err := exportRun(ctx, c, id, dir)
		if err != nil {
			var refused *refusal
			if errors.As(err, &refused) {
				fmt.Printf("%s  passed over: %s\n", id, refused.msg)
				passed++
				continue
			}
			return fmt.Errorf("run %s: %w", id, err)
		}
		fmt.Printf("%s  %d files, %d spans\n", id, files, spans)
		done++
	}
	fmt.Printf("%d runs exported into %s, %d passed over\n", done, *out, passed)
	return nil
}

// exportRuns is the finished runs the flags name, oldest first.
func exportRuns(ctx context.Context, c *client, batches, runs []string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string
	for _, b := range batches {
		if _, err := path.Match(b, ""); err != nil {
			return nil, fmt.Errorf("--batch %q: %v", b, err)
		}
		q := "?limit=10000"
		if !strings.ContainsAny(b, "*?[") {
			q += "&batch=" + url.QueryEscape(b)
		}
		var vs []daemon.RunView
		if err := c.do(ctx, "GET", "/v1/runs"+q, nil, &vs); err != nil {
			return nil, err
		}
		for i := len(vs) - 1; i >= 0; i-- {
			v := vs[i]
			if ok, _ := path.Match(b, v.Batch); ok && v.State == "finished" && !seen[v.ID] {
				seen[v.ID] = true
				ids = append(ids, v.ID)
			}
		}
	}
	for _, r := range runs {
		if !seen[r] {
			seen[r] = true
			ids = append(ids, r)
		}
	}
	return ids, nil
}

// refusal is the daemon declining to export a run, in its own words.
type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }

// exportRun writes one run's export into dir, through a directory beside it
// that is renamed only once the whole tar has arrived.
func exportRun(ctx context.Context, c *client, id, dir string) (files, spans int, err error) {
	resp, err := c.open(ctx, "GET", "/v1/runs/"+url.PathEscape(id)+"/export", nil)
	if err != nil {
		if strings.Contains(err.Error(), "daemon does not answer") {
			return 0, 0, err
		}
		return 0, 0, &refusal{err.Error()}
	}
	defer resp.Body.Close()
	part := dir + ".partial"
	os.RemoveAll(part)
	if err := os.MkdirAll(part, 0o755); err != nil {
		return 0, 0, err
	}
	defer os.RemoveAll(part)
	tr := tar.NewReader(resp.Body)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, fmt.Errorf("the export was cut short: %v", err)
		}
		name := path.Base(h.Name)
		if h.Typeflag != tar.TypeReg || name == "." || name == ".." {
			continue
		}
		f, err := os.Create(filepath.Join(part, name))
		if err != nil {
			return 0, 0, err
		}
		_, err = io.Copy(f, tr)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return 0, 0, err
		}
		if name == "spans.jsonl" {
			b, _ := os.ReadFile(filepath.Join(part, name))
			spans = strings.Count(string(b), "\n")
		}
		files++
	}
	if files == 0 {
		return 0, 0, errors.New("the export was empty")
	}
	return files, spans, os.Rename(part, dir)
}
