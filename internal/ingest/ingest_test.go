package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// agentLine is one line of the runner's stream holding an agent event.
func agentLine(t string, event string) string {
	return fmt.Sprintf(`{"t":"2026-01-02T03:04:%sZ","k":"event","event":{"type":"agent_event","payload":{"event":%s}}}`, t, event)
}

const stream = `{"t":"2026-01-02T03:04:00Z","k":"runner","note":"prepared"}
{"t":"2026-01-02T03:04:00Z","k":"runner","note":"config_resolved","derived":{"thinking_budget_tokens":48000}}
not json at all
{"t":"2026-01-02T03:04:01Z","k":"event","event":{"type":"chunk","payload":{"chunk":"{\"type\":\"content_end\",\"contentType\":\"tool\"}"}}}
`

func events(t *testing.T) *Events {
	t.Helper()
	lines := []string{
		agentLine("01", `{"type":"iteration_start","iteration":1}`),
		agentLine("02", `{"type":"content_end","contentType":"reasoning","reasoning":"think think"}`),
		agentLine("03", `{"type":"content_start","contentType":"tool","toolName":"read_files","toolCallId":"a"}`),
		agentLine("05", `{"type":"content_end","contentType":"tool","toolName":"read_files","toolCallId":"a","output":[{"success":true}]}`),
		agentLine("06", `{"type":"usage","timings":{"firstTokenMs":900},"outputSplit":{"reasoningTokens":10,"textTokens":2,"toolInputTokens":5,"method":"stream-chunks"}}`),
		agentLine("07", `{"type":"iteration_start","iteration":2}`),
		agentLine("08", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"b","output":[{"success":false,"error":"not read"}],"durationMs":12}`),
		agentLine("09", `{"type":"content_end","contentType":"tool","toolName":"run_commands","toolCallId":"c","error":"spawn failed"}`),
		agentLine("10", `{"type":"content_end","contentType":"tool","toolName":"check_file","toolCallId":"d","output":"{\"success\":false}"}`),
		agentLine("11", `{"type":"content_end","contentType":"text","text":"done"}`),
		agentLine("12", `{"type":"notice","noticeType":"status","message":"context-breakdown"}`),
		agentLine("13", `{"type":"notice","noticeType":"recovery","reason":"auto_compaction"}`),
		agentLine("14", `{"type":"usage","timings":{"firstTokenMs":300},"outputSplit":{"reasoningTokens":1,"textTokens":1,"toolInputTokens":1,"method":"stream-chunks"}}`),
		agentLine("15", `{"type":"done","reason":"completed"}`),
	}
	ev, err := ReadEvents(strings.NewReader(stream + strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestReadEvents(t *testing.T) {
	ev := events(t)
	if ev.Lines != 18 || ev.BadLines != 1 {
		t.Errorf("lines %d, unreadable %d; want 18 and 1", ev.Lines, ev.BadLines)
	}
	if ev.Iterations != 2 || ev.AgentEnd != "completed" {
		t.Errorf("iterations %d, end %q", ev.Iterations, ev.AgentEnd)
	}
	// The chunk line repeats a tool call as text and must not count.
	want := []store.ToolCall{
		{Turn: 1, Seq: 0, Tool: "read_files", OK: true, DurationMS: 2000},
		{Turn: 2, Seq: 1, Tool: "editor", ErrorClass: "tool_error", DurationMS: 12},
		{Turn: 2, Seq: 2, Tool: "run_commands", ErrorClass: "tool_error"},
		{Turn: 2, Seq: 3, Tool: "check_file", ErrorClass: "tool_error"},
	}
	if fmt.Sprint(ev.ToolCalls) != fmt.Sprint(want) {
		t.Errorf("tool calls\n got %v\nwant %v", ev.ToolCalls, want)
	}
	if len(ev.Blocks) != 2 || ev.Blocks[0] != (Block{1, "reasoning", "think think"}) || ev.Blocks[1] != (Block{2, "text", "done"}) {
		t.Errorf("blocks %v", ev.Blocks)
	}
	if ev.FirstTokenMS != 900 || ev.ReasoningTokens != 11 || ev.TextTokens != 3 || ev.ToolInputTokens != 6 {
		t.Errorf("first token %v, split %d %d %d", ev.FirstTokenMS, ev.ReasoningTokens, ev.TextTokens, ev.ToolInputTokens)
	}
	if ev.Notices["status:context-breakdown"] != 1 || ev.Notices["auto_compaction"] != 1 {
		t.Errorf("notices %v", ev.Notices)
	}
	if ev.Derived["thinking_budget_tokens"] != 48000 {
		t.Errorf("derived %v", ev.Derived)
	}
	if got := ev.Last.Sub(ev.First).Seconds(); got != 15 {
		t.Errorf("span %v s, want 15", got)
	}
}

// chat is one line of a wire capture: an agent request and its answer.
func chat(seq int, body string, answer string) string {
	l := map[string]any{"seq": seq, "method": "POST", "url": "http://endpoint:11434/api/chat", "status": 200, "ms_total": 1000.0,
		"request_body": body, "response_body": answer}
	b, _ := json.Marshal(l)
	return string(b)
}

const (
	sent   = `{"model":"m:1","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function"}],"think":true,"options":{"num_ctx":8192,"temperature":1,"top_k":20,"num_predict":4096}}`
	answer = `{"message":{"thinking":"hm"}}` + "\n" + `{"message":{"content":"ok"}}` + "\n" +
		`{"done":true,"done_reason":"stop","think_budget":"medium","think_budget_tokens":24000,"prompt_eval_count":100,"prompt_eval_cached_count":60,"prompt_eval_duration":2000000000,"eval_count":50,"eval_duration":1000000000}`
)

func wire(t *testing.T, lines ...string) []Request {
	t.Helper()
	reqs, err := ReadWire(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return reqs
}

func TestReadWire(t *testing.T) {
	reqs := wire(t,
		chat(3, sent, `{"done":true,"done_reason":"length","eval_count":7}`),
		`{"seq":1,"url":"http://endpoint:11434/api/show","status":200,"request_body":"{\"model\":\"m:1\"}"}`,
		chat(2, sent, answer),
		`{"seq":4,"url":"http://endpoint:11434/api/chat","status":200,"request_body":"{\"model\":\"m:1\",\"messages\":[]}"}`,
		`{"seq":5,"url":"http://endpoint:11434/api/me?x=1","status":404}`,
		`{"seq":6,"url":"http://endpoint:11434/api/chat","status":0,"error":"reset","request_body":"{\"tools\":[{}]}"}`,
	)
	if len(reqs) != 6 {
		t.Fatalf("%d requests", len(reqs))
	}
	for i, q := range reqs {
		if q.Seq != i+1 {
			t.Fatalf("not in order: %d at %d", q.Seq, i)
		}
	}
	if reqs[0].Agent || reqs[0].Path != "/api/show" || reqs[3].Agent {
		t.Error("a request without tools is not the agent's")
	}
	q := reqs[1]
	if !q.Agent || q.PromptTokens != 100 || q.CachedTokens != 60 || q.EvalTokens != 50 || q.DoneReason != "stop" ||
		q.ThinkBudget != "medium" || q.ThinkBudgetTokens != 24000 || q.Empty || q.Failed {
		t.Errorf("agent request %+v", q)
	}
	if q.Options["num_ctx"] != 8192.0 || string(q.Body["think"]) != "true" || q.Body["messages"] != nil || q.Body["tools"] != nil {
		t.Errorf("what was sent: options %v body %v", q.Options, q.Body)
	}
	if !reqs[2].Empty || reqs[2].DoneReason != "length" {
		t.Errorf("an answer with nothing in it: %+v", reqs[2])
	}
	if !reqs[4].Failed || reqs[4].Path != "/api/me" || !reqs[5].Failed || reqs[5].Empty {
		t.Errorf("failures: %+v %+v", reqs[4], reqs[5])
	}
}

func arm(t *testing.T, doc string) *config.Arm {
	t.Helper()
	var a config.Arm
	if err := json.Unmarshal([]byte(doc), &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

// deviations indexes what CompareWire found by kind and field.
func deviations(devs []Deviation) map[string]Deviation {
	out := map[string]Deviation{}
	for _, d := range devs {
		out[d.Kind+" "+d.Field] = d
	}
	return out
}

const tag = "think_budget                   \"medium\"\ntop_k                          20\npresence_penalty               0.5\nstop                           \"a\"\nstop                           \"b\"\n"

func TestCompareWireClean(t *testing.T) {
	a := arm(t, `{"model":{"model":"m:1","context_window":8192,"output_tokens":4096},
		"thinking":{"owner":"tag"},"sampler":{"owner":"arm","temperature":1.0,"top_k":20}}`)
	if devs := CompareWire(a, tag, wire(t, chat(1, sent, answer), chat(2, sent, answer))); len(devs) != 0 {
		t.Errorf("a run that sent what the arm states: %+v", devs)
	}
	if devs := CompareWire(a, tag, wire(t, `{"seq":1,"url":"http://e/api/show","status":200}`)); devs != nil {
		t.Errorf("no agent request, nothing to compare: %+v", devs)
	}
}

func TestCompareWire(t *testing.T) {
	a := arm(t, `{"model":{"model":"m:2","context_window":4096,"output_tokens":0},
		"thinking":{"owner":"arm","enabled":true,"effort":"high","budget_tokens":1000},
		"sampler":{"owner":"arm","temperature":1.0,"presence_penalty":0.0,"repeat_last_n":0}}`)
	later := strings.Replace(strings.Replace(sent, `"temperature":1`, `"temperature":0.7`, 1), `"num_predict":4096`, `"num_predict":2048,"mirostat":2`, 1)
	later = strings.Replace(later, `"think":true`, `"think":true,"placement":{"num_ctx":1}`, 1)
	devs := CompareWire(a, tag, wire(t, chat(1, sent, answer), chat(2, sent, answer), chat(3, later, answer)))
	got := deviations(devs)
	check := func(key string, want Deviation) {
		t.Helper()
		d, ok := got[key]
		if !ok {
			t.Errorf("missing %q in %v", key, devs)
			return
		}
		delete(got, key)
		if d.Requests != want.Requests || d.Of != 3 || d.FirstSeq != want.FirstSeq || d.Effective != want.Effective || d.Source != want.Source ||
			string(d.Stated) != string(want.Stated) || string(d.Sent) != string(want.Sent) || fmt.Sprint(d.Values) != fmt.Sprint(want.Values) {
			t.Errorf("%s\n got %+v\nwant %+v", key, d, want)
		}
	}
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	check("differs model", Deviation{Requests: 3, FirstSeq: 1, Stated: raw(`"m:2"`), Sent: raw(`"m:1"`)})
	check("differs options.num_ctx", Deviation{Requests: 3, FirstSeq: 1, Stated: raw(`4096`), Sent: raw(`8192`)})
	check("differs options.temperature", Deviation{Requests: 1, FirstSeq: 3, Stated: raw(`1`), Sent: raw(`0.7`)})
	// A zero the arm states and nobody sends: the tag's own value applied.
	check("stated_not_sent options.presence_penalty", Deviation{Requests: 3, FirstSeq: 1, Stated: raw(`0`), Effective: "0.5", Source: "model tag"})
	check("stated_not_sent options.repeat_last_n", Deviation{Requests: 3, FirstSeq: 1, Stated: raw(`0`)})
	check("stated_not_sent think", Deviation{Requests: 3, FirstSeq: 1, Stated: raw(`"high"`), Sent: raw(`true`), Effective: "medium (24000 tokens)", Source: "server answer"})
	check("stated_not_sent options.think_budget", Deviation{Requests: 3, FirstSeq: 1, Stated: raw(`1000`), Effective: "medium (24000 tokens)", Source: "server answer"})
	check("sent_not_stated options.top_k", Deviation{Requests: 3, FirstSeq: 1, Sent: raw(`20`)})
	check("sent_not_stated options.mirostat", Deviation{Requests: 1, FirstSeq: 3, Sent: raw(`2`)})
	check("drift options.temperature", Deviation{Requests: 1, FirstSeq: 3, Values: []string{"1", "0.7"}})
	check("drift options.num_predict", Deviation{Requests: 1, FirstSeq: 3, Values: []string{"4096", "2048"}})
	check("drift options.mirostat", Deviation{Requests: 1, FirstSeq: 3, Values: []string{"2", "(absent)"}})
	check("drift placement", Deviation{Requests: 1, FirstSeq: 3, Values: []string{`{"num_ctx":1}`, "(absent)"}})
	for key := range got {
		t.Errorf("not expected: %s %+v", key, got[key])
	}

	// Severity: what an arm governs is a warning, the rest is a note.
	sev := map[string]string{}
	for _, f := range wireFindings("r", devs) {
		var d Deviation
		json.Unmarshal(f.Evidence, &d)
		sev[d.Kind+" "+d.Field] = f.Severity
		if f.Detector != WireArm || f.Count != d.Requests || f.Turn != d.FirstSeq {
			t.Errorf("finding %+v", f)
		}
	}
	for key, want := range map[string]string{"drift placement": "info", "drift options.num_predict": "info",
		"drift options.temperature": "warning", "stated_not_sent think": "warning"} {
		if sev[key] != want {
			t.Errorf("severity of %q is %q, want %q", key, sev[key], want)
		}
	}
}

func TestCompareWireTagOwns(t *testing.T) {
	a := arm(t, `{"model":{"model":"m:1","context_window":8192,"output_tokens":4096},
		"thinking":{"owner":"tag"},"sampler":{"owner":"tag","temperature":1.0}}`)
	level := strings.Replace(sent, `"think":true`, `"think":"high"`, 1)
	got := deviations(CompareWire(a, tag, wire(t, chat(1, sent, answer), chat(2, level, answer))))
	for _, key := range []string{"sent_not_stated options.temperature", "sent_not_stated options.top_k", "sent_not_stated think", "drift think"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing %q", key)
		}
		delete(got, key)
	}
	if len(got) != 0 {
		t.Errorf("not expected: %v", got)
	}
}

func TestTagParameters(t *testing.T) {
	p := tagParameters(tag)
	if p["think_budget"] != "medium" || p["top_k"] != "20" || p["stop"] != "a, b" {
		t.Errorf("%v", p)
	}
}

func TestDistribution(t *testing.T) {
	got := map[string]float64{}
	distribution(func(n string, v float64) { got[n] = v }, "x", []float64{5, 1, 4, 2, 3, 10, 9, 8, 7, 6})
	want := map[string]float64{"x.n": 10, "x.sum": 55, "x.p10": 1, "x.p50": 5, "x.p90": 9, "x.max": 10}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v want %v", got, want)
	}
	distribution(func(string, float64) { t.Error("an empty list has no distribution") }, "y", nil)
}

// memory is a store that holds one run.
type memory struct {
	store.State
	run      store.Run
	refs     []store.BlobRef
	blobs    map[blob.Hash]string
	metrics  []store.Metric
	calls    []store.ToolCall
	findings []store.Finding
}

func (m *memory) GetRun(context.Context, string) (*store.Run, error)        { r := m.run; return &r, nil }
func (m *memory) BlobRefs(context.Context, string) ([]store.BlobRef, error) { return m.refs, nil }
func (m *memory) Findings(context.Context, string) ([]store.Finding, error) { return m.findings, nil }
func (m *memory) ReplaceMetrics(_ context.Context, _ string, v []store.Metric) error {
	m.metrics = v
	return nil
}
func (m *memory) ReplaceToolCalls(_ context.Context, _ string, v []store.ToolCall) error {
	m.calls = v
	return nil
}
func (m *memory) ReplaceFindings(_ context.Context, _ string, v []store.Finding) error {
	m.findings = v
	return nil
}
func (m *memory) Reader(_ context.Context, h blob.Hash) (io.ReadCloser, blob.Info, error) {
	return io.NopCloser(strings.NewReader(m.blobs[h])), blob.Info{}, nil
}

func TestRun(t *testing.T) {
	he, hw := blob.Hash("e"), blob.Hash("w")
	lines := []string{
		agentLine("01", `{"type":"iteration_start","iteration":1}`),
		agentLine("02", `{"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"b","output":[{"success":false}]}`),
		agentLine("03", `{"type":"content_end","contentType":"reasoning","reasoning":"abcd"}`),
	}
	m := &memory{
		run: store.Run{ID: "r1", WallMS: 5000,
			Arm:        json.RawMessage(`{"model":{"model":"m:1","context_window":8192,"output_tokens":4096},"thinking":{"owner":"tag"},"sampler":{"owner":"arm","temperature":1.0,"top_k":20,"presence_penalty":0.0}}`),
			Provenance: json.RawMessage(`{"endpoint_before":{"parameters":"presence_penalty 1.5"}}`)},
		refs: []store.BlobRef{{RunID: "r1", Kind: "events", Hash: he}, {RunID: "r1", Kind: "wire", Name: "wire.jsonl", Hash: hw},
			{RunID: "r1", Kind: "oracle", Name: "game"}},
		blobs: map[blob.Hash]string{he: strings.Join(lines, "\n"), hw: chat(1, sent, answer) + "\n" + chat(2, sent, answer)},
		findings: []store.Finding{{RunID: "r1", Detector: "someone_else", Count: 1},
			{RunID: "r1", Detector: WireArm, Count: 99}},
	}
	for round := 0; round < 2; round++ { // a second ingest changes nothing
		if _, err := Run(context.Background(), m, m, "r1"); err != nil {
			t.Fatal(err)
		}
		got := map[string]float64{}
		for _, x := range m.metrics {
			if x.RunID != "r1" {
				t.Fatalf("metric without its run: %+v", x)
			}
			got[x.Name] = x.Value
		}
		for name, want := range map[string]float64{
			"time.wall_ms": 5000, "turns": 1, "tool_calls": 1, "tool_calls.failed": 1, "tool_calls.by_tool.editor": 1,
			"tool_calls.failed.by_tool.editor": 1, "requests": 2, "requests.other": 0, "tokens.prompt": 200,
			"tokens.prompt.cached": 120, "tokens.prompt.fresh": 80, "tokens.completion": 100, "rate.generation_tok_s": 50,
			"rate.prompt_tok_s": 20, "finish.stop": 2, "completions.empty": 0, "context.window": 8192, "context.peak_tokens": 150,
			"context.cached_share": 0.6, "thinking.blocks": 1, "thinking.chars.max": 4, "thinking.budget_tokens.served": 24000,
			"context.compactions": 0,
		} {
			if v, ok := got[name]; !ok || v != want {
				t.Errorf("round %d: %s = %v (present %v), want %v", round, name, v, ok, want)
			}
		}
		if len(m.calls) != 1 || m.calls[0].RunID != "r1" || m.calls[0].OK {
			t.Errorf("tool calls %+v", m.calls)
		}
		if len(m.findings) != 2 {
			t.Fatalf("round %d: findings %+v", round, m.findings)
		}
		var d Deviation
		json.Unmarshal(m.findings[0].Evidence, &d)
		if m.findings[0].Detector != WireArm || d.Kind != StatedNotSent || d.Field != "options.presence_penalty" || d.Effective != "1.5" {
			t.Errorf("wire finding %+v %+v", m.findings[0], d)
		}
		if m.findings[1].Detector != "someone_else" {
			t.Errorf("another detector's finding was not kept: %+v", m.findings[1])
		}
	}

}

