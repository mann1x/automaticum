package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
)

// WireArm is the detector that compares what was sent with the arm.
const (
	WireArm        = "wire_arm"
	WireArmVersion = "3"
)

// The kinds of difference between the arm and the wire.
const (
	StatedNotSent = "stated_not_sent" // the arm states it, no request carries it
	SentNotStated = "sent_not_stated" // a request carries it, the arm does not state it
	Differs       = "differs"         // both have it, with different values
	Drift         = "drift"           // it changed between the requests of one run
)

// Deviation is the evidence of one wire_arm finding.
type Deviation struct {
	Kind  string `json:"kind"`
	Field string `json:"field"` // as it is named on the wire
	Arm   string `json:"arm_field,omitempty"`
	// Stated is the arm's value and Sent the request's, as JSON.
	Stated json.RawMessage `json:"stated,omitempty"`
	Sent   json.RawMessage `json:"sent,omitempty"`
	// Effective is the value that applied when the arm's was not sent, and
	// Source says where it was read: the model tag or the server's answer.
	Effective string `json:"effective,omitempty"`
	Source    string `json:"effective_source,omitempty"`
	Requests  int    `json:"requests"`  // how many requests show it
	Of        int    `json:"of"`        // out of how many agent requests
	FirstSeq  int    `json:"first_seq"` // the first such request in the wire capture
	// Values lists what was seen, for a drift.
	Values []string `json:"values,omitempty"`
}

// maxValues is how many distinct values a drift lists.
const maxValues = 8

// samplerOptions maps the arm's sampler fields to the options of a request.
var samplerOptions = []string{
	"temperature", "top_p", "top_k", "min_p", "typical_p", "repeat_penalty",
	"repeat_last_n", "presence_penalty", "frequency_penalty", "seed", "stop",
}

// ownOptions are options the harness or the SDK sets for reasons other than
// the sampler; they are compared on their own or not at all.
var ownOptions = map[string]bool{"num_ctx": true, "num_predict": true}

