// Package export hands a run's model requests out as training traces
// (110 §3.5, D102, D104, D106): the SDK's own capture as it was written,
// the run's metadata beside it, and the loop spans in the responses, from
// the SDK guard's cuts and from the detector library.
//
// Nothing here rebuilds a turn. The capture is passed on file for file; the
// only things read out of it are each response's text fields, to place the
// spans in them.
package export

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mann1x/automaticum/internal/detect"
)

// The fields of a streamed response that hold text, by the name the stream
// gives them. A span names the field it is in.
var textFields = []string{"reasoning_content", "reasoning", "content"}

// Response is one captured response, as the SDK wrote it beside its
// request: <stem>.response.json.
type Response struct {
	Stem     string
	At       time.Time
	Status   int
	Complete bool
	// Fields is each text field's whole text, the stream's deltas joined.
	Fields map[string]string
	// Calls are the tool calls in the response, by their index in it.
	Calls []ToolCall
	// Finish is the stream's finish reason; "length" is a response that
	// ended at its output limit.
	Finish string
}

// ToolCall is one tool call of a streamed response: its index among the
// response's calls, its name and its arguments, the deltas joined.
type ToolCall struct {
	Index     int
	Name      string
	Arguments string
}

// ReadResponse reads one response file.
func ReadResponse(stem string, b []byte) (Response, error) {
	var f struct {
		At       time.Time `json:"at"`
		Status   int       `json:"status"`
		Body     string    `json:"body"`
		Complete *bool     `json:"complete"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Response{}, fmt.Errorf("%s.response.json: %v", stem, err)
	}
	resp := Response{Stem: stem, At: f.At, Status: f.Status, Complete: f.Complete == nil || *f.Complete, Fields: map[string]string{}}
	parts := map[string]*strings.Builder{}
	for _, l := range strings.Split(f.Body, "\n") {
		data, ok := strings.CutPrefix(l, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta        map[string]json.RawMessage `json:"delta"`
				FinishReason string                     `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		if f := chunk.Choices[0].FinishReason; f != "" {
			resp.Finish = f
		}
		var calls []struct {
			Index    int `json:"index"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if raw, ok := chunk.Choices[0].Delta["tool_calls"]; ok && json.Unmarshal(raw, &calls) == nil {
			for _, c := range calls {
				tc := callAt(&resp, c.Index)
				if c.Function.Name != "" {
					tc.Name = c.Function.Name
				}
				tc.Arguments += c.Function.Arguments
			}
		}
		for _, name := range textFields {
			raw, ok := chunk.Choices[0].Delta[name]
			var s string
			if !ok || json.Unmarshal(raw, &s) != nil || s == "" {
				continue
			}
			if parts[name] == nil {
				parts[name] = &strings.Builder{}
			}
			parts[name].WriteString(s)
		}
	}
	for name, b := range parts {
		resp.Fields[name] = b.String()
	}
	return resp, nil
}

// callAt is the response's tool call of that index, added if it is new.
func callAt(r *Response, index int) *ToolCall {
	for i := range r.Calls {
		if r.Calls[i].Index == index {
			return &r.Calls[i]
		}
	}
	r.Calls = append(r.Calls, ToolCall{Index: index})
	return &r.Calls[len(r.Calls)-1]
}

// Cut is one cut by an SDK guard, as its notice in the event log says it:
// the verdict the guard reached and the channel it read.
type Cut struct {
	At        time.Time `json:"at"`
	Guard     string    `json:"guard"`
	Phase     string    `json:"phase"`
	Cause     string    `json:"cause,omitempty"`
	Channel   string    `json:"channel"`
	Iteration int       `json:"iteration"`
	Kind      string    `json:"kind"`
	Period    int       `json:"period,omitempty"`
	Cycles    int       `json:"cycles,omitempty"`
	// Chars is how much of the channel the guard had read, in the SDK's
	// own count (UTF-16 code units).
	Chars int `json:"chars"`
	// Request is the response the cut is in, once it is found.
	Request string `json:"request,omitempty"`
	// Unplaced says why no span could be placed for it.
	Unplaced string `json:"unplaced,omitempty"`
}

// ReadCuts reads the guards' cuts out of a run's event log: every SDK
// notice that carries a verdict.
func ReadCuts(events io.Reader) ([]Cut, error) {
	var cuts []Cut
	rd := bufio.NewReaderSize(events, 1<<20)
	for {
		l, err := rd.ReadBytes('\n')
		if len(l) > 0 && bytes.Contains(l, []byte(`"verdict"`)) {
			if c, ok := readCut(l); ok {
				cuts = append(cuts, c)
			}
		}
		if err == io.EOF {
			return cuts, nil
		}
		if err != nil {
			return cuts, err
		}
	}
}

func readCut(l []byte) (Cut, bool) {
	var e struct {
		T     time.Time `json:"t"`
		K     string    `json:"k"`
		Event struct {
			Payload struct {
				Event struct {
					Type     string `json:"type"`
					Metadata struct {
						Kind      string `json:"kind"`
						Phase     string `json:"phase"`
						Cause     string `json:"cause"`
						Channel   string `json:"channel"`
						Iteration int    `json:"iteration"`
						Verdict   *struct {
							Kind   string `json:"kind"`
							Period int    `json:"period"`
							Cycles int    `json:"cycles"`
							Chars  int    `json:"chars"`
						} `json:"verdict"`
					} `json:"metadata"`
				} `json:"event"`
			} `json:"payload"`
		} `json:"event"`
	}
	if json.Unmarshal(l, &e) != nil || e.K != "event" {
		return Cut{}, false
	}
	ev := e.Event.Payload.Event
	m := ev.Metadata
	if ev.Type != "notice" || m.Verdict == nil || m.Verdict.Chars <= 0 {
		return Cut{}, false
	}
	return Cut{At: e.T, Guard: m.Kind, Phase: m.Phase, Cause: m.Cause, Channel: m.Channel, Iteration: m.Iteration,
		Kind: m.Verdict.Kind, Period: m.Verdict.Period, Cycles: m.Verdict.Cycles, Chars: m.Verdict.Chars}, true
}

// Line is one loop span, one JSON line of the export's spans (D106).
type Line struct {
	Request string `json:"request"`
	// Field is the message field the span is in: reasoning or content,
	// whichever key the stream gave it (reasoning_content or reasoning).
	// For a tool call it is tool_calls[<index>].function.arguments, the
	// path of the arguments in the response's message.
	Field  string `json:"field"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Source string `json:"source"` // guard or detector
	// Detail is which guard verdict or detector found it.
	Detail string `json:"detail"`
}

// The sources of a span.
const (
	Guard    = "guard"
	Detector = "detector"
)

// Spans places every loop span in a run's responses: each guard cut in
// the response it cut, every detector hit in every response's text fields,
// and every repeat of a tool call in the arguments of the calls that
// repeat it. The cuts come back with the response each one is in, or why
// none was found.
func Spans(resps []Response, cuts []Cut) ([]Line, []Cut) {
	var out []Line
	placed := make([]Cut, len(cuts))
	used := map[string]bool{}
	for i, c := range cuts {
		line, c := guardSpan(resps, used, c)
		placed[i] = c
		if line != nil {
			out = append(out, *line)
		}
	}
	for _, r := range resps {
		for _, name := range textFields {
			text, ok := r.Fields[name]
			if !ok {
				continue
			}
			for _, h := range detect.Block(text, detect.Options{}) {
				detail := h.Detector
				if h.Kind != "" {
					detail += ":" + h.Kind
				}
				for _, s := range detect.Spans(text, h) {
					out = append(out, Line{Request: r.Stem, Field: messageField(name), Start: s.Start, End: s.End, Source: Detector, Detail: detail})
				}
			}
		}
	}
	out = append(out, repeatedCallSpans(resps)...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Request != out[j].Request {
			return out[i].Request < out[j].Request
		}
		return out[i].Start < out[j].Start
	})
	return out, placed
}

// repeatedCallSpans marks the tool calls that repeat the one before them:
// the run's calls in the order the responses came, each compared as the
// analysis compares them (detect.RepeatedCalls, the SDK's own threshold),
// the first of a sequence left out as the text detectors leave out the
// first occurrence. A span is the whole of a repeat's arguments, in the
// field tool_calls[<index>].function.arguments of its response.
func repeatedCallSpans(resps []Response) []Line {
	ordered := make([]*Response, len(resps))
	for i := range resps {
		ordered[i] = &resps[i]
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Stem < ordered[j].Stem })
	type at struct {
		stem string
		call *ToolCall
	}
	var calls []detect.Call
	var where []at
	for turn, r := range ordered {
		for i := range r.Calls {
			c := &r.Calls[i]
			calls = append(calls, detect.Call{Turn: turn, Tool: c.Name, Input: compactArgs(c.Arguments)})
			where = append(where, at{r.Stem, c})
		}
	}
	var out []Line
	for _, seq := range detect.RepeatedCalls(calls, 0) {
		for k := seq.Index + 1; k < seq.Index+seq.Count; k++ {
			w := where[k]
			out = append(out, Line{Request: w.stem, Field: fmt.Sprintf("tool_calls[%d].function.arguments", w.call.Index),
				Start: 0, End: len([]rune(w.call.Arguments)), Source: Detector, Detail: detect.RepeatedToolCall})
		}
	}
	return out
}