// A model loaded again in the middle of a run is a finding; the load before
// the first request is not.
func TestModelReloaded(t *testing.T) {
	done := func(loadNS int64) string {
		return fmt.Sprintf(`{"message":{"content":"ok"}}`+"\n"+`{"done":true,"done_reason":"stop","load_duration":%d,"eval_count":1}`, loadNS)
	}
	probe := `{"model":"m:1","messages":[]}` // the SDK's own chat request, without tools
	r := &store.Run{ID: "r"}
	find := func(reqs []Request) (*store.Finding, map[string]float64) {
		res := Analyse(r, &Events{Notices: map[string]int{}}, reqs)
		got := map[string]float64{}
		for _, m := range res.Metrics {
			got[m.Name] = m.Value
		}
		for _, f := range res.Findings {
			if f.Detector == ModelReloaded {
				return &f, got
			}
		}
		return nil, got
	}
	f, m := find(wire(t, chat(1, probe, done(8_000_000_000)), chat(2, sent, done(15_000_000)), chat(3, sent, done(14_000_000))))
	if f != nil || m["endpoint.first_load_s"] != 8 || m["endpoint.reloads"] != 0 {
		t.Errorf("a run whose model stayed in memory: %+v %v", f, m)
	}
	f, m = find(wire(t, chat(1, probe, done(8_000_000_000)), chat(2, sent, done(15_000_000)), chat(3, sent, done(6_500_000_000)), chat(4, sent, done(1_000_000))))
	if f == nil || f.Count != 1 || f.Turn != 3 || !strings.Contains(string(f.Evidence), `"seconds":6.5`) || m["endpoint.reloads"] != 1 {
		t.Errorf("a run whose model was loaded again: %+v %v", f, m)
	}
}

