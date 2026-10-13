package legacyimport

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseExit(t *testing.T) {
	e, err := ParseExit("verdict=FIXED kept=1 discarded=0 wall=848s exit=0\n")
	if err != nil {
		t.Fatal(err)
	}
	if e.Verdict != "FIXED" || !e.HasWall || e.WallSeconds != 848 || !e.HasExitCode || e.ExitCode != 0 {
		t.Fatalf("oldest form: %+v", e)
	}
	// The front of the line is positional.
	want := []string{"verdict", "kept", "discarded", "wall", "exit"}
	for i, k := range want {
		if e.Fields[i].Key != k {
			t.Fatalf("field %d is %q, want %q", i, e.Fields[i].Key, k)
		}
	}

	e, err = ParseExit(`verdict=TIMEOUT kept=0 discarded=0 empty=0 wall=7203s exit=0 arm=manual run_checks=0 reason="clock: 7203s vs 7200s cap, exit=0" oracle=v3 loads=yes logic=13/23 failed=landing,run`)
	if err != nil {
		t.Fatal(err)
	}
	if e.Reason != "clock: 7203s vs 7200s cap, exit=0" {
		t.Fatalf("reason %q", e.Reason)
	}
	if v, _ := e.Get("oracle"); v != "v3" {
		t.Fatalf("field after the quoted reason: %q", v)
	}
	if v, _ := e.Get("failed"); v != "landing,run" {
		t.Fatalf("failed=%q", v)
	}
	if v, _ := e.Get("exit"); v != "0" || e.ExitCode != 0 {
		t.Fatalf("exit inside the reason leaked into the exit field: %q", v)
	}

	e, err = ParseExit("verdict=FIXED transactions=3 wall=21421s\n")
	if err != nil || e.HasExitCode || e.WallSeconds != 21421 {
		t.Fatalf("atomic form: %+v %v", e, err)
	}

	for _, bad := range []string{"", "\n\n", "garbage without a verdict", "kept=1 verdict=FIXED", "verdict= kept=1"} {
		if _, err := ParseExit(bad); err == nil {
			t.Errorf("ParseExit(%q) did not fail", bad)
		}
	}
}

