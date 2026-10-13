package legacyimport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustImport(t *testing.T, o Options) *Report {
	t.Helper()
	rep, err := Import(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func summary(t *testing.T, rep *Report, id string) RunSummary {
	t.Helper()
	for _, r := range rep.Runs {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("run %s is not in the report", id)
	return RunSummary{}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestDiscover(t *testing.T) {
	fx := newFixture(t)
	d, err := Discover(fx.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Runs) != 14 {
		t.Fatalf("%d runs", len(d.Runs))
	}
	if !reflect.DeepEqual(d.Ignored, []string{"runs-native/stray-file.txt"}) || len(d.Collisions) != 0 {
		t.Fatalf("ignored %v collisions %v", d.Ignored, d.Collisions)
	}
	var void RunSource
	sets := map[string]int{}
	for _, r := range d.Runs {
		sets[r.Set]++
		if r.DirName == "20260907-114615-0151-VOID-badquant" {
			void = r
		}
	}
	if !reflect.DeepEqual(sets, map[string]int{SetNative: 9, SetAtomic: 2, SetTeams: 1, SetDiscarded: 1, SetAbandoned: 1}) {
		t.Fatalf("sets %v", sets)
	}
	// A renamed directory keeps the id the verdict file knows it by.
	if void.ID != "20260907-114615-0151" || void.Annotation != "VOID-badquant" || void.RelDir != "runs-native/20260907-114615-0151-VOID-badquant" {
		t.Fatalf("%+v", void)
	}

	// Two directories with one id: the second keeps its directory name.
	writeFile(t, filepath.Join(fx.root, "runs-native", "aborted-20260817-074236-0001", "build.txt"), "model=x\n")
	d, err = Discover(fx.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Collisions) != 1 || d.Runs[len(d.Runs)-6].ID == "20260817-074236-0001" && d.Runs[len(d.Runs)-6].DirName != "20260817-074236-0001" {
		t.Fatalf("collisions %v", d.Collisions)
	}
	ids := map[string]bool{}
	for _, r := range d.Runs {
		if ids[r.ID] {
			t.Fatalf("id %s given twice", r.ID)
		}
		ids[r.ID] = true
	}
	if !ids["aborted-20260817-074236-0001"] {
		t.Fatal("the colliding directory did not keep its name as id")
	}
}

func TestImportFixture(t *testing.T) {
	fx := newFixture(t)
	before, beforeArchive := treeDigest(t, fx.root), treeDigest(t, fx.archive)
	sink := NewMemorySink()
	sink.KeepBlobs = true
	rep := mustImport(t, Options{LegacyRoot: fx.root, StateArchive: fx.archive, Sink: sink, Workers: 3})

	if treeDigest(t, fx.root) != before || treeDigest(t, fx.archive) != beforeArchive {
		t.Fatal("the import changed the legacy tree")
	}

	// Counts per loop and per set.
	wantLoops := map[string]Counts{
		SetNative: {Dirs: 11, Finished: 10, Imported: 10, Failed: 1},
		SetAtomic: {Dirs: 2, Finished: 2, Imported: 2},
		SetTeams:  {Dirs: 1, Finished: 1, Imported: 1},
	}
	for k, want := range wantLoops {
		if got := rep.Loops[k]; got == nil || *got != want {
			t.Errorf("loop %s: %+v, want %+v", k, got, want)
		}
	}
	wantSets := map[string]Counts{
		SetNative:    {Dirs: 9, Finished: 8, Imported: 8, Failed: 1},
		SetDiscarded: {Dirs: 1, Finished: 1, Imported: 1},
		SetAbandoned: {Dirs: 1, Finished: 1, Imported: 1},
	}
	for k, want := range wantSets {
		if got := rep.Sets[k]; got == nil || *got != want {
			t.Errorf("set %s: %+v, want %+v", k, got, want)
		}
	}
	if got, want := rep.OriginalVerdicts[SetNative], (map[string]int{"FIXED": 3, "broken": 4, "TIMEOUT": 1, "ABORTED": 1, "": 1}); !reflect.DeepEqual(got, want) {
		t.Errorf("native original verdicts %v", got)
	}
	if got, want := rep.Verdicts[SetNative], (map[string]int{"PASS": 3, "FAIL": 2, "INVALID": 2, "TIMEOUT": 1, "ABORTED": 2}); !reflect.DeepEqual(got, want) {
		t.Errorf("native verdicts %v", got)
	}
	if got, want := rep.Verdicts[SetAtomic], (map[string]int{"PASS": 1, "ABORTED": 1}); !reflect.DeepEqual(got, want) {
		t.Errorf("atomic verdicts %v", got)
	}
	if got, want := rep.OriginalVerdicts[SetAtomic], (map[string]int{"FIXED": 1, "ROLLBACK-FAILED": 1}); !reflect.DeepEqual(got, want) {
		t.Errorf("atomic original verdicts %v", got)
	}

	// The bad run is recorded and did not stop the import.
	if len(rep.Failed) != 1 || rep.Failed[0].ID != "20261012-000000-0999" || !strings.Contains(rep.Failed[0].Reason, "verdict") {
		t.Fatalf("failed %+v", rep.Failed)
	}
	if _, ok := sink.Runs["20261012-000000-0999"]; ok {
		t.Fatal("the bad run was written")
	}
	if len(sink.Runs) != 13 {
		t.Fatalf("%d runs written", len(sink.Runs))
	}

	// Every run: original id, imported mark, valid verdict, JSON objects.
	for id, r := range sink.Runs {
		if !r.Imported || r.ImporterVersion != ImporterVersion || !r.Verdict.Valid() {
			t.Errorf("%s: %+v", id, r)
		}
		if r.Suite != LegacySuite || r.TaskID != LegacyTaskID || !strings.HasPrefix(r.Campaign, CampaignPrefix) {
			t.Errorf("%s: identity %q %q %q", id, r.Suite, r.TaskID, r.Campaign)
		}
		var arm, prov map[string]any
		if json.Unmarshal(r.Arm, &arm) != nil || json.Unmarshal(r.Provenance, &prov) != nil {
			t.Errorf("%s: arm or provenance is not a JSON object", id)
		}
		if prov["import_complete"] != true || Fingerprint(&r) == "" {
			t.Errorf("%s: not marked complete", id)
		}
		// Unknown is named, never defaulted.
		for field, value := range map[string]string{"host": r.Host, "arm_id": r.ArmID, "thinking": r.Thinking, "provider": r.Provider, "endpoint": r.Endpoint, "model": r.Model, "harness": r.Harness, "agent_end": r.AgentEnd} {
			if value == "" {
				t.Errorf("%s: %s is empty", id, field)
			}
			if (value == Unknown) != has(r.Missing, field) {
				t.Errorf("%s: %s=%q but Missing=%v", id, field, value, r.Missing)
			}
		}
		if !has(r.Missing, "wire") {
			t.Errorf("%s: wire capture not named as missing", id)
		}
		for _, ref := range sink.Refs[id] {
			if ref.RunID != id || ref.Kind == "" || ref.Hash == "" {
				t.Errorf("%s: bad ref %+v", id, ref)
			}
			if size, ok := sink.BlobSizes[string(ref.Hash)]; !ok || size != ref.Size {
				t.Errorf("%s: ref %s size %d, blob %d", id, ref.Name, ref.Size, size)
			}
		}
	}

	// Task version 1, oldest build file.
	r := sink.Runs["20260817-074236-0001"]
	if r.Verdict != Pass || r.OriginalVerdict != "FIXED" || r.PriorVerdict != "" || r.TaskVersion != 1 {
		t.Errorf("0001 verdict: %+v", r)
	}
	if r.Model != "model-a:Q4" || r.Endpoint != Unknown || r.Harness != "4.100.40+aaaaaaaaa" || r.Provider != "ollama" || r.AgentEnd != "completed" {
		t.Errorf("0001 arm fields: %+v", r)
	}
	if r.WallMS != 848000 || r.StartedAt.IsZero() || !r.EndedAt.After(r.StartedAt) || r.ClaimedSuccess != nil {
		t.Errorf("0001 clock: %+v", r)
	}
	for _, f := range []string{"endpoint", "protocol_arm", "accumulated_text", "sampler"} {
		if f == "sampler" {
			// The archived state has the settings file, with a sampler.
			if has(r.Missing, f) {
				t.Errorf("0001: sampler should come from the archived state")
			}
			continue
		}
		if !has(r.Missing, f) {
			t.Errorf("0001: %s not in Missing %v", f, r.Missing)
		}
	}
	if has(r.Missing, "session_state") {
		t.Errorf("0001: archived state not found")
	}
	var prov struct {
		Findings []map[string]any `json:"findings"`
		Legacy   map[string]any   `json:"legacy"`
		Exit     map[string]string
		Events   StreamStats
		State    map[string]any `json:"session_state"`
	}
	if err := json.Unmarshal(r.Provenance, &prov); err != nil {
		t.Fatal(err)
	}
	if len(prov.Findings) != 1 || prov.Findings[0]["detector"] != "legacy_oracle" {
		t.Errorf("0001: no finding that the oracle could not fail: %v", prov.Findings)
	}
	if prov.Legacy["dir"] != "runs-native/20260817-074236-0001" || prov.Exit["kept"] != "1" || prov.Events.ReasoningDeltas != 2 {
		t.Errorf("0001 provenance: %+v", prov)
	}
	if prov.State["archive"] != "state/20260817-074236-0001.tar.zst" {
		t.Errorf("0001 state: %v", prov.State)
	}
	refs := map[string]BlobRef{}
	for _, ref := range sink.Refs[r.ID] {
		refs[ref.Kind+"|"+ref.Name] = ref
	}
	for _, key := range []string{
		"events|run.jsonl", "build|build.txt", "exit|exit.txt", "prompt|prompt.txt", "oracle|oracle.txt",
		"transactions|transactions.txt", "runner_stderr|run.stderr", "server_log|ollama.log",
		"workspace_before|page.before.html", "workspace_after|page.after.html", "scratch|tmp-scratch.tar",
		"scratch|tmp/cline-tools.jsonl", "session_archive|state.tar",
	} {
		if _, ok := refs[key]; !ok {
			t.Errorf("0001: no blob ref %s (have %v)", key, sink.Refs[r.ID])
		}
	}
	if len(refs) != 13 {
		t.Errorf("0001: %d refs", len(refs))
	}
	// A compressed stream is stored uncompressed, byte for byte.
	ev := refs["events|run.jsonl"]
	if want := stream("model-a:Q4", true); string(sink.BlobData[string(ev.Hash)]) != want || ev.Size != int64(len(want)) {
		t.Errorf("0001: stored stream differs from the source")
	}
	calls := sink.Calls[r.ID]
	if len(calls) != 3 || calls[0].RunID != r.ID || calls[0].Tool != "run_commands" || !calls[0].OK ||
		calls[1].ErrorClass != "missing_argument" || calls[2].ErrorClass != "tool_reported_error" || calls[2].Turn != 2 || calls[2].Seq != 2 {
		t.Errorf("0001 tool calls: %+v", calls)
	}

	// Voided in the verdict file and by the directory's name.
	r = sink.Runs["20260907-114615-0151"]
	if r.Verdict != Invalid || r.PriorVerdict != Fail || r.OriginalVerdict != "broken" || !strings.Contains(r.VerdictReason, "VOID") || !strings.Contains(r.VerdictReason, "kill artefact") {
		t.Errorf("0151: %+v", r)
	}
	// Listed in the notes file.
	r = sink.Runs["20260912-150953-0267"]
	if r.Verdict != Invalid || r.PriorVerdict != Fail || r.OriginalVerdict != "broken" || !strings.Contains(r.VerdictReason, "swallowed its template") {
		t.Errorf("0267: %+v", r)
	}
	if has(r.Missing, "session_state") {
		t.Errorf("0267: state archive inside the legacy root not found")
	}
	if !has(r.Missing, "events") || r.AgentEnd != Unknown || !has(r.Missing, "started_at") {
		t.Errorf("0267 has no stream: %+v", r.Missing)
	}

	// Task version 2; dirty build; default endpoint; stream cut by a guard.
	r = sink.Runs["20260911-155111-0213"]
	if r.Verdict != Fail || r.TaskVersion != 2 || r.Harness != "4.100.97+ccccccccc.dirty" || r.Endpoint != "http://localhost:11434" {
		t.Errorf("0213: %+v", r)
	}
	if r.AgentEnd != Unknown || !has(r.Missing, "agent_end") || !has(r.Missing, "session_state") || !has(r.Missing, "sampler") {
		t.Errorf("0213: %+v", r)
	}
	var arm map[string]any
	_ = json.Unmarshal(r.Arm, &arm)
	if arm["endpoint_defaulted"] != true || arm["protocol_arm"] != "oracle" || arm["thinking"] != Unknown || arm["sampling"] != Unknown || arm["values_are"] != "as configured" {
		t.Errorf("0213 arm: %v", arm)
	}

	// No exit.txt.
	r = sink.Runs["20260913-214530-0296"]
	if r.Verdict != Aborted || r.OriginalVerdict != "" || !has(r.Missing, "verdict") || !strings.Contains(r.VerdictReason, "no exit.txt") {
		t.Errorf("0296: %+v", r)
	}
	if s := summary(t, rep, r.ID); s.Finished {
		t.Errorf("0296 reported as finished")
	}

	// Task version 3 with the state still on disk.
	r = sink.Runs["20260930-131610-0411"]
	if r.Verdict != Pass || r.TaskVersion != 3 || has(r.Missing, "session_state") || has(r.Missing, "sampler") || has(r.Missing, "accumulated_text") == false {
		t.Errorf("0411: %+v", r)
	}
	_ = json.Unmarshal(r.Arm, &arm)
	if s, ok := arm["sampling"].(map[string]any); !ok || s["temperature"] != 0.8 {
		t.Errorf("0411 sampling: %v", arm["sampling"])
	}
	if strings.Contains(string(r.Arm), "not-a-secret") {
		t.Errorf("0411: the API key reached the arm")
	}
	kinds := map[string]int{}
	for _, ref := range sink.Refs[r.ID] {
		kinds[ref.Kind]++
	}
	if kinds["session"] != 3 || kinds["compaction"] != 1 || kinds["server_log"] != 1 || kinds["oracle"] != 2 || kinds["escalations"] != 1 || kinds["checks"] != 1 {
		t.Errorf("0411 kinds: %v", kinds)
	}

	// TIMEOUT and ABORTED are unchanged, with the harness's reason.
	r = sink.Runs["20261011-112103-0430"]
	if r.Verdict != Timeout || r.OriginalVerdict != "TIMEOUT" || r.VerdictReason != "clock: 7203s vs 7200s cap, exit=0" {
		t.Errorf("0430: %+v", r)
	}
	r = sink.Runs["20261011-132114-0431"]
	if r.Verdict != Aborted || r.OriginalVerdict != "ABORTED" || r.VerdictReason != "exit=143 (SIGTERM)" {
		t.Errorf("0431: %+v", r)
	}
	// Its stream is not valid gzip: kept as written, and said so.
	found := false
	for _, ref := range sink.Refs[r.ID] {
		if ref.Kind == "events" && ref.Name == "run.jsonl.gz" && string(sink.BlobData[string(ref.Hash)]) == "this is not gzip" {
			found = true
		}
	}
	if !found {
		t.Errorf("0431: the undecodable stream was not kept as written: %v", sink.Refs[r.ID])
	}
	warned := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "20261011-132114-0431") && strings.Contains(w, "run.jsonl.gz") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("0431: no warning about the stream: %v", rep.Warnings)
	}

	// The atomic loop: one stream per transaction, turns carried over.
	r = sink.Runs["20260814-211259-0001"]
	if r.Verdict != Pass || r.Campaign != CampaignPrefix+SetAtomic || r.WallMS != 21421000 {
		t.Errorf("atomic 0001: %+v", r)
	}
	calls = sink.Calls[r.ID]
	if len(calls) != 6 || calls[3].Turn != 3 || calls[5].Turn != 4 || calls[5].Seq != 5 {
		t.Errorf("atomic tool calls: %+v", calls)
	}
	var aprov struct {
		TX map[string]txMeta `json:"atomic_transactions"`
	}
	_ = json.Unmarshal(r.Provenance, &aprov)
	if len(aprov.TX) != 2 || !aprov.TX["TX-01"].HasStream || !aprov.TX["TX-01"].HasState || aprov.TX["TX-01"].Oracle == nil || aprov.TX["TX-01"].Oracle.OK ||
		aprov.TX["TX-02"].Oracle == nil || !aprov.TX["TX-02"].Oracle.OK || aprov.TX["TX-02"].Diffstat != "page.html modified" {
		t.Errorf("atomic transactions: %+v", aprov.TX)
	}
	r = sink.Runs["20260815-160854-0002"]
	if r.Verdict != Aborted || r.OriginalVerdict != "ROLLBACK-FAILED" || !strings.Contains(r.VerdictReason, "rollback") {
		t.Errorf("atomic 0002: %+v", r)
	}

	// Teams, discarded, abandoned.
	if r = sink.Runs["20260819-175024-0001"]; r.Verdict != Fail || r.Campaign != CampaignPrefix+SetTeams || !strings.Contains(string(r.Provenance), `"delegations":"3"`) {
		t.Errorf("teams: %+v %s", r, r.Provenance)
	}
	if s := summary(t, rep, "20260818-071005-0008"); s.Set != SetDiscarded || s.Loop != SetNative || s.Verdict != Fail {
		t.Errorf("discarded: %+v", s)
	}
	if s := summary(t, rep, "20260912-090403-0257"); s.Set != SetAbandoned || s.Verdict != Pass || s.TaskVersion != 2 {
		t.Errorf("abandoned: %+v", s)
	}

	// Corpus-wide facts.
	if rep.StateOnDisk != 2 || rep.StateArchived != 2 || rep.StateArchiveFiles != 3 ||
		!reflect.DeepEqual(rep.OrphanStateArchive, []string{"state/20200101-000000-0001.tar.zst"}) {
		t.Errorf("state: on disk %d, archived %d, files %d, orphans %v", rep.StateOnDisk, rep.StateArchived, rep.StateArchiveFiles, rep.OrphanStateArchive)
	}
	var changes []string
	for _, b := range rep.Boundaries {
		if b.Kind == "harness_change" {
			changes = append(changes, b.From)
		}
	}
	if !reflect.DeepEqual(changes, []string{"20260911-155111-0213", "20261011"}) {
		t.Errorf("harness changes %v", changes)
	}
	if rep.UnknownFields["wire"] != 13 || rep.UnknownFields["thinking"] != 13 || rep.UnknownFields["verdict"] != 1 {
		t.Errorf("unknown fields %v", rep.UnknownFields)
	}
	if rep.EventTypes["agent_event/error"] == 0 || rep.EventTypes["run_aborted"] != 1 || rep.EventTypes["hook_event/tool_call"] == 0 {
		t.Errorf("event types %v", rep.EventTypes)
	}
	if rep.BytesRead == 0 || rep.StreamBytes == 0 || rep.ToolCalls != 3*11 || int(rep.Blobs) != sink.Puts {
		t.Errorf("bytes=%d stream=%d calls=%d blobs=%d puts=%d", rep.BytesRead, rep.StreamBytes, rep.ToolCalls, rep.Blobs, sink.Puts)
	}
	if len(rep.Inconsistencies) != 0 {
		t.Errorf("inconsistencies %v", rep.Inconsistencies)
	}

	// The verdict files are reproduced.
	ver, err := VerifyAgainstLegacy(rep, fx.root)
	if err != nil {
		t.Fatal(err)
	}
	if !ver.OK() {
		t.Fatalf("verification:\n%s", ver)
	}
	var native FileCheck
	for _, f := range ver.Files {
		if f.Loop == SetNative {
			native = f
		}
	}
	if native.Lines != 9 || native.Malformed != 1 || !reflect.DeepEqual(native.Expected, map[string]int{"FIXED": 3, "broken": 3, "TIMEOUT": 1, "ABORTED": 1}) || !reflect.DeepEqual(native.Expected, native.Imported) {
		t.Errorf("native check %+v", native)
	}

	// Resumable: a second pass skips everything it already has and writes
	// nothing; the bad run is tried again.
	puts, upserts := sink.Puts, sink.Upserts
	rep2 := mustImport(t, Options{LegacyRoot: fx.root, StateArchive: fx.archive, Sink: sink})
	if len(rep2.Skipped) != 13 || len(rep2.Failed) != 1 || sink.Puts != puts || sink.Upserts != upserts {
		t.Fatalf("second pass: skipped %d, failed %d, puts %d->%d, upserts %d->%d", len(rep2.Skipped), len(rep2.Failed), puts, sink.Puts, upserts, sink.Upserts)
	}
	// Skipped runs still carry their verdicts, so the check still passes.
	if ver, err := VerifyAgainstLegacy(rep2, fx.root); err != nil || !ver.OK() {
		t.Fatalf("verification after resume: %v\n%s", err, ver)
	}
	// A run whose source changed is imported again. (The fixture is ours.)
	writeFile(t, filepath.Join(fx.root, "runs-teams", "20260819-175024-0001", "NOTES.txt"), "added later\n")
	rep3 := mustImport(t, Options{LegacyRoot: fx.root, StateArchive: fx.archive, Sink: sink})
	if len(rep3.Skipped) != 12 || summary(t, rep3, "20260819-175024-0001").Status != StatusImported {
		t.Fatalf("third pass: skipped %d", len(rep3.Skipped))
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	fx := newFixture(t)
	before := treeDigest(t, fx.root)
	sink := NewMemorySink()
	for _, metaOnly := range []bool{false, true} {
		rep := mustImport(t, Options{LegacyRoot: fx.root, StateArchive: fx.archive, Sink: sink, DryRun: true, MetadataOnly: metaOnly})
		if sink.Puts != 0 || sink.Upserts != 0 || len(sink.Runs) != 0 || len(sink.Refs) != 0 || len(sink.Calls) != 0 {
			t.Fatalf("the dry run wrote to the sink")
		}
		if rep.Blobs != 0 || !rep.DryRun {
			t.Fatalf("%+v", rep)
		}
		if got := rep.Loops[SetNative]; got.Dirs != 11 || got.Imported != 10 || got.Failed != 1 {
			t.Fatalf("native %+v", got)
		}
		if ver, err := VerifyAgainstLegacy(rep, fx.root); err != nil || !ver.OK() {
			t.Fatalf("verification: %v\n%s", err, ver)
		}
		if metaOnly {
			if rep.StreamBytes != 0 || rep.ToolCalls != 0 || rep.UnknownFields["agent_end"] != 0 {
				t.Fatalf("metadata only read streams: %+v", rep)
			}
		} else if rep.ToolCalls != 33 || rep.StreamBytes == 0 {
			t.Fatalf("dry run did not scan the streams: calls=%d", rep.ToolCalls)
		}
		if s := rep.String(); !strings.Contains(s, "dry run") || !strings.Contains(s, "FAILED runs-native/20261012-000000-0999") {
			t.Fatalf("report text:\n%s", s)
		}
	}
	// A nil sink is fine in a dry run and refused otherwise.
	mustImport(t, Options{LegacyRoot: fx.root, DryRun: true})
	if _, err := Import(context.Background(), Options{LegacyRoot: fx.root}); err == nil {
		t.Fatal("an import without a sink started")
	}
	if _, err := Import(context.Background(), Options{LegacyRoot: fx.root, Sink: sink, MetadataOnly: true}); err == nil {
		t.Fatal("MetadataOnly accepted outside a dry run")
	}
	if _, err := Import(context.Background(), Options{LegacyRoot: filepath.Join(fx.root, "missing"), DryRun: true}); err == nil {
		t.Fatal("a missing legacy root was accepted")
	}
	if treeDigest(t, fx.root) != before {
		t.Fatal("the dry run changed the legacy tree")
	}
}

// failingSink fails on the blobs of one run and on the rows of another.
type failingSink struct {
	*MemorySink
	failRows string
}

func (f *failingSink) AddBlobRefs(ctx context.Context, refs []BlobRef) error {
	if len(refs) > 0 && refs[0].RunID == f.failRows {
		return errors.New("disk full")
	}
	return f.MemorySink.AddBlobRefs(ctx, refs)
}

func TestSinkFailureIsRecordedAndRedone(t *testing.T) {
	fx := newFixture(t)
	sink := &failingSink{MemorySink: NewMemorySink(), failRows: "20260819-175024-0001"}
	rep := mustImport(t, Options{LegacyRoot: fx.root, Sink: sink, Sets: []string{SetTeams, SetAtomic}})
	if len(rep.Failed) != 1 || rep.Failed[0].ID != sink.failRows || !strings.Contains(rep.Failed[0].Reason, "disk full") {
		t.Fatalf("failed %+v", rep.Failed)
	}
	if rep.Loops[SetAtomic].Imported != 2 || rep.Loops[SetTeams].Failed != 1 || len(rep.Runs) != 3 {
		t.Fatalf("%+v %+v", rep.Loops[SetAtomic], rep.Loops[SetTeams])
	}
	// The half-written run is not marked complete, so the next pass redoes it.
	half := sink.Runs[sink.failRows]
	if Fingerprint(&half) != "" {
		t.Fatal("a half-imported run carries a fingerprint")
	}
	sink.failRows = ""
	rep = mustImport(t, Options{LegacyRoot: fx.root, Sink: sink, Sets: []string{SetTeams, SetAtomic}})
	if len(rep.Failed) != 0 || len(rep.Skipped) != 2 || rep.Loops[SetTeams].Imported != 1 {
		t.Fatalf("second pass: failed %v skipped %v", rep.Failed, rep.Skipped)
	}
	done := sink.Runs["20260819-175024-0001"]
	if Fingerprint(&done) == "" {
		t.Fatal("the redone run is not marked complete")
	}
}

func TestImportCancelled(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep, err := Import(ctx, Options{LegacyRoot: fx.root, DryRun: true})
	if !errors.Is(err, context.Canceled) || rep == nil {
		t.Fatalf("rep=%v err=%v", rep, err)
	}
}

func TestVerifyReportsDifferences(t *testing.T) {
	fx := newFixture(t)
	rep := mustImport(t, Options{LegacyRoot: fx.root, DryRun: true, MetadataOnly: true})

	// A run goes missing, one changes its word, one maps wrongly.
	var kept []RunSummary
	for _, r := range rep.Runs {
		switch r.ID {
		case "20260817-074236-0001":
			continue
		case "20260911-155111-0213":
			r.OriginalVerdict = "FIXED"
		case "20261011-112103-0430":
			r.Verdict = Fail
		}
		kept = append(kept, r)
	}
	rep.Runs = kept
	ver, err := VerifyAgainstLegacy(rep, fx.root)
	if err != nil {
		t.Fatal(err)
	}
	if ver.OK() {
		t.Fatal("a tampered report verified")
	}
	text := strings.Join(ver.Differences, "\n")
	for _, want := range []string{
		"run 20260817-074236-0001 is not in the import",
		`20260911-155111-0213: file says broken, imported as "FIXED"`,
		`20261011-112103-0430: TIMEOUT should map to "TIMEOUT", run carries "FAIL"`,
		"verdicts-native.txt: broken: 3 in the file, 2 imported",
		"verdicts-native.txt: FIXED: 3 in the file, 3 imported",
	} {
		if strings.Contains(want, "FIXED: 3 in the file, 3") {
			if strings.Contains(text, want) {
				t.Errorf("equal counts reported as a difference")
			}
			continue
		}
		if !strings.Contains(text, want) {
			t.Errorf("missing difference %q in:\n%s", want, text)
		}
	}
	if _, err := VerifyAgainstLegacy(rep, t.TempDir()); err == nil {
		t.Fatal("a root without verdict files verified")
	}
}

func TestMemorySinkPutBlob(t *testing.T) {
	s := NewMemorySink()
	h1, n, err := s.PutBlob(context.Background(), strings.NewReader("abc"))
	if err != nil || n != 3 || len(h1) != 64 {
		t.Fatal(h1, n, err)
	}
	h2, _, _ := s.PutBlob(context.Background(), strings.NewReader("abc"))
	if h1 != h2 || len(s.BlobSizes) != 1 || s.Puts != 2 {
		t.Fatal("content addressing")
	}
	if _, _, err := s.PutBlob(context.Background(), io.MultiReader(strings.NewReader("a"), errReader{})); err == nil {
		t.Fatal("a failing reader stored a blob")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

// --- the real corpus ------------------------------------------------------

// legacyPaths returns the real legacy root and state archive, from the
// environment or from the untracked spike.local.toml at the repository root.
func legacyPaths(t *testing.T) (root, archive string) {
	root, archive = os.Getenv("AUTOMATICUM_LEGACY_ROOT"), os.Getenv("AUTOMATICUM_LEGACY_STATE_ARCHIVE")
	if root != "" {
		return root, archive
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
	b, err := os.ReadFile(filepath.Join(dir, "spike.local.toml"))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch strings.TrimSpace(k) {
		case "legacy_root":
			root = v
		case "legacy_state_archive":
			archive = v
		}
	}
	return root, archive
}

// TestRealCorpusDryRun runs a dry-run import over the real corpus when it is
// available and checks it against what the corpus's own files say. With
// -short it reads the metadata only. Set AUTOMATICUM_LEGACY_REPORT to a file
// path to keep the report.
//
// The numbers asserted here were taken from the corpus on 2026-10-13. The
// old harness is paused, not retired: if it runs again they will move, and
// the verdict-file check at the end is the part that must keep holding.
func TestRealCorpusDryRun(t *testing.T) {
	root, archive := legacyPaths(t)
	if root == "" {
		t.Skip("no legacy corpus configured")
	}
	if _, err := os.Stat(filepath.Join(root, "verdicts-native.txt")); err != nil {
		t.Skip("legacy corpus not available")
	}
	o := Options{LegacyRoot: root, StateArchive: archive, DryRun: true, MetadataOnly: testing.Short(), Workers: 8}
	rep, err := Import(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	ver, err := VerifyAgainstLegacy(rep, root)
	if err != nil {
		t.Fatal(err)
	}
	text := rep.String() + "\n" + ver.String()
	if path := os.Getenv("AUTOMATICUM_LEGACY_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("\n" + text)

	// Threshold T3: the old verdict counts reproduced exactly, ids kept.
	if !ver.OK() {
		t.Errorf("the verdict files are not reproduced:\n%s", strings.Join(ver.Differences, "\n"))
	}
	if len(rep.Failed) != 0 {
		t.Errorf("%d runs failed to import", len(rep.Failed))
	}
	for _, f := range ver.Files {
		if f.Loop != SetNative {
			continue
		}
		want := map[string]int{"FIXED": 284, "broken": 96, "TIMEOUT": 24, "ABORTED": 19}
		if f.Lines != 424 || f.Malformed != 1 || !reflect.DeepEqual(f.Expected, want) {
			t.Errorf("verdicts-native.txt: %d lines, %d not run lines, %v", f.Lines, f.Malformed, f.Expected)
		}
	}
	wantSets := map[string]Counts{
		SetNative:    {Dirs: 438, Finished: 418, Imported: 438},
		SetAtomic:    {Dirs: 21, Finished: 13, Imported: 21},
		SetTeams:     {Dirs: 6, Finished: 6, Imported: 6},
		SetDiscarded: {Dirs: 3, Finished: 1, Imported: 3},
		SetAbandoned: {Dirs: 6, Finished: 5, Imported: 6},
	}
	for k, want := range wantSets {
		if got := rep.Sets[k]; got == nil || *got != want {
			t.Errorf("set %s: %+v, want %+v", k, got, want)
		}
	}
	ids := map[string]bool{}
	for _, r := range rep.Runs {
		if ids[r.ID] {
			t.Errorf("run id %s appears twice", r.ID)
		}
		ids[r.ID] = true
	}
}
