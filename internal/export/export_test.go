package export

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// sse is a streamed response body carrying these reasoning deltas.
func sse(deltas ...string) string {
	var b strings.Builder
	for _, d := range deltas {
		c, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"reasoning_content": d}}}})
		b.WriteString("data: " + string(c) + "\n\n")
	}
	return b.String() + "data: [DONE]\n\n"
}

func response(t *testing.T, stem string, at time.Time, complete bool, body string) Response {
	b, _ := json.Marshal(map[string]any{"at": at, "status": 200, "body": body, "complete": complete})
	r, err := ReadResponse(stem, b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestGuardSpan(t *testing.T) {
	// A character outside the BMP before the loop: two UTF-16 units, one
	// code point, so the guard's count and the span's differ by one.
	lead := "Thinking 𝄞 about it.\n"
	block := "Wait, check w[1] again.\nIt is the method.\n"
	text := lead + strings.Repeat(block, 3)
	chars := len(utf16.Encode([]rune(text)))
	t0 := time.Date(2026, 10, 24, 4, 6, 30, 0, time.UTC)
	resps := []Response{
		response(t, "0001", t0.Add(-time.Minute), true, sse("done\n")),
		response(t, "0002", t0.Add(-40*time.Millisecond), false, sse(lead, block, block, block)),
	}
	cut := Cut{At: t0, Guard: "reasoning_loop", Phase: "cut", Channel: "reasoning", Kind: "block-cycle", Period: 2, Cycles: 3, Chars: chars}
	lines, cuts := Spans(resps, []Cut{cut})
	if cuts[0].Request != "0002" || cuts[0].Unplaced != "" {
		t.Fatalf("cut %+v", cuts[0])
	}
	var g *Line
	for i := range lines {
		if lines[i].Source == Guard {
			g = &lines[i]
		}
	}
	if g == nil {
		t.Fatalf("no guard span in %+v", lines)
	}
	start := len([]rune(lead + block))
	if g.Field != "reasoning" || g.Start != start || g.End != len([]rune(text)) {
		t.Fatalf("span %+v, want %d to %d", *g, start, len([]rune(text)))
	}
}

func TestGuardSpanUnplaced(t *testing.T) {
	t0 := time.Now()
	resps := []Response{response(t, "0001", t0, false, sse("short\n"))}
	_, cuts := Spans(resps, []Cut{{At: t0, Channel: "reasoning", Kind: "line-cycle", Period: 1, Cycles: 21, Chars: 99999}})
	if cuts[0].Unplaced == "" || cuts[0].Request != "" {
		t.Fatalf("a cut with no response of its length was placed: %+v", cuts[0])
	}
}

func TestReadCuts(t *testing.T) {
	log := `{"t":"2026-10-24T04:06:30.337Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"notice","message":"m","metadata":{"kind":"reasoning_loop","phase":"cut","cause":"reasoning_loop","iteration":16,"verdict":{"kind":"block-cycle","period":30,"cycles":3,"chars":35734,"sample":"x"},"channel":"reasoning"}}}}}
{"t":"2026-10-24T04:06:30.337Z","k":"runner","note":"guard_trip","verdict":"no"}
`
	cuts, err := ReadCuts(strings.NewReader(log))
	if err != nil || len(cuts) != 1 {
		t.Fatalf("%v %+v", err, cuts)
	}
	if c := cuts[0]; c.Kind != "block-cycle" || c.Period != 30 || c.Chars != 35734 || c.Iteration != 16 || c.Channel != "reasoning" {
		t.Fatalf("%+v", c)
	}
}
