package ingest

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// The findings of what the model gave turn by turn (070 §4).
const (
	// GuardTrip lists every guard's trip with what the model did next.
	GuardTrip = "guard_trip"
	// ControlMarkers is visible text holding a control token or a channel
	// marker, or opening a thinking tag it never closes.
	ControlMarkers = "control_markers"
	// GuardReplay sets each guard's trips against its own rule replayed
	// over the run (070 §4): turns where the rule holds and the guard did
	// not trip, and trips the replay does not confirm, for review.
	GuardReplay = "guard_replay"
)

// replayed pairs each guard (and phase, where it matters) with the
// detectors that replay its rule: the call guard and identical calls in a
// row for loop_detection; the reasoning-loop guard's cut and abort for
// reasoning_loop; its removal of a repeated passage for
// reasoning_repetition (the SDK's repetition guard, which reports under
// the reasoning_loop kind with the phase "removed").
var replayed = []struct {
	guard, phase string
	detectors    []string
}{
	{"loop_detection", "", []string{"loop_detection"}},
	{"reasoning_loop", "cut", []string{detect.ReasoningLoop}},
	{"reasoning_loop", "removed", []string{detect.ReasoningRepetition}},
}

// guardReplay compares, guard by guard, the turns it tripped in with the
// turns its rule replayed fires in.
func guardReplay(runID string, ev *Events, fired FiredTurns, m func(string, float64), res *Result) {
	type replay struct {
		Guard       string `json:"guard"`
		Phase       string `json:"phase,omitempty"`
		Missed      []int  `json:"missed"`
		Unconfirmed []int  `json:"unconfirmed"`
	}
	var out []replay
	review := 0
	for _, g := range replayed {
		trips := map[int]bool{}
		for _, t := range ev.Trips {
			// A cut and an abort are the same rule's trips.
			if t.Guard == g.guard && (g.phase == "" || t.Phase == g.phase || (g.phase == "cut" && t.Phase == "aborted")) {
				trips[t.Turn] = true
			}
		}
		rule := map[int]bool{}
		for _, d := range g.detectors {
			for turn := range fired[d] {
				rule[turn] = true
			}
		}
		r := replay{Guard: g.guard, Phase: g.phase, Missed: []int{}, Unconfirmed: []int{}}
		for turn := range rule {
			if !trips[turn] {
				r.Missed = append(r.Missed, turn)
			}
		}
		for turn := range trips {
			if !rule[turn] {
				r.Unconfirmed = append(r.Unconfirmed, turn)
			}
		}
		sort.Ints(r.Missed)
		sort.Ints(r.Unconfirmed)
		name := g.guard
		if g.phase != "" {
			name += "." + g.phase
		}
		m("guard_replay."+name+".missed", float64(len(r.Missed)))
		m("guard_replay."+name+".unconfirmed", float64(len(r.Unconfirmed)))
		review += len(r.Unconfirmed)
		if len(r.Missed)+len(r.Unconfirmed) > 0 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return
	}
	raw, _ := json.Marshal(map[string]any{"guards": out})
	n := 0
	for _, r := range out {
		n += len(r.Missed) + len(r.Unconfirmed)
	}
	sev := "note"
	if review > 0 {
		sev = "warning" // a trip to review
	}
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: GuardReplay, DetectorVersion: Versions[GuardReplay],
		Severity: sev, Count: n, Evidence: raw})
}

// markerRE finds the control tokens and channel markers a model's
// template uses (`<|channel|>`, `<|channel>`, `<channel|>`,
// `<|im_end|>`) and thinking tags.
var markerRE = regexp.MustCompile(`<\|[A-Za-z_][A-Za-z0-9_]{0,30}\|?>|<[A-Za-z_][A-Za-z0-9_]{0,30}\|>|</?think>`)

