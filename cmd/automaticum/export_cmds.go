package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
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
// traces (110 §3.5, D102, D104, D106), in the layout omnimergekit's reader
// takes: under <out>/conversations/ only the capture, <run>/<stem>.json and
// <stem>.response.json, as the SDK wrote them; beside it loop_spans.jsonl
// (every span, its request named by its path under conversations/),
// manifest.jsonl (one line per run: id, batch, task, verdict) and
// runs/<run>.json (the run as `run show` gives it, and its guard cuts). A
// run the daemon will not export (not a training task, INVALID, no capture)
// is named and passed over.
func exportConversations(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export conversations", flag.ExitOnError)
	_, dial := addClient(fs)
	var batches, runs list
	fs.Var(&batches, "batch", "a batch, or a pattern such as 'k-pool-*'; give it several times for several")
	fs.Var(&runs, "run", "a run; give it several times for several")
	out := fs.String("out", "", "the directory to write into; a run already in it is refused")
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
	for _, d := range []string{"conversations", "runs", ".partial"} {
		if err := os.MkdirAll(filepath.Join(*out, d), 0o755); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join(*out, "conversations", id)); err == nil {
			return fmt.Errorf("run %s is already in %s; export into another directory", id, *out)
		}
	}
	spansF, err := os.OpenFile(filepath.Join(*out, "loop_spans.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer spansF.Close()
	manifestF, err := os.OpenFile(filepath.Join(*out, "manifest.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer manifestF.Close()
	done, passed := 0, 0
	for _, id := range ids {
		x, err := exportRun(ctx, c, id, *out)
		if err != nil {
			var refused *refusal
			if errors.As(err, &refused) {
				fmt.Printf("%s  passed over: %s\n", id, refused.msg)
				passed++
				continue
			}
			return fmt.Errorf("run %s: %w", id, err)
		}
		// The run's lines go in only once its files are all in place.
		if _, err := spansF.Write(x.spans); err != nil {
			return err
		}
		line, _ := json.Marshal(x.manifest)
		if _, err := manifestF.Write(append(line, '\n')); err != nil {
			return err
		}
		fmt.Printf("%s  %d requests, %d spans\n", id, x.manifest.Requests, x.manifest.Spans)
		done++
	}
	os.Remove(filepath.Join(*out, ".partial"))
	fmt.Printf("%d runs exported into %s, %d passed over\n", done, *out, passed)
	return nil
}

// manifestLine is one run of manifest.jsonl.
type manifestLine struct {
	Run           string `json:"run"`
	Batch         string `json:"batch"`
	Task          string `json:"task"`
	TaskVersion   int    `json:"task_version"`
	Verdict       string `json:"verdict"`
	VerdictReason string `json:"verdict_reason"`
	AgentEnd      string `json:"agent_end"`
	ArmID         string `json:"arm_id"`
	Model         string `json:"model"`
	Endpoint      string `json:"endpoint"`
	SDKRelease    string `json:"sdk_release"`
	Requests      int    `json:"requests"`
	Spans         int    `json:"spans"`
	// Meta is the run's whole record, under the export's directory.
	Meta string `json:"meta"`
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

// exported is one run's export once its files are in place: its lines of
// loop_spans.jsonl and of manifest.jsonl.
type exported struct {
	spans    []byte
	manifest manifestLine
}

// exportRun writes one run's capture into <out>/conversations/<run>/,
// through <out>/.partial/<run>, renamed only once the whole tar has
// arrived, and its record into <out>/runs/<run>.json.
func exportRun(ctx context.Context, c *client, id, out string) (*exported, error) {
	resp, err := c.open(ctx, "GET", "/v1/runs/"+url.PathEscape(id)+"/export", nil)
	if err != nil {
		if strings.Contains(err.Error(), "daemon does not answer") {
			return nil, err
		}
		return nil, &refusal{err.Error()}
	}
	defer resp.Body.Close()
	dir := filepath.Join(out, "conversations", id)
	// Outside conversations/, which a reader globs whole.
	part := filepath.Join(out, ".partial", id)
	os.RemoveAll(part)
	if err := os.MkdirAll(part, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(part)
	var meta, spans []byte
	tr := tar.NewReader(resp.Body)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the export was cut short: %v", err)
		}
		name := path.Base(h.Name)
		if h.Typeflag != tar.TypeReg || name == "." || name == ".." {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("the export was cut short: %v", err)
		}
		switch name {
		case "meta.json":
			meta = b
		case "spans.jsonl":
			spans = b
		default:
			if err := os.WriteFile(filepath.Join(part, name), b, 0o644); err != nil {
				return nil, err
			}
		}
	}
	var m daemon.ExportMeta
	if err := json.Unmarshal(meta, &m); err != nil {
		return nil, fmt.Errorf("the export has no readable meta.json: %v", err)
	}
	// A span names its request by its path under conversations/.
	var lines bytes.Buffer
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(spans)), "\n") {
		if l == "" {
			continue
		}
		var s map[string]any
		if err := json.Unmarshal([]byte(l), &s); err != nil {
			return nil, fmt.Errorf("spans.jsonl: %v", err)
		}
		s["request"] = id + "/" + fmt.Sprint(s["request"]) + ".json"
		b, _ := json.Marshal(s)
		lines.Write(append(b, '\n'))
		n++
	}
	if err := os.WriteFile(filepath.Join(out, "runs", id+".json"), meta, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(part, dir); err != nil {
		return nil, err
	}
	v := m.Run
	return &exported{spans: lines.Bytes(), manifest: manifestLine{Run: v.ID, Batch: v.Batch, Task: v.Task, TaskVersion: v.TaskVersion,
		Verdict: v.Verdict, VerdictReason: v.VerdictReason, AgentEnd: v.AgentEnd, ArmID: v.ArmID, Model: v.Model, Endpoint: v.Endpoint,
		SDKRelease: v.SDKRelease, Requests: len(m.Requests), Spans: n, Meta: "runs/" + id + ".json"}}, nil
}
