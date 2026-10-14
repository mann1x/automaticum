package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// list is a flag that may be given several times.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

// compare prints the runs of some batches side by side, one column per
// arm: every number, and no conclusion (D78).
func compare(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	_, dial := addClient(fs)
	var batches list
	fs.Var(&batches, "batch", "a batch; give it several times to compare batches")
	all := fs.Bool("all", false, "every metric the runs have, not only the main ones")
	rest := parse(fs, args)
	batches = append(batches, rest...)
	if len(batches) == 0 {
		return errors.New("usage: automaticum compare <batch> [<batch> ...] [--all]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	q := url.Values{"batch": batches}
	var v daemon.CompareView
	if err := c.do(ctx, "GET", "/v1/compare?"+q.Encode(), nil, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	letter := func(i int) string { return string(rune('A' + i)) }
	for i, g := range v.Groups {
		fmt.Printf("%s  arm %s (%s), task %s\n   model %s, SDK %s\n", letter(i), g.Arm, short(g.ArmID), g.Task, g.Model, g.SDK)
		ids := make([]string, 0, len(g.Excluded))
		for id := range g.Excluded {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("   left out: %s  %s\n", id, g.Excluded[id])
		}
	}
	if n := len(v.NeverStarted); n > 0 {
		reasons := map[string]int{}
		for _, why := range v.NeverStarted {
			reasons[why]++
		}
		fmt.Printf("\n%d runs of these batches never started and are in no arm's numbers:\n", n)
		for why, k := range reasons {
			fmt.Printf("  %d: %s\n", k, why)
		}
	}
	for _, p := range v.Pairs {
		fmt.Printf("\nwhat differs between %s and %s, besides the runs:\n", letter(p.A), letter(p.B))
		if len(p.Differs) == 0 {
			fmt.Println("  nothing: the same arm")
		}
		for _, d := range p.Differs {
			fmt.Println("  " + d)
		}
		fmt.Printf("\nthe basis (020 §5), every fact in which %s and %s differ:\n", letter(p.A), letter(p.B))
		printBasisDiff(p.Basis, "  ")
	}
	for i, g := range v.Groups {
		for _, f := range g.Basis.Fields {
			if f.Mixed {
				fmt.Printf("\n%s's runs differ among themselves in %s: %s\n", letter(i), f.Name, strings.Join(f.Values, " | "))
			}
		}
	}
	fmt.Println("\nthe model, as the server said at the start of every run:")
	facts := map[string]bool{}
	for _, g := range v.Groups {
		for f := range g.Served {
			facts[f] = true
		}
	}
	sw := table()
	fmt.Fprint(sw, "  FACT")
	for i := range v.Groups {
		fmt.Fprintf(sw, "\t%s", letter(i))
	}
	fmt.Fprintln(sw)
	changed := false
	for _, f := range sorted(facts) {
		fmt.Fprintf(sw, "  %s", f)
		for _, g := range v.Groups {
			vals := g.Served[f]
			switch {
			case len(vals) == 0:
				fmt.Fprint(sw, "\tnot said")
			case len(vals) > 1:
				changed = true
				fmt.Fprintf(sw, "\tCHANGED BETWEEN RUNS: %s", strings.Join(vals, " | "))
			default:
				fmt.Fprintf(sw, "\t%s", vals[0])
			}
		}
		fmt.Fprintln(sw)
	}
	sw.Flush()
	if changed {
		fmt.Println("  A fact that changed between the runs of one arm: those runs did not all run on the same thing.")
	}
	fmt.Printf("\nverdicts (intervals at %s):\n", human.Percent(v.Level))
	w := table()
	fmt.Fprint(w, "  \tRUNS\tPASS\tINTERVAL\tOTHER\tTOKENS PER SOLVED\n")
	for i, g := range v.Groups {
		var other []string
		for verdict, n := range g.Verdicts {
			if verdict != "PASS" {
				other = append(other, fmt.Sprintf("%d %s", n, verdict))
			}
		}
		sort.Strings(other)
		solved := "-"
		if g.TokensPerSolved > 0 {
			solved = human.Number(g.TokensPerSolved)
		}
		fmt.Fprintf(w, "  %s\t%d\t%d\t%s to %s\t%s\t%s\n", letter(i), len(g.Runs), g.Pass, human.Percent(g.PassLow), human.Percent(g.PassHigh), strings.Join(other, ", "), solved)
	}
	w.Flush()
	for _, p := range v.Pairs {
		fmt.Printf("  %s against %s, pass counts: Fisher's exact test, two-sided p = %.3f\n", letter(p.A), letter(p.B), p.PassP)
	}

	fmt.Println("\ngeneration cap, at any turn:")
	w = table()
	fmt.Fprintln(w, "     VERDICT\tCAPPED REQUESTS\tCAPPED RUNS\tOF THEM PASSED\tOUTPUT LIMIT\tTHINKING BUDGET\tWHY")
	tokens := func(vs []float64) string {
		var out []string
		for _, x := range vs {
			out = append(out, human.Count(int64(x))+" tokens")
		}
		if len(out) == 0 {
			return "-"
		}
		return strings.Join(out, ", ")
	}
	for i, g := range v.Groups {
		c := g.Cap
		fmt.Fprintf(w, "  %s  %s\t%d of %d\t%d of %d\t%d\t%s\t%s\t%s\n", letter(i), c.Verdict, c.CappedRequests, c.Requests,
			c.CappedRuns, len(g.Runs), c.CappedPassed, tokens(c.Declared), tokens(c.ThinkBudget), c.Why)
	}
	w.Flush()
	for _, p := range v.Pairs {
		c := p.Cap
		switch {
		case c.Confounded:
			fmt.Printf("  %s against %s: CONFOUNDED by the cap, %d against %d capped requests; the difference in scores is not clean: the bench has measured length.\n", letter(p.A), letter(p.B), c.A, c.B)
		case c.Material:
			fmt.Printf("  %s against %s: %d against %d capped requests, a material number but not lopsided.\n", letter(p.A), letter(p.B), c.A, c.B)
		}
	}
	for i := range v.Groups {
		if v.Groups[i].Cap.Verdict == "CAPPED" {
			fmt.Println("  A CAPPED arm is not comparable with an arm at another output budget.")
			break
		}
	}

	fmt.Println("\nruns with a finding:")
	w = table()
	names := map[string]bool{}
	for _, g := range v.Groups {
		for name := range g.Findings {
			names[name] = true
		}
	}
	fmt.Fprint(w, "  FINDING")
	for i := range v.Groups {
		fmt.Fprintf(w, "\t%s", letter(i))
	}
	fmt.Fprintln(w)
	for _, name := range sorted(names) {
		fmt.Fprintf(w, "  %s", name)
		for _, g := range v.Groups {
			fmt.Fprintf(w, "\t%d of %d", g.Findings[name], len(g.Runs))
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	fmt.Println("\nper run: median (least to most), and how much it swings (standard deviation over mean):")
	names = map[string]bool{}
	for _, g := range v.Groups {
		for name, s := range g.Metrics {
			if (*all && (s.Min != 0 || s.Max != 0)) || mainMetrics[name] {
				names[name] = true
			}
		}
	}
	w = table()
	fmt.Fprint(w, "  METRIC")
	for i := range v.Groups {
		fmt.Fprintf(w, "\t%s\tSWING", letter(i))
	}
	for _, p := range v.Pairs {
		fmt.Fprintf(w, "\t%s>%s\tp", letter(p.A), letter(p.B))
	}
	fmt.Fprintln(w)
	for _, name := range sorted(names) {
		fmt.Fprintf(w, "  %s", name)
		for _, g := range v.Groups {
			s, ok := g.Metrics[name]
			if !ok {
				fmt.Fprint(w, "\t-\t-")
				continue
			}
			fmt.Fprintf(w, "\t%s (%s to %s)\t%s", bare(human.Metric(name, s.Median)), bare(human.Metric(name, s.Min)), bare(human.Metric(name, s.Max)), human.Percent(s.CV))
		}
		for _, p := range v.Pairs {
			r, ok := p.Metrics[name]
			if !ok {
				fmt.Fprint(w, "\t-\t-")
				continue
			}
			how := "~"
			if r.Exact {
				how = ""
			}
			fmt.Fprintf(w, "\t%s\t%s%.3f", human.Percent(r.Superiority), how, r.P)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
	fmt.Println("\nA>B is the share of pairs of runs, one of each arm, in which A's value is the larger (50% is no difference).")
	fmt.Println("p is the Mann-Whitney test, two-sided; ~ marks the approximation used when values tie. No line is drawn at any p:")
	fmt.Println("with this many runs the swing from run to run is large, and the numbers are given for the reader to judge.")
	return nil
}

// mainMetrics are the ones printed without --all: time, thinking, tokens,
// context, tools and speed.
var mainMetrics = map[string]bool{"time.wall_ms": true, "time.model_ms": true, "time.tools_ms": true, "time.first_token_ms": true,
	"requests": true, "requests.failed": true, "tokens.completion": true, "tokens.completion.reasoning": true, "tokens.completion.text": true,
	"tokens.completion.tool_input": true, "tokens.prompt": true, "thinking.chars.sum": true, "thinking.chars.max": true,
	"thinking.budget_tokens.served": true, "context.peak_share": true, "context.compactions": true, "tool_calls": true, "tool_calls.failed": true,
	"rate.generation_tok_s": true, "endpoint.reloads": true, "endpoint.gpu_share": true}

// bare drops the unit a value was formatted with: the metric's name says it.
func bare(s string) string {
	for _, unit := range []string{" tokens", " characters", " blocks", " requests"} {
		s = strings.TrimSuffix(s, unit)
	}
	return s
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// printBasisDiff prints each fact in which two bases differ.
func printBasisDiff(diff []daemon.BasisDiff, indent string) {
	if len(diff) == 0 {
		fmt.Println(indent + "none: the same basis")
		return
	}
	w := table()
	for _, d := range diff {
		fmt.Fprintf(w, "%s%s\t%s\t->\t%s\n", indent, d.Name, strings.Join(d.A, " | "), strings.Join(d.B, " | "))
	}
	w.Flush()
}

// basisGroups asks the daemon for the bases of some batches.
func basisGroups(ctx context.Context, c *client, batches []string) ([]daemon.BasisGroup, error) {
	q := url.Values{"batch": batches}
	var out []daemon.BasisGroup
	err := c.do(ctx, "GET", "/v1/basis?"+q.Encode(), nil, &out)
	return out, err
}

// basisShow prints the basis of every arm on every task of some batches.
func basisShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("basis show", flag.ExitOnError)
	_, dial := addClient(fs)
	arm := fs.String("arm", "", "only this arm")
	rest := parse(fs, args)
	if len(rest) == 0 {
		return errors.New("usage: automaticum basis show <batch> [<batch> ...] [--arm A]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	groups, err := basisGroups(ctx, c, rest)
	if err != nil {
		return err
	}
	if *arm != "" {
		groups = slices.DeleteFunc(groups, func(g daemon.BasisGroup) bool { return g.Arm != *arm })
	}
	if c.json {
		return printJSON(groups)
	}
	for i, g := range groups {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("arm %s (%s), task %s, %d runs\nbasis sha256 %s\n", g.Arm, short(g.ArmID), g.Task, len(g.Runs), g.Basis.SHA256)
		w := table()
		for _, f := range g.Basis.Fields {
			mark := ""
			if f.Mixed {
				mark = "MIXED: "
			}
			fmt.Fprintf(w, "  %s\t%s%s\n", f.Name, mark, strings.Join(f.Values, " | "))
		}
		w.Flush()
	}
	return nil
}

// basisDiff prints exactly the facts in which two bases differ. Each side
// is a batch, or batch/arm when the batch has several arms.
func basisDiff(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("basis diff", flag.ExitOnError)
	_, dial := addClient(fs)
	rest := parse(fs, args)
	if len(rest) != 2 {
		return errors.New("usage: automaticum basis diff <batch[/arm]> <batch[/arm]>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var sides [2]daemon.BasisGroup
	for i, sel := range rest {
		batch, arm, _ := strings.Cut(sel, "/")
		groups, err := basisGroups(ctx, c, []string{batch})
		if err != nil {
			return err
		}
		if arm != "" {
			groups = slices.DeleteFunc(groups, func(g daemon.BasisGroup) bool { return g.Arm != arm })
		}
		switch len(groups) {
		case 0:
			return fmt.Errorf("%s: no arm of that name in the batch", sel)
		case 1:
			sides[i] = groups[0]
		default:
			var names []string
			for _, g := range groups {
				names = append(names, batch+"/"+g.Arm)
			}
			return fmt.Errorf("%s has several arms; name one: %s", sel, strings.Join(names, ", "))
		}
	}
	diff := daemon.DiffBasis(sides[0].Basis, sides[1].Basis)
	if c.json {
		return printJSON(diff)
	}
	fmt.Printf("A  %s, arm %s, %d runs, basis %s\n", rest[0], sides[0].Arm, len(sides[0].Runs), short(sides[0].Basis.SHA256))
	fmt.Printf("B  %s, arm %s, %d runs, basis %s\n\n", rest[1], sides[1].Arm, len(sides[1].Runs), short(sides[1].Basis.SHA256))
	printBasisDiff(diff, "")
	return nil
}
