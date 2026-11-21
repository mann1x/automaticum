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
// run the daemon will not export (not a training task, INVALID, no capture,
// a held-out batch without --heldout, D115)
// is named and passed over.
//
// With --anti-loop (D158) every run, a held-out task's included, is
// exported only up to the onset of its first loop (the daemon cuts it; a
// run without a loop is passed over), each manifest line says
// "use": "anti_loop", and the directory is marked ANTI_LOOP_ONLY: an
// anti-loop export and a training export never share a directory.
func exportConversations(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export conversations", flag.ExitOnError)
	_, dial := addClient(fs)
	var batches, runs list
	fs.Var(&batches, "batch", "a batch, or a pattern such as 'k-pool-*'; give it several times for several")
	fs.Var(&runs, "run", "a run; give it several times for several")
	heldout := fs.Bool("heldout", false, "also export runs of batches started held out (D115), for an eval set; never into a training set")
	antiLoop := fs.Bool("anti-loop", false, "export every run, a held-out task's too, only up to the onset of its first loop, for anti-loop training (D158); the directory is marked anti-loop only")
	out := fs.String("out", "", "the directory to write into; a run already in it is refused")
	dry := fs.Bool("dry-run", false, "say which runs would be exported; write nothing")
	parse(fs, args)
	if *out == "" || len(batches)+len(runs) == 0 {
		return errors.New("usage: automaticum export conversations --out <dir> [--batch B|GLOB ...] [--run R ...] [--heldout] [--anti-loop]")
	}
	if err := checkExportDir(*out, *antiLoop); err != nil {
		return err
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
	if *dry {
		for _, id := range ids {
			if _, err := os.Stat(filepath.Join(*out, "conversations", id)); err == nil {
				fmt.Printf("%s: already in %s, would be refused\n", id, *out)
			} else {
				if *antiLoop {
					fmt.Printf("%s: would be exported up to its first loop's onset (a run without a loop, INVALID or without a capture is passed over then)\n", id)
				} else {
					fmt.Printf("%s: would be exported (a run of a task not for training, INVALID or without a capture is passed over then)\n", id)
				}
			}
		}
		return nil
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
	if *antiLoop {
		if err := os.WriteFile(filepath.Join(*out, antiLoopMarker), []byte(antiLoopNote), 0o644); err != nil {
			return err
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
	type result struct {
		Run        string `json:"run"`
		Exported   bool   `json:"exported"`
		Requests   int    `json:"requests,omitempty"`
		Spans      int    `json:"spans,omitempty"`
		PassedOver string `json:"passed_over,omitempty"`
	}
	var results []result
	say := func(format string, args ...any) {
		if !c.json {
			fmt.Printf(format, args...)
		}
	}
	for _, id := range ids {
		x, err := exportRun(ctx, c, id, *out, *heldout, *antiLoop)
		if err != nil {
			var refused *refusal
			if errors.As(err, &refused) {
				say("%s  passed over: %s\n", id, refused.msg)
				results = append(results, result{Run: id, PassedOver: refused.msg})
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
		say("%s  %d requests, %d spans\n", id, x.manifest.Requests, x.manifest.Spans)
		results = append(results, result{Run: id, Exported: true, Requests: x.manifest.Requests, Spans: x.manifest.Spans})
		done++
	}
	os.Remove(filepath.Join(*out, ".partial"))
	if c.json {
		return printJSON(map[string]any{"out": *out, "exported": done, "passed_over": passed, "runs": results})
	}
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
	// Use is "anti_loop" in an anti-loop export (D158), HeldOutTask a run
	// whose task is not a training task.
	Use         string `json:"use,omitempty"`
	HeldOutTask bool   `json:"held_out_task,omitempty"`
}

// antiLoopMarker is the file that marks a directory as an anti-loop
// export (D158).
const antiLoopMarker = "ANTI_LOOP_ONLY"

const antiLoopNote = `This export is for anti-loop training only (D158). Each run stops at the
onset of its first loop: the requests up to that turn, and that turn's
response cut where the loop's first repeat starts. Runs of held-out tasks
(not training = true) are here: never use their text as a target beyond
the loop's onset, and never mix this directory into a training export.
`

// checkExportDir refuses to mix an anti-loop export (D158) with a
// training export in one directory.
func checkExportDir(out string, antiLoop bool) error {
	_, err := os.Stat(filepath.Join(out, antiLoopMarker))
	marked := err == nil
	_, err = os.Stat(filepath.Join(out, "manifest.jsonl"))
	used := err == nil
	switch {
	case antiLoop && used && !marked:
		return fmt.Errorf("%s holds a training export; an anti-loop export goes into a directory of its own (D158)", out)
	case !antiLoop && marked:
		return fmt.Errorf("%s is an anti-loop export (%s); export training runs into another directory (D158)", out, antiLoopMarker)
	}
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

// exported is one run's export once its files are in place: its lines of
// loop_spans.jsonl and of manifest.jsonl.
type exported struct {
	spans    []byte
	manifest manifestLine
}

// exportRun writes one run's capture into <out>/conversations/<run>/,
// through <out>/.partial/<run>, renamed only once the whole tar has
// arrived, and its record into <out>/runs/<run>.json.
func exportRun(ctx context.Context, c *client, id, out string, heldout, antiLoop bool) (*exported, error) {
	v := url.Values{}
	if heldout {
		v.Set("heldout", "1")
	}
	if antiLoop {
		v.Set("anti_loop", "1")
	}
	q := ""
	if len(v) > 0 {
		q = "?" + v.Encode()
	}
	resp, err := c.open(ctx, "GET", "/v1/runs/"+url.PathEscape(id)+"/export"+q, nil)
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
	// A daemon before D158 ignores anti_loop and hands out the whole run.
	if antiLoop && m.Use != daemon.UseAntiLoop {
		return nil, errors.New("the daemon handed out the whole run: it does not know --anti-loop (D158); update it")
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
	r := m.Run
	return &exported{spans: lines.Bytes(), manifest: manifestLine{Run: r.ID, Batch: r.Batch, Task: r.Task, TaskVersion: r.TaskVersion,
		Verdict: r.Verdict, VerdictReason: r.VerdictReason, AgentEnd: r.AgentEnd, ArmID: r.ArmID, Model: r.Model, Endpoint: r.Endpoint,
		SDKRelease: r.SDKRelease, Requests: len(m.Requests), Spans: n, Meta: "runs/" + id + ".json", Use: m.Use, HeldOutTask: m.HeldOutTask}}, nil
}