func TestParseBuild(t *testing.T) {
	b, err := ParseBuild(strings.Join([]string{
		"version=4.100.220",
		"sha=eeeeeeeee",
		"dirty=37",
		"model=model-b:q4",
		"source=some/dir/hard.html sha=5b6559abbb62",
		"endpoint=http://localhost:11434 (default)",
		"mode=native max_changes=6 max_tx=6 run_timeout=7200s",
		"arm=manual atomic=off propose_check=n/a reconsider_after=0",
		`oracle=node run_game.js page.html expect="ok":\s*true.*"reached_playing":\s*true`,
		"oracle_version=v3 pinned=f13e481f2578",
		"agents_model=helper:cloud agents_num_ctx=model default parallel_sessions=3",
		"agents=auditor.md implementer.md ",
		"edit_verification=<host default>",
		"expert=none expert_num_ctx=default",
		"",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"version":           "4.100.220",
		"sha":               "eeeeeeeee",
		"source":            "some/dir/hard.html",
		"source_sha":        "5b6559abbb62",
		"endpoint":          "http://localhost:11434 (default)",
		"mode":              "native",
		"run_timeout":       "7200s",
		"arm":               "manual",
		"atomic":            "off",
		"oracle":            "node run_game.js page.html",
		"expect":            `"ok":\s*true.*"reached_playing":\s*true`,
		"oracle_version":    "v3",
		"pinned":            "f13e481f2578",
		"agents_num_ctx":    "model default",
		"parallel_sessions": "3",
		"agents":            "auditor.md implementer.md",
		"edit_verification": "<host default>",
		"expert_num_ctx":    "default",
	}
	for k, v := range want {
		if got := b.Fields[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if b, err := ParseBuild("oracle=none expect=\n"); err != nil || b.Fields["expect"] != "" || b.Fields["oracle"] != "none" {
		t.Errorf("empty value: %+v %v", b, err)
	}
	if _, err := ParseBuild("nothing here\n"); err == nil {
		t.Error("a file with no fields did not fail")
	}
}

func TestParseVerdictFile(t *testing.T) {
	in := strings.Join([]string{
		"20260817-074236-0001  FIXED  kept=1 discarded=0 848s",
		"20260907-114615-0151  broken  kept=0 discarded=0 empty=0 4186s  arm=proposed",
		"# 20260907-114615-0151 VOID — stopped by operator",
		"",
		"20261011-112103-0430  TIMEOUT  kept=0 discarded=0 empty=0 7203s  arm=manual oracle=v3 logic=13/23 failed=a,b",
		"20261011-132114-0431  ABORTED  kept=0 2233s",
		"not a run line",
		"20260814-211259-0001  FIXED  transactions=3 21421s",
	}, "\n")
	v, err := ParseVerdictFile(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if v.Lines != 7 || len(v.Runs) != 5 || len(v.Malformed) != 2 {
		t.Fatalf("lines=%d runs=%d malformed=%d", v.Lines, len(v.Runs), len(v.Malformed))
	}
	if got := v.Counts(); !reflect.DeepEqual(got, map[string]int{"FIXED": 2, "broken": 1, "TIMEOUT": 1, "ABORTED": 1}) {
		t.Fatalf("counts %v", got)
	}
	if len(v.Notes) != 1 || v.Notes[0].RunID != "20260907-114615-0151" || !strings.HasPrefix(v.Notes[0].Text, "20260907-114615-0151 VOID") {
		t.Fatalf("notes %+v", v.Notes)
	}
	if r := v.Runs[0]; r.RunID != "20260817-074236-0001" || !r.HasSeconds || r.Seconds != 848 || r.LineNo != 1 {
		t.Fatalf("first run %+v", r)
	}
	if r := v.Runs[2]; r.Seconds != 7203 || r.Fields[len(r.Fields)-1] != (KV{"failed", "a,b"}) {
		t.Fatalf("appended fields %+v", r)
	}
}

func TestParseTransactionsAndSummaries(t *testing.T) {
	tx := ParseTransactions("TX-01 discarded — the check failed.\nTX-02 kept — the check passed.\nkept=1 discarded=1 empty=0\n")
	if len(tx.Entries) != 2 || tx.Entries[0].Outcome != "discarded" || tx.Entries[1].TX != "TX-02" || tx.Entries[1].Outcome != "kept" {
		t.Fatalf("entries %+v", tx.Entries)
	}
	if tx.Counts["kept"] != "1" || tx.Counts["discarded"] != "1" || tx.Counts["empty"] != "0" {
		t.Fatalf("counts %v", tx.Counts)
	}
	if tx := ParseTransactions(""); len(tx.Entries) != 0 || len(tx.Counts) != 0 {
		t.Fatalf("empty file: %+v", tx)
	}

	fields, detail := ParseSummaryFile("agent auditor: 1\nagent implementer: 2\ndelegations=3\n")
	if fields["delegations"] != "3" || len(detail) != 2 || detail[0] != "agent auditor: 1" {
		t.Fatalf("delegation: %v %v", fields, detail)
	}
	fields, _ = ParseSummaryFile("proposals=0 adopted=0 run_checks=49 rethink_offered=0\n")
	if fields["run_checks"] != "49" || len(fields) != 4 {
		t.Fatalf("checks: %v", fields)
	}
	if fields, _ := ParseSummaryFile(""); len(fields) != 0 {
		t.Fatalf("empty: %v", fields)
	}
}

func TestParseOracle(t *testing.T) {
	o := ParseOracle(`{"ok":true,"error":null,"frames_run":30,"started":false}` + "\n\n")
	if !o.Parsed || !o.OK || o.FramesRun != 30 || o.ReachedPlaying != nil || o.HasGameLogic {
		t.Fatalf("v1: %+v", o)
	}
	o = ParseOracle("some noise\n" + `{"ok":false,"error":"SyntaxError: x","frames_run":0,"reached_playing":false,"game_logic":null}` + "\n")
	if !o.Parsed || o.OK || o.Error != "SyntaxError: x" || o.ReachedPlaying == nil || *o.ReachedPlaying {
		t.Fatalf("v2/v3 failing: %+v", o)
	}
	o = ParseOracle(`{"ok":true,"frames_run":400,"reached_playing":true,"game_logic":{"ok":false,"passed":21,"total":23,"failed":[{"trap":"jump"},{"trap":"gem"}]}}`)
	if !o.HasGameLogic || o.LogicPassed != 21 || o.LogicTotal != 23 || !reflect.DeepEqual(o.LogicFailed, []string{"jump", "gem"}) {
		t.Fatalf("v3: %+v", o)
	}
	if o := ParseOracle("Error: cannot find module\n"); o.Parsed {
		t.Fatalf("no report line parsed as one: %+v", o)
	}
}

func TestParseInvalidRunsAndBoundary(t *testing.T) {
	md := "# Title\n\n## 0267-0268 — broken tag\n\n```\n20260912-150953-0267  broken  4201s\n20260912-161956-0268  broken  3475s\n```\n\n**Why invalid.** The tag was\nmalformed.\n\nMore text mentioning 20260101-000000-0001 outside a block.\n\n## 0300 — other\n\n```\n20260914-000000-0300  FIXED  10s\n```\n"
	notes := ParseInvalidRuns(md)
	if len(notes) != 3 {
		t.Fatalf("notes %+v", notes)
	}
	if notes[0].RunID != "20260912-150953-0267" || notes[0].Reason != "0267-0268 — broken tag: The tag was malformed." {
		t.Fatalf("first note %+v", notes[0])
	}
	if notes[2].RunID != "20260914-000000-0300" || notes[2].Reason != "0300 — other" {
		t.Fatalf("third note %+v", notes[2])
	}

	if got := ParseChangeBoundary("x\n**boundary run\nis `20260911-155111-0213`** — first"); got != "20260911-155111-0213" {
		t.Fatalf("boundary %q", got)
	}
	if got := ParseChangeBoundary("no run named"); got != "" {
		t.Fatalf("boundary %q", got)
	}
}

func TestMapVerdictAndTaskVersion(t *testing.T) {
	cases := map[string]Verdict{"FIXED": Pass, "broken": Fail, "TIMEOUT": Timeout, "ABORTED": Aborted, "ROLLBACK-FAILED": Aborted}
	for word, want := range cases {
		got, ok := MapVerdict(word)
		if !ok || got != want || !got.Valid() {
			t.Errorf("MapVerdict(%q) = %q, %v", word, got, ok)
		}
	}
	for _, word := range []string{"", "fixed", "BROKEN", "PASS", "VOID"} {
		if _, ok := MapVerdict(word); ok {
			t.Errorf("MapVerdict(%q) accepted", word)
		}
	}
	versions := map[string]int{
		"20260817-074236-0001":               1,
		"20260911-154542-0212":               1,
		"20260911-155111-0213":               2,
		"20260930-131609-0410":               2,
		"20260930-131610-0411":               3,
		"20261011-091650-0428":               3,
		"20260907-114615-0151-VOID-badquant": 1,
		"aborted-110k-20260902-211237-0056":  1,
		"20261001-104801-0428-STOPPED":       3,
	}
	for id, want := range versions {
		if got := TaskVersionFor(id, DefaultTaskV2From, DefaultTaskV3From); got != want {
			t.Errorf("TaskVersionFor(%s) = %d, want %d", id, got, want)
		}
	}
}

func TestClassifyArtefact(t *testing.T) {
	cases := []struct {
		rel, kind, name, comp, tx string
		stream                    bool
	}{
		{"run.jsonl.gz", "events", "run.jsonl", "gzip", "", true},
		{"run.jsonl", "events", "run.jsonl", "", "", true},
		{"TX-03.jsonl.gz", "events", "TX-03.jsonl", "gzip", "TX-03", true},
		{"TX-01.prompt.txt", "prompt", "TX-01.prompt.txt", "", "TX-01", false},
		{"TX-01.oracle.txt", "oracle", "TX-01.oracle.txt", "", "TX-01", false},
		{"TX-01.diffstat.txt", "workspace_diff", "TX-01.diffstat.txt", "", "TX-01", false},
		{"TX-02.stderr", "runner_stderr", "TX-02.stderr", "", "TX-02", false},
		{"state-TX-02/logs/cline.log", "session", "state-TX-02/logs/cline.log", "", "TX-02", false},
		{"state/settings/providers.json", "session", "state/settings/providers.json", "", "", false},
		{"build.txt", "build", "build.txt", "", "", false},
		{"prompt.txt", "prompt", "prompt.txt", "", "", false},
		{"oracle.txt", "oracle", "oracle.txt", "", "", false},
		{"logic.txt", "oracle", "logic.txt", "", "", false},
		{"exit.txt", "exit", "exit.txt", "", "", false},
		{"exit.txt.pre", "exit", "exit.txt.pre", "", "", false},
		{"exit.txt.bak-pre-manual-correction", "exit", "exit.txt.bak-pre-manual-correction", "", "", false},
		{"transactions.txt", "transactions", "transactions.txt", "", "", false},
		{"checks.txt", "checks", "checks.txt", "", "", false},
		{"escalations.txt", "escalations", "escalations.txt", "", "", false},
		{"delegation.txt", "delegation", "delegation.txt", "", "", false},
		{"abort-reason.txt", "abort_reason", "abort-reason.txt", "", "", false},
		{"ollama.log", "server_log", "ollama.log", "", "", false},
		{"server.log.gz", "server_log", "server.log", "gzip", "", false},
		{"auth.log", "runtime_log", "auth.log", "", "", false},
		{"run.stderr", "runner_stderr", "run.stderr", "", "", false},
		{"page.before.html", "workspace_before", "page.before.html", "", "", false},
		{"page.after.html", "workspace_after", "page.after.html", "", "", false},
		{".tx-base/page.html", "workspace_before", ".tx-base/page.html", "", "", false},
		{"tmp-scratch.tar.gz", "scratch", "tmp-scratch.tar", "gzip", "", false},
		{"tmp/cline-tools.jsonl", "scratch", "tmp/cline-tools.jsonl", "", "", false},
		{"compactions/001-1.json", "compaction", "compactions/001-1.json", "", "", false},
		{"NOTES.txt", "note", "NOTES.txt", "", "", false},
		{"VOID-stopped.txt", "note", "VOID-stopped.txt", "", "", false},
		{".snap-done", "other", ".snap-done", "", "", false},
	}
	for _, c := range cases {
		a := ClassifyArtefact(c.rel)
		if a.Kind != c.kind || a.Name != c.name || a.Compression != c.comp || a.TX != c.tx || a.Stream != c.stream {
			t.Errorf("%s: got kind=%q name=%q comp=%q tx=%q stream=%v", c.rel, a.Kind, a.Name, a.Compression, a.TX, a.Stream)
		}
	}
}

func TestProviderSettingsAndStateTar(t *testing.T) {
	p, err := ParseProviderSettings([]byte(providersJSON))
	if err != nil {
		t.Fatal(err)
	}
	if p.Provider != "ollama" || p.Model != "model-b:q4" || p.ContextWindow != 96000 || p.BaseURL != fixtureEndpoint || p.MaxToolResultChars != 64000 {
		t.Fatalf("%+v", p)
	}
	var s map[string]float64
	if err := json.Unmarshal(p.Sampling, &s); err != nil || s["temperature"] != 0.8 {
		t.Fatalf("sampling %s %v", p.Sampling, err)
	}
	out, _ := json.Marshal(p)
	if bytes.Contains(out, []byte("not-a-secret")) {
		t.Fatal("the API key was carried into the result")
	}
	if _, err := ParseProviderSettings([]byte(`{"providers":{"a":{},"b":{}}}`)); err == nil {
		t.Fatal("an ambiguous settings file did not fail")
	}

	st, err := ScanStateTar(bytes.NewReader(stateTar(t, map[string]string{
		"state/settings/providers.json": providersJSON,
		"state/sessions/s1/s1.json":     "{}",
		"state/sessions/s1/s1.messages": "[1,2]",
		"state/sessions/s2/s2.json":     "{}",
		"state/logs/cline.log":          "line\n",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 5 || st.Sessions != 2 || !st.HasLog || st.Settings == nil || st.Settings.Model != "model-b:q4" {
		t.Fatalf("%+v", st)
	}
	if st.Bytes != int64(len(providersJSON)+2+5+2+5) {
		t.Fatalf("bytes %d", st.Bytes)
	}
	if _, err := ScanStateTar(strings.NewReader(strings.Repeat("not a tar archive ", 100))); err == nil {
		t.Fatal("garbage scanned as a tar")
	}
}
