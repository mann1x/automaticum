package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/ingest"
)

const detectUsage = `usage: automaticum detect [<detector>...] [<run>...] [--batch B] [--cohort C] [--campaign X]
       [--lane L] [--arm A] [--task T] [--suite S] [--model M] [--verdict V] [--since T] [--until T] [--all]
       [--detector D] [--metric M] [--group AXIS] [--firings] [--calls] [--quiet]
       [--fail-if "EXPR"] [--fail-scope total|run|AXIS] [--record] [--finished] [--imported] [--json]
   or: automaticum detect <file> [--runaway N] [--sdk R] [--first-match]
   or: automaticum detect --self-test`

// detectCmd runs the detector library: on its own controls, on a text file
// taken as one block, or again on any set of stored runs and runs in
// flight (070 §3.3).
func detectCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("detect", flag.ExitOnError)
	p, dial := addClient(fs)
	self := fs.Bool("self-test", false, "run the library's controls")
	size := fs.Int("runaway", 0, "the block size, in characters, that counts as a runaway (a file only; 0 reports none)")
	sdkRel := fs.String("sdk", "", "a file only: the SDK release whose reasoning-loop rules to read with (4.100.258 on have the block cycle); empty reads with the 4.100.250 rules")
	first := fs.Bool("first-match", false, "a file only: look at the first repeat alone, as the older reference code did (a blind spot; for reproducing older counts)")
	var runs, batches, cohorts, campaigns, lanes, arms, tasks, suites, models, verdicts, detectors, metrics, groups, failIf list
	fs.Var(&runs, "run", "a run (any number; a run may also be given bare)")
	fs.Var(&batches, "batch", "a batch, or a glob of batches (k-gate-*)")
	fs.Var(&cohorts, "cohort", "a cohort's batches (a glob)")
	fs.Var(&campaigns, "campaign", "a campaign's batches (a glob)")
	fs.Var(&lanes, "lane", "only runs on this lane (a glob)")
	fs.Var(&arms, "arm", "only runs of this arm, by name or id (a glob)")
	fs.Var(&tasks, "task", "only runs of this task, suite/task or task (a glob)")
	fs.Var(&suites, "suite", "only runs of this suite (a glob)")
	fs.Var(&models, "model", "only runs of this model (a glob; leaves out runs in flight)")
	fs.Var(&verdicts, "verdict", "only runs with this verdict (leaves out runs in flight)")
	fs.Var(&detectors, "detector", "only this detector (any number; a detector may also be given bare); default every one")
	fs.Var(&metrics, "metric", "also this metric, a name, a prefix or a glob (any number); summed and averaged in the totals")
	fs.Var(&groups, "group", "totals by: batch, arm, arm_id, cohort, campaign, lane, task, model, state, verdict, arm.<field>, label.<label> or finding.<detector> (any number; default batch, arm and cohort)")
	fs.Var(&failIf, "fail-if", `exit 1 when this holds, e.g. "swallowed_argument>0", "runs_with.reasoning_loop>=2", "share.swallowed_argument>0.1", "failed>10", "metric.tool_calls.failed>5" (any number; any one holding fails)`)
	scope := fs.String("fail-scope", "total", "what --fail-if is tested on: total (all runs together), run (each run), or a totals axis (each group)")
	since := fs.String("since", "", "only runs started from this date (2026-11-01) or time (RFC 3339)")
	until := fs.String("until", "", "only runs started before this date or time")
	all := fs.Bool("all", false, "every run, when nothing else names or narrows them")
	firings := fs.Bool("firings", false, "each firing: its finding's evidence, and every call with a swallowed argument")
	calls := fs.Bool("calls", false, "each run's calls by tool: failed (by error class), refused, unreadable, control-token argument names")
	quiet := fs.Bool("quiet", false, "the totals only, no line per run")
	finished := fs.Bool("finished", false, "leave out runs in flight")
	imported := fs.Bool("imported", false, "also the imported runs of the older harness")
	record := fs.Bool("record", false, "also store the new analysis on the finished runs, as reanalyse does (needs the daemon's token)")
	rest := parse(fs, args)
	if *self {
		return detectSelfTest(*p.json)
	}
	selectors := len(runs)+len(batches)+len(cohorts)+len(campaigns)+len(lanes)+len(arms)+len(tasks)+len(suites)+len(models)+len(verdicts)+len(detectors) > 0 || *since != "" || *until != "" || *all
	if len(rest) == 1 && !selectors {
		if text, err := os.ReadFile(rest[0]); err == nil {
			opt := detect.Options{RunawayChars: *size, FirstMatch: *first}
			if *sdkRel != "" {
				opt.Rules = detect.RulesFor(*sdkRel)
			}
			return detectFile(rest[0], string(text), opt, *p.json)
		}
	}
	known := ingest.Detectors()
	for _, a := range rest {
		if slices.Contains(known, a) {
			detectors = append(detectors, a)
		} else {
			runs = append(runs, a)
		}
	}
	if len(runs)+len(batches)+len(cohorts)+len(campaigns)+len(lanes)+len(arms)+len(tasks)+len(suites)+len(models)+len(verdicts) == 0 && *since == "" && *until == "" && !*all {
		return errors.New(detectUsage)
	}
	tests, err := parseFailIf(failIf)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		groups = list{"batch", "arm", "cohort"}
	}
	daemonAxis := ""
	for _, g := range groups {
		if !slices.Contains(rowAxes, g) {
			if daemonAxis != "" {
				return fmt.Errorf("--group: one of arm.<field>, label.<label> and finding.<detector> at a time (%s and %s)", daemonAxis, g)
			}
			daemonAxis = g
		}
	}
	if *scope != "total" && *scope != "run" && !slices.Contains(groups, *scope) {
		return fmt.Errorf("--fail-scope %q: total, run, or one of the --group axes (%s)", *scope, strings.Join(groups, ", "))
	}
	for _, t := range tests {
		if m, ok := strings.CutPrefix(t.name, "metric."); ok && !slices.Contains(metrics, m) {
			metrics = append(metrics, m)
		}
		if d := strings.TrimPrefix(strings.TrimPrefix(t.name, "runs_with."), "share."); slices.Contains(known, d) && len(detectors) > 0 && !slices.Contains(detectors, d) {
			return fmt.Errorf("--fail-if %q: %s is not among the detectors asked", t.text, d)
		}
	}
	q := url.Values{}
	for k, l := range map[string]list{"run": runs, "batch": batches, "cohort": cohorts, "campaign": campaigns, "lane": lanes, "arm": arms, "task": tasks,
		"suite": suites, "model": models, "verdict": verdicts, "detector": detectors, "metric": metrics} {
		for _, v := range l {
			q.Add(k, v)
		}
	}
	for k, v := range map[string]string{"since": *since, "until": *until, "group": daemonAxis} {
		if v != "" {
			q.Set(k, v)
		}
	}
	for k, v := range map[string]bool{"all": *all, "firings": *firings, "finished": *finished, "imported": *imported} {
		if v {
			q.Set(k, "1")
		}
	}
	c, err := dial()
	if err != nil {
		return err
	}
	method := "GET"
	if *record {
		method = "POST"
	}
	resp, err := c.open(ctx, method, "/v1/detect?"+q.Encode(), nil)
	if err != nil {
		var ae *apiErr
		if errors.As(err, &ae) && ae.Status == 404 {
			return errors.New("this daemon is older than `detect` over runs; deploy the new build")
		}
		return err
	}
	defer resp.Body.Close()
	out := detectOut{json: c.json, quiet: *quiet, firings: *firings, calls: *calls, detectors: detectors, groups: groups}
	dec := json.NewDecoder(resp.Body)
	for {
		var l daemon.DetectLine
		if err := dec.Decode(&l); err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("the daemon's answer broke off: %w", err)
		}
		switch {
		case l.Error != "":
			return errors.New(l.Error)
		case l.Header != nil:
			out.header(l.Header)
		case l.Run != nil:
			out.row(l.Run)
		case l.End != nil:
			out.end = l.End
		}
	}
	if out.end == nil {
		return errors.New("the daemon's answer broke off before its end")
	}
	held := out.finish(tests, *scope)
	if out.failed > 0 {
		return fmt.Errorf("%d of %d runs could not be read", out.failed, len(out.rows))
	}
	if len(held) > 0 {
		if !c.json {
			for _, h := range held {
				fmt.Fprintln(os.Stderr, "automaticum: --fail-if holds:", h)
			}
		}
		return errFailed
	}
	return nil
}

