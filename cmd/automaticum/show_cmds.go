package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/task"
)

// The read commands of 090 §1 that list or show one thing: lanes, batches,
// cells, arms, endpoints, tasks.

// laneShow is one lane: its configuration, what it does, its queue, its
// last runs. Without a daemon it is the configuration alone.
func laneShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("lane show", flag.ExitOnError)
	p, dial := addClient(fs)
	recent := fs.Int("recent", 10, "how many of its last runs")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum lane show <lane> [--recent 10]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.LaneView
	if err := c.do(ctx, "GET", "/v1/lanes/"+url.PathEscape(rest[0])+"?recent="+fmt.Sprint(*recent), nil, &v); err != nil {
		if !strings.Contains(err.Error(), "does not answer") {
			return err
		}
		l, lerr := p.local()
		if lerr != nil {
			return lerr
		}
		ln := l.Lanes[rest[0]]
		if ln == nil {
			return fmt.Errorf("there is no lane %s", rest[0])
		}
		fmt.Fprintln(os.Stderr, "automaticum: the daemon does not answer; the lane as configured:")
		v = daemon.LaneView{Config: ln}
		v.Lane = rest[0]
	}
	if c.json {
		return printJSON(v)
	}
	ln := v.Config
	fmt.Printf("lane %s on %s: %s\n", v.Lane, ln.Host, v.State)
	fmt.Printf("  endpoints %s; user %s; %g CPUs, %g GB memory, %d processes\n", strings.Join(ln.Endpoints, ", "), ln.User, ln.CPUs, ln.MemoryGB, ln.Pids)
	if v.RunID != "" {
		fmt.Printf("  run %s: %s on %s, for %s\n", v.RunID, v.Arm, v.Task, ago(v.Since))
	}
	if v.Waiting != "" {
		fmt.Printf("  waiting: %s\n", v.Waiting)
	}
	if v.Quarantined != "" {
		fmt.Printf("  quarantined: %s\n", v.Quarantined)
	}
	for what, t := range v.Control {
		fmt.Printf("  %s since %s\n", what, human.Time(t))
	}
	if len(v.Queue) > 0 {
		fmt.Printf("\n%d queued\n", len(v.Queue))
		w := table()
		fmt.Fprintln(w, "RUN\tBATCH\tARM\tTASK\tWAITING\tNOTE")
		for _, q := range v.Queue {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", q.RunID, q.Batch, q.Arm, q.Task, ago(q.EnqueuedAt), q.Stopped)
		}
		w.Flush()
	}
	if len(v.Recent) > 0 {
		fmt.Printf("\nlast %d runs\n", len(v.Recent))
		w := table()
		fmt.Fprintln(w, "RUN\tVERDICT\tTASK\tARM\tWALL\tENDED")
		for _, r := range v.Recent {
			wall := ""
			if r.WallMS > 0 {
				wall = human.Millis(float64(r.WallMS))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Verdict, r.Task, r.Arm, wall, ago(r.EndedAt))
		}
		w.Flush()
	}
	return nil
}

// batchLs lists the batches, the latest first.
func batchLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("batch ls", flag.ExitOnError)
	_, dial := addClient(fs)
	limit := fs.Int("limit", 30, "how many")
	campaign := fs.String("campaign", "", "only this campaign's batches")
	if rest := parse(fs, args); len(rest) != 0 {
		return errors.New("usage: automaticum batch ls [--limit 30] [--campaign C]")
	}
	return listBatches(ctx, dial, url.Values{"limit": {fmt.Sprint(*limit)}, "campaign": {*campaign}}, false)
}

// cellLs lists the batches started by `cell run` or as a mode C cell.
func cellLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cell ls", flag.ExitOnError)
	_, dial := addClient(fs)
	limit := fs.Int("limit", 30, "how many")
	if rest := parse(fs, args); len(rest) != 0 {
		return errors.New("usage: automaticum cell ls [--limit 30]")
	}
	return listBatches(ctx, dial, url.Values{"limit": {fmt.Sprint(*limit)}, "cell": {"1"}}, true)
}