// A run whose model has no stated identity carries a finding that names
// what is missing; one with a whole identity carries none.
func TestModelIdentity(t *testing.T) {
	find := func(prov string) *store.Finding {
		r := &store.Run{ID: "r", Provenance: json.RawMessage(prov)}
		for _, f := range Analyse(r, &Events{Notices: map[string]int{}}, nil).Findings {
			if f.Detector == ModelIdentity {
				return &f
			}
		}
		return nil
	}
	f := find(`{"inference":{"model":{"tag":"m:1","stated":null,"not_stated":["family","size","variant"]}}}`)
	if f == nil || f.Count != 3 || !strings.Contains(string(f.Evidence), `"variant"`) || !strings.Contains(string(f.Evidence), `"m:1"`) {
		t.Errorf("no identity: %+v", f)
	}
	if f := find(`{"inference":{"model":{"tag":"m:1","stated":{"family":"F","size":"1B","variant":"base"}}}}`); f != nil {
		t.Errorf("a whole identity: %+v", f)
	}
}

// What was in effect, from the arm, the tag and the wire's findings.
func TestInEffect(t *testing.T) {
	a := arm(t, `{"model":{"model":"m:1","context_window":8192,"output_tokens":0},
		"thinking":{"owner":"arm","enabled":true,"effort":"high"},
		"sampler":{"owner":"arm","temperature":1.0,"top_k":20,"presence_penalty":0.0}}`)
	tagText := "top_p 0.95\npresence_penalty 0.5\ntop_k 40\nthink_budget \"medium\"\nnum_ctx 4096"
	by := func(list []Setting) map[string]Setting {
		out := map[string]Setting{}
		for _, s := range list {
			out[s.Name] = s
		}
		return out
	}
	// Everything sent as stated; the server served the high budget.
	got := by(InEffect(a, tagText, nil, 48000))
	for name, want := range map[string]Setting{
		"temperature":      {Stated: "1", Sent: "1", Effective: "1", Source: "sent"},
		"top_k":            {Stated: "20", Sent: "20", Tag: "40", Effective: "20", Source: "sent"},
		"presence_penalty": {Stated: "0", Sent: "0", Tag: "0.5", Effective: "0", Source: "sent"},
		"top_p":            {Tag: "0.95", Effective: "0.95", Source: "model tag"},
		"context_window":   {Stated: "8192", Sent: "8192", Tag: "4096", Effective: "8192", Source: "sent"},
		"thinking":         {Stated: "effort high", Sent: "high", Tag: "medium", Effective: "high (budget 48000 tokens)", Source: "server answer"},
	} {
		want.Name = name
		if got[name] != want {
			t.Errorf("%s\n got %+v\nwant %+v", name, got[name], want)
		}
	}
	if _, ok := got["min_p"]; ok {
		t.Error("a parameter nobody speaks of is listed")
	}
	// The release that dropped them: the tag's values applied.
	devs := []Deviation{
		{Kind: StatedNotSent, Field: "options.presence_penalty", Effective: "0.5", Source: "model tag"},
		{Kind: StatedNotSent, Field: "think", Sent: json.RawMessage(`true`), Effective: "medium (24000 tokens)", Source: "server answer"},
		{Kind: Drift, Field: "options.top_k", Values: []string{"20", "30"}},
	}
	got = by(InEffect(a, tagText, wireFindings("r", devs), 24000))
	if s := got["presence_penalty"]; s.Sent != "" || s.Effective != "0.5" || s.Source != "model tag" || s.Note != "stated, not sent" {
		t.Errorf("presence penalty: %+v", s)
	}
	if s := got["thinking"]; s.Sent != "true" || s.Effective != "medium (24000 tokens)" || s.Source != "server answer" || s.Note == "" {
		t.Errorf("thinking: %+v", s)
	}
	if s := got["top_k"]; !strings.Contains(s.Note, "changed during the run: 20, 30") {
		t.Errorf("top_k: %+v", s)
	}
}

