package ingest

import (
	"encoding/json"
	"sort"
	"strings"
)

// ToolOutcome is what one tool's calls in a run came to, for `automaticum
// detect` (070 §3.3): how many there were, how many failed or the tool
// refused, the failures by their error's class, the calls
// whose arguments were not a JSON object, whether the run's requests ever
// offered the tool, and the calls one of whose argument names holds a
// control token.
type ToolOutcome struct {
	Tool    string `json:"tool"`
	Calls   int    `json:"calls"`
	Failed  int    `json:"failed"`
	Refused int    `json:"refused"`
	// Errors counts the failed calls by their error's class, as 070 §2
	// classes them (ErrorClass).
	Errors     map[string]int `json:"errors,omitempty"`
	Unreadable int            `json:"unreadable_arguments"`
	// NotOffered is a tool no request of the run offered; only said when
	// the requests were captured.
	NotOffered   bool `json:"not_offered,omitempty"`
	ControlNames int  `json:"control_token_names"`
}

// Outcomes reads every call of a run, by tool, the tool with the most calls
// first.
func Outcomes(ev *Events, reqs []Request) []ToolOutcome {
	offered, captured := map[string]bool{}, false
	for _, q := range reqs {
		for tool := range q.ToolArgs {
			offered[tool], captured = true, true
		}
	}
	by := map[string]*ToolOutcome{}
	get := func(tool string) *ToolOutcome {
		o := by[tool]
		if o == nil {
			o = &ToolOutcome{Tool: tool, NotOffered: captured && !offered[tool]}
			by[tool] = o
		}
		return o
	}
	for _, c := range ev.ToolCalls {
		o := get(c.Tool)
		o.Calls++
		if !c.OK {
			o.Failed++
			if o.Errors == nil {
				o.Errors = map[string]int{}
			}
			o.Errors[c.ErrorClass]++
		}
	}
	for _, c := range ev.Calls {
		o := get(c.Tool)
		if c.Refused {
			o.Refused++
		}
		var args map[string]any
		if strings.TrimSpace(c.Input) != "" && (json.Unmarshal([]byte(c.Input), &args) != nil || args == nil) {
			o.Unreadable++
		}
		for k := range args {
			if controlName(k) {
				o.ControlNames++
				break
			}
		}
	}
	out := make([]ToolOutcome, 0, len(by))
	for _, o := range by {
		out = append(out, *o)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Calls > out[j].Calls || out[i].Calls == out[j].Calls && out[i].Tool < out[j].Tool
	})
	return out
}

// controlName is an argument name holding a control token or channel
// marker, or a piece of one.
func controlName(k string) bool {
	return markerRE.MatchString(k) || strings.Contains(k, "<|") || strings.Contains(k, "|>")
}