func listBatches(ctx context.Context, dial func() (*client, error), q url.Values, cells bool) error {
	c, err := dial()
	if err != nil {
		return err
	}
	var out []daemon.BatchSummary
	if err := c.do(ctx, "GET", "/v1/batches?"+q.Encode(), nil, &out); err != nil {
		return err
	}
	if c.json {
		return printJSON(out)
	}
	w := table()
	head := "BATCH\tSTATE\tRUNS\tQUEUED\tRUNNING\tVERDICTS\tARMS\tTASKS\tCAMPAIGN\tLAST"
	if cells {
		head = "BATCH\tSUITE\tSTATE\tRUNS\tQUEUED\tRUNNING\tVERDICTS\tARMS\tLAST"
	}
	fmt.Fprintln(w, head)
	for _, b := range out {
		last := b.Last
		if last.IsZero() {
			last = b.First
		}
		if cells {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\t%s\n", b.Batch, b.Cell, b.State, b.Total, b.Queued, b.Running,
				verdictText(b.Finished), strings.Join(b.Arms, ","), ago(last))
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\t%s\t%d\t%s\t%s\n", b.Batch, b.State, b.Total, b.Queued, b.Running,
			verdictText(b.Finished), strings.Join(b.Arms, ","), b.Tasks, b.Campaign, ago(last))
	}
	return w.Flush()
}

// verdictText is counts by verdict in a fixed order.
func verdictText(m map[string]int) string {
	var parts []string
	for _, v := range []string{"PASS", "FAIL", "TIMEOUT", "ABORTED", "INVALID", "SKIPPED"} {
		if m[v] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", m[v], v))
		}
	}
	return strings.Join(parts, ", ")
}

// cellShow is a batch read as a cell (065 §3): the result file's numbers,
// without writing anything.
func cellShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cell show", flag.ExitOnError)
	_, dial := addClient(fs)
	rows := fs.Bool("runs", false, "also each run's line, as runs.jsonl has it")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum cell show <batch> [--runs]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.CellView
	if err := c.do(ctx, "GET", "/v1/batches/"+url.PathEscape(rest[0])+"/cell", nil, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	r := v.Result
	if r.EngineNote != "" {
		fmt.Printf("note: %s\n", r.EngineNote)
	}
	fmt.Printf("cell %s: arm %s (id %s), %d of %d runs counted, complete %v\n", r.Batch, r.Arm, r.ArmID,
		r.NExpected-r.Counts["unfinished"], r.NExpected, r.Complete)
	fmt.Printf("  %s %s\n", r.Headline.Metric, human.Number(r.Headline.Value))
	fmt.Printf("  %s\n", countsText(r.Counts))
	names := make([]string, 0, len(r.Scores))
	for k := range r.Scores {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("  %s %s\n", k, human.Metric(k, r.Scores[k]))
	}
	for _, wn := range r.Warnings {
		fmt.Printf("  warning: %s\n", wn)
	}
	if len(v.Unfinished) > 0 {
		fmt.Printf("  unfinished: %s\n", strings.Join(v.Unfinished, " "))
	}
	if v.Refusal != nil {
		fmt.Printf("  refused (code %d): %s %s\n", v.Refusal.Code, v.Refusal.Gate, v.Refusal.Reason)
	}
	if *rows {
		w := table()
		fmt.Fprintln(w, "\nRUN\tTASK\tVERDICT\tCOUNTED AS\tREASON")
		for _, row := range v.Rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", row.RunID, row.Task, row.Verdict, row.CountedAs, row.VerdictReason)
		}
		w.Flush()
	}
	return nil
}

// armLs lists the installation's arms (D118): name, id, model, endpoint and
// SDK build. An arm that does not load is listed with why.
// armRow is one arm in arm ls: its file's arm, and what the daemon does
// with it (GET /v1/arms).
type armRow struct {
	Name        string   `json:"name"`
	ID          string   `json:"id,omitempty"`
	Model       string   `json:"model,omitempty"`
	Endpoint    string   `json:"endpoint,omitempty"`
	SDK         string   `json:"sdk_release,omitempty"`
	Problem     string   `json:"problem,omitempty"`
	Running     int      `json:"running"`
	Queued      int      `json:"queued"`
	RunningRuns []string `json:"running_runs,omitempty"`
	Lanes       []string `json:"lanes,omitempty"`
	Batches     []string `json:"batches,omitempty"`
}