// A model that ended the run partly off the GPU is a finding.
func TestResidencyAfter(t *testing.T) {
	r := &store.Run{ID: "r", Provenance: json.RawMessage(`{"endpoint_after":{"loaded":{"size":100,"size_vram":60}}}`)}
	res := Analyse(r, &Events{Notices: map[string]int{}}, nil)
	found := false
	for _, f := range res.Findings {
		found = found || (f.Detector == GPUResidency && strings.Contains(string(f.Evidence), `"share":0.6`))
	}
	share := -1.0
	for _, m := range res.Metrics {
		if m.Name == "endpoint.gpu_share" {
			share = m.Value
		}
	}
	if !found || share != 0.6 {
		t.Errorf("partly on the GPU: found %v, share %v, findings %+v", found, share, res.Findings)
	}
	r.Provenance = json.RawMessage(`{"endpoint_after":{"loaded":{"size":100,"size_vram":100}}}`)
	for _, f := range Analyse(r, &Events{Notices: map[string]int{}}, nil).Findings {
		if f.Detector == GPUResidency {
			t.Errorf("fully on the GPU, and yet: %+v", f)
		}
	}
}

// An imported run has the older harness's stream and no wire capture: it
// gets the detectors' findings and nothing else is touched (D65).
func TestRunImported(t *testing.T) {
	loop := strings.Repeat("I will fix the jump. ", 9)
	old := func(event string) string { return `{"ts":1791710224660,"type":"agent_event","event":` + event + `}` }
	lines := []string{
		old(`{"type":"iteration_start","iteration":1}`),
		old(`{"type":"content_start","contentType":"reasoning","reasoning":"` + loop[:40] + `"}`),
		old(`{"type":"content_start","contentType":"reasoning","reasoning":"` + loop[40:] + `"}`),
		old(`{"type":"content_end","contentType":"reasoning"}`), // the whole block is not repeated here
		old(`{"type":"content_end","contentType":"text","text":"fine"}`),
		`{"ts":1791710224661,"type":"hook_event","hookEventName":"tool_call"}`,
		old(`{"type":"iteration_start","iteration":2}`),
		old(`{"type":"content_start","contentType":"tool","toolName":"read_files","toolCallId":"a","input":{"path":"x","n":1}}`),
		old(`{"type":"content_end","contentType":"tool","toolName":"read_files","toolCallId":"a"}`),
		old(`{"type":"content_start","contentType":"tool","toolName":"read_files","toolCallId":"b","input":{"n":1,"path":"x"}}`),
		old(`{"type":"content_end","contentType":"tool","toolName":"read_files","toolCallId":"b"}`),
		old(`{"type":"content_end","contentType":"tool","toolName":"read_files","toolCallId":"c","input":{"path": "x", "n": 1}}`),
	}
	h1, h2 := blob.Hash("1"), blob.Hash("2")
	m := &memory{
		run:  store.Run{ID: "old", Imported: true},
		refs: []store.BlobRef{{RunID: "old", Kind: "events", Name: "run.jsonl", Hash: h1}, {RunID: "old", Kind: "events", Name: "run.2.jsonl", Hash: h2}},
		blobs: map[blob.Hash]string{h1: strings.Join(lines, "\n"),
			h2: old(`{"type":"content_end","contentType":"text","text":"x` + strings.Repeat("z", 50) + `"}`)},
		findings: []store.Finding{{RunID: "old", Detector: "legacy_oracle", Count: 1}, {RunID: "old", Detector: detect.CharRun, Count: 7}},
		metrics:  []store.Metric{{RunID: "old", Name: "kept", Value: 1}},
		calls:    []store.ToolCall{{RunID: "old", Tool: "from the importer"}},
	}
	res, err := Run(context.Background(), m, m, "old")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Blocks) != 3 || res.Blocks[0].Text != loop {
		t.Errorf("blocks, the first put together from its pieces: %+v", res.Blocks)
	}
	got := map[string]store.Finding{}
	for _, f := range m.findings {
		got[f.Detector] = f
	}
	if len(m.findings) != 4 || got["legacy_oracle"].Count != 1 {
		t.Fatalf("findings: %+v", m.findings)
	}
	var fire Firing
	json.Unmarshal(got[detect.ShortUnitRepeat].Evidence, &fire)
	if fire.Channel != "reasoning" || fire.Blocks != 1 || fire.Of != 1 || fire.First.Count != 9 || got[detect.ShortUnitRepeat].Turn != 1 {
		t.Errorf("short unit: %+v %+v", got[detect.ShortUnitRepeat], fire)
	}
	json.Unmarshal(got[detect.CharRun].Evidence, &fire)
	if fire.Channel != "text" || fire.First.Count != 50 || got[detect.CharRun].Count != 1 {
		t.Errorf("letter run, from the second stream: %+v %+v", got[detect.CharRun], fire)
	}
	// The same arguments, spelt three ways, are the same call.
	var seq detect.RepeatedCall
	json.Unmarshal(got[detect.RepeatedToolCall].Evidence, &seq)
	if seq.Tool != "read_files" || seq.Count != 3 || seq.Turn != 2 || seq.Input != `{"n":1,"path":"x"}` {
		t.Errorf("repeated call: %+v", seq)
	}
	if len(m.metrics) != 1 || len(m.calls) != 1 {
		t.Errorf("an imported run's metrics and tool calls were touched: %+v %+v", m.metrics, m.calls)
	}
}