// compactArgs gives a call's arguments one spelling, as the analysis does,
// so that the same arguments compare equal however they were spaced.
func compactArgs(s string) string {
	var v any
	if strings.TrimSpace(s) == "" || json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	b, _ := json.Marshal(v) // maps are written with sorted keys
	return string(b)
}

// guardSpan finds the response a cut is in and places its span (D106): the
// response the guard cut is not complete, ends just before the notice, and
// its field is as long as the guard had read; the span runs from the second
// of the trailing repeats to where the guard stopped reading.
func guardSpan(resps []Response, used map[string]bool, c Cut) (*Line, Cut) {
	var field string
	switch c.Channel {
	case "reasoning":
		field = "reasoning_content"
	case "reply":
		field = "content"
	default:
		c.Unplaced = fmt.Sprintf("channel %q is not a text field of the response", c.Channel)
		return nil, c
	}
	// The latest response that was not complete by the notice, as long as
	// the guard had read; another of that length if that one is not.
	var hit *Response
	for i := len(resps) - 1; i >= 0 && hit == nil; i-- {
		r := &resps[i]
		if text, ok := fieldOf(r, field); ok && !r.Complete && !used[r.Stem] && !r.At.After(c.At.Add(time.Second)) && utf16Len(text) == c.Chars {
			hit = r
		}
	}
	if hit == nil {
		c.Unplaced = fmt.Sprintf("no incomplete response before the notice has %d characters of %s", c.Chars, field)
		return nil, c
	}
	used[hit.Stem] = true
	c.Request = hit.Stem
	text, _ := fieldOf(hit, field)
	field = fieldName(hit, field)
	r := []rune(text)
	end := runeAt(r, c.Chars)
	var start int
	var ok bool
	switch c.Kind {
	case "line-cycle", "block-cycle":
		start, ok = detect.CycleStart(r[:end], c.Period, c.Cycles)
	case "low-uniqueness":
		start, ok = detect.WindowRepeatStart(r[:end])
	case "phrase-cycle":
		start = runeAt(r, c.Chars-(c.Cycles-1)*c.Period)
		ok = c.Cycles > 1 && c.Period > 0 && start < end
	}
	if !ok {
		c.Unplaced = fmt.Sprintf("the %s verdict (period %d, %d cycles) does not match the end of %s", c.Kind, c.Period, c.Cycles, field)
		return nil, c
	}
	return &Line{Request: hit.Stem, Field: messageField(field), Start: start, End: end, Source: Guard, Detail: c.Guard + ":" + c.Kind}, c
}

// messageField is the message field a stream's text key fills.
func messageField(key string) string {
	if key == "content" {
		return "content"
	}
	return "reasoning"
}

// fieldOf is a response's reasoning or reply text, under whichever name
// its stream gave it.
func fieldOf(r *Response, field string) (string, bool) {
	t, ok := r.Fields[fieldName(r, field)]
	return t, ok
}

func fieldName(r *Response, field string) string {
	if _, ok := r.Fields[field]; !ok && field == "reasoning_content" {
		if _, ok := r.Fields["reasoning"]; ok {
			return "reasoning"
		}
	}
	return field
}

// utf16Len is a text's length as JavaScript counts it, which is how the
// SDK's guard counts what it read.
func utf16Len(s string) int {
	n := 0
	for _, c := range s {
		n += utf16.RuneLen(c)
	}
	return n
}

// runeAt is the code point that stands at a UTF-16 offset.
func runeAt(r []rune, units int) int {
	n := 0
	for i, c := range r {
		if n >= units {
			return i
		}
		n += utf16.RuneLen(c)
	}
	return len(r)
}
