package legacyimport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Synthetic, like fixture_test.go. The run ids of the second pass's tables
// are the ones spec 100 section 8 decides on; everything in the runs is made
// up.

const (
	miniBuildV1 = "version=4.100.73\nsha=111111111\ndirty=0\nmodel=model-f:q4\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=3 max_tx=6 run_timeout=21600s\narm=oracle atomic=auto\noracle=node run_game.js page.html expect=\"ok\":\\s*true\n"
	miniBuildV2 = "version=4.100.98\nsha=222222222\ndirty=0\nmodel=model-f:q4\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=oracle atomic=auto\noracle=node run_game.js page.html expect=\"ok\":\\s*true.*\"reached_playing\":\\s*true\noracle_sha=0123456789abcdef\n"
	miniBuildV3 = "version=4.100.220\nsha=444444444\ndirty=0\nmodel=model-f:q4\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=manual atomic=off\noracle=none expect=\noracle_version=v3 pinned=0123456789ab\n"
	// A manual arm: the build file gives no oracle pattern.
	miniBuildManual = "version=4.100.135\nsha=333333333\ndirty=0\nmodel=model-f:q4\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=manual atomic=off\noracle=none expect=\noracle_sha=0123456789abcdef\n"
)

// miniRun writes a run directory with a build file and, unless exit is
// empty, an exit file.
func miniRun(t *testing.T, root, dir, build, exit string, more map[string]string) {
	t.Helper()
	base := filepath.Join(root, "runs-native", dir)
	writeFile(t, filepath.Join(base, "build.txt"), build)
	if exit != "" {
		writeFile(t, filepath.Join(base, "exit.txt"), exit+"\n")
	}
	for name, content := range more {
		writeFile(t, filepath.Join(base, name), content)
	}
}

// mainBuild returns a build file that fits the task version a run id falls
// in on the main lane.
func mainBuild(id string) string {
	switch TaskVersionFor(id, DefaultTaskV2From, DefaultTaskV3From) {
	case 1:
		return miniBuildV1
	case 2:
		return miniBuildV2
	}
	return miniBuildV3
}

