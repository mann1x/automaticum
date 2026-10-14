package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/gate"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/run"
)

// client talks to the daemon of this installation.
type client struct {
	base, token string
	json        bool
}

func addClient(fs *flag.FlagSet) (place, func() (*client, error)) {
	p := addPlace(fs)
	return p, func() (*client, error) {
		l, err := p.local()
		if err != nil {
			return nil, err
		}
		// The token is only needed to change something; a command that
		// reads works without the right to read the token file.
		token, _ := l.Token()
		return &client{base: "http://" + l.Daemon.Listen, token: token, json: *p.json}, nil
	}
}

// do sends a request and decodes the answer into out. A refusal comes back
// as an error holding the daemon's own words.
func (c *client) do(ctx context.Context, method, path string, in, out any) error {
	resp, err := c.open(ctx, method, path, in)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) open(ctx context.Context, method, path string, in any) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("the daemon does not answer; is automaticumd running?")
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return nil, errors.New(e.Error)
	}
	return resp, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return human.Since(t)
}

func status(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	_, dial := addClient(fs)
	parse(fs, args)
	c, err := dial()
	if err != nil {
		return err
	}
	var st daemon.Status
	if err := c.do(ctx, "GET", "/v1/status", nil, &st); err != nil {
		return err
	}
	if c.json {
		return printJSON(st)
	}
	state := "up"
	if st.Draining {
		state = "draining"
	}
	fmt.Printf("automaticumd %s, %s for %s\n\n", st.Version, state, ago(st.Started))
	w := table()
	fmt.Fprintln(w, "LANE\tHOST\tSTATE\tRUN\tARM\tTASK\tFOR")
	for _, l := range st.Lanes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", l.Lane, l.Host, l.State, l.RunID, l.Arm, l.Task, ago(l.Since))
	}
	w.Flush()
	for _, l := range st.Lanes {
		if l.Waiting != "" {
			fmt.Printf("\n%s waits: %s\n", l.Lane, l.Waiting)
		}
		if l.Drained && l.RunID != "" {
			fmt.Printf("\n%s is drained: %s finishes, then nothing else starts\n", l.Lane, l.RunID)
		}
	}
	waiting := 0
	for _, q := range st.Queue {
		if !q.Running {
			waiting++
		}
	}
	fmt.Printf("\n%d queued\n", waiting)
	if waiting > 0 {
		w = table()
		fmt.Fprintln(w, "RUN\tARM\tTASK\tLANE\tWAITING\tNOTE")
		for _, q := range st.Queue {
			if !q.Running {
				var note []string
				if q.Stopped != "" {
					note = append(note, q.Stopped)
				}
				if q.RetryOf != "" {
					note = append(note, "retries "+q.RetryOf)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", q.RunID, q.Arm, q.Task, q.Lane, ago(q.EnqueuedAt), strings.Join(note, "; "))
			}
		}
		w.Flush()
	}
	return nil
}

func runStart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run start", flag.ExitOnError)
	_, dial := addClient(fs)
	var req daemon.StartRequest
	var arms list
	fs.Var(&arms, "arm", "the arm; given several times, a batch of several arms with -n runs of each")
	fs.StringVar(&req.Order, "order", "", "with several arms: interleaved (the default, one run of each in turn) or blocks")
	fs.StringVar(&req.Task, "task", "", "the task")
	fs.StringVar(&req.Lane, "lane", "", "the lane; the first that may use the arm's endpoint when not given")
	fs.IntVar(&req.Count, "n", 1, "how many runs")
	fs.StringVar(&req.Key, "idempotency-key", "", "repeating the command with the same key starts nothing new")
	fs.BoolVar(&req.DryRun, "dry-run", false, "say what would start, start nothing")
	if rest := parse(fs, args); len(rest) > 0 || len(arms) == 0 || req.Task == "" {
		return errors.New("usage: automaticum run start --arm <arm> [--arm <arm> ...] --task <task> [-n 5] [--order interleaved|blocks] [--lane <lane>]")
	}
	req.Arms = arms
	c, err := dial()
	if err != nil {
		return err
	}
	var resp daemon.StartResponse
	if err := c.do(ctx, "POST", "/v1/runs", req, &resp); err != nil {
		return err
	}
	if c.json {
		return printJSON(resp)
	}
	what := "queued"
	switch {
	case resp.DryRun:
		what = "would be queued"
	case resp.Repeated:
		what = "already made by this key; nothing new was started"
	}
	fmt.Printf("%d %s on %s: arm %s, task %s, image %s, batch %s\n", len(resp.RunIDs), what, resp.Lane, strings.Join(arms, ", "), resp.Task, resp.Image, resp.Batch)
	for i, id := range resp.RunIDs {
		if i < len(resp.Order) {
			fmt.Printf("%s  %s\n", id, resp.Order[i])
			continue
		}
		fmt.Println(id)
	}
	return nil
}

func runLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run ls", flag.ExitOnError)
	_, dial := addClient(fs)
	q := url.Values{}
	batch := fs.String("batch", "", "only this batch")
	verdict := fs.String("verdict", "", "only this verdict")
	taskID := fs.String("task", "", "only this task id")
	imported := fs.Bool("imported", false, "only runs imported from the old harness")
	limit := fs.Int("limit", 30, "how many")
	parse(fs, args)
	for k, v := range map[string]string{"batch": *batch, "verdict": *verdict, "task": *taskID} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if *imported {
		q.Set("imported", "true")
	}
	q.Set("limit", fmt.Sprint(*limit))
	c, err := dial()
	if err != nil {
		return err
	}
	var runs []daemon.RunView
	if err := c.do(ctx, "GET", "/v1/runs?"+q.Encode(), nil, &runs); err != nil {
		return err
	}
	if c.json {
		return printJSON(runs)
	}
	w := table()
	fmt.Fprintln(w, "RUN\tSTATE\tVERDICT\tTASK\tMODEL\tSDK\tWALL\tREASON")
	for _, r := range runs {
		wall := ""
		if r.WallMS > 0 {
			wall = human.Millis(float64(r.WallMS))
		}
		reason := r.VerdictReason
		if len(reason) > 60 {
			reason = reason[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.State, r.Verdict, r.Task, r.Model, r.SDKRelease, wall, reason)
	}
	return w.Flush()
}

func runShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run show", flag.ExitOnError)
	_, dial := addClient(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum run show <run>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var r daemon.RunView
	if err := c.do(ctx, "GET", "/v1/runs/"+url.PathEscape(rest[0]), nil, &r); err != nil {
		return err
	}
	if c.json {
		return printJSON(r)
	}
	line := func(k, v string) {
		if v != "" {
			fmt.Printf("%-16s %s\n", k, v)
		}
	}
	line("run", r.ID)
	line("state", r.State)
	line("verdict", r.Verdict)
	line("reason", r.VerdictReason)
	line("verdict before", r.PriorVerdict)
	line("agent ended", r.AgentEnd)
	if r.Claimed != nil {
		line("claimed success", fmt.Sprint(*r.Claimed))
	}
	line("task", fmt.Sprintf("%s, version %d", r.Task, r.TaskVersion))
	line("arm", strings.TrimSpace(r.Arm+" "+r.ArmID))
	line("batch", r.Batch)
	line("retries", r.RetryOf)
	line("retried by", r.RetriedBy)
	if r.Host != "" {
		line("lane", r.Lane+" on "+r.Host)
	} else {
		line("lane", r.Lane)
	}
	line("endpoint", r.Endpoint)
	line("model", strings.TrimSpace(r.Provider+" "+r.Model))
	line("thinking", r.Thinking)
	if r.SDKRelease != "" {
		line("SDK release", r.SDKRelease)
	} else if r.State == "finished" && !r.Imported {
		line("SDK release", "not established")
	}
	if !r.StartedAt.IsZero() {
		line("started", human.Time(r.StartedAt))
	}
	if r.WallMS > 0 {
		wall := human.Millis(float64(r.WallMS))
		var held struct {
			MS int64 `json:"held_ms"`
		}
		if json.Unmarshal(r.Provenance, &held); r.Paused || held.MS > 0 {
			wall += fmt.Sprintf(" (paused for %s more, which is not counted)", human.Millis(float64(held.MS)))
		}
		line("wall", wall)
	}
	printInference(r)
	if len(r.Blobs) > 0 {
		fmt.Println("\nraw material:")
		w := table()
		for _, b := range r.Blobs {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", b.Kind, b.Name, human.Bytes(b.Size), b.Hash[:12])
		}
		w.Flush()
	}
	if len(r.Metrics) > 0 {
		fmt.Println("\nmetrics:")
		w := table()
		for _, m := range r.Metrics {
			fmt.Fprintf(w, "  %s\t%s\n", m.Name, human.Metric(m.Name, m.Value))
		}
		w.Flush()
	}
	if len(r.Findings) > 0 {
		fmt.Println("\nfindings:")
		for _, f := range r.Findings {
			fmt.Printf("  %s\n", findingLine(f))
		}
	}
	if len(r.Provenance) > 0 {
		var buf bytes.Buffer
		json.Indent(&buf, r.Provenance, "", "  ")
		fmt.Printf("\nhow it was made:\n%s\n", buf.String())
	}
	fmt.Printf("\nThe resolved arm: automaticum run show %s --json. The first request as sent: automaticum logs %s --wire.\n", r.ID, r.ID)
	return nil
}