// CompareWire compares every agent request of a run with its arm. tag holds
// the model tag's own parameters as the endpoint served them before the run.
// A difference never changes a verdict: it becomes a finding.
func CompareWire(arm *config.Arm, tag string, reqs []Request) []Deviation {
	var agent []Request
	for _, q := range reqs {
		if q.Agent {
			agent = append(agent, q)
		}
	}
	if len(agent) == 0 || arm == nil {
		return nil
	}
	params := tagParameters(tag)
	var out []Deviation
	add := func(d Deviation, hit func(q *Request) bool) {
		first := true
		for i := range agent {
			if hit(&agent[i]) {
				d.Requests++
				if first {
					d.FirstSeq, first = agent[i].Seq, false
				}
			}
		}
		if d.Requests > 0 {
			d.Of = len(agent)
			out = append(out, d)
		}
	}
	// expect compares one stated value with one field of the requests.
	expect := func(armField, field string, stated any, get func(q *Request) (any, bool)) {
		raw, _ := json.Marshal(stated)
		missing := Deviation{Kind: StatedNotSent, Field: field, Arm: armField, Stated: raw}
		if v, ok := params[strings.TrimPrefix(field, "options.")]; ok {
			missing.Effective, missing.Source = v, "model tag"
		}
		add(missing, func(q *Request) bool { _, ok := get(q); return !ok })
		// One finding per value sent in place of the stated one.
		seen := map[string]bool{}
		for i := range agent {
			v, ok := get(&agent[i])
			if !ok || same(stated, v) {
				continue
			}
			sent, _ := json.Marshal(v)
			if seen[string(sent)] {
				continue
			}
			seen[string(sent)] = true
			add(Deviation{Kind: Differs, Field: field, Arm: armField, Stated: raw, Sent: sent}, func(q *Request) bool {
				w, ok := get(q)
				if !ok {
					return false
				}
				b, _ := json.Marshal(w)
				return string(b) == string(sent)
			})
		}
	}
	option := func(name string) func(q *Request) (any, bool) {
		return func(q *Request) (any, bool) { v, ok := q.Options[name]; return v, ok }
	}
	top := func(name string) func(q *Request) (any, bool) {
		return func(q *Request) (any, bool) {
			raw, ok := q.Body[name]
			if !ok {
				return nil, false
			}
			var v any
			json.Unmarshal(raw, &v)
			return v, true
		}
	}

	expect("model.model", "model", arm.Model.Model, top("model"))
	// An OpenAI-compatible request has no context field: the server holds
	// the context, and a mode C cell checks it against /props (065 §3.1).
	openAI := true
	for i := range agent {
		openAI = openAI && agent[i].OpenAI
	}
	if arm.Model.ContextWindow != nil && !openAI {
		expect("model.context_window", "options.num_ctx", *arm.Model.ContextWindow, option("num_ctx"))
	}
	if arm.Model.OutputTokens != nil && *arm.Model.OutputTokens > 0 {
		expect("model.output_tokens", "options.num_predict", *arm.Model.OutputTokens, option("num_predict"))
	}

	// The sampler. The arm's values are read through its JSON form, so a
	// field added to the arm is compared without a line here.
	var stated map[string]any
	raw, _ := json.Marshal(arm.Sampler)
	json.Unmarshal(raw, &stated)
	for _, name := range samplerOptions {
		v, ok := stated[name]
		if arm.Sampler.Owner == "arm" && ok {
			expect("sampler."+name, "options."+name, v, option(name))
			continue
		}
		// Not stated, or the tag owns the sampler: nothing should be sent.
		for _, sent := range distinct(agent, option(name)) {
			sent := sent
			add(Deviation{Kind: SentNotStated, Field: "options." + name, Sent: json.RawMessage(sent)}, func(q *Request) bool {
				w, ok := q.Options[name]
				b, _ := json.Marshal(w)
				return ok && string(b) == sent
			})
		}
	}
	// Options nobody accounts for.
	extra := map[string]bool{}
	for i := range agent {
		for name := range agent[i].Options {
			extra[name] = true
		}
	}
	for _, name := range samplerOptions {
		delete(extra, name)
	}
	for _, name := range sortedKeys(extra) {
		if ownOptions[name] || strings.HasPrefix(name, "think") {
			continue
		}
		name := name
		add(Deviation{Kind: SentNotStated, Field: "options." + name, Sent: first(agent, option(name))}, func(q *Request) bool {
			_, ok := q.Options[name]
			return ok
		})
	}

	// Thinking. On the wire "think" is false, true (on, the budget left to
	// the tag) or an effort level.
	think := top("think")
	budget := func(q *Request) string {
		if q.ThinkBudget == "" && q.ThinkBudgetTokens == 0 {
			return ""
		}
		return fmt.Sprintf("%s (%d tokens)", q.ThinkBudget, q.ThinkBudgetTokens)
	}
	served := func(d Deviation) Deviation {
		for i := range agent {
			if b := budget(&agent[i]); b != "" {
				d.Effective, d.Source = b, "server answer"
				return d
			}
		}
		if v, ok := params["think_budget"]; ok {
			d.Effective, d.Source = v, "model tag"
		}
		return d
	}
	switch arm.Thinking.Owner {
	case "arm":
		on := arm.Thinking.Enabled == nil || *arm.Thinking.Enabled
		if !on {
			expect("thinking.enabled", "think", false, think)
			break
		}
		if e := arm.Thinking.Effort; e != "" {
			raw, _ := json.Marshal(e)
			// An OpenAI-compatible request carries the effort as a budget
			// in tokens, which the SDK works out from the output limit.
			asBudget := func(q *Request) bool {
				b, ok := q.Options["think_budget_tokens"].(float64)
				return q.OpenAI && ok && b > 0
			}
			add(served(Deviation{Kind: StatedNotSent, Field: "think", Arm: "thinking.effort", Stated: raw, Sent: first(agent, think)}),
				func(q *Request) bool { v, _ := think(q); return v != e && !asBudget(q) })
		} else {
			add(Deviation{Kind: StatedNotSent, Field: "think", Arm: "thinking.enabled", Stated: json.RawMessage("true"), Sent: first(agent, think)},
				func(q *Request) bool { v, ok := think(q); return !ok || v == false })
		}
		if b := arm.Thinking.BudgetTokens; b != nil {
			get := option("think_budget")
			raw, _ := json.Marshal(*b)
			add(served(Deviation{Kind: StatedNotSent, Field: "options.think_budget", Arm: "thinking.budget_tokens", Stated: raw}),
				func(q *Request) bool { v, ok := get(q); return !ok || !same(*b, v) })
		}
	default:
		// The tag owns thinking: "think": true says only "on", so anything
		// else sent is the harness speaking where it said it would not.
		for _, sent := range distinct(agent, think) {
			if sent == "true" {
				continue
			}
			sent := sent
			add(Deviation{Kind: SentNotStated, Field: "think", Sent: json.RawMessage(sent)}, func(q *Request) bool {
				v, ok := think(q)
				b, _ := json.Marshal(v)
				return ok && string(b) == sent
			})
		}
		for _, name := range []string{"think_budget", "think_budget_message", "think_budget_tokens"} {
			name := name
			add(Deviation{Kind: SentNotStated, Field: "options." + name, Sent: first(agent, option(name))}, func(q *Request) bool {
				_, ok := q.Options[name]
				return ok
			})
		}
	}

	// Drift: a setting that was not the same in every request of the run.
	fields := map[string]func(q *Request) (any, bool){}
	for i := range agent {
		for name := range agent[i].Options {
			fields["options."+name] = option(name)
		}
		for name := range agent[i].Body {
			if name != "options" {
				fields[name] = top(name)
			}
		}
	}
	for _, field := range sortedKeys(fields) {
		get := fields[field]
		values := distinct(agent, get)
		absent, firstSeq := 0, -1
		firstValue, _ := get(&agent[0])
		_, firstHas := get(&agent[0])
		for i := range agent {
			v, ok := get(&agent[i])
			if !ok {
				absent++
			}
			if firstSeq < 0 && (ok != firstHas || (ok && !same(firstValue, v))) {
				firstSeq = agent[i].Seq
			}
		}
		if len(values) < 2 && absent == 0 {
			continue
		}
		if absent > 0 {
			values = append(values, "(absent)")
		}
		if len(values) < 2 {
			continue
		}
		changed := 0
		for i := range agent {
			v, ok := get(&agent[i])
			if ok != firstHas || (ok && !same(firstValue, v)) {
				changed++
			}
		}
		if len(values) > maxValues {
			values = append(values[:maxValues:maxValues], fmt.Sprintf("(%d more)", len(values)-maxValues))
		}
		d := Deviation{Kind: Drift, Field: field, Requests: changed, Of: len(agent), FirstSeq: firstSeq, Values: values}
		if field == "options.num_predict" && (arm.Model.OutputTokens == nil || *arm.Model.OutputTokens == 0) {
			d.Arm = "model.output_tokens" // left to the SDK's own rule
		}
		out = append(out, d)
	}
	return out
}

