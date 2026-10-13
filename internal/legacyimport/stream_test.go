package legacyimport

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

func scanAll(t *testing.T, content string, chunk int) *StreamScanner {
	t.Helper()
	sc := NewStreamScanner()
	for i := 0; i < len(content); i += chunk {
		end := i + chunk
		if end > len(content) {
			end = len(content)
		}
		if n, err := sc.Write([]byte(content[i:end])); err != nil || n != end-i {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	sc.EndStream()
	return sc
}

func TestStreamScanner(t *testing.T) {
	content := stream("model-a:Q4", true)
	var first StreamStats
	// The result must not depend on where the writes are cut.
	for _, chunk := range []int{1, 7, 64, 4096, len(content)} {
		sc := scanAll(t, content, chunk)
		st := sc.Stats()
		if chunk == 1 {
			first = st
		} else if !reflect.DeepEqual(first, st) {
			t.Fatalf("chunk %d: stats differ:\n%+v\n%+v", chunk, first, st)
		}
		calls := sc.ToolCalls()
		want := []ToolCall{
			{Turn: 1, Seq: 0, Tool: "run_commands", OK: true, ParsePath: Unknown, DurationMS: 12},
			{Turn: 2, Seq: 1, Tool: "editor", OK: false, ErrorClass: "missing_argument", ParsePath: Unknown, DurationMS: 3},
			{Turn: 2, Seq: 2, Tool: "editor", OK: false, ErrorClass: "tool_reported_error", ParsePath: Unknown, DurationMS: 4},
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("chunk %d: tool calls %+v", chunk, calls)
		}
	}
	st := first
	if st.Streams != 1 || st.Bytes != int64(len(content)) || st.BadLines != 0 || st.OversizeLines != 0 {
		t.Fatalf("%+v", st)
	}
	if st.Lines != int64(strings.Count(content, "\n")) {
		t.Fatalf("lines %d", st.Lines)
	}
	wantTop := map[string]int64{"agent_event": 21, "hook_event": 4, "run_result": 1}
	if !reflect.DeepEqual(st.TopLevel, wantTop) {
		t.Fatalf("top level %v", st.TopLevel)
	}
	wantInner := map[string]int64{
		"iteration_start": 2, "iteration_end": 2, "content_start": 6, "content_update": 1,
		"content_end": 5, "usage": 2, "notice": 1, "error": 1, "done": 1,
	}
	if !reflect.DeepEqual(st.Inner, wantInner) {
		t.Fatalf("inner %v", st.Inner)
	}
	if st.Hooks["tool_call"] != 1 || st.Hooks["agent_start"] != 1 || st.Hooks["agent_end"] != 1 {
		t.Fatalf("hooks %v", st.Hooks)
	}
	if st.ReasoningDeltas != 2 || st.TextDeltas != 1 || st.AccumulatedLines != 0 || st.ErrorEvents != 1 || st.UsageEvents != 2 {
		t.Fatalf("%+v", st)
	}
	if st.FinishReason != "completed" || st.DoneReason != "completed" || st.Provider != "ollama" || st.Model != "model-a:Q4" ||
		st.ContextWindow != 131072 || st.Iterations != 2 || st.InputTokens != 300 || st.OutputTokens != 30 || !st.HasRunResult {
		t.Fatalf("%+v", st)
	}
	if st.FirstTS.Format("15:04:05") != "05:42:01" || !st.LastTS.After(st.FirstTS) {
		t.Fatalf("timestamps %v %v", st.FirstTS, st.LastTS)
	}
}

func TestStreamScannerCutStream(t *testing.T) {
	// No run_result, a tool call with no result, a broken line, a last line
	// without a newline, and a line that still carries the accumulated text.
	content := stream("m", false) +
		`{"ts":"2026-08-17T06:00:00.000Z","type":"agent_event","event":{"type":"content_start","contentType":"text","text":"a","accumulated":"Donea"}}` + "\n" +
		`{"ts":"2026-08-17T06:00:01.000Z","type":"agent_event","event":{"type":"content_start","contentType":"tool","toolName":"read_files","toolCallId":"call_9","input":{}}}` + "\n" +
		`{"ts":"2026-08-17T06:00:02.000Z","type":"agent_event","event":{"type":"content_sta` + "\n" +
		`{"ts":"2026-08-17T06:00:03.000Z","type":"run_aborted","reason":"timeout","message":"the clock"}`
	sc := scanAll(t, content, 13)
	st := sc.Stats()
	if st.BadLines != 1 || st.AccumulatedLines != 1 || st.TextDeltas != 2 {
		t.Fatalf("%+v", st)
	}
	if st.HasRunResult || st.FinishReason != "" || st.AbortReason != "timeout" || st.AbortMessage != "the clock" {
		t.Fatalf("%+v", st)
	}
	// Totals fall back to what the usage events and iterations showed.
	if st.Iterations != 2 || st.InputTokens != 300 || st.OutputTokens != 30 {
		t.Fatalf("fallback totals: %+v", st)
	}
	if st.LastTS.Format("15:04:05") != "06:00:03" {
		t.Fatalf("last ts %v", st.LastTS)
	}
	calls := sc.ToolCalls()
	if len(calls) != 4 {
		t.Fatalf("calls %+v", calls)
	}
	if last := calls[3]; last.Tool != "read_files" || last.OK || last.ErrorClass != "no_result" || last.Seq != 3 {
		t.Fatalf("open call %+v", last)
	}
}

func TestStreamScannerSeveralStreams(t *testing.T) {
	sc := NewStreamScanner()
	for i := 0; i < 2; i++ {
		if _, err := io.Copy(sc, strings.NewReader(stream("m", i == 1))); err != nil {
			t.Fatal(err)
		}
		sc.EndStream()
	}
	st := sc.Stats()
	if st.Streams != 2 || st.Iterations != 4 || st.InputTokens != 600 {
		t.Fatalf("%+v", st)
	}
	var turns, seqs []int
	for _, c := range sc.ToolCalls() {
		turns = append(turns, c.Turn)
		seqs = append(seqs, c.Seq)
	}
	if !reflect.DeepEqual(turns, []int{1, 2, 2, 3, 4, 4}) || !reflect.DeepEqual(seqs, []int{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("turns %v seqs %v", turns, seqs)
	}
}

func TestStreamScannerOversizeLine(t *testing.T) {
	sc := NewStreamScanner()
	sc.MaxLine = 4096
	big := `{"ts":"2026-08-17T06:00:00.000Z","type":"agent_event","event":{"type":"content_end","contentType":"text","text":"` + strings.Repeat("x", 100000) + `"}}` + "\n"
	content := stream("m", true) + big + `{"ts":"2026-08-17T07:00:00.000Z","type":"hook_event","hookEventName":"late"}` + "\n"
	for i := 0; i < len(content); i += 100 {
		end := i + 100
		if end > len(content) {
			end = len(content)
		}
		sc.Write([]byte(content[i:end]))
	}
	sc.EndStream()
	st := sc.Stats()
	if st.OversizeLines != 1 || st.BadLines != 0 {
		t.Fatalf("%+v", st)
	}
	if st.Hooks["late"] != 1 {
		t.Fatal("the line after the oversize one was lost")
	}
	if cap(sc.buf) > 1<<20 {
		t.Fatalf("buffer kept %d bytes", cap(sc.buf))
	}
	if st.Lines != int64(strings.Count(content, "\n")) {
		t.Fatalf("lines %d", st.Lines)
	}
}

func TestClassifyToolResult(t *testing.T) {
	cases := []struct {
		err, out string
		ok       bool
		class    string
	}{
		{``, `[{"query":"ls","result":"a","success":true}]`, true, ""},
		{`null`, `"plain text result"`, true, ""},
		{``, ``, true, ""},
		{``, `{"query":"edit","result":"done"}`, true, ""},
		{``, `{"query":"edit","result":"","error":"Editor operation failed"}`, false, "tool_reported_error"},
		{``, `[{"query":"a","success":false},{"query":"b","success":false}]`, false, "command_failed"},
		{``, `[{"query":"a","success":true},{"query":"b","success":false}]`, false, "partial_failure"},
		{``, `[{"query":"a","success":true,"error":"stderr text"}]`, true, ""},
		{`"{\"error\":\"Missing required argument path.\"}"`, `{}`, false, "missing_argument"},
		{`"{\"error\":\"✖ Invalid input: expected string, received undefined\"}"`, `{}`, false, "invalid_input"},
		{`"{\"error\":\"This looks like a JSON array that was sent as a string and did not survive\"}"`, `{}`, false, "malformed_arguments"},
		{`"Tool call write_file was rejected before execution: AI_NoSuchToolError"`, ``, false, "unknown_tool"},
		{`"command timed out after 120s"`, ``, false, "timeout"},
		{`"something else broke"`, ``, false, "tool_error"},
	}
	for _, c := range cases {
		ok, class := ClassifyToolResult(json.RawMessage(c.err), json.RawMessage(c.out))
		if ok != c.ok || class != c.class {
			t.Errorf("err=%s out=%s: got %v %q, want %v %q", c.err, c.out, ok, class, c.ok, c.class)
		}
	}
}

func TestConvertLine(t *testing.T) {
	out, sdk, err := ConvertLine([]byte(`{"ts":"2026-08-17T05:42:01.000Z","type":"agent_event","event":{"type":"iteration_start","iteration":3}}`))
	if err != nil || !sdk {
		t.Fatal(err, sdk)
	}
	var ev map[string]any
	if err := json.Unmarshal(out, &ev); err != nil {
		t.Fatal(err)
	}
	if ev["type"] != "iteration_start" || ev["iteration"] != float64(3) || ev["ts"] != "2026-08-17T05:42:01.000Z" || len(ev) != 3 {
		t.Fatalf("%v", ev)
	}
	in := `{"ts":"x","type":"run_result","finishReason":"completed"}`
	out, sdk, err = ConvertLine([]byte(in + "\n"))
	if err != nil || sdk || string(out) != in {
		t.Fatalf("CLI line: %q %v %v", out, sdk, err)
	}
	if _, _, err := ConvertLine([]byte(`{"ts":`)); err == nil {
		t.Fatal("a broken line converted")
	}
}