func exitLine(word string, wall, code int) string {
	return "verdict=" + word + " kept=0 discarded=0 empty=0 wall=" + itoa(wall) + "s exit=" + itoa(code) + " arm=oracle"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func verdictFileLine(id, word string, wall int) string {
	return id + "  " + word + "  kept=0 discarded=0 empty=0 " + itoa(wall) + "s  arm=oracle"
}

func TestRemapVerdict(t *testing.T) {
	exit := func(code int, wall int64) *Exit {
		return &Exit{ExitCode: code, HasExitCode: true, WallSeconds: wall, HasWall: true}
	}
	const other = "20260901-000000-0001"
	cases := []struct {
		name     string
		id, word string
		exit     *Exit
		to       Verdict
		basis    string
		limit    int64
		note     string
	}{
		{"documented kill, exit 0", "20261011-144820-0432", "broken", exit(0, 5776), Aborted, BasisDocumented, 0, ""},
		{"reported kill, exit 143", "20260908-133809-0166", "broken", exit(143, 6975), Aborted, BasisReported, 0, ""},
		{"inferred kill", "20260910-152929-0190", "broken", exit(143, 18336), Aborted, BasisInferred, 0, ""},
		{"listed, other exit code", "20260910-152929-0190", "broken", exit(0, 18336), "", "", 0, "the exit file says 0"},
		{"listed, not failed", "20260910-152929-0190", "FIXED", exit(143, 18336), "", "", 0, `recorded "FIXED"`},
		{"listed, no exit file", "20260910-152929-0190", "broken", nil, "", "", 0, "no exit code"},
		{"listed, a listed run never falls through to the clock", "20260908-090521-0164", "broken", exit(1, 21600), "", "", 0, "the exit file says 1"},
		{"exit 143 is not a rule, only a list", other, "broken", exit(143, 100), "", "", 0, ""},
		{"clock, long cap", other, "broken", exit(1, 21602), Timeout, BasisInferred, 21600, ""},
		{"clock, short cap", other, "broken", exit(1, 5405), Timeout, BasisInferred, 5402, ""},
		{"clock, lower edge", other, "broken", exit(1, 5342), Timeout, BasisInferred, 5402, ""},
		{"clock, upper edge", other, "broken", exit(1, 21660), Timeout, BasisInferred, 21600, ""},
		{"near miss below", other, "broken", exit(1, 5341), "", "", 0, ""},
		{"near miss above", other, "broken", exit(1, 21661), "", "", 0, ""},
		{"at the cap with exit 0", other, "broken", exit(0, 5402), "", "", 0, ""},
		{"at the cap, passed", other, "FIXED", exit(1, 5402), "", "", 0, ""},
		{"at the cap, no exit code", other, "broken", &Exit{WallSeconds: 5402, HasWall: true}, "", "", 0, ""},
		{"exit 1, no wall time", other, "broken", &Exit{ExitCode: 1, HasExitCode: true}, "", "", 0, ""},
		{"no exit file", other, "broken", nil, "", "", 0, ""},
	}
	for _, c := range cases {
		r, note := remapVerdict(c.id, c.word, c.exit)
		if c.to == "" {
			if r != nil {
				t.Errorf("%s: re-mapped to %s", c.name, r.To)
			}
		} else if r == nil || r.From != Fail || r.To != c.to || r.Basis != c.basis || r.CapSeconds != c.limit || r.Cause == "" {
			t.Errorf("%s: %+v", c.name, r)
		}
		if (c.note == "") != (note == "") || !strings.Contains(note, c.note) {
			t.Errorf("%s: note %q, want %q", c.name, note, c.note)
		}
	}
}

func TestSecondPassMainLane(t *testing.T) {
	root := filepath.Join(t.TempDir(), "legacy")
	type run struct {
		dir, word  string
		wall, code int
	}
	runs := []run{
		// A: listed as invalid; one passed, one failed, one was aborted.
		{"20260911-141110-0207", "FIXED", 481, 0},
		{"20260911-151011-0209", "broken", 862, 0},
		{"20261011-132114-0431", "ABORTED", 2233, 143},
		// B.1: operator aborts recorded as failed.
		{"20261011-144820-0432", "broken", 5776, 0},
		{"20260908-133809-0166", "broken", 6975, 143},
		{"20260908-090521-0164", "broken", 14522, 143},
		// B.1, listed but the corpus disagrees: wrong exit code, wrong word.
		{"20260908-153544-0167", "broken", 14362, 0},
		{"20260909-121718-0178", "FIXED", 17160, 143},
		// B.2: the CLI's clock, found by rule.
		{"20260822-025042-0024", "broken", 21602, 1},
		{"20260910-231714-0197", "broken", 5402, 1},
		// B.2 near misses: a minute and a second early, exit 0, passed.
		{"20260910-100000-0181", "broken", 5341, 1},
		{"20260910-110000-0182", "broken", 5402, 0},
		{"20260910-120000-0183", "FIXED", 5402, 1},
		// An exit code 143 that is not listed stays a failure.
		{"20260905-120000-0100", "broken", 300, 143},
		// At the cap but voided by the operator: INVALID wins.
		{"20260907-082856-0149-VOID-nobudget", "broken", 21600, 1},
	}
	var lines []string
	for _, r := range runs {
		miniRun(t, root, r.dir, mainBuild(r.dir), exitLine(r.word, r.wall, r.code), nil)
		lines = append(lines, verdictFileLine(RunIDPattern.FindString(r.dir), r.word, r.wall))
	}
	// A: listed as invalid and never finished. 20260910-152929-0190 (B.1)
	// and the rest of table A are not in this corpus at all.
	miniRun(t, root, "20260911-152434-0210", miniBuildV1, "", nil)
	writeFile(t, filepath.Join(root, "verdicts-native.txt"), strings.Join(lines, "\n")+"\n")
	before := treeDigest(t, root)

	sink := NewMemorySink()
	rep := mustImport(t, Options{LegacyRoot: root, Sink: sink, Workers: 2})
	if treeDigest(t, root) != before {
		t.Fatal("the import changed the legacy tree")
	}
	if len(rep.Failed) != 0 || len(rep.Runs) != 16 {
		t.Fatalf("failed %v, %d runs", rep.Failed, len(rep.Runs))
	}

	type want struct {
		verdict, prior Verdict
		original       string
		reason         string // part of the verdict reason
		basis          string // of the re-mapping; "" when none
	}
	wants := map[string]want{
		"20260911-141110-0207": {Invalid, Pass, "FIXED", "partial states and operator kills", ""},
		"20260911-151011-0209": {Invalid, Fail, "broken", "partial states and operator kills", ""},
		"20260911-152434-0210": {Invalid, "", "", "partial states and operator kills", ""},
		"20261011-132114-0431": {Invalid, Aborted, "ABORTED", "unloaded the arm's model mid-run", ""},
		"20261011-144820-0432": {Aborted, Fail, "broken", "operator's kill to free the host", BasisDocumented},
		"20260908-133809-0166": {Aborted, Fail, "broken", "operator kill at 1h56m", BasisReported},
		"20260908-090521-0164": {Aborted, Fail, "broken", "exit code 143 on a run recorded as failed", BasisInferred},
		"20260908-153544-0167": {Fail, "", "broken", "", ""},
		"20260909-121718-0178": {Pass, "", "FIXED", "", ""},
		"20260822-025042-0024": {Timeout, Fail, "broken", "exit code 1 at the CLI's own time limit", BasisInferred},
		"20260910-231714-0197": {Timeout, Fail, "broken", "exit code 1 at the CLI's own time limit", BasisInferred},
		"20260910-100000-0181": {Fail, "", "broken", "", ""},
		"20260910-110000-0182": {Fail, "", "broken", "", ""},
		"20260910-120000-0183": {Pass, "", "FIXED", "", ""},
		"20260905-120000-0100": {Fail, "", "broken", "", ""},
		"20260907-082856-0149": {Invalid, Fail, "broken", "VOID", ""},
	}
	for id, w := range wants {
		r, ok := sink.Runs[id]
		if !ok {
			t.Errorf("%s: not imported", id)
			continue
		}
		if r.Verdict != w.verdict || r.PriorVerdict != w.prior || r.OriginalVerdict != w.original || !strings.Contains(r.VerdictReason, w.reason) || r.Lane != MainLane {
			t.Errorf("%s: verdict %s prior %q original %q reason %q lane %q", id, r.Verdict, r.PriorVerdict, r.OriginalVerdict, r.VerdictReason, r.Lane)
		}
		var remaps []Finding
		for _, f := range sink.Findings[id] {
			if f.Detector == RemapDetector {
				remaps = append(remaps, f)
			}
		}
		var prov struct {
			Remap  *Remap `json:"verdict_remap"`
			Legacy struct {
				Lane       string `json:"lane"`
				OriginalID string `json:"original_id"`
			} `json:"legacy"`
		}
		if err := json.Unmarshal(r.Provenance, &prov); err != nil {
			t.Fatal(err)
		}
		if prov.Legacy.Lane != MainLane || prov.Legacy.OriginalID != id {
			t.Errorf("%s: provenance %+v", id, prov.Legacy)
		}
		s := summary(t, rep, id)
		if w.basis == "" {
			if len(remaps) != 0 || prov.Remap != nil || s.Remap != nil {
				t.Errorf("%s: carries a re-mapping", id)
			}
			continue
		}
		if len(remaps) != 1 || prov.Remap == nil || s.Remap == nil {
			t.Errorf("%s: %d re-mapping findings, provenance %v", id, len(remaps), prov.Remap)
			continue
		}
		f := remaps[0]
		var ev map[string]any
		if err := json.Unmarshal(f.Evidence, &ev); err != nil {
			t.Fatal(err)
		}
		if f.RunID != id || f.DetectorVersion != ImporterVersion || f.Severity != "warning" ||
			ev["from"] != "FAIL" || ev["to"] != string(w.verdict) || ev["basis"] != w.basis || !strings.Contains(ev["cause"].(string), w.reason) {
			t.Errorf("%s: finding %+v %s", id, f, f.Evidence)
		}
		if w.verdict == Timeout && (ev["cap_seconds"] == nil || ev["exit_code"] != 1.0) {
			t.Errorf("%s: the cap is not in the evidence: %s", id, f.Evidence)
		}
	}
	if ev := string(sink.Findings["20260822-025042-0024"][len(sink.Findings["20260822-025042-0024"])-1].Evidence); !strings.Contains(ev, `"cap_seconds":21600`) || !strings.Contains(ev, `"wall_seconds":21602`) {
		t.Errorf("0024 evidence %s", ev)
	}
	if ev := string(sink.Findings["20260910-231714-0197"][len(sink.Findings["20260910-231714-0197"])-1].Evidence); !strings.Contains(ev, `"cap_seconds":5402`) {
		t.Errorf("0197 evidence %s", ev)
	}
	// A run marked INVALID with no verdict on record still names it missing.
	if r := sink.Runs["20260911-152434-0210"]; !has(r.Missing, "verdict") {
		t.Errorf("0210: %v", r.Missing)
	}

	if rep.InvalidByTable != 4 || rep.RemappedAborted != 3 || rep.RemappedTimeout != 2 || rep.InvalidStale != 0 {
		t.Errorf("counters: table %d, aborted %d, timeout %d, stale %d", rep.InvalidByTable, rep.RemappedAborted, rep.RemappedTimeout, rep.InvalidStale)
	}
	if got, want := rep.Verdicts[SetNative], (map[string]int{"INVALID": 5, "ABORTED": 3, "TIMEOUT": 2, "FAIL": 4, "PASS": 2}); !reflect.DeepEqual(got, want) {
		t.Errorf("mapped verdicts %v", got)
	}
	text := rep.String()
	for _, s := range []string{
		"second pass: invalid by table=4, invalid by stale lane script=0, re-mapped to ABORTED=3, re-mapped to TIMEOUT=2",
		"re-mapped 20260910-231714-0197: FAIL -> TIMEOUT (inferred) exit=1 wall=5402s cap=5402s",
		"re-mapped 20261011-144820-0432: FAIL -> ABORTED (documented) exit=0 wall=5776s",
	} {
		if !strings.Contains(text, s) {
			t.Errorf("report lacks %q:\n%s", s, text)
		}
	}

	// Listed runs that are absent or do not match are reported, not fatal.
	incons := strings.Join(rep.Inconsistencies, "\n")
	for _, s := range []string{
		"runs-native/20260908-153544-0167: listed as an operator abort with exit code 143, but the exit file says 0; not re-mapped",
		`runs-native/20260909-121718-0178: listed as an operator abort recorded as failed, but the old harness recorded "FIXED"; not re-mapped`,
		"run 20260910-152929-0190 is listed as an operator abort recorded as failed but is not in the corpus",
		"run 20260913-214530-0296 is listed as invalid by the old harness's records but is not in the corpus",
	} {
		if !strings.Contains(incons, s) {
			t.Errorf("no inconsistency %q in:\n%s", s, incons)
		}
	}
	if len(rep.Inconsistencies) != 2+1+4 {
		t.Errorf("%d inconsistencies:\n%s", len(rep.Inconsistencies), incons)
	}

	// The verdict file is still reproduced on the original words.
	ver, err := VerifyAgainstLegacy(rep, root)
	if err != nil {
		t.Fatal(err)
	}
	if !ver.OK() || !reflect.DeepEqual(ver.Files[0].Expected, map[string]int{"FIXED": 3, "broken": 11, "ABORTED": 1}) || !reflect.DeepEqual(ver.Files[0].Expected, ver.Files[0].Imported) {
		t.Fatalf("verification:\n%s", ver)
	}
	// A re-mapped run whose prior verdict is lost no longer verifies.
	for i := range rep.Runs {
		if rep.Runs[i].ID == "20260822-025042-0024" {
			rep.Runs[i].PriorVerdict = Pass
		}
	}
	if ver, _ := VerifyAgainstLegacy(rep, root); ver.OK() {
		t.Fatal("a re-mapped run with the wrong prior verdict verified")
	}
}

type laneFixture struct {
	root          string
	second, third Lane
	sameID        string // an original id that is in the main lane and in the second
}

// newLaneFixture writes a main lane of one run and two sibling lanes: one
// whose lane script was replaced, with a verdict file, and one with neither.
func newLaneFixture(t *testing.T) laneFixture {
	t.Helper()
	dir := t.TempDir()
	fx := laneFixture{
		root:   filepath.Join(dir, "legacy"),
		second: Lane{Name: "second", Root: filepath.Join(dir, "legacy-second"), StateArchive: filepath.Join(dir, "archive-second")},
		third:  Lane{Name: "third", Root: filepath.Join(dir, "legacy-third")},
		sameID: "20260912-103247-0030",
	}
	oracleOld := map[string]string{"oracle.txt": `{"ok":true,"error":null,"frames_run":30,"started":false}` + "\n"}
	oracleNew := map[string]string{"oracle.txt": `{"ok":true,"error":null,"frames_run":400,"state":"PLAYING","reached_playing":true}` + "\n"}

	miniRun(t, fx.root, fx.sameID, miniBuildV2, exitLine("FIXED", 700, 0), nil)
	writeFile(t, filepath.Join(fx.root, "verdicts-native.txt"), verdictFileLine(fx.sameID, "FIXED", 700)+"\n")

	// The second lane: a backup of the replaced lane script, runs on both
	// sides of the replacement, run numbers that restart.
	writeFile(t, filepath.Join(fx.second.Root, "backups-20260911-185634", "native.sh.pre-20260911"), "#!/bin/bash\n")
	miniRun(t, fx.second.Root, "20260907-152441-0001-VOID-oldquant", miniBuildV1, "", nil)
	miniRun(t, fx.second.Root, "20260907-160645-0001", miniBuildV1, exitLine("broken", 13186, 143), oracleOld)
	miniRun(t, fx.second.Root, "20260911-190459-0017", miniBuildV1, exitLine("FIXED", 1153, 0), oracleOld)
	miniRun(t, fx.second.Root, "20260911-190505-0018", miniBuildV2, exitLine("FIXED", 535, 0), oracleNew)
	// At the CLI's clock with exit code 1: re-mapped in the main lane only.
	miniRun(t, fx.second.Root, fx.sameID, miniBuildV2, exitLine("broken", 5402, 1), nil)
	writeFile(t, filepath.Join(fx.second.Root, "verdicts-native.txt"), strings.Join([]string{
		verdictFileLine("20260907-160645-0001", "broken", 13186),
		verdictFileLine("20260911-190459-0017", "FIXED", 1153),
		verdictFileLine("20260911-190505-0018", "FIXED", 535),
		verdictFileLine(fx.sameID, "broken", 5402),
		"",
	}, "\n"))
	tarBytes := stateTar(t, map[string]string{"state/settings/providers.json": providersJSON})
	writeFile(t, filepath.Join(fx.second.Root, "state-archive", "20260911-190505-0018-state.tar.gz"), gz(t, string(tarBytes)))
	writeFile(t, filepath.Join(fx.second.StateArchive, fx.sameID+".tar.zst"), zst(t, tarBytes))

	// The third lane: no verdict file, no replaced script, manual arms.
	miniRun(t, fx.third.Root, "20260907-100000-0001", miniBuildManual, exitLine("FIXED", 1313, 0), oracleNew)
	miniRun(t, fx.third.Root, "20260919-100000-0002", miniBuildManual, exitLine("ABORTED", 443, 143), nil)
	miniRun(t, fx.third.Root, "20260919-110000-0003", miniBuildManual, exitLine("FIXED", 900, 0), oracleOld)
	miniRun(t, fx.third.Root, "20260919-120000-0004", miniBuildManual, exitLine("FIXED", 900, 0),
		map[string]string{"oracle.txt": `{"ok":true,"frames_run":400,"reached_playing":true,"game_logic":null}` + "\n"})
	writeFile(t, filepath.Join(fx.third.Root, "results-native.jsonl"), "{}\n")
	return fx
}

func TestSiblingLanes(t *testing.T) {
	fx := newLaneFixture(t)
	lanes := []Lane{fx.second, fx.third}
	var before []string
	for _, dir := range []string{fx.root, fx.second.Root, fx.second.StateArchive, fx.third.Root} {
		before = append(before, treeDigest(t, dir))
	}
	sink := NewMemorySink()
	rep := mustImport(t, Options{LegacyRoot: fx.root, Lanes: lanes, Sink: sink, Workers: 3})
	for i, dir := range []string{fx.root, fx.second.Root, fx.second.StateArchive, fx.third.Root} {
		if treeDigest(t, dir) != before[i] {
			t.Fatalf("the import changed %s", dir)
		}
	}
	if len(rep.Failed) != 0 || len(sink.Runs) != 10 || !reflect.DeepEqual(rep.Lanes, []string{MainLane, "second", "third"}) {
		t.Fatalf("failed %v, %d runs, lanes %v", rep.Failed, len(sink.Runs), rep.Lanes)
	}

	// The main lane keeps its ids; the same original id in the second lane
	// is another run.
	main, twin := sink.Runs[fx.sameID], sink.Runs["second-"+fx.sameID]
	if main.Lane != MainLane || main.Verdict != Pass || main.TaskVersion != 2 {
		t.Errorf("main lane run: %+v", main)
	}
	if twin.ID != LaneRunID("second", fx.sameID) || twin.Lane != "second" || twin.OriginalVerdict != "broken" {
		t.Errorf("second lane run: %+v", twin)
	}
	// Rule B is the main lane's: no re-mapping here.
	if twin.Verdict != Fail || twin.PriorVerdict != "" || len(sink.Findings[twin.ID]) != 0 {
		t.Errorf("second lane run re-mapped: %s %v", twin.Verdict, sink.Findings[twin.ID])
	}
	if has(twin.Missing, "session_state") {
		t.Errorf("the lane's external state archive was not found: %v", twin.Missing)
	}
	for id, r := range sink.Runs {
		var prov struct {
			Legacy struct {
				Lane       string `json:"lane"`
				OriginalID string `json:"original_id"`
				Dir        string `json:"dir"`
			} `json:"legacy"`
			Reason string `json:"task_version_reason"`
		}
		if err := json.Unmarshal(r.Provenance, &prov); err != nil {
			t.Fatal(err)
		}
		s := summary(t, rep, id)
		if r.Lane == "" || prov.Legacy.Lane != r.Lane || s.Lane != r.Lane || LaneRunID(r.Lane, prov.Legacy.OriginalID) != id || s.OriginalID != prov.Legacy.OriginalID ||
			!runIDExact.MatchString(prov.Legacy.OriginalID) || !strings.Contains(prov.Legacy.Dir, prov.Legacy.OriginalID) || prov.Reason == "" {
			t.Errorf("%s: lane %q, provenance %+v", id, r.Lane, prov)
		}
		for _, ref := range sink.Refs[id] {
			if ref.RunID != id {
				t.Errorf("%s: blob ref of run %s", id, ref.RunID)
			}
		}
	}

	// The stale lane script: every run before the cut-off, none after.
	type want struct {
		verdict, prior Verdict
		version        int
	}
	for id, w := range map[string]want{
		"second-20260907-152441-0001": {Invalid, "", 1},
		"second-20260907-160645-0001": {Invalid, Fail, 1},
		"second-20260911-190459-0017": {Invalid, Pass, 1},
		"second-20260911-190505-0018": {Pass, "", 2},
		// The third lane has runs before the cut-off and no replaced
		// script: the rule is not the third lane's.
		"third-20260907-100000-0001": {Pass, "", 2},
		"third-20260919-100000-0002": {Aborted, "", 0},
		"third-20260919-110000-0003": {Pass, "", 1},
		"third-20260919-120000-0004": {Pass, "", 0},
	} {
		r, ok := sink.Runs[id]
		if !ok {
			t.Errorf("%s: not imported", id)
			continue
		}
		if r.Verdict != w.verdict || r.PriorVerdict != w.prior || r.TaskVersion != w.version {
			t.Errorf("%s: verdict %s prior %q version %d", id, r.Verdict, r.PriorVerdict, r.TaskVersion)
		}
		stale := strings.Contains(r.VerdictReason, "stale lane script (old oracle and limits) until 2026-09-11 19:05")
		if stale != (w.verdict == Invalid) || summary(t, rep, id).StaleLaneScript != stale {
			t.Errorf("%s: reason %q", id, r.VerdictReason)
		}
		// A task version that cannot be established is named, not guessed.
		if has(r.Missing, "task_version") != (w.version == 0) {
			t.Errorf("%s: version %d, missing %v", id, w.version, r.Missing)
		}
		oracle := 0
		for _, f := range sink.Findings[id] {
			if f.Detector == "legacy_oracle" {
				oracle++
			}
		}
		if (oracle == 1) != (w.version == 1) {
			t.Errorf("%s: %d findings about the first oracle in task version %d", id, oracle, w.version)
		}
	}
	if r := sink.Runs["second-20260907-152441-0001"]; !strings.Contains(r.VerdictReason, "VOID-oldquant") || !has(r.Missing, "verdict") {
		t.Errorf("voided and stale: %q %v", r.VerdictReason, r.Missing)
	}
	if has(sink.Runs["second-20260911-190505-0018"].Missing, "session_state") {
		t.Errorf("the state archive inside the lane was not found")
	}

	// Counts are per lane; the main lane's keys are unchanged.
	wantSets := map[string]Counts{
		SetNative:             {Dirs: 1, Finished: 1, Imported: 1},
		"second/" + SetNative: {Dirs: 5, Finished: 4, Imported: 5},
		"third/" + SetNative:  {Dirs: 4, Finished: 4, Imported: 4},
	}
	for k, want := range wantSets {
		if got := rep.Sets[k]; got == nil || *got != want {
			t.Errorf("set %s: %+v, want %+v", k, got, want)
		}
	}
	if len(rep.Sets) != 3 || !reflect.DeepEqual(rep.Verdicts["second/"+SetNative], map[string]int{"INVALID": 3, "PASS": 1, "FAIL": 1}) ||
		!reflect.DeepEqual(rep.OriginalVerdicts["second/"+SetNative], map[string]int{"": 1, "FIXED": 2, "broken": 2}) {
		t.Errorf("sets %v, verdicts %v, original %v", rep.Sets, rep.Verdicts, rep.OriginalVerdicts)
	}
	if rep.InvalidStale != 3 || rep.InvalidByTable != 0 || rep.RemappedTimeout != 0 || rep.UnknownFields["task_version"] != 2 {
		t.Errorf("stale %d, by table %d, timeouts %d, unknown %v", rep.InvalidStale, rep.InvalidByTable, rep.RemappedTimeout, rep.UnknownFields)
	}
	var stale []Boundary
	for _, b := range rep.Boundaries {
		if b.Kind == "stale_lane_script" {
			stale = append(stale, b)
		}
	}
	if len(stale) != 1 || stale[0].From != "20260911-190500" || stale[0].Source != "second:backups-20260911-185634/native.sh.pre-20260911" {
		t.Errorf("stale boundaries %+v", stale)
	}
	text := rep.String()
	for _, s := range []string{"lanes: main second third", "second/native/v1=3", "third/native/v0=2", "invalid by stale lane script=3"} {
		if !strings.Contains(text, s) {
			t.Errorf("report lacks %q:\n%s", s, text)
		}
	}

	// Each lane with a verdict file is verified; one without is named.
	ver, err := VerifyAgainstLegacy(rep, fx.root, lanes...)
	if err != nil {
		t.Fatal(err)
	}
	if !ver.OK() || len(ver.Files) != 2 || !reflect.DeepEqual(ver.NoVerdictFile, []string{"third"}) {
		t.Fatalf("verification:\n%s", ver)
	}
	if f := ver.Files[1]; f.File != "second:verdicts-native.txt" || f.Lane != "second" || f.Lines != 4 ||
		!reflect.DeepEqual(f.Expected, map[string]int{"FIXED": 2, "broken": 2}) || !reflect.DeepEqual(f.Expected, f.Imported) {
		t.Errorf("second lane check %+v", f)
	}
	if !strings.Contains(ver.String(), "lane third: no verdict file") {
		t.Errorf("verification text:\n%s", ver)
	}
	// A lane's verdict file is matched to the lane's runs, not to the main
	// lane's run of the same original id.
	alone := mustImport(t, Options{LegacyRoot: fx.root, DryRun: true})
	ver, err = VerifyAgainstLegacy(alone, fx.root, lanes...)
	if err != nil {
		t.Fatal(err)
	}
	if ver.OK() || !strings.Contains(strings.Join(ver.Differences, "\n"), "second:verdicts-native.txt: run "+fx.sameID+" is not in the import") {
		t.Fatalf("a lane that was not imported verified:\n%s", ver)
	}

	// A second pass skips every lane's runs.
	rep2 := mustImport(t, Options{LegacyRoot: fx.root, Lanes: lanes, Sink: sink})
	if len(rep2.Skipped) != 10 {
		t.Fatalf("second pass skipped %d", len(rep2.Skipped))
	}
	// One lane's run can be asked for by its imported id.
	one := mustImport(t, Options{LegacyRoot: fx.root, Lanes: lanes, DryRun: true, RunIDs: []string{"second-" + fx.sameID}})
	if len(one.Runs) != 1 || one.Runs[0].Lane != "second" {
		t.Fatalf("filter by id: %+v", one.Runs)
	}
}

func TestLaneNames(t *testing.T) {
	fx := newLaneFixture(t)
	for _, lanes := range [][]Lane{
		{{Name: MainLane, Root: fx.second.Root}},
		{{Name: "", Root: fx.second.Root}},
		{{Name: "a/b", Root: fx.second.Root}},
		{{Name: "a-b", Root: fx.second.Root}},
		{{Name: "second", Root: ""}},
		{fx.second, {Name: "second", Root: fx.third.Root}},
		{{Name: "gone", Root: filepath.Join(fx.root, "missing")}},
	} {
		if _, err := Import(context.Background(), Options{LegacyRoot: fx.root, Lanes: lanes, DryRun: true}); err == nil {
			t.Errorf("lanes %+v were accepted", lanes)
		}
	}
	if LaneRunID(MainLane, "x") != "x" || LaneRunID("", "x") != "x" || LaneRunID("second", "x") != "second-x" {
		t.Error("LaneRunID")
	}
}