// Findings turns deviations into findings of the wire_arm detector.
func wireFindings(runID string, devs []Deviation) []store.Finding {
	var out []store.Finding
	for _, d := range devs {
		ev, _ := json.Marshal(d)
		sev := "warning"
		// A drift is a warning when the arm governs the field. An output
		// budget the arm leaves to the SDK may move with the context.
		if d.Kind == Drift && (!settingField(d.Field) || d.Arm == "model.output_tokens") {
			sev = "info"
		}
		out = append(out, store.Finding{RunID: runID, Detector: WireArm, DetectorVersion: WireArmVersion,
			Severity: sev, Turn: d.FirstSeq, Count: d.Requests, Evidence: ev})
	}
	return out
}

// settingField reports whether a field of a request is one an arm governs.
func settingField(field string) bool {
	return field == "model" || field == "think" || strings.HasPrefix(field, "options.")
}

// same compares a stated value with a sent one, numbers by value.
func same(a, b any) bool {
	fa, oka := number(a)
	fb, okb := number(b)
	if oka && okb {
		return fa == fb
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func number(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// distinct lists the values a field took, as JSON, in order of appearance.
func distinct(agent []Request, get func(q *Request) (any, bool)) []string {
	var out []string
	seen := map[string]bool{}
	for i := range agent {
		v, ok := get(&agent[i])
		if !ok {
			continue
		}
		b, _ := json.Marshal(v)
		if !seen[string(b)] {
			seen[string(b)] = true
			out = append(out, string(b))
		}
	}
	return out
}

func first(agent []Request, get func(q *Request) (any, bool)) json.RawMessage {
	if d := distinct(agent, get); len(d) > 0 {
		return json.RawMessage(d[0])
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tagParameters reads a model tag's parameters as Ollama prints them: one
// "name value" per line, strings quoted.
func tagParameters(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if s, err := strconv.Unquote(value); err == nil {
			value = s
		}
		if old, dup := out[name]; dup {
			value = old + ", " + value // "stop" may repeat
		}
		out[name] = value
	}
	return out
}
