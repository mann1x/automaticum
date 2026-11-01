// Package wait decides when a wait is over (090 §5): a run, a batch, a
// cell, a campaign, a lane or the whole daemon, against a condition such as
// "pass>=5 or fail>=3". It holds no state of its own: the daemon, or the
// command line against a daemon that does not wait itself, reads a
// snapshot and asks Eval whether the condition holds.
package wait

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The kinds of thing a wait follows.
const (
	KindRun      = "run"
	KindBatch    = "batch"
	KindCell     = "cell"
	KindCampaign = "campaign"
	KindLane     = "lane"
	KindIdle     = "idle"
)

// Kinds are the kinds in the order the usage names them.
var Kinds = []string{KindRun, KindBatch, KindCell, KindCampaign, KindLane, KindIdle}

// Run is what a wait reads of one run.
type Run struct {
	ID      string    `json:"id"`
	State   string    `json:"state"` // queued, running, finished
	Verdict string    `json:"verdict,omitempty"`
	Reason  string    `json:"verdict_reason,omitempty"`
	Task    string    `json:"task,omitempty"`
	Arm     string    `json:"arm,omitempty"`
	Lane    string    `json:"lane,omitempty"`
	Batch   string    `json:"batch,omitempty"`
	RetryOf string    `json:"retry_of,omitempty"`
	WallMS  int64     `json:"wall_ms,omitempty"`
	Started time.Time `json:"started_at,omitzero"`
	Ended   time.Time `json:"ended_at,omitzero"`
}