func (m *memory) UpsertRun(_ context.Context, r *store.Run) error { m.run = *r; return nil }

// A run that ended partly off the GPU is INVALID and keeps the verdict it
// had; an arm that accepts the condition keeps its verdict, and a verdict
// this rule set is taken back once the cause is gone (D73).
func TestEndpointConductInvalidates(t *testing.T) {
	partly := json.RawMessage(`{"endpoint_after":{"loaded":{"size":100,"size_vram":60}}}`)
	m := &memory{run: store.Run{ID: "r", Verdict: store.Pass, Arm: json.RawMessage(`{"model":{"model":"m:1"}}`), Provenance: partly}}
	ctx := context.Background()
	if _, err := Run(ctx, m, m, "r"); err != nil {
		t.Fatal(err)
	}
	if m.run.Verdict != store.Invalid || m.run.PriorVerdict != store.Pass || !strings.Contains(m.run.VerdictReason, "partly on the GPU (60%)") {
		t.Errorf("partly on the GPU: %s (was %s), %q", m.run.Verdict, m.run.PriorVerdict, m.run.VerdictReason)
	}
	if len(m.findings) != 1 || m.findings[0].Severity != "error" {
		t.Errorf("findings: %+v", m.findings)
	}
	// Analysed again, nothing changes.
	Run(ctx, m, m, "r")
	if m.run.Verdict != store.Invalid || m.run.PriorVerdict != store.Pass {
		t.Errorf("a second analysis: %s (was %s)", m.run.Verdict, m.run.PriorVerdict)
	}
	// The cause gone, the verdict comes back.
	m.run.Provenance = json.RawMessage(`{"endpoint_after":{"loaded":{"size":100,"size_vram":100}}}`)
	Run(ctx, m, m, "r")
	if m.run.Verdict != store.Pass || m.run.PriorVerdict != "" || m.run.VerdictReason != "" {
		t.Errorf("cause gone: %s (was %s), %q", m.run.Verdict, m.run.PriorVerdict, m.run.VerdictReason)
	}
	// An arm that accepts it: the finding stays, as a note.
	m.run.Provenance, m.run.Arm = partly, json.RawMessage(`{"model":{"model":"m:1"},"accepts":{"partial_gpu":true}}`)
	Run(ctx, m, m, "r")
	if m.run.Verdict != store.Pass || len(m.findings) != 1 || m.findings[0].Severity != "note" {
		t.Errorf("accepted: %s, %+v", m.run.Verdict, m.findings)
	}
	// An operator's INVALID is not this rule's to take back.
	m.run.Verdict, m.run.PriorVerdict, m.run.VerdictReason = store.Invalid, store.Pass, "operator: wrong lane"
	Run(ctx, m, m, "r")
	if m.run.Verdict != store.Invalid {
		t.Errorf("an operator's verdict was changed to %s", m.run.Verdict)
	}
}

