package ingest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
)

// ModelIdentity is the finding that a run's model has no stated identity,
// or an incomplete one: its family, size or variant is not on record.
const ModelIdentity = "model_identity"

// Setting is one sampling or thinking parameter of a run, from every side:
// what the arm states, what the requests carried, what the model tag sets
// for itself, and from those, what was in effect.
type Setting struct {
	Name   string `json:"name"`
	Stated string `json:"stated,omitempty"` // by the arm
	Sent   string `json:"sent,omitempty"`   // on the wire
	Tag    string `json:"tag,omitempty"`    // the tag's own value
	// Effective is the value that applied, and Source why: "sent" (the
	// request carried it), "model tag" (nothing was sent, the tag's value
	// applied), "server answer" (the server said so), or "server default"
	// (nobody set it).
	Effective string `json:"effective"`
	Source    string `json:"source"`
	// Note says what is unusual: stated and not sent, changed during the
	// run.
	Note string `json:"note,omitempty"`
}

// InEffect lists a run's sampling and thinking parameters as they were in
// effect. It reads what is already derived: the arm, the tag's parameters as
// the endpoint served them, and the wire_arm findings, which name every
// field whose request differed from the arm. A field without a finding was
// sent as stated.
func InEffect(arm *config.Arm, tag string, findings []store.Finding, served float64) []Setting {
	params := tagParameters(tag)
	devs := map[string][]Deviation{}
	for _, f := range findings {
		if f.Detector != WireArm {
			continue
		}
		var d Deviation
		if json.Unmarshal(f.Evidence, &d) == nil {
			devs[d.Field] = append(devs[d.Field], d)
		}
	}
	var stated map[string]any
	raw, _ := json.Marshal(arm.Sampler)
	json.Unmarshal(raw, &stated)
	show := func(v any) string {
		b, _ := json.Marshal(v)
		return strings.Trim(string(b), `"`)
	}
	var out []Setting
	for _, name := range samplerOptions {
		s := Setting{Name: name, Tag: params[name]}
		v, ok := stated[name]
		if ok && arm.Sampler.Owner == "arm" {
			s.Stated, s.Sent = show(v), show(v)
		}
		for _, d := range devs["options."+name] {
			switch d.Kind {
			case StatedNotSent:
				s.Sent, s.Note = "", "stated, not sent"
			case Differs, SentNotStated:
				s.Sent = strings.Trim(string(d.Sent), `"`)
				s.Note = map[string]string{Differs: "sent another value than stated", SentNotStated: "sent, not stated"}[d.Kind]
			case Drift:
				s.Note = strings.TrimSpace(s.Note + "; changed during the run: " + strings.Join(d.Values, ", "))
				s.Note = strings.TrimPrefix(s.Note, "; ")
			}
		}
		switch {
		case s.Sent != "":
			s.Effective, s.Source = s.Sent, "sent"
		case s.Tag != "":
			s.Effective, s.Source = s.Tag, "model tag"
		default:
			if s.Stated == "" {
				continue // nobody speaks of it
			}
			s.Effective, s.Source = "unknown", "server default"
		}
		out = append(out, s)
	}
	// The window and thinking, which are not sampler options but decide
	// what was generated as much.
	if w := arm.Model.ContextWindow; w != nil {
		s := Setting{Name: "context_window", Stated: strconv.Itoa(*w), Sent: strconv.Itoa(*w), Tag: params["num_ctx"]}
		for _, d := range devs["options.num_ctx"] {
			if d.Kind == StatedNotSent {
				s.Sent, s.Note = "", "stated, not sent"
			} else if d.Kind == Differs {
				s.Sent, s.Note = string(d.Sent), "sent another value than stated"
			}
		}
		s.Effective, s.Source = s.Sent, "sent"
		if s.Sent == "" {
			s.Effective, s.Source = s.Tag, "model tag"
		}
		out = append(out, s)
	}
	t := Setting{Name: "thinking", Tag: params["think_budget"]}
	switch th := arm.Thinking; {
	case th.Owner != "arm":
		t.Stated = "left to the tag"
	case th.Enabled != nil && !*th.Enabled:
		t.Stated, t.Sent = "off", "false"
	case th.Effort != "":
		t.Stated, t.Sent = "effort "+th.Effort, th.Effort
	default:
		t.Stated, t.Sent = "on", "true"
	}
	for _, d := range devs["think"] {
		switch d.Kind {
		case StatedNotSent:
			t.Sent, t.Note = strings.Trim(string(d.Sent), `"`), "stated, not sent as stated"
			if d.Effective != "" {
				t.Effective, t.Source = d.Effective, d.Source
			}
		case SentNotStated:
			t.Sent, t.Note = strings.Trim(string(d.Sent), `"`), "sent, not stated"
		}
	}
	if t.Effective == "" {
		switch {
		case served > 0:
			t.Effective, t.Source = fmt.Sprintf("budget %.0f tokens", served), "server answer"
			if t.Sent != "" && t.Sent != "true" && t.Sent != "false" {
				t.Effective = t.Sent + " (" + t.Effective + ")"
			}
		case t.Sent == "false":
			t.Effective, t.Source = "off", "sent"
		case t.Tag != "":
			t.Effective, t.Source = t.Tag, "model tag"
		default:
			t.Effective, t.Source = "unknown", "server default"
		}
	}
	return append(out, t)
}