// armLs lists the arms in the configuration with what the daemon is doing
// with each: runs running and queued. -active lists only the arms with runs
// running or queued, -running only those with a run in flight; both come
// from the daemon, and include an arm whose file has changed or gone since
// its runs were queued.
func armLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("arm ls", flag.ExitOnError)
	p, dial := addClient(fs)
	active := fs.Bool("active", false, "only the arms with runs running or queued")
	running := fs.Bool("running", false, "only the arms with a run in flight")
	parse(fs, args)
	only := *active || *running
	// What the daemon runs: needed for -active and -running, shown
	// beside the files otherwise.
	var acts []daemon.ArmActivity
	known := true
	c, err := dial()
	if err == nil {
		path := "/v1/arms"
		if *running {
			path += "?running=1"
		}
		err = c.do(ctx, "GET", path, nil, &acts)
		var ae *apiErr
		if errors.As(err, &ae) && ae.Status == 404 {
			err = errors.New("this daemon is older than the list of arms it runs; deploy the new build")
		}
	}
	if err != nil {
		if only {
			return err
		}
		known = false
		fmt.Fprintf(os.Stderr, "RUNNING and QUEUED are not known: %v\n", err)
	}
	var files []armRow
	if !only {
		dir, err := p.arms()
		if err != nil {
			return err
		}
		paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			name := strings.TrimSuffix(filepath.Base(path), ".toml")
			a, err := config.LoadArm(dir, name)
			if err != nil {
				files = append(files, armRow{Name: name, Problem: firstLine(err.Error())})
				continue
			}
			if a.Template {
				continue
			}
			files = append(files, armRow{Name: name, ID: a.ID, Model: a.Model.Model, Endpoint: a.Endpoint, SDK: a.Harness.SDKRelease})
		}
	}
	rows := mergeArms(files, acts, only)
	if *p.json {
		return printJSON(rows)
	}
	w := table()
	fmt.Fprintln(w, "ARM\tID\tRUNNING\tQUEUED\tLANES\tMODEL\tENDPOINT\tSDK\tPROBLEM")
	for _, r := range rows {
		id := r.ID
		if len(id) > 12 {
			id = id[:12]
		}
		run, queued := "-", "-"
		if known {
			run, queued = fmt.Sprint(r.Running), fmt.Sprint(r.Queued)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, id, run, queued, strings.Join(r.Lanes, ","), r.Model, r.Endpoint, r.SDK, r.Problem)
	}
	return w.Flush()
}