// A reload on the first request after a pause's release keeps the verdict
// (D85): the pause dropped the model on purpose. A reload anywhere else
// still makes the run INVALID.
func TestReloadAfterPause(t *testing.T) {
	line := func(seq int, start string, loadS float64) string {
		resp, _ := json.Marshal(map[string]any{"message": map[string]any{"content": "x"}, "done": true, "done_reason": "stop",
			"load_duration": int64(loadS * 1e9)})
		l, _ := json.Marshal(map[string]any{"seq": seq, "url": "http://h/api/chat", "status": 200, "t_start": start,
			"request_body": `{"tools":[{"type":"function"}],"messages":[]}`, "response_body": string(resp) + "\n"})
		return string(l)
	}
	hw := blob.Hash("w")
	wire := strings.Join([]string{
		line(1, "2026-10-14T12:00:00Z", 20),
		line(2, "2026-10-14T12:01:00Z", 0.01),
		line(3, "2026-10-14T12:30:00Z", 26.5), // the first after the release
		line(4, "2026-10-14T12:31:00Z", 0.01),
	}, "\n")
	held := `{"holds":[{"from":"2026-10-14T12:02:00Z","to":"2026-10-14T12:29:00Z"}],"held_ms":1620000}`
	m := &memory{run: store.Run{ID: "r", Verdict: store.Invalid, PriorVerdict: store.Pass,
		VerdictReason: "endpoint: the model was loaded again during the run (1 times)", Paused: true,
		Arm: json.RawMessage(`{"model":{"model":"m:1"}}`), Provenance: json.RawMessage(held)},
		refs:  []store.BlobRef{{RunID: "r", Kind: "wire", Name: "wire.jsonl", Hash: hw}},
		blobs: map[blob.Hash]string{hw: wire}}
	ctx := context.Background()
	if _, err := Run(ctx, m, m, "r"); err != nil {
		t.Fatal(err)
	}
	if m.run.Verdict != store.Pass || m.run.PriorVerdict != "" {
		t.Errorf("after a pause: %s (was %s), %q", m.run.Verdict, m.run.PriorVerdict, m.run.VerdictReason)
	}
	var f *store.Finding
	for i := range m.findings {
		if m.findings[i].Detector == ModelReloaded {
			f = &m.findings[i]
		}
	}
	if f == nil || f.Severity != "note" || f.Turn != 3 || !strings.Contains(string(f.Evidence), `"after_pause":[{"seq":3`) {
		t.Errorf("finding: %+v", f)
	}
	// The same reload one request later is not the pause's.
	m.blobs[hw] = strings.Join([]string{
		line(1, "2026-10-14T12:00:00Z", 20),
		line(2, "2026-10-14T12:01:00Z", 0.01),
		line(3, "2026-10-14T12:30:00Z", 0.01),
		line(4, "2026-10-14T12:31:00Z", 26.5),
	}, "\n")
	Run(ctx, m, m, "r")
	if m.run.Verdict != store.Invalid || !strings.Contains(m.run.VerdictReason, "loaded again") {
		t.Errorf("a reload not after the release: %s, %q", m.run.Verdict, m.run.VerdictReason)
	}
}

