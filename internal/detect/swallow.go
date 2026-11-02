package detect

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// SwallowedArgument is a tool call one of whose string arguments holds
// another argument of the same tool (D131): the model wrote the next
// argument's name and value inside the string instead of closing it, so
// the tool got a string with an argument in it and the argument missing.
// A client found it on its trained models' editor calls ("new_text" holding
// `"start_line": 12`, the call refused for lack of start_line) and on none
// of the base model's.
//
// The rule: an argument the tool's schema names, missing from the call,
// whose name stands inside one of the call's string arguments after a
// quote, a backtick, a comma or a closing brace and before a colon (with a
// closing quote between, as JSON writes it, or not). The
// argument names come from the schemas the run's requests offered, and
// from the arguments the run's calls of the same tool did pass, for a run
// whose requests were not captured.
const SwallowedArgument = "swallowed_argument"

// SwallowFiring is one call with a swallowed argument: which string
// argument held it, which argument it was, and the text around it.
type SwallowFiring struct {
	Index     int    `json:"index"`
	Turn      int    `json:"turn"`
	Tool      string `json:"tool"`
	In        string `json:"in"`
	Swallowed string `json:"swallowed"`
	Around    string `json:"around"`
}

// swallowAround is how many characters on each side of the swallowed name
// a firing keeps.
const swallowAround = 24

// SwallowedArgs reads every call. schemas are the argument names of each
// tool, as the requests offered them; nil when none were captured.
func SwallowedArgs(calls []Call, schemas map[string][]string) []SwallowFiring {
	type parsed struct {
		args map[string]any
		ok   bool
	}
	ps := make([]parsed, len(calls))
	names := map[string]map[string]bool{}
	add := func(tool, n string) {
		if names[tool] == nil {
			names[tool] = map[string]bool{}
		}
		names[tool][n] = true
	}
	for tool, ns := range schemas {
		for _, n := range ns {
			add(tool, n)
		}
	}
	for i, c := range calls {
		var m map[string]any
		if json.Unmarshal([]byte(c.Input), &m) == nil && m != nil {
			ps[i] = parsed{m, true}
			for k := range m {
				add(c.Tool, k)
			}
		}
	}
	res := map[string]*regexp.Regexp{}
	pattern := func(n string) *regexp.Regexp {
		if r, ok := res[n]; ok {
			return r
		}
		r := regexp.MustCompile("[`,}\"']\\s*" + regexp.QuoteMeta(n) + `["']?\s*:`)
		res[n] = r
		return r
	}
	var out []SwallowFiring
	for i, c := range calls {
		p := ps[i]
		if !p.ok {
			continue
		}
		var missing []string
		for n := range names[c.Tool] {
			if _, ok := p.args[n]; !ok {
				missing = append(missing, n)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)
		keys := make([]string, 0, len(p.args))
		for k := range p.args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	call:
		for _, k := range keys {
			s, ok := p.args[k].(string)
			if !ok {
				continue
			}
			for _, n := range missing {
				if loc := pattern(n).FindStringIndex(s); loc != nil {
					out = append(out, SwallowFiring{Index: i, Turn: c.Turn, Tool: c.Tool, In: k, Swallowed: n, Around: around(s, loc[0], loc[1])})
					break call
				}
			}
		}
	}
	return out
}

// around is the text of s about [from, to), cut at character boundaries.
func around(s string, from, to int) string {
	r := []rune(s)
	// Rune offsets of the byte offsets.
	rf, rt := len([]rune(s[:from])), len([]rune(s[:to]))
	a, b := max(0, rf-swallowAround), min(len(r), rt+swallowAround)
	return strings.TrimSpace(string(r[a:b]))
}