// runTail prints what a run in flight prints, as it arrives.
func runTail(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run tail", flag.ExitOnError)
	_, dial := addClient(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum run tail <run>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	resp, err := c.open(ctx, "GET", "/v1/runs/"+url.PathEscape(rest[0])+"/stream", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rd := bufio.NewReaderSize(resp.Body, 1<<20)
	for {
		line, err := rd.ReadString('\n')
		if data, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "data: "); ok && data != "{}" {
			fmt.Println(data)
		}
		if err != nil {
			return nil
		}
	}
}

func runPost(verb, done string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("run "+verb, flag.ExitOnError)
		_, dial := addClient(fs)
		reason := fs.String("reason", "", "why (needed to mark a run invalid)")
		rest := parse(fs, args)
		if len(rest) != 1 {
			return fmt.Errorf("usage: automaticum run %s <run>", verb)
		}
		c, err := dial()
		if err != nil {
			return err
		}
		var in any
		if verb == "invalidate" {
			in = map[string]string{"reason": *reason}
		}
		var out map[string]any
		if err := c.do(ctx, "POST", "/v1/runs/"+url.PathEscape(rest[0])+"/"+verb, in, &out); err != nil {
			return err
		}
		if c.json {
			return printJSON(out)
		}
		fmt.Printf("%s: %s\n", rest[0], done)
		return nil
	}
}

// logs prints one kind of a run's raw material.
func logs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	_, dial := addClient(fs)
	kinds := map[string]*bool{
		"events":        fs.Bool("events", false, "the event stream (the default)"),
		"wire":          fs.Bool("wire", false, "every request and response as sent"),
		"runner_stderr": fs.Bool("runner", false, "what the runner wrote to stderr"),
		"oracle":        fs.Bool("oracle", false, "the oracle's output"),
	}
	kind := fs.String("kind", "", "any other kind, as `run show` lists them")
	name := fs.String("name", "", "the name, when a run has several of one kind")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum logs <run> [--events|--wire|--runner|--oracle|--kind K]")
	}
	chosen := *kind
	for k, set := range kinds {
		if *set {
			chosen = k
		}
	}
	if chosen == "" {
		chosen = "events"
	}
	c, err := dial()
	if err != nil {
		return err
	}
	path := "/v1/runs/" + url.PathEscape(rest[0]) + "/blobs/" + url.PathEscape(chosen)
	if *name != "" {
		path += "?name=" + url.QueryEscape(*name)
	}
	resp, err := c.open(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}

