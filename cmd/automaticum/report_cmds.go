package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
)

// The reports of 070 §6, each a view the daemon already gives, under the
// name 090 §1 lists: `report run` is `run show`, `report batch` and `report
// ab` are `compare` of one batch and of two, `report cell` is `cell show`;
// `report regression` and `report census` are their own.
func reportRun(ctx context.Context, args []string) error { return runShow(ctx, args) }

func reportCell(ctx context.Context, args []string) error { return cellShow(ctx, args) }

func reportBatch(ctx context.Context, args []string) error {
	return compareAs(ctx, args, "report batch", "")
}

func reportAB(ctx context.Context, args []string) error {
	if n := len(positional(args)); n != 2 {
		return fmt.Errorf("usage: automaticum report ab <batch A> <batch B> [--all] (%d batches given)", n)
	}
	return compareAs(ctx, args, "report ab", "")
}

// reportRegression puts the runs of some batches side by side by harness
// build: what changed between two SDK releases, lane images or automaticum
// versions, on the same arm, for every metric and finding (070 §6).
func reportRegression(ctx context.Context, args []string) error {
	return compareAs(ctx, args, "report regression", "build")
}

// positional are the arguments that are not flags or their values.
func positional(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && a != "--all" && a != "-all" && a != "--json" && a != "-json" && i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// reportCensus counts the tool calls of the runs a filter selects by tool,
// model, error class and parse path (070 §6).
func reportCensus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("report census", flag.ExitOnError)
	_, dial := addClient(fs)
	where := fs.String("where", "", "which runs: suite=, task=, arm_id=, model=, verdict=, lane=, batch=, since=, until=, comma-separated")
	var batches list
	fs.Var(&batches, "batch", "a batch; give it several times")
	rest := parse(fs, args)
	batches = append(batches, rest...)
	c, err := dial()
	if err != nil {
		return err
	}
	q := url.Values{}
	if *where != "" {
		q.Set("where", *where)
	}
	if len(batches) > 0 {
		q["batch"] = batches
	}
	var v daemon.Census
	if err := c.do(ctx, "GET", "/v1/report/census?"+q.Encode(), nil, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	fmt.Printf("%d runs counted, %d of them made no tool call\n", v.Runs, v.NoCalls)
	if len(v.Rows) == 0 {
		return nil
	}
	fmt.Printf("%-24s %-28s %7s %6s %7s  %s\n", "tool", "model", "calls", "runs", "failed", "errors; parse paths")
	for _, r := range v.Rows {
		fmt.Printf("%-24s %-28s %7d %6d %7d  %s; %s\n", r.Tool, r.Model, r.Calls, r.Runs, r.Failed, countList(r.Errors), countList(r.Parse))
	}
	return nil
}

// countList is "a 3, b 1", the largest first.
func countList(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

// exportCell writes a batch's cell files (065 §3, §5) into a directory, as
// `cell run` does at its end: runs.jsonl, basis.txt, refusal.json when the
// cell was refused or is not finished, and automaticum_result.json last;
// with --format csv also runs.csv, one row per run and every per-run number
// as a column.
func exportCell(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export cell", flag.ExitOnError)
	_, dial := addClient(fs)
	out := fs.String("out", "", "the directory to write into")
	format := fs.String("format", "jsonl", "jsonl, or csv to also write runs.csv")
	dry := fs.Bool("dry-run", false, "say what would be written; write nothing")
	rest := parse(fs, args)
	if len(rest) != 1 || *out == "" {
		return errors.New("usage: automaticum export cell <batch> --out D [--format jsonl|csv]")
	}
	if err := checkFormat(*format); err != nil {
		return err
	}
	c, err := dial()
	if err != nil {
		return err
	}
	if *dry {
		var v daemon.CellView
		if err := c.do(ctx, "GET", "/v1/batches/"+url.PathEscape(rest[0])+"/cell", nil, &v); err != nil {
			return err
		}
		fmt.Printf("would write into %s: runs.jsonl (%d runs), basis.txt, automaticum_result.json", *out, len(v.Rows))
		if *format == "csv" {
			fmt.Print(", runs.csv")
		}
		fmt.Println()
		return nil
	}
	code, err := writeBatchCell(ctx, c, rest[0], *out, *format)
	if err != nil {
		return err
	}
	if c.json {
		return printJSON(map[string]any{"batch": rest[0], "out": *out, "code": code})
	}
	fmt.Printf("%s: written to %s (cell code %d)\n", rest[0], *out, code)
	return nil
}

func checkFormat(f string) error {
	switch f {
	case "jsonl", "csv":
		return nil
	case "parquet":
		return errors.New("--format parquet is not built yet (it is to be written by the daemon's DuckDB); use jsonl or csv")
	}
	return fmt.Errorf("--format %q: jsonl or csv", f)
}

// writeBatchCell fetches a batch's cell view and writes its files; it gives
// the cell's code (065 §2.1) as `cell run` would have ended with it.
func writeBatchCell(ctx context.Context, c *client, batch, dir, format string) (int, error) {
	var v daemon.CellView
	if err := c.do(ctx, "GET", "/v1/batches/"+url.PathEscape(batch)+"/cell", nil, &v); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	code := cellClean
	switch {
	case v.Refusal != nil && v.Refusal.Code == cellRefused:
		code = cellRefused
	case !v.Result.Complete:
		code = cellIncomplete
	case len(v.Result.Warnings) > 0:
		code = cellWarnings
	}
	if format == "csv" {
		if err := writeRunsCSV(filepath.Join(dir, "runs.csv"), v.Rows); err != nil {
			return code, err
		}
	}
	return code, writeCell(dir, v.Result, v, code)
}

// writeRunsCSV writes the rows with a column for every metric any run has
// and one for every detector that fired in any run, sorted by name.
func writeRunsCSV(path string, rows []daemon.CellRow) error {
	metrics, findings := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		for k := range r.Metrics {
			metrics[k] = true
		}
		for k := range r.Findings {
			findings[k] = true
		}
	}
	mk, fk := sorted(metrics), sorted(findings)
	tmp := path + ".new"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	head := []string{"run_id", "task", "verdict", "verdict_reason", "counted_as", "retry_of", "superseded_by"}
	for _, k := range fk {
		head = append(head, "finding."+k)
	}
	w.Write(append(head, mk...))
	for _, r := range rows {
		line := []string{r.RunID, r.Task, r.Verdict, r.VerdictReason, r.CountedAs, r.RetryOf, r.SupersededBy}
		for _, k := range fk {
			line = append(line, strconv.Itoa(r.Findings[k]))
		}
		for _, k := range mk {
			if v, ok := r.Metrics[k]; ok {
				line = append(line, strconv.FormatFloat(v, 'g', -1, 64))
			} else {
				line = append(line, "")
			}
		}
		w.Write(line)
	}
	w.Flush()
	if err := errors.Join(w.Error(), f.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// exportCampaign writes every started batch of a campaign as a cell under
// <out>/<batch>/, and the campaign's state as <out>/campaign.json, so that
// the directory can be copied whole to another host (065 §5).
func exportCampaign(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export campaign", flag.ExitOnError)
	_, dial := addClient(fs)
	out := fs.String("out", "", "the directory to write into")
	format := fs.String("format", "jsonl", "jsonl, or csv to also write each batch's runs.csv")
	dry := fs.Bool("dry-run", false, "say which batches would be written; write nothing")
	rest := parse(fs, args)
	if len(rest) != 1 || *out == "" {
		return errors.New("usage: automaticum export campaign <name> --out D [--format jsonl|csv]")
	}
	if err := checkFormat(*format); err != nil {
		return err
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.CampaignView
	if err := c.do(ctx, "GET", "/v1/campaigns/"+url.PathEscape(rest[0]), nil, &v); err != nil {
		return err
	}
	if *dry {
		for _, b := range v.Batches {
			if b.Batch != "" {
				fmt.Printf("would write %s\n", filepath.Join(*out, b.Batch))
			}
		}
		fmt.Printf("would write %s\n", filepath.Join(*out, "campaign.json"))
		return nil
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	codes := map[string]int{}
	for _, b := range v.Batches {
		if b.Batch == "" {
			continue
		}
		code, err := writeBatchCell(ctx, c, b.Batch, filepath.Join(*out, b.Batch), *format)
		if err != nil {
			return fmt.Errorf("batch %s: %w", b.Batch, err)
		}
		codes[b.Batch] = code
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	if err := writeWhole(filepath.Join(*out, "campaign.json"), append(data, '\n')); err != nil {
		return err
	}
	if c.json {
		return printJSON(map[string]any{"campaign": rest[0], "out": *out, "batches": codes})
	}
	fmt.Printf("campaign %s: %d batches written to %s\n", rest[0], len(codes), *out)
	for _, b := range sorted(keysOf(codes)) {
		fmt.Printf("  %s (cell code %d)\n", b, codes[b])
	}
	return nil
}

func keysOf(m map[string]int) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// exportRunArtefacts writes a run's record as run.json and each of its
// stored artefacts (065 §5: transcript, requests as sent, the workspace
// before and after, the oracle's output, ...) as <kind>[.<name>] under
// <out>/<run>/.
func exportRunArtefacts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export run", flag.ExitOnError)
	_, dial := addClient(fs)
	out := fs.String("out", "", "the directory to write into")
	var kinds list
	fs.Var(&kinds, "kind", "only this kind of artefact (events, wire, workspace_after, oracle, ...); give it several times")
	dry := fs.Bool("dry-run", false, "say which files would be written; write nothing")
	rest := parse(fs, args)
	if len(rest) == 0 || *out == "" {
		return errors.New("usage: automaticum export run <run> [<run> ...] --out D [--kind K ...]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	type written struct {
		Run   string   `json:"run"`
		Dir   string   `json:"dir"`
		Files []string `json:"files"`
	}
	var all []written
	for _, id := range rest {
		var v daemon.RunView
		if err := c.do(ctx, "GET", "/v1/runs/"+url.PathEscape(id), nil, &v); err != nil {
			return fmt.Errorf("run %s: %w", id, err)
		}
		dir := filepath.Join(*out, v.ID)
		if !*dry {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			rec, _ := json.MarshalIndent(v, "", "  ")
			if err := writeWhole(filepath.Join(dir, "run.json"), append(rec, '\n')); err != nil {
				return err
			}
		}
		wr := written{Run: v.ID, Dir: dir, Files: []string{"run.json"}}
		for _, b := range v.Blobs {
			if len(kinds) > 0 && !contains(kinds, b.Kind) {
				continue
			}
			name := b.Kind
			if b.Name != "" {
				name += "." + strings.NewReplacer("/", "_", "\\", "_").Replace(b.Name)
			}
			path := "/v1/runs/" + url.PathEscape(v.ID) + "/blobs/" + url.PathEscape(b.Kind)
			if b.Name != "" {
				path += "?name=" + url.QueryEscape(b.Name)
			}
			if *dry {
				fmt.Printf("would write %s (%d bytes)\n", filepath.Join(dir, name), b.Size)
			} else if err := download(ctx, c, path, filepath.Join(dir, name)); err != nil {
				return fmt.Errorf("run %s, %s: %w", v.ID, name, err)
			}
			wr.Files = append(wr.Files, name)
		}
		all = append(all, wr)
	}
	if *dry {
		return nil
	}
	if c.json {
		return printJSON(all)
	}
	for _, w := range all {
		fmt.Printf("%s: %d files in %s\n", w.Run, len(w.Files), w.Dir)
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// download writes what the daemon answers on path into a file, whole or not
// at all.
func download(ctx context.Context, c *client, path, file string) error {
	resp, err := c.open(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp := file + ".new"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if err = errors.Join(err, f.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, file)
}

// writeWhole writes a file whole or not at all.
func writeWhole(path string, data []byte) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