// detectSelfTest runs the library's controls.
func detectSelfTest(asJSON bool) error {
	results, ok := detect.SelfTest()
	if asJSON {
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

// detectFile reads a text file as one block.
func detectFile(name, text string, opt detect.Options, asJSON bool) error {
	hits := detect.Block(text, opt)
	if asJSON {
		if hits == nil {
			hits = []detect.Hit{}
		}
		return printJSON(hits)
	}
	how := "every match (the reference)"
	if opt.FirstMatch {
		how = "first match only (the older way, with its blind spot)"
	}
	fmt.Printf("%s: one block of %d characters; detector library version %s, matching: %s\n", name, len([]rune(text)), detect.Version, how)
	for _, h := range hits {
		fmt.Printf("  %-18s %s\n", h.Detector, hitLine(h))
	}
	if len(hits) == 0 {
		fmt.Println("  no detector fired")
	}
	return nil
}

// rowAxes are the totals axes the command groups by itself; any other is
// the daemon's (arm.<field>, label.<label>, finding.<detector>).
var rowAxes = []string{"batch", "arm", "arm_id", "cohort", "campaign", "lane", "task", "model", "state", "verdict"}

// axisValue is a run's value on a totals axis.
func axisValue(r *daemon.DetectRow, axis string) string {
	v := map[string]string{"batch": r.Batch, "arm": r.Arm, "arm_id": r.ArmID, "cohort": r.Cohort, "campaign": r.Campaign, "lane": r.Lane,
		"task": r.Task, "model": r.Model, "state": r.State, "verdict": r.Verdict}
	s, ok := v[axis]
	if !ok {
		s = r.Group
	}
	if s == "" {
		return "-"
	}
	return s
}

// failTest is one --fail-if: a name, a comparison and a number.
type failTest struct {
	text, name, op string
	value          float64
}

var failRE = regexp.MustCompile(`^\s*([A-Za-z0-9_.\-]+)\s*(>=|<=|==|!=|>|<)\s*([0-9]+(?:\.[0-9]+)?)\s*$`)

// failNames are the names --fail-if knows besides detectors and metrics.
var failNames = []string{"runs", "calls", "failed", "refused", "unreadable", "control_token_names", "not_offered", "partial"}

func parseFailIf(exprs list) ([]failTest, error) {
	known := ingest.Detectors()
	var out []failTest
	for _, e := range exprs {
		m := failRE.FindStringSubmatch(e)
		if m == nil {
			return nil, fmt.Errorf("--fail-if %q: write NAME OP NUMBER, OP one of > >= < <= == !=", e)
		}
		name := m[1]
		d := strings.TrimPrefix(strings.TrimPrefix(name, "runs_with."), "share.")
		if !slices.Contains(failNames, name) && !slices.Contains(known, d) && !strings.HasPrefix(name, "metric.") {
			return nil, fmt.Errorf("--fail-if %q: %s is not a detector (%s), runs_with.<detector>, share.<detector>, metric.<name> or one of %s",
				e, name, strings.Join(known, ", "), strings.Join(failNames, ", "))
		}
		v, _ := strconv.ParseFloat(m[3], 64)
		out = append(out, failTest{text: e, name: name, op: m[2], value: v})
	}
	return out, nil
}

// total is what a set of runs comes to.
type total struct {
	Value        string             `json:"value,omitempty"`
	Runs         int                `json:"runs"`
	Partial      int                `json:"partial,omitempty"`
	Calls        int                `json:"tool_calls"`
	Failed       int                `json:"failed"`
	Refused      int                `json:"refused"`
	Unreadable   int                `json:"unreadable_arguments"`
	ControlNames int                `json:"control_token_names"`
	NotOffered   int                `json:"not_offered"`
	Firings      map[string]int     `json:"firings"`
	RunsWith     map[string]int     `json:"runs_with"`
	ByTool       map[string]float64 `json:"by_tool,omitempty"`
	Errors       map[string]int     `json:"errors,omitempty"`
	Metrics      map[string]float64 `json:"metrics_sum,omitempty"`
	metricRuns   map[string]int
}

func newTotal(value string) *total {
	return &total{Value: value, Firings: map[string]int{}, RunsWith: map[string]int{}, Metrics: map[string]float64{}, metricRuns: map[string]int{}}
}

func (t *total) add(r *daemon.DetectRow) {
	if r.Error != "" {
		return
	}
	t.Runs++
	if r.Partial != "" {
		t.Partial++
	}
	for d, n := range r.Firings {
		t.Firings[d] += n
		if n > 0 {
			t.RunsWith[d]++
		}
	}
	for base, tools := range r.ByTool {
		for tool, v := range tools {
			if t.ByTool == nil {
				t.ByTool = map[string]float64{}
			}
			t.ByTool[base+"."+tool] += v
		}
	}
	for _, o := range r.Outcomes {
		t.Calls += o.Calls
		t.Failed += o.Failed
		t.Refused += o.Refused
		t.Unreadable += o.Unreadable
		t.ControlNames += o.ControlNames
		if o.NotOffered {
			t.NotOffered += o.Calls
		}
		for class, n := range o.Errors {
			if t.Errors == nil {
				t.Errors = map[string]int{}
			}
			t.Errors[o.Tool+": "+class] += n
		}
	}
	for k, v := range r.Metrics {
		t.Metrics[k] += v
		t.metricRuns[k]++
	}
}

// get is a --fail-if name's value on the total.
func (t *total) get(name string) float64 {
	switch name {
	case "runs":
		return float64(t.Runs)
	case "partial":
		return float64(t.Partial)
	case "calls":
		return float64(t.Calls)
	case "failed":
		return float64(t.Failed)
	case "refused":
		return float64(t.Refused)
	case "unreadable":
		return float64(t.Unreadable)
	case "control_token_names":
		return float64(t.ControlNames)
	case "not_offered":
		return float64(t.NotOffered)
	}
	if d, ok := strings.CutPrefix(name, "runs_with."); ok {
		return float64(t.RunsWith[d])
	}
	if d, ok := strings.CutPrefix(name, "share."); ok {
		if t.Runs == 0 {
			return 0
		}
		return float64(t.RunsWith[d]) / float64(t.Runs)
	}
	if m, ok := strings.CutPrefix(name, "metric."); ok {
		s := 0.0
		for k, v := range t.Metrics {
			if k == m || strings.HasPrefix(k, m+".") {
				s += v
			}
		}
		return s
	}
	return float64(t.Firings[name])
}

func (f failTest) holds(t *total) bool {
	v := t.get(f.name)
	switch f.op {
	case ">":
		return v > f.value
	case ">=":
		return v >= f.value
	case "<":
		return v < f.value
	case "<=":
		return v <= f.value
	case "==":
		return v == f.value
	}
	return v != f.value
}

// detectOut prints the answer as it comes, and the totals at its end.
type detectOut struct {
	json, quiet, firings, calls bool
	detectors, groups           []string
	head                        *daemon.DetectHeader
	rows                        []*daemon.DetectRow
	end                         *daemon.DetectEnd
	failed                      int
	printedHead                 bool
}

func (o *detectOut) header(h *daemon.DetectHeader) {
	o.head = h
	if o.json {
		return
	}
	in := ""
	if h.InFlight > 0 {
		in = fmt.Sprintf(" (%d in flight, read from what they printed so far)", h.InFlight)
	}
	which := "every detector"
	if len(o.detectors) > 0 {
		which = strings.Join(h.Detectors, ", ")
	}
	rec := "nothing is written"
	if h.Record {
		rec = "the new analysis is recorded on the finished runs"
	}
	fmt.Printf("detector library version %s, analysis version %d; %d runs%s; %s; %s\n", h.Library, h.Analysis, h.Runs, in, which, rec)
}

func (o *detectOut) row(r *daemon.DetectRow) {
	o.rows = append(o.rows, r)
	if r.Error != "" {
		o.failed++
	}
	if o.json || o.quiet {
		return
	}
	if !o.printedHead {
		fmt.Printf("%-30s %-10s %-8s %6s %6s  %s\n", "RUN", "STATE", "VERDICT", "CALLS", "FAILED", "FIRINGS")
		o.printedHead = true
	}
	if r.Error != "" {
		fmt.Printf("%-30s cannot be read: %s\n", r.Run, r.Error)
		return
	}
	failed := 0
	for _, t := range r.Outcomes {
		failed += t.Failed
	}
	state := r.State
	if r.Partial != "" {
		state += "*"
	}
	fmt.Printf("%-30s %-10s %-8s %6d %6d  %s\n", r.Run, state, orDash(r.Verdict), r.Calls, failed, firingsText(r))
	if r.Partial != "" {
		fmt.Printf("    * %s\n", r.Partial)
	}
	if o.calls {
		for _, t := range r.Outcomes {
			fmt.Printf("    %s: %s\n", t.Tool, outcomeText(t))
		}
	}
	for _, k := range slices.Sorted(maps.Keys(r.Metrics)) {
		fmt.Printf("    %s = %s\n", k, strconv.FormatFloat(r.Metrics[k], 'f', -1, 64))
	}
	if o.firings {
		for _, f := range r.Findings {
			ev := string(f.Evidence)
			if len(ev) > 300 {
				ev = ev[:300] + "…"
			}
			fmt.Printf("    turn %-4d %-22s %-8s %dx %s\n", f.Turn, f.Detector, f.Severity, max(1, f.Count), ev)
		}
		for _, s := range r.Swallowed {
			fmt.Printf("    turn %-4d %s: %s swallowed %s: %q\n", s.Turn, s.Tool, s.In, s.Swallowed, s.Around)
		}
	}
}

// firingsText is a run's firings: each detector that fired, or whose
// stored count differs, with what was stored and the tools it fired on.
func firingsText(r *daemon.DetectRow) string {
	var parts []string
	for _, d := range slices.Sorted(maps.Keys(r.Firings)) {
		n := r.Firings[d]
		s, stored := r.Stored[d]
		if n == 0 && (!stored || s == 0) {
			continue
		}
		p := fmt.Sprintf("%s=%d", d, n)
		if stored && s != n {
			p += fmt.Sprintf(" (stored %d)", s)
		}
		for base, tools := range r.ByTool {
			if strings.Contains(base, d) && n > 0 {
				var ts []string
				for _, t := range slices.Sorted(maps.Keys(tools)) {
					ts = append(ts, fmt.Sprintf("%s %s", t, strconv.FormatFloat(tools[t], 'f', -1, 64)))
				}
				p += " [" + strings.Join(ts, ", ") + "]"
			}
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "  ")
}

// outcomeText is one tool's calls in a run.
func outcomeText(t ingest.ToolOutcome) string {
	s := fmt.Sprintf("%d calls, %d failed", t.Calls, t.Failed)
	if len(t.Errors) > 0 {
		s += " (" + classesText(t.Errors, "") + ")"
	}
	s += fmt.Sprintf(", %d refused, %d unreadable, %d with control-token argument names", t.Refused, t.Unreadable, t.ControlNames)
	if t.NotOffered {
		s += ", never offered by a request"
	}
	return s
}

// classesText lists error classes, the most frequent first.
func classesText(m map[string]int, sep string) string {
	keys := slices.Collect(maps.Keys(m))
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] || m[keys[i]] == m[keys[j]] && keys[i] < keys[j] })
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", m[k], k))
	}
	if sep == "" {
		sep = "; "
	}
	return strings.Join(parts, sep)
}

