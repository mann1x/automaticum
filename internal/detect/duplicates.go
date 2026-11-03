package detect

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// DuplicateDefinitions is a tool call one of whose string arguments
// defines the same names again and again: more than DuplicateDefinitionsMax
// definitions of a name the same string already defined. A client's
// trained model, asked to fix a game file of about 130 lines, wrote an
// editor call of 218,643 characters whose text turned into invented stubs
// (`function drawPlayers5()?{}`, `function drawPlayers6()?{}`) until the
// output limit cut it: 1,541 definitions of 1,049 names. No detector of
// what a model writes saw it, as they read reasoning and text, not the
// arguments of calls.
//
// A definition is a line holding `function <name>`, or starting with
// `def`, `fn` or `func` and a name, or starting with a name, its
// parameters and an opening brace (a method in a class), keywords left
// out. What a call's string holds after the output limit cut it is read
// as far as it went.
const DuplicateDefinitions = "duplicate_definitions"

// DuplicateDefinitionsMax is the most repeated definitions in one string
// that is not a firing. It is measured, not chosen: on 2026-11-03, over
// the 637 string arguments of 20 lines or more in the 371 runs then on
// the client's lane host, every run of a base model (413 strings, 15
// tasks) and of the harness's stub wrote at most 3. The reference game
// file itself has 3, methods of one name in several classes. The trained
// models that broke wrote 6, 18, 33 and 492.
const DuplicateDefinitionsMax = 3

// DefinitionFiring is one string argument with repeated definitions.
type DefinitionFiring struct {
	Index int `json:"index"`
	Turn  int `json:"turn,omitempty"`
	// Request is the wire's request, for a call read from the wire.
	Request     int    `json:"request,omitempty"`
	Tool        string `json:"tool"`
	In          string `json:"in"`
	Chars       int    `json:"chars"`
	Definitions int    `json:"definitions"`
	Names       int    `json:"names"`
	// Repeated is the definitions of names already defined.
	Repeated int `json:"repeated"`
	// Top are the names defined most often, at most five.
	Top []NameCount `json:"top"`
	// Cut is a call whose arguments end before their JSON does.
	Cut bool `json:"cut,omitempty"`
}

// NameCount is a name and how often it was defined.
type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

var definitionRE = regexp.MustCompile(`\bfunction\s+([A-Za-z_$][A-Za-z0-9_$]*)|^\s*(?:def|fn|func)\s+([A-Za-z_][A-Za-z0-9_]*)|^\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\([^)]*\)\s*\{`)

// notNames are the words that open a line like a definition and are none.
var notNames = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "with": true, "function": true,
	"return": true, "else": true, "do": true, "try": true, "foreach": true, "elif": true, "match": true, "loop": true, "unless": true,
	"until": true, "using": true, "lock": true, "fixed": true, "sizeof": true, "typeof": true, "new": true, "await": true, "async": true,
	"super": true, "this": true}

// Definitions counts the definitions in a text, by name.
func Definitions(text string) map[string]int {
	out := map[string]int{}
	for _, l := range strings.Split(text, "\n") {
		d := definitionRE.FindStringSubmatch(l)
		if d == nil {
			continue
		}
		if n := d[1] + d[2] + d[3]; !notNames[n] {
			out[n]++
		}
	}
	return out
}

// StringArgs are a call's string arguments, and whether its arguments
// were cut off: JSON that ends early is read as far as it went.
func StringArgs(input string) (map[string]string, bool) {
	var m map[string]any
	cut := false
	if json.Unmarshal([]byte(input), &m) != nil {
		cut = true
		for _, end := range []string{`"}`, `}`, `"}}`, `"]}`, `\"}`, `\\"}`} {
			if json.Unmarshal([]byte(input+end), &m) == nil {
				break
			}
		}
	}
	out := map[string]string{}
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out, cut
}

// DuplicateDefinitionCalls reads every call's string arguments.
func DuplicateDefinitionCalls(calls []Call) []DefinitionFiring {
	var out []DefinitionFiring
	for i, c := range calls {
		args, cut := StringArgs(c.Input)
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			defs := Definitions(args[k])
			total := 0
			for _, n := range defs {
				total += n
			}
			if total-len(defs) <= DuplicateDefinitionsMax {
				continue
			}
			f := DefinitionFiring{Index: i, Turn: c.Turn, Tool: c.Tool, In: k, Chars: len([]rune(args[k])), Definitions: total, Names: len(defs),
				Repeated: total - len(defs), Cut: cut}
			for n, c := range defs {
				if c > 1 {
					f.Top = append(f.Top, NameCount{n, c})
				}
			}
			sort.Slice(f.Top, func(a, b int) bool {
				return f.Top[a].Count > f.Top[b].Count || f.Top[a].Count == f.Top[b].Count && f.Top[a].Name < f.Top[b].Name
			})
			if len(f.Top) > 5 {
				f.Top = f.Top[:5]
			}
			out = append(out, f)
		}
	}
	return out
}