// turnFindings counts thinking-only turns, turns with no call and no
// answer, and short visible text; finds control markers in visible text;
// and lists the guards' trips with what came next.
func turnFindings(runID string, ev *Events, m func(string, float64), res *Result) {
	if len(ev.Turns) > 0 || ev.Iterations > 0 {
		thinkingOnly, nothing, short := 0, 0, 0
		lastThinkingOnly := false
		turns := make([]int, 0, len(ev.Turns))
		for n := range ev.Turns {
			turns = append(turns, n)
		}
		sort.Ints(turns)
		for i, n := range turns {
			t := ev.Turns[n]
			only := t.Reasoning && t.TextChars == 0 && t.Calls == 0
			if only {
				thinkingOnly++
			}
			if t.TextChars == 0 && t.Calls == 0 {
				nothing++
			}
			if t.TextChars > 0 && t.TextChars < 5 && t.Calls == 0 {
				short++
			}
			if i == len(turns)-1 {
				lastThinkingOnly = only
			}
		}
		m("turns.thinking_only", float64(thinkingOnly))
		m("turns.no_call_no_answer", float64(nothing))
		m("turns.short_text", float64(short))
		if ev.Iterations > 0 {
			m("turns.no_call_no_answer_share", float64(nothing)/float64(ev.Iterations))
		}
		// A plan written to the reasoning and an empty reply, at the end
		// (D128): the last turn thought and then the run ended.
		if lastThinkingOnly && ev.AgentEnd != "" {
			m("turns.ended_thinking_only", 1)
		}
	}

	type hit struct {
		Turn    int    `json:"turn"`
		Channel string `json:"channel"`
		Marker  string `json:"marker"`
	}
	var hits []hit
	unclosed := 0
	for _, b := range ev.Blocks {
		if b.Kind != "text" {
			continue
		}
		for _, mk := range markerRE.FindAllString(b.Text, -1) {
			hits = append(hits, hit{b.Turn, b.Kind, mk})
		}
		if strings.Count(b.Text, "<think>") > strings.Count(b.Text, "</think>") {
			unclosed++
		}
	}
	m("hygiene.control_markers", float64(len(hits)))
	m("hygiene.unclosed_thinking", float64(unclosed))
	if len(hits) > 0 || unclosed > 0 {
		first := hits
		if len(first) > 20 {
			first = first[:20]
		}
		raw, _ := json.Marshal(map[string]any{"markers": first, "unclosed_thinking": unclosed})
		turn := 0
		if len(hits) > 0 {
			turn = hits[0].Turn
		}
		res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: ControlMarkers, DetectorVersion: Versions[ControlMarkers],
			Severity: "warning", Turn: turn, Count: len(hits) + unclosed, Evidence: raw})
	}

	if len(ev.Trips) > 0 {
		next := map[string]int{}
		for _, t := range ev.Trips {
			k := t.Next
			if k == "" {
				k = "nothing"
			}
			next[k]++
		}
		for _, k := range sortedKeys(next) {
			m("guard_trips.next."+k, float64(next[k]))
		}
		raw, _ := json.Marshal(map[string]any{"trips": ev.Trips})
		res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: GuardTrip, DetectorVersion: Versions[GuardTrip],
			Severity: "note", Turn: ev.Trips[0].Turn, Count: len(ev.Trips), Evidence: raw})
	}
}

// rereads counts the agent's requests after the first whose prompt the
// server read again whole (nothing served from its cache), with the cause
// when it is known: a compaction since the request before, or a reload of
// the model. A server that never says what it served from cache says
// nothing here (070 §2).
func rereads(m func(string, float64), agent []Request, compactions []time.Time) {
	reports := false
	for _, q := range agent {
		reports = reports || q.CachedTokens > 0
	}
	if !reports || len(agent) < 2 {
		return
	}
	by := map[string]int{}
	n := 0
	for i := 1; i < len(agent); i++ {
		q := agent[i]
		if q.Failed || q.PromptTokens == 0 || q.CachedTokens > 0 {
			continue
		}
		n++
		cause := "unknown"
		for _, t := range compactions {
			if !t.Before(agent[i-1].Start) && t.Before(q.Start) {
				cause = "compaction"
				break
			}
		}
		if cause == "unknown" && float64(q.LoadNS)/1e9 >= reloadSeconds {
			cause = "reload"
		}
		by[cause]++
	}
	m("context.rereads", float64(n))
	for _, k := range sortedKeys(by) {
		m("context.rereads."+k, float64(by[k]))
	}
}

// callPaths are the files a call names, as the SDK's loop guard reads
// them: path, file_path, paths[] and files[].path, lower-cased, with
// backslashes as slashes.
func callPaths(input string) []string {
	var in struct {
		Path     string   `json:"path"`
		FilePath string   `json:"file_path"`
		Paths    []string `json:"paths"`
		Files    []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	json.Unmarshal([]byte(input), &in)
	var out []string
	add := func(p string) {
		if p != "" {
			out = append(out, strings.ToLower(strings.ReplaceAll(p, `\`, "/")))
		}
	}
	add(in.Path)
	add(in.FilePath)
	for _, p := range in.Paths {
		add(p)
	}
	for _, f := range in.Files {
		add(f.Path)
	}
	return out
}

// edits counts the writing tools' calls: edits made and refused, the files
// they touched, restores, and edits left without a check (D127): an edit
// is checked when a run_commands call follows it before the next edit of
// the same file, or before the run ends.
func edits(m func(string, float64), ev *Events) {
	made, refused, restores, unchecked := 0, 0, 0, 0
	files := map[string]bool{}
	open := map[string]bool{} // files edited since the last check
	for i, c := range ev.Calls {
		ok := i < len(ev.ToolCalls) && ev.ToolCalls[i].OK
		switch {
		case c.Tool == "run_commands":
			clear(open)
		case detect.WritingTools[c.Tool] && ok:
			made++
			if c.Tool == "restore_file" {
				restores++
			}
			for _, p := range callPaths(c.Input) {
				files[p] = true
				if open[p] {
					unchecked++
				}
				open[p] = true
			}
		case detect.WritingTools[c.Tool]:
			refused++
		}
	}
	unchecked += len(open)
	if made+refused == 0 {
		return
	}
	m("edits", float64(made))
	m("edits.failed", float64(refused))
	m("edits.files", float64(len(files)))
	m("edits.restores", float64(restores))
	m("edits.without_check", float64(unchecked))
}
