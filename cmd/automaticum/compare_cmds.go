package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
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
	for _, p := range v.Pairs {
		fmt.Printf("\nwhat differs between %s and %s, besides the runs:\n", letter(p.A), letter(p.B))
		if len(p.Differs) == 0 {
			fmt.Println("  nothing: the same arm")
		}
		for _, d := range p.Differs {
			fmt.Println("  " + d)
		}
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