// findingLine says one finding in one line.
func findingLine(f daemon.FindingView) string {
	head := fmt.Sprintf("%-8s %s", f.Severity, f.Detector)
	switch f.Detector {
	case ingest.WireArm:
	case ingest.ModelIdentity:
		var e struct {
			Tag       string   `json:"tag"`
			NotStated []string `json:"not_stated"`
		}
		json.Unmarshal(f.Evidence, &e)
		return fmt.Sprintf("%s  the model's identity is not on record: %s of %s not stated; add it to the model catalogue (models/)", head, strings.Join(e.NotStated, ", "), e.Tag)
	case ingest.ModelReloaded:
		return fmt.Sprintf("%s  the server loaded the model again %d times after the run's first request, first at request #%d: %s", head, f.Count, f.Turn, f.Evidence)
	case ingest.GPUResidency:
		var e struct {
			Size     int64   `json:"size"`
			SizeVRAM int64   `json:"size_vram"`
			Share    float64 `json:"share"`
		}
		json.Unmarshal(f.Evidence, &e)
		return fmt.Sprintf("%s  the model ended the run only partly on the GPU: %s (%s of %s)", head, human.Percent(e.Share), human.Bytes(e.SizeVRAM), human.Bytes(e.Size))
	case detect.RepeatedToolCall:
		var c detect.RepeatedCall
		json.Unmarshal(f.Evidence, &c)
		return fmt.Sprintf("%s  %s called %d times in a row with the same arguments, from turn %d: %s", head, c.Tool, c.Count, c.Turn, c.Input)
	case detect.ShortUnitRepeat, detect.CharRun, detect.SentenceRepeat, detect.Runaway, detect.ReasoningRepetition, detect.ReasoningLoop:
		var fire ingest.Firing
		json.Unmarshal(f.Evidence, &fire)
		how := ""
		if fire.Matching == detect.FirstMatch {
			how = " [older matching: first match only]"
		}
		return fmt.Sprintf("%s  %d of %d %s blocks, first in turn %d: %s%s", head, fire.Blocks, fire.Of, fire.Channel, f.Turn, hitLine(fire.First), how)
	default:
		return fmt.Sprintf("%s  turn %d, %d times  %s", head, f.Turn, f.Count, f.Evidence)
	}
	var d ingest.Deviation
	if json.Unmarshal(f.Evidence, &d) != nil {
		return fmt.Sprintf("%s  %s", head, f.Evidence)
	}
	where := fmt.Sprintf("%d of %d requests, first #%d", d.Requests, d.Of, d.FirstSeq)
	effective := ""
	if d.Effective != "" {
		effective = fmt.Sprintf("; in effect: %s (%s)", d.Effective, d.Source)
	}
	switch d.Kind {
	case ingest.StatedNotSent:
		sent := "nothing"
		if len(d.Sent) > 0 {
			sent = string(d.Sent)
		}
		return fmt.Sprintf("%s  stated, not sent: %s = %s, sent %s as %s%s (%s)", head, d.Arm, d.Stated, d.Field, sent, effective, where)
	case ingest.SentNotStated:
		return fmt.Sprintf("%s  sent, not stated: %s = %s (%s)", head, d.Field, d.Sent, where)
	case ingest.Differs:
		return fmt.Sprintf("%s  differs: %s = %s, sent %s = %s (%s)", head, d.Arm, d.Stated, d.Field, d.Sent, where)
	case ingest.Drift:
		return fmt.Sprintf("%s  changed during the run: %s took %s (%s)", head, d.Field, strings.Join(d.Values, ", "), where)
	}
	return fmt.Sprintf("%s  %s", head, f.Evidence)
}

