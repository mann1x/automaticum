package ingest

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// GuardFire is one output the repetition guard would have tripped on
// (D135): where in the run, which output, and how far it had gone. Tokens
// and Seconds are estimates, by the share of the request's output written
// by then, where the wire capture says the request.
type GuardFire struct {
	detect.GuardFire
	Turn    int    `json:"turn"`
	Request int    `json:"request,omitempty"`
	Channel string `json:"channel"` // text, reasoning or argument
	// In is the tool and argument, for an argument.
	In         string  `json:"in,omitempty"`
	Unfinished bool    `json:"unfinished,omitempty"`
	Tokens     int     `json:"tokens_estimated,omitempty"`
	Seconds    float64 `json:"seconds_estimated,omitempty"`
	at         time.Time
}

// guardExamples is how many outputs a finding lists besides the first.
const guardExamples = 10

// GuardFires are every output of a run the repetition guard would have
// tripped on, the first first; opt are the run's detector options
// (DetectOptions).
func GuardFires(ev *Events, reqs []Request, opt detect.Options) []GuardFire {
	if !detect.Available(detect.RepetitionGuard) {
		return nil
	}
	given := ev.Given.String()
	var out []GuardFire
	for _, b := range ev.Blocks {
		var f *detect.GuardFire
		if b.Kind == "text" {
			g := given
			if b.GivenTo <= len(given) {
				g = given[:b.GivenTo]
			}
			f = detect.GuardText(b.Text, g, blockOptions(opt, given, b))
		} else {
			f = detect.GuardReasoning(b.Text)
		}
		if f == nil {
			continue
		}
		gf := GuardFire{GuardFire: *f, Turn: b.Turn, Request: b.Request, Channel: b.Kind, Unfinished: b.Unfinished, at: b.Start}
		if !b.Start.IsZero() && !b.End.IsZero() && f.Chars > 0 {
			gf.at = b.Start.Add(time.Duration(float64(b.End.Sub(b.Start)) * float64(f.Offset) / float64(f.Chars)))
		}
		if q := requestAt(reqs, b); q != nil {
			gf.Request = q.Seq
			before := 0
			if b.Kind == "text" {
				before = len([]rune(q.Reasoning))
			}
			gf.estimate(q, before)
		}
		out = append(out, gf)
	}
	// Arguments: the wire's calls as the server streamed them, else the
	// event stream's.
	type call struct {
		c detect.Call
		q *Request
	}
	var calls []call
	for i := range reqs {
		q := &reqs[i]
		if !q.Agent {
			continue
		}
		for _, c := range q.Calls {
			calls = append(calls, call{detect.Call{Tool: c.Name, Input: c.Arguments}, q})
		}
	}
	if len(calls) == 0 {
		for _, c := range ev.Calls {
			calls = append(calls, call{c: c})
		}
	}
	for _, c := range calls {
		args, _ := detect.StringArgs(c.c.Input)
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f := detect.GuardArgument(args[k], given)
			if f == nil {
				continue
			}
			gf := GuardFire{GuardFire: *f, Turn: c.c.Turn, Channel: "argument", In: c.c.Tool + "." + k}
			if c.q != nil {
				gf.Request, gf.Turn, gf.at = c.q.Seq, ev.turnAt(c.q.Start), c.q.Start
				gf.estimate(c.q, len([]rune(c.q.Reasoning))+len([]rune(c.q.Visible)))
			}
			out = append(out, gf)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Turn != out[b].Turn {
			return out[a].Turn < out[b].Turn
		}
		return !out[a].at.IsZero() && out[a].at.Before(out[b].at)
	})
	return out
}

// estimate is how many of the request's tokens, and seconds, had gone by
// when the guard would have tripped: by the share of the answer's
// characters written by then, before being what came ahead of the output.
func (gf *GuardFire) estimate(q *Request, before int) {
	total := len([]rune(q.Reasoning)) + len([]rune(q.Visible))
	for _, c := range q.Calls {
		total += len([]rune(c.Arguments))
	}
	if total == 0 {
		return
	}
	share := min(1, float64(before+gf.Offset)/float64(total))
	gf.Tokens = int(share * float64(q.EvalTokens))
	gf.Seconds = share * q.MS / 1000
}

// requestAt is the agent request a block was written in: the one running
// when it started.
func requestAt(reqs []Request, b Block) *Request {
	if b.Request != 0 {
		for i := range reqs {
			if reqs[i].Seq == b.Request {
				return &reqs[i]
			}
		}
	}
	if b.Start.IsZero() {
		return nil
	}
	for i := range reqs {
		q := &reqs[i]
		end := q.Start.Add(time.Duration(q.MS * float64(time.Millisecond)))
		if q.Agent && !q.Start.IsZero() && !b.Start.Before(q.Start) && !b.Start.After(end) {
			return q
		}
	}
	return nil
}

// repetitionGuard writes repetition_guard.would_fire (outputs it would have
// tripped on) and, where one was, the finding: info, as the guard only
// observes unless an arm makes it stop.
func repetitionGuard(runID string, ev *Events, reqs []Request, opt detect.Options, m func(string, float64), res *Result) {
	if !detect.Available(detect.RepetitionGuard) {
		return
	}
	fs := GuardFires(ev, reqs, opt)
	m("repetition_guard.would_fire", float64(len(fs)))
	if len(fs) == 0 {
		return
	}
	by := map[string]int{}
	for _, f := range fs {
		by[f.Channel+"."+f.Rule]++
	}
	for k, n := range by {
		m("repetition_guard.would_fire."+k, float64(n))
	}
	m("repetition_guard.first_turn", float64(fs[0].Turn))
	m("repetition_guard.first_offset", float64(fs[0].Offset))
	rest := fs[1:]
	if len(rest) > guardExamples {
		rest = rest[:guardExamples]
	}
	raw, _ := json.Marshal(struct {
		Mode  string         `json:"mode"`
		First GuardFire      `json:"first"`
		By    map[string]int `json:"by"`
		More  []GuardFire    `json:"more,omitempty"`
	}{"observe", fs[0], by, rest})
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: detect.RepetitionGuard, DetectorVersion: detect.Version,
		Severity: "info", Turn: fs[0].Turn, Count: len(fs), Evidence: raw})
}