// mergeArms puts the daemon's activity on the arm files' rows, matched by
// name and id. Activity no file row has is a row of its own: an arm whose
// file says another arm now (its runs started with, or were queued as, the
// earlier one), or one whose file is gone. only lists the activity alone.
func mergeArms(files []armRow, acts []daemon.ArmActivity, only bool) []armRow {
	rows := slices.Clone(files)
	at := map[string]int{}
	for i, r := range rows {
		at[r.Name+"\x00"+r.ID] = i
	}
	for _, x := range acts {
		i, ok := at[x.Arm+"\x00"+x.ArmID]
		if !ok {
			r := armRow{Name: x.Arm, ID: x.ArmID, Model: x.Model, Endpoint: x.Endpoint, SDK: x.SDK, Problem: x.Problem}
			if r.Problem == "" && !only {
				r.Problem = "not the arm its file says now"
			}
			rows = append(rows, r)
			i = len(rows) - 1
		}
		r := &rows[i]
		r.Running, r.Queued = x.Running, x.Queued
		r.RunningRuns, r.Lanes, r.Batches = x.RunningRuns, x.Lanes, x.Batches
	}
	if rows == nil {
		rows = []armRow{}
	}
	return rows
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// endpointShow is one endpoint as configured. Its address is printed only
// when asked: a command's output ends up in reports and issues.
func endpointShow(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("endpoint show", flag.ExitOnError)
	p := addPlace(fs)
	address := fs.Bool("address", false, "also print the address")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum endpoint show <endpoint> [--address]")
	}
	l, err := p.local()
	if err != nil {
		return err
	}
	e := l.Endpoints[rest[0]]
	if e == nil {
		return fmt.Errorf("there is no endpoint %s", rest[0])
	}
	var lanes []string
	for name, ln := range l.Lanes {
		for _, n := range ln.Endpoints {
			if n == e.Name {
				lanes = append(lanes, name)
			}
		}
	}
	sort.Strings(lanes)
	v := struct {
		Name         string          `json:"name"`
		Kind         string          `json:"kind"`
		Host         string          `json:"host"`
		Capacity     int             `json:"capacity"`
		Key          bool            `json:"api_key"`
		UnloadOthers bool            `json:"unload_others"`
		Hardware     config.Hardware `json:"hardware"`
		Lanes        []string        `json:"lanes"`
		Address      string          `json:"address,omitempty"`
	}{e.Name, e.Kind, e.Host, e.Capacity, e.APIKeyFile != "", e.UnloadOthers, e.Hardware, lanes, ""}
	if *address {
		v.Address = e.BaseURL
	}
	if *p.json {
		return printJSON(v)
	}
	fmt.Printf("endpoint %s: %s on %s, %d at once, api key %v, unload others %v\n", v.Name, v.Kind, v.Host, v.Capacity, v.Key, v.UnloadOthers)
	if hw := e.Hardware; hw.GPU != "" || hw.VRAMGB > 0 || hw.Notes != "" {
		fmt.Printf("  hardware (declared): %s, %g GB; %s\n", hw.GPU, hw.VRAMGB, hw.Notes)
	}
	fmt.Printf("  lanes: %s\n", strings.Join(lanes, ", "))
	if *address {
		fmt.Printf("  address: %s\n", e.BaseURL)
	}
	return nil
}

// endpointModels lists the models an endpoint serves, as it says.
func endpointModels(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("endpoint models", flag.ExitOnError)
	p := addPlace(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum endpoint models <endpoint>")
	}
	e, key, err := endpointOf(p, rest[0])
	if err != nil {
		return err
	}
	models, err := listModels(ctx, e, key)
	if err != nil {
		return err
	}
	if *p.json {
		return printJSON(models)
	}
	for _, m := range models {
		fmt.Println(m)
	}
	return nil
}

// endpointProbe asks an endpoint what it is, as a run does before it
// starts: the server's version and, with --model, the model's digest,
// template hash, parameters and residency. It generates nothing.
func endpointProbe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("endpoint probe", flag.ExitOnError)
	p := addPlace(fs)
	model := fs.String("model", "", "the model to ask about; without it the endpoint lists what it serves")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum endpoint probe <endpoint> [--model M]")
	}
	e, key, err := endpointOf(p, rest[0])
	if err != nil {
		return err
	}
	start := time.Now()
	if *model == "" {
		models, err := listModels(ctx, e, key)
		if err != nil {
			return err
		}
		if *p.json {
			return printJSON(map[string]any{"endpoint": e.Name, "answers": true, "models": models, "took_ms": time.Since(start).Milliseconds()})
		}
		fmt.Printf("endpoint %s answers in %s and serves %d models: %s\n", e.Name, human.Duration(time.Since(start)), len(models), strings.Join(models, ", "))
		return nil
	}
	hc := &http.Client{Timeout: 2 * time.Minute}
	id, err := run.Probe(ctx, hc, e, key, *model)
	if err != nil {
		return err
	}
	if *p.json {
		return printJSON(id)
	}
	fmt.Printf("endpoint %s (%s), server %s, answered in %s\n", e.Name, id.Kind, id.ServerVersion, human.Duration(time.Since(start)))
	fmt.Printf("  model %s, digest %s\n", id.Model, id.Digest)
	if id.TemplateSHA256 != "" {
		fmt.Printf("  template sha256 %s (%s)\n", id.TemplateSHA256, human.Bytes(int64(id.TemplateBytes)))
	}
	if id.Parameters != "" {
		fmt.Printf("  parameters: %s\n", strings.ReplaceAll(strings.TrimSpace(id.Parameters), "\n", "; "))
	}
	if id.Loaded != nil {
		share := 0.0
		if id.Loaded.Size > 0 {
			share = float64(id.Loaded.SizeVRAM) / float64(id.Loaded.Size)
		}
		fmt.Printf("  loaded: %s, %s on the GPU, context %s\n", human.Bytes(id.Loaded.Size), human.Percent(share), human.Count(int64(id.Loaded.ContextLength)))
	} else {
		fmt.Println("  not loaded now")
	}
	if len(id.OtherModels) > 0 {
		fmt.Printf("  also in memory: %s\n", strings.Join(id.OtherModels, ", "))
	}
	if len(id.NotProbed) > 0 {
		fmt.Printf("  this kind of endpoint cannot say: %s\n", strings.Join(id.NotProbed, ", "))
	}
	return nil
}