// reanalyse derives metrics, tool calls and findings again from what is
// stored.
func reanalyse(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("reanalyse", flag.ExitOnError)
	_, dial := addClient(fs)
	var req daemon.ReanalyseRequest
	fs.StringVar(&req.Run, "run", "", "one run")
	fs.StringVar(&req.Batch, "batch", "", "the runs of one batch")
	fs.BoolVar(&req.Imported, "imported", false, "also the imported runs, which get the detectors' findings only")
	if rest := parse(fs, args); len(rest) != 0 {
		return errors.New("usage: automaticum reanalyse [--run <run> | --batch <batch>] [--imported]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var st daemon.ReanalyseStatus
	if err := c.do(ctx, "POST", "/v1/reanalyse", req, &st); err != nil {
		return err
	}
	// The daemon does the work; this only watches it.
	shown := time.Now()
	for st.Running {
		select {
		case <-ctx.Done():
			fmt.Printf("the reanalysis goes on in the daemon: %d of %d runs done\n", st.Done, st.Total)
			return nil
		case <-time.After(time.Second):
		}
		if err := c.do(ctx, "GET", "/v1/reanalyse", nil, &st); err != nil {
			if ctx.Err() != nil {
				continue
			}
			return err
		}
		if !c.json && st.Running && time.Since(shown) >= 30*time.Second {
			fmt.Printf("%d of %d runs\n", st.Done, st.Total)
			shown = time.Now()
		}
	}
	if c.json {
		return printJSON(st)
	}
	fmt.Printf("%d runs analysed again in %s\n", st.Done-len(st.Failed), human.Duration(st.Ended.Sub(st.Started)))
	for id, why := range st.Failed {
		fmt.Printf("  %s: %s\n", id, why)
	}
	if len(st.Failed) > 0 {
		return fmt.Errorf("%d runs could not be analysed", len(st.Failed))
	}
	return nil
}

// hitLine says what a detector counted.
func hitLine(h detect.Hit) string {
	switch h.Detector {
	case detect.Runaway:
		return human.Count(int64(h.Count)) + " characters"
	case detect.CharRun:
		return fmt.Sprintf("%q %d times in a row at character %d", h.Unit, h.Count, h.Offset)
	case detect.ReasoningRepetition:
		return fmt.Sprintf("one paragraph %d times, %.0f%% of the block's paragraphs duplicates: %q", h.Count, h.Share*100, h.Unit)
	case detect.ReasoningLoop:
		switch h.Kind {
		case detect.LowUniqueness:
			return fmt.Sprintf("%d distinct lines in a window of 60: %q", h.Count, h.Unit)
		case detect.LineCycle:
			return fmt.Sprintf("a cycle of %d lines, %d times: %q", h.Period, h.Count, h.Unit)
		}
		return fmt.Sprintf("a phrase of %d characters, %d times in one unbroken line: %q", h.Period, h.Count, h.Unit)
	case detect.SentenceRepeat:
		return fmt.Sprintf("%d times, %.0f%% of the block's sentences: %q", h.Count, h.Share*100, h.Unit)
	}
	return fmt.Sprintf("%q %d times in a row at character %d", h.Unit, h.Count, h.Offset)
}

// detectCmd runs the detector library: on its own controls, on a text file
// taken as one block, or on a stored run.
func detectCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("detect", flag.ExitOnError)
	p, dial := addClient(fs)
	self := fs.Bool("self-test", false, "run the library's controls")
	size := fs.Int("runaway", 0, "the block size, in characters, that counts as a runaway (a file only; 0 reports none)")
	first := fs.Bool("first-match", false, "a file only: look at the first repeat alone, as the older reference code did (a blind spot; for reproducing older counts)")
	rest := parse(fs, args)
	if *self {
		results, ok := detect.SelfTest()
		if *p.json {
			if err := printJSON(results); err != nil {
				return err
			}
		} else {
			w := table()
			fmt.Fprintln(w, "CONTROL\tDOMAIN\tMUST FIRE\tFIRED\t")
			for _, r := range results {
				mark := "ok"
				if !r.OK {
					mark = "WRONG"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Control, r.Domain, orNone(r.Want), orNone(r.Got), mark)
			}
			w.Flush()
			fmt.Printf("detector library version %s\n", detect.Version)
		}
		if !ok {
			return errors.New("a detector did not give its known answer on a control")
		}
		return nil
	}
	if len(rest) != 1 {
		return errors.New("usage: automaticum detect <file|run> [--runaway N], or automaticum detect --self-test")
	}
	if text, err := os.ReadFile(rest[0]); err == nil {
		opt := detect.Options{RunawayChars: *size, FirstMatch: *first}
		hits := detect.Block(string(text), opt)
		if *p.json {
			if hits == nil {
				hits = []detect.Hit{}
			}
			return printJSON(hits)
		}
		how := "every match (the reference)"
		if opt.FirstMatch {
			how = "first match only (the older way, with its blind spot)"
		}
		fmt.Printf("%s: one block of %d characters; detector library version %s, matching: %s\n", rest[0], len([]rune(string(text))), detect.Version, how)
		for _, h := range hits {
			fmt.Printf("  %-18s %s\n", h.Detector, hitLine(h))
		}
		if len(hits) == 0 {
			fmt.Println("  no detector fired")
		}
		return nil
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.DetectView
	if err := c.do(ctx, "GET", "/v1/runs/"+url.PathEscape(rest[0])+"/detect", nil, &v); err != nil {
		return fmt.Errorf("%s is not a readable file, and as a run: %w", rest[0], err)
	}
	if c.json {
		return printJSON(v)
	}
	fmt.Printf("run %s: %d blocks, %d tool calls, detector library version %s\n", v.Run, v.Blocks, v.Calls, v.Version)
	for _, h := range v.Hits {
		fmt.Printf("  turn %-3d %-9s %-18s %s\n", h.Turn, h.Channel, h.Detector, hitLine(h.Hit))
	}
	for _, r := range v.Repeated {
		fmt.Printf("  turn %-3d %-9s %-18s %s %d times in a row: %s\n", r.Turn, "calls", detect.RepeatedToolCall, r.Tool, r.Count, r.Input)
	}
	if len(v.Hits)+len(v.Repeated) == 0 {
		fmt.Println("  no detector fired")
	}
	return nil
}

func orNone(v []string) string {
	if len(v) == 0 {
		return "-"
	}
	return strings.Join(v, ", ")
}

// doctor asks every gate now, without starting a run. It fails when a gate
// refuses.
func doctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	_, dial := addClient(fs)
	q := url.Values{}
	lane := fs.String("lane", "", "one lane")
	arm := fs.String("arm", "", "one arm; without it, every arm a lane may run")
	tk := fs.String("task", "", "one task")
	all := fs.Bool("all", false, "show every gate's answer, not only those that stand in the way or could not look")
	if rest := parse(fs, args); len(rest) != 0 {
		return errors.New("usage: automaticum doctor [--lane L] [--arm A] [--task T] [--all]")
	}
	for k, v := range map[string]string{"lane": *lane, "arm": *arm, "task": *tk} {
		if v != "" {
			q.Set(k, v)
		}
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var entries []daemon.DoctorEntry
	if err := c.do(ctx, "GET", "/v1/doctor?"+q.Encode(), nil, &entries); err != nil {
		return err
	}
	if c.json {
		return printJSON(entries)
	}
	refused, waits := 0, 0
	for _, e := range entries {
		counts := map[gate.Status]int{}
		for _, g := range e.Gates {
			counts[g.Status]++
		}
		refused += counts[gate.Refuse]
		waits += counts[gate.Wait]
		fmt.Printf("%s  %s  %s: %d pass, %d refuse, %d wait, %d not looked at\n", e.Lane, e.Arm, e.Task,
			counts[gate.Pass], counts[gate.Refuse], counts[gate.Wait], counts[gate.Unchecked])
		w := table()
		for _, g := range e.Gates {
			if *all || g.Status != gate.Pass {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", g.Status, g.Gate, g.Detail)
			}
		}
		w.Flush()
	}
	if len(entries) == 0 {
		fmt.Println("nothing to ask: no lane, arm and task match")
	}
	switch {
	case refused > 0:
		return fmt.Errorf("%d gates refuse", refused)
	case waits > 0:
		fmt.Printf("%d gates say wait: a run would stay queued\n", waits)
	}
	return nil
}

// printInference says where and on what a run's model ran, each fact with
// where it comes from, and the sampling parameters that were in effect.
func printInference(r daemon.RunView) {
	var p struct {
		Inference      *run.Inference `json:"inference"`
		EndpointBefore *run.Identity  `json:"endpoint_before"`
	}
	if json.Unmarshal(r.Provenance, &p) != nil || (p.Inference == nil && p.EndpointBefore == nil) {
		return
	}
	or := func(v, missing string) string {
		if strings.TrimSpace(v) == "" {
			return missing
		}
		return v
	}
	fmt.Println("\nwhere and what:")
	w := table()
	row := func(k, v, source string) { fmt.Fprintf(w, "  %s\t%s\t%s\n", k, v, source) }
	inf := p.Inference
	if inf == nil {
		// A run made before this was recorded: only what the server said.
		e := p.EndpointBefore
		row("inference point", strings.TrimSpace(r.Endpoint+" ("+e.Kind+" "+e.ServerVersion+")"), "server")
		row("endpoint host", "not recorded", "")
		row("GPU", "not recorded", "")
		row("model tag", e.Model, "arm")
		row("model, by the server", string(e.Details), "server")
		row("model, stated", "not recorded", "")
		w.Flush()
	} else {
		point := inf.Endpoint + " (" + inf.Kind + " " + inf.ServerVersion
		if inf.Engine != "" {
			point += ", engine " + inf.Engine
		}
		row("inference point", point+")", "server")
		if l := inf.AddedLater; l != nil {
			row("added to the record", human.Time(l.At), l.Source)
		}
		row("endpoint host", or(inf.Host, "not declared"), "configuration")
		row("lane host", inf.LaneHost, "configuration")
		gpu := or(inf.GPU.Declared, "not declared")
		if inf.GPU.VRAMGB > 0 {
			gpu += fmt.Sprintf(", %s GB", human.Number(inf.GPU.VRAMGB))
		}
		row("GPU", gpu, "declared in the configuration, not measured")
		if inf.GPU.Share != nil {
			row("model on the GPU", human.Percent(*inf.GPU.Share), "server, after the run")
		}
		row("model tag", inf.Model.Tag, "arm")
		var said struct {
			Quantization string `json:"quantization_level"`
			Size         string `json:"parameter_size"`
		}
		if json.Unmarshal(inf.Model.Server, &said) == nil && said.Quantization != "" {
			row("quantization", strings.TrimSpace(said.Quantization+" "+said.Size), "server, at the start of this run")
		}
		if len(inf.Model.Digest) >= 12 {
			row("model digest", inf.Model.Digest[:12], "server")
		}
		if st := inf.Model.Stated; st != nil {
			from := "catalogue"
			if len(st.Queried) > 0 {
				from = "weights file (" + strings.Join(st.Queried, ", ") + "), read through the server"
			}
			row("model family", or(strings.TrimSpace(st.Family+" "+st.Size), "not stated"), from)
			row("model variant", or(st.Variant, "not stated"), "catalogue")
			if st.Lineage != "" {
				row("lineage", st.Lineage, "catalogue")
			}
		} else {
			row("model family", "not stated", "the catalogue has no entry for this tag")
			row("model variant", "not stated", "")
		}
		var srv struct {
			Family string `json:"family"`
			Size   string `json:"parameter_size"`
			Quant  string `json:"quantization_level"`
			Format string `json:"format"`
		}
		if json.Unmarshal(inf.Model.Server, &srv) == nil && srv.Family != "" {
			row("model, by the server", strings.TrimSpace(srv.Family+" "+srv.Size+" "+srv.Quant+" "+srv.Format), "server")
		}
		wt := inf.Model.Weights
		if base := fmt.Sprint(wt["general.base_model.0.name"]); wt["general.base_model.0.name"] != nil {
			text := base
			if by, ok := wt["general.quantized_by"].(string); ok && by != "" {
				text += ", quantized by " + by
			}
			if n, ok := wt["general.parameter_count"].(float64); ok {
				text += ", " + human.Count(int64(n)) + " parameters"
			}
			row("model, by the weights file", text, "weights file")
		}
		w.Flush()
	}
	if len(r.Settings) > 0 {
		fmt.Println("\nsampling and thinking, as in effect:")
		w := table()
		fmt.Fprintln(w, "  PARAMETER\tIN EFFECT\tWHY\tARM STATES\tSENT\tTAG'S OWN\tNOTE")
		dash := func(v string) string { return or(v, "-") }
		for _, s := range r.Settings {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.Name, s.Effective, s.Source, dash(s.Stated), dash(s.Sent), dash(s.Tag), s.Note)
		}
		w.Flush()
	}
}