// detectJSON is the whole answer, for --json.
type detectJSON struct {
	Header *daemon.DetectHeader `json:"header"`
	Runs   []*daemon.DetectRow  `json:"runs"`
	Total  *total               `json:"total"`
	Groups map[string][]*total  `json:"groups"`
	End    *daemon.DetectEnd    `json:"end"`
	Held   []string             `json:"fail_if_held,omitempty"`
}

// finish prints the totals and says which --fail-if tests hold, and where.
func (o *detectOut) finish(tests []failTest, scope string) []string {
	all := newTotal("all")
	groups := map[string][]*total{}
	for _, axis := range o.groups {
		by := map[string]*total{}
		for _, r := range o.rows {
			v := axisValue(r, axis)
			if by[v] == nil {
				by[v] = newTotal(v)
			}
			by[v].add(r)
		}
		for _, v := range slices.Sorted(maps.Keys(by)) {
			groups[axis] = append(groups[axis], by[v])
		}
	}
	for _, r := range o.rows {
		all.add(r)
	}
	var held []string
	check := func(where string, t *total) {
		for _, f := range tests {
			if f.holds(t) {
				held = append(held, fmt.Sprintf("%s on %s (%s)", f.text, where, strconv.FormatFloat(t.get(f.name), 'f', -1, 64)))
			}
		}
	}
	switch scope {
	case "total":
		check("all runs", all)
	case "run":
		for _, r := range o.rows {
			t := newTotal(r.Run)
			t.add(r)
			check("run "+r.Run, t)
		}
	default:
		for _, t := range groups[scope] {
			check(scope+" "+t.Value, t)
		}
	}
	if o.json {
		printJSON(detectJSON{Header: o.head, Runs: o.rows, Total: all, Groups: groups, End: o.end, Held: held})
		return held
	}
	// The detectors in the totals: those asked, else those that fired.
	shown := o.detectors
	if len(shown) == 0 {
		for _, d := range slices.Sorted(maps.Keys(all.Firings)) {
			if all.Firings[d] > 0 {
				shown = append(shown, d)
			}
		}
	}
	for _, axis := range o.groups {
		ts := groups[axis]
		if len(ts) == 0 || len(ts) == 1 && ts[0].Value == "-" {
			continue
		}
		fmt.Printf("\nby %s\n", axis)
		printTotals(ts, shown)
	}
	fmt.Println()
	printTotals([]*total{all}, shown)
	if len(all.Errors) > 0 {
		fmt.Printf("failed calls by error class: %s\n", classesText(all.Errors, "; "))
	}
	if all.ControlNames+all.Unreadable+all.NotOffered > 0 {
		fmt.Printf("calls with control-token argument names %d, with unreadable arguments %d, of tools never offered %d\n", all.ControlNames, all.Unreadable, all.NotOffered)
	}
	for _, k := range slices.Sorted(maps.Keys(all.Metrics)) {
		fmt.Printf("%s: sum %s, mean %s over %d runs\n", k, strconv.FormatFloat(all.Metrics[k], 'f', -1, 64),
			strconv.FormatFloat(all.Metrics[k]/float64(all.metricRuns[k]), 'f', 3, 64), all.metricRuns[k])
	}
	fmt.Printf("%d runs read in %s", o.end.Runs, o.end.Took)
	if o.end.Failed > 0 {
		fmt.Printf(", %d could not be read", o.end.Failed)
	}
	fmt.Println()
	return held
}

// printTotals prints a totals table: firings, then in how many runs.
func printTotals(ts []*total, shown []string) {
	w := table()
	head := "\tRUNS\tCALLS\tFAILED\t"
	for _, d := range shown {
		head += d + "\t"
	}
	fmt.Fprintln(w, head)
	for _, t := range ts {
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t", t.Value, human.Count(int64(t.Runs)), human.Count(int64(t.Calls)), human.Count(int64(t.Failed)))
		for _, d := range shown {
			line += fmt.Sprintf("%d in %d/%d\t", t.Firings[d], t.RunsWith[d], t.Runs)
		}
		fmt.Fprintln(w, line)
	}
	w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