func endpointOf(p place, name string) (*config.Endpoint, string, error) {
	l, err := p.local()
	if err != nil {
		return nil, "", err
	}
	e := l.Endpoints[name]
	if e == nil {
		return nil, "", fmt.Errorf("there is no endpoint %s", name)
	}
	key, err := l.APIKey(e)
	return e, key, err
}

// listModels asks an endpoint for its models: Ollama's tags, or the
// OpenAI-compatible model list.
func listModels(ctx context.Context, e *config.Endpoint, key string) ([]string, error) {
	path, ollama := "/models", run.Ollama(e)
	if ollama {
		path = "/api/tags"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(e.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		// The address is private; say what failed, not where.
		return nil, fmt.Errorf("endpoint %s does not answer %s", e.Name, path)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoint %s: %s gave status %d", e.Name, path, resp.StatusCode)
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("endpoint %s: %s is not readable: %v", e.Name, path, err)
	}
	var out []string
	for _, m := range body.Models {
		out = append(out, m.Name)
	}
	for _, m := range body.Data {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out, nil
}

// taskVerify checks a task without running anything (030 §1): it loads
// and validates, each artefact is built with its pinned hash, and its
// oracle is proven for the task as it is now. `task build` builds what is
// missing; `oracle verify` proves the oracle.
func taskVerify(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("task verify", flag.ExitOnError)
	p := addPlace(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum task verify <task>")
	}
	t, err := task.Find(p.tasks(), rest[0])
	if err != nil {
		return err
	}
	env, err := taskEnv(p)
	if err != nil {
		return err
	}
	type check struct {
		What   string `json:"what"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	checks := []check{{What: "task.toml", OK: true, Detail: "valid; hash " + t.Hash}}
	for i := range t.Artefacts {
		a := &t.Artefacts[i]
		sum, err := task.HashFile(env.ArtefactPath(a))
		switch {
		case err != nil:
			checks = append(checks, check{What: "artefact " + a.Name, Detail: "not built: automaticum task build " + t.Name()})
		case sum != a.SHA256:
			checks = append(checks, check{What: "artefact " + a.Name, Detail: "built with sha256 " + sum + ", pinned " + a.SHA256})
		default:
			checks = append(checks, check{What: "artefact " + a.Name, OK: true, Detail: "sha256 " + a.SHA256})
		}
	}
	if pr := env.Proven(t); pr != nil {
		checks = append(checks, check{What: "oracle", OK: true, Detail: fmt.Sprintf("proven %s against %d fixtures, %d times", human.Time(pr.At), pr.Fixtures, pr.Repeats)})
	} else {
		checks = append(checks, check{What: "oracle", Detail: "not proven for the task as it is now: automaticum oracle verify " + t.Name()})
	}
	ok := true
	for _, c := range checks {
		ok = ok && c.OK
	}
	if *p.json {
		if err := printJSON(map[string]any{"task": t.Name(), "ok": ok, "checks": checks}); err != nil {
			return err
		}
	} else {
		for _, c := range checks {
			mark := "ok  "
			if !c.OK {
				mark = "FAIL"
			}
			fmt.Printf("%s %s: %s\n", mark, c.What, c.Detail)
		}
	}
	if !ok {
		return errFailed
	}
	return nil
}
