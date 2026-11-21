package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// An anti-loop export (D158) hands out a run only up to where its first
// loop begins: the requests up to the turn the loop is in, and that turn's
// response cut where the loop's first repeat starts. Nothing the model
// wrote after it, and nothing of its final answer, leaves the daemon; so a
// run of a held-out task (manic_miner, tbench2) may go to anti-loop
// training without its solution becoming a target.

// Onset is a run's first loop span in the order the model wrote it: the
// earliest response, in it the reasoning before the reply before the tool
// calls (by index), and the earliest start. False when there is none.
func Onset(lines []Line) (Line, bool) {
	if len(lines) == 0 {
		return Line{}, false
	}
	best := lines[0]
	for _, l := range lines[1:] {
		if l.Request != best.Request {
			if l.Request < best.Request {
				best = l
			}
			continue
		}
		if a, b := fieldRank(l.Field), fieldRank(best.Field); a != b {
			if a < b {
				best = l
			}
			continue
		}
		if l.Start < best.Start {
			best = l
		}
	}
	return best, true
}

// fieldRank orders a response's fields as the stream gives them.
func fieldRank(field string) int {
	switch field {
	case "reasoning":
		return 0
	case "content":
		return 1
	}
	if i, ok := callIndex(field); ok {
		return 2 + i
	}
	return 1 << 30
}

// callIndex is the index in tool_calls[<index>].function.arguments.
func callIndex(field string) (int, bool) {
	s, ok := strings.CutPrefix(field, "tool_calls[")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, "].function.arguments")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(s)
	return i, err == nil && i >= 0
}

// streamKeys are the stream's keys that fill a message field.
func streamKeys(field string) []string {
	switch field {
	case "reasoning":
		return []string{"reasoning_content", "reasoning"}
	case "content":
		return []string{"content"}
	}
	return nil
}

// CutResponse is a response file (<stem>.response.json) cut at the onset
// of a loop: its stream kept chunk for chunk up to the point where the
// span starts, in the order the stream gave it, and nothing after (no
// finish reason, no usage, no [DONE]); "complete" is false. For a text
// span the field ends at the span's start; for a repeated tool call the
// stream ends before that call's first delta. A span the stream never
// reaches is an error: nothing is handed out then.
func CutResponse(b []byte, at Line) ([]byte, error) {
	var f map[string]json.RawMessage
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s.response.json: %v", at.Request, err)
	}
	var body string
	if err := json.Unmarshal(f["body"], &body); err != nil {
		return nil, fmt.Errorf("%s.response.json: no body: %v", at.Request, err)
	}
	keys := streamKeys(at.Field)
	call, isCall := callIndex(at.Field)
	if keys == nil && !isCall {
		return nil, fmt.Errorf("a span in %s cannot be cut", at.Field)
	}
	var out strings.Builder
	n := 0 // code points of the field kept so far
	reached := false
	for _, l := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(l, "data:")
		data = strings.TrimSpace(data)
		if !ok || data == "" || data == "[DONE]" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(data))
		dec.UseNumber()
		var chunk map[string]any
		if dec.Decode(&chunk) != nil {
			continue
		}
		delta := deltaOf(chunk)
		if delta == nil {
			continue
		}
		if isCall {
			calls, _ := delta["tool_calls"].([]any)
			var before []any
			hit := false
			for _, c := range calls {
				m, _ := c.(map[string]any)
				if i, err := strconv.Atoi(fmt.Sprint(m["index"])); err == nil && i >= call {
					hit = true
					continue
				}
				before = append(before, c)
			}
			if hit {
				reached = true
				if len(before) > 0 {
					writeChunk(&out, chunk, map[string]any{"tool_calls": before})
				}
				break
			}
		} else {
			key, s := "", ""
			for _, k := range keys {
				if v, ok := delta[k].(string); ok && v != "" {
					key, s = k, v
					break
				}
			}
			if key != "" {
				r := []rune(s)
				if n+len(r) >= at.Start {
					reached = true
					if keep := at.Start - n; keep > 0 {
						writeChunk(&out, chunk, map[string]any{key: string(r[:keep])})
					}
					break
				}
				n += len(r)
			}
		}
		out.WriteString("data: " + data + "\n\n")
	}
	if !reached {
		return nil, fmt.Errorf("%s: the stream never reaches the span's start (%s at %d)", at.Request, at.Field, at.Start)
	}
	nb, _ := json.Marshal(out.String())
	f["body"] = nb
	f["complete"] = json.RawMessage("false")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// Keys in a stable order, so the same run cuts to the same file.
	names := make([]string, 0, len(f))
	for k := range f {
		names = append(names, k)
	}
	sort.Strings(names)
	buf.WriteByte('{')
	for i, k := range names {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(f[k])
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// deltaOf is a chunk's first choice's delta.
func deltaOf(chunk map[string]any) map[string]any {
	cs, _ := chunk["choices"].([]any)
	if len(cs) == 0 {
		return nil
	}
	c, _ := cs[0].(map[string]any)
	d, _ := c["delta"].(map[string]any)
	return d
}

// writeChunk writes the chunk with its first choice's delta replaced and
// no finish reason: the last chunk of a cut stream.
func writeChunk(out *strings.Builder, chunk map[string]any, delta map[string]any) {
	cs := chunk["choices"].([]any)
	c := map[string]any{}
	for k, v := range cs[0].(map[string]any) {
		if k != "finish_reason" {
			c[k] = v
		}
	}
	if role, ok := deltaOf(chunk)["role"]; ok {
		delta["role"] = role
	}
	c["delta"] = delta
	chunk["choices"] = []any{c}
	delete(chunk, "usage")
	b, _ := json.Marshal(chunk)
	out.WriteString("data: " + string(b) + "\n\n")
}