// Snapshot is the state a condition is read against: counts ("pass",
// "queued") and facts that hold or not ("done", "idle"). Finished lists
// the finished runs in the order they ended, so that a caller that has seen
// n of them is told about the rest.
type Snapshot struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id,omitempty"`
	State    string          `json:"state,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Counts   map[string]int  `json:"counts"`
	Facts    map[string]bool `json:"facts"`
	Finished []Run           `json:"-"`
}

// The counts of a run, a batch or a cell: one per verdict, then by state.
var verdictCounts = []string{"pass", "fail", "timeout", "aborted", "invalid", "skipped"}

// Names lists the counts and the facts a condition may name for a kind.
func Names(kind string) (counts, facts []string) {
	switch kind {
	case KindRun, KindBatch, KindCell:
		counts = append(slices.Clone(verdictCounts), "finished", "queued", "running", "total")
		facts = []string{"started", "done"}
		if kind == KindRun {
			facts = append(facts, "finished")
		}
	case KindCampaign:
		counts = []string{"batches", "batches_finished", "batches_stopped", "batches_running", "batches_waiting"}
		facts = []string{"running", "paused", "stopped", "finished", "done"}
	case KindLane:
		counts = []string{"running", "queued"}
		facts = []string{"idle", "free", "drained", "quarantined", "paused"}
	case KindIdle:
		counts = []string{"running", "queued"}
		facts = []string{"idle", "free"}
	}
	return counts, facts
}

// Default is the condition a wait of a kind has when none is given: the
// run or batch has ended, the campaign is over, the lane or daemon idle.
func Default(kind string) string {
	switch kind {
	case KindLane, KindIdle:
		return "idle"
	}
	return "done"
}

// Runs is the snapshot of a run, a batch or a cell from its runs. Every
// finished record counts once, a retried run's first attempt too, so that
// the counts only grow.
func Runs(kind, id string, runs []Run) Snapshot {
	s := Snapshot{Kind: kind, ID: id, Counts: map[string]int{}, Facts: map[string]bool{}}
	for _, name := range verdictCounts {
		s.Counts[name] = 0
	}
	for _, r := range runs {
		s.Counts["total"]++
		switch r.State {
		case "queued":
			s.Counts["queued"]++
		case "running":
			s.Counts["running"]++
		default:
			s.Counts["finished"]++
			s.Counts[strings.ToLower(r.Verdict)]++
			s.Finished = append(s.Finished, r)
		}
	}
	sort.SliceStable(s.Finished, func(i, j int) bool {
		a, b := s.Finished[i], s.Finished[j]
		if !a.Ended.Equal(b.Ended) {
			return a.Ended.Before(b.Ended)
		}
		return a.ID < b.ID
	})
	c := s.Counts
	s.Facts["started"] = c["running"]+c["finished"] > 0
	s.Facts["done"] = c["total"] > 0 && c["queued"] == 0 && c["running"] == 0
	if kind == KindRun {
		s.Facts["finished"] = s.Facts["done"]
	}
	switch {
	case s.Facts["done"]:
		s.State = "done"
	case c["running"] > 0:
		s.State = "running"
	default:
		s.State = "queued"
	}
	if kind == KindRun && len(runs) == 1 {
		s.State = runs[0].State
	}
	return s
}

// Lane is the snapshot of one lane: whether a run is in flight on it (or
// held there) and how many runs are queued for it.
func Lane(name, state string, inFlight bool, queued int) Snapshot {
	s := Snapshot{Kind: KindLane, ID: name, State: state, Counts: map[string]int{"queued": queued, "running": 0}, Facts: map[string]bool{}}
	if inFlight {
		s.Counts["running"] = 1
	}
	s.Facts["free"] = !inFlight
	s.Facts["idle"] = !inFlight && queued == 0
	// A quarantine is a drain with a reason (050 §4).
	s.Facts["drained"] = state == "drained" || state == "quarantined"
	s.Facts["quarantined"] = state == "quarantined"
	s.Facts["paused"] = state == "paused"
	return s
}

// Idle is the snapshot of the whole daemon: runs in flight and queued.
func Idle(running, queued int) Snapshot {
	s := Snapshot{Kind: KindIdle, Counts: map[string]int{"running": running, "queued": queued}, Facts: map[string]bool{}}
	s.Facts["free"] = running == 0
	s.Facts["idle"] = running == 0 && queued == 0
	s.State = "busy"
	if s.Facts["idle"] {
		s.State = "idle"
	}
	return s
}

// Campaign is the snapshot of a campaign from its state and its batches'.
func Campaign(name, state, reason string, batchStates []string) Snapshot {
	s := Snapshot{Kind: KindCampaign, ID: name, State: state, Reason: reason, Counts: map[string]int{}, Facts: map[string]bool{}}
	for _, k := range []string{"batches", "batches_finished", "batches_stopped", "batches_running", "batches_waiting"} {
		s.Counts[k] = 0
	}
	for _, b := range batchStates {
		s.Counts["batches"]++
		s.Counts["batches_"+b]++
	}
	for _, f := range []string{"running", "paused", "stopped", "finished"} {
		s.Facts[f] = state == f
	}
	s.Facts["done"] = state == "stopped" || state == "finished"
	return s
}

// Over says that nothing more will happen to what is followed, so a
// condition that does not hold now never will: the run or batch has ended,
// the campaign is over. A lane or the daemon is never over.
func (s Snapshot) Over() bool {
	return s.Facts["done"]
}

// Cond is a parsed condition.
type Cond struct {
	text string
	root node
}

func (c *Cond) String() string { return c.text }

// Eval says whether the condition holds on s.
func (c *Cond) Eval(s Snapshot) bool { return c.root.eval(s) }

type node interface{ eval(Snapshot) bool }

type or []node
type and []node
type fact string
type cmp struct {
	name string
	op   string
	n    int
}

func (o or) eval(s Snapshot) bool {
	for _, n := range o {
		if n.eval(s) {
			return true
		}
	}
	return false
}

func (a and) eval(s Snapshot) bool {
	for _, n := range a {
		if !n.eval(s) {
			return false
		}
	}
	return true
}

func (f fact) eval(s Snapshot) bool { return s.Facts[string(f)] }

func (c cmp) eval(s Snapshot) bool {
	v := s.Counts[c.name]
	switch c.op {
	case ">=":
		return v >= c.n
	case ">":
		return v > c.n
	case "<=":
		return v <= c.n
	case "<":
		return v < c.n
	case "==", "=":
		return v == c.n
	case "!=":
		return v != c.n
	}
	return false
}

// Parse reads a condition for a kind: counts compared with a number and
// facts, joined by "and" and "or" ("&&", "||"), in parentheses where
// needed. "and" binds tighter than "or". An empty text is the kind's
// default.
func Parse(kind, text string) (*Cond, error) {
	if !slices.Contains(Kinds, kind) {
		return nil, fmt.Errorf("a wait follows a %s, not a %q", strings.Join(Kinds, ", "), kind)
	}
	if strings.TrimSpace(text) == "" {
		text = Default(kind)
	}
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	counts, facts := Names(kind)
	p := &parser{toks: toks, kind: kind, counts: counts, facts: facts}
	root, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.i < len(p.toks) {
		return nil, fmt.Errorf("condition %q: %q is not expected here", text, p.toks[p.i])
	}
	return &Cond{text: text, root: root}, nil
}

func lex(text string) ([]string, error) {
	var toks []string
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case strings.ContainsRune("<>=!", rune(c)):
			j := i + 1
			if j < len(text) && text[j] == '=' {
				j++
			}
			toks = append(toks, text[i:j])
			i = j
		case c == '&' || c == '|':
			if i+1 >= len(text) || text[i+1] != c {
				return nil, fmt.Errorf("condition %q: write %c%c, or \"and\" and \"or\"", text, c, c)
			}
			toks = append(toks, map[byte]string{'&': "and", '|': "or"}[c])
			i += 2
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9':
			j := i
			for j < len(text) && (text[j] == '_' || text[j] >= 'a' && text[j] <= 'z' || text[j] >= 'A' && text[j] <= 'Z' || text[j] >= '0' && text[j] <= '9') {
				j++
			}
			toks = append(toks, strings.ToLower(text[i:j]))
			i = j
		default:
			return nil, fmt.Errorf("condition %q: %q is not part of a condition", text, string(c))
		}
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("the condition is empty")
	}
	return toks, nil
}

type parser struct {
	toks          []string
	i             int
	kind          string
	counts, facts []string
}

func (p *parser) peek() string {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return ""
}

func (p *parser) or() (node, error) {
	var out or
	for {
		n, err := p.and()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
		if p.peek() != "or" {
			break
		}
		p.i++
	}
	if len(out) == 1 {
		return out[0], nil
	}
	return out, nil
}

func (p *parser) and() (node, error) {
	var out and
	for {
		n, err := p.atom()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
		if p.peek() != "and" {
			break
		}
		p.i++
	}
	if len(out) == 1 {
		return out[0], nil
	}
	return out, nil
}

func (p *parser) atom() (node, error) {
	t := p.peek()
	if t == "" {
		return nil, fmt.Errorf("the condition ends too early")
	}
	p.i++
	if t == "(" {
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("a parenthesis is not closed")
		}
		p.i++
		return n, nil
	}
	if op := p.peek(); op != "" && strings.ContainsAny(op[:1], "<>=!") {
		if !slices.Contains(p.counts, t) {
			return nil, fmt.Errorf("a %s has no count %q; it has %s", p.kind, t, strings.Join(p.counts, ", "))
		}
		if op == "!" {
			return nil, fmt.Errorf("write != for not equal")
		}
		p.i++
		num := p.peek()
		n, err := strconv.Atoi(num)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s %s needs a number, not %q", t, op, num)
		}
		p.i++
		return cmp{name: t, op: op, n: n}, nil
	}
	if slices.Contains(p.facts, t) {
		return fact(t), nil
	}
	return nil, fmt.Errorf("a %s has no fact %q; facts are %s, counts (compared with a number) are %s", p.kind, t,
		strings.Join(p.facts, ", "), strings.Join(p.counts, ", "))
}

// Seen holds a condition that also needs more finished runs than a caller
// has seen: "the next run to finish" is Seen(n) with n the runs finished
// when the caller last looked.
func Seen(n int) *Cond {
	return &Cond{text: fmt.Sprintf("finished>%d", n), root: cmp{name: "finished", op: ">", n: n}}
}

// Any holds when any of the conditions holds.
func Any(conds ...*Cond) *Cond {
	var o or
	var texts []string
	for _, c := range conds {
		o = append(o, c.root)
		texts = append(texts, "("+c.text+")")
	}
	return &Cond{text: strings.Join(texts, " or "), root: o}
}