// The scripted user's lines (040 §6.2): its questions are counted with the
// rest, an action is a line of a rule (a resume is its cancel's second
// half), a denial is counted whoever made it, and an action the SDK refused
// is counted apart.
func TestScriptedUserLines(t *testing.T) {
	ev, err := ReadEvents(strings.NewReader(strings.Join([]string{
		`{"t":"2026-10-14T10:00:00Z","k":"user","on":"approval","rule":0,"do":"approve","tool":"read_files","approved":true}`,
		`{"t":"2026-10-14T10:00:01Z","k":"user","on":"approval","rule":1,"do":"deny","tool":"check_file","approved":false}`,
		`{"t":"2026-10-14T10:00:02Z","k":"user","on":"approval","rule":0,"do":"deny","tool":"grep","approved":false}`,
		`{"t":"2026-10-14T10:00:03Z","k":"user","on":"question","rule":0,"do":"answer","answer":"x"}`,
		`{"t":"2026-10-14T10:00:04Z","k":"user","on":"turn","rule":2,"do":"cancel","text":"again"}`,
		`{"t":"2026-10-14T10:00:05Z","k":"user","on":"resume","rule":2,"do":"send","text":"again"}`,
		`{"t":"2026-10-14T10:00:06Z","k":"user","on":"turn","rule":3,"do":"restore"}`,
		`{"t":"2026-10-14T10:00:07Z","k":"runner","note":"user_action_failed","rule":3,"do":"restore"}`,
		`{"t":"2026-10-14T10:00:08Z","k":"runner","note":"mode_switched","session":"s","from":"plan","to":"act","messages":4}`,
		`{"t":"2026-10-14T10:00:08Z","k":"runner","note":"mode_switched","session":"s","from":"plan","to":"act","source":"agent","messages":6}`,
		`{"t":"2026-10-14T10:00:09Z","k":"runner","note":"fork","from":"s","to":"s-fork1","checkpoint":1,"workspace":true,"messages":1,"restarted":["iterations","loop guards"]}`,
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Questions != 1 || ev.UserActions != 3 || ev.UserDenials != 2 || ev.UserFailed != 1 {
		t.Errorf("questions %d, actions %d, denials %d, failed %d", ev.Questions, ev.UserActions, ev.UserDenials, ev.UserFailed)
	}
	// A switch is counted; a fork (D86) is kept with its time and what
	// restarted.
	if ev.ModeSwitches != 1 || ev.AgentModeSwitches != 1 || len(ev.Forks) != 1 {
		t.Fatalf("switches %d by the user, %d by the agent, forks %+v", ev.ModeSwitches, ev.AgentModeSwitches, ev.Forks)
	}
	if f := ev.Forks[0]; f.To != "s-fork1" || f.Checkpoint != 1 || !f.Workspace || len(f.Restarted) != 2 || f.T.Second() != 9 {
		t.Errorf("fork: %+v", f)
	}
}

// The guards' trips the runner noted (protocol 4) are counted by guard and
// phase, and the trip that stopped the run is kept (D99).
func TestGuardTrips(t *testing.T) {
	ev, err := ReadEvents(strings.NewReader(strings.Join([]string{
		`{"t":"2026-10-23T10:00:00Z","k":"runner","note":"guard_trip","guard":"reasoning_loop","phase":"cut","iteration":4,"message":"m"}`,
		`{"t":"2026-10-23T10:00:01Z","k":"runner","note":"guard_trip","guard":"reasoning_loop","phase":"cut","iteration":5,"message":"m"}`,
		`{"t":"2026-10-23T10:00:02Z","k":"runner","note":"guard_trip","guard":"loop_detection","phase":"","iteration":null,"message":""}`,
		`{"t":"2026-10-23T10:00:02Z","k":"runner","note":"guard_stop","guard":"reasoning_loop","phase":"cut","iteration":4,"message":"m"}`,
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"reasoning_loop.cut": 2, "loop_detection": 1}
	if len(ev.GuardTrips) != len(want) || ev.GuardTrips["reasoning_loop.cut"] != 2 || ev.GuardTrips["loop_detection"] != 1 {
		t.Errorf("trips %v, want %v", ev.GuardTrips, want)
	}
	if ev.GuardStop != "reasoning_loop.cut" {
		t.Errorf("stop %q", ev.GuardStop)
	}
}
