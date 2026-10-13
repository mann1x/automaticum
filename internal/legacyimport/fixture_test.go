package legacyimport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Everything below is synthetic: it mimics the formats of the old harness
// and copies none of its data.

const fixtureEndpoint = "http://endpoint.invalid:11434"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gz(t *testing.T, content string) string {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func stateTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	_ = tw.WriteHeader(&tar.Header{Name: "state/", Typeflag: tar.TypeDir, Mode: 0o755})
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(files[n]))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zst(t *testing.T, b []byte) string {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	return string(enc.EncodeAll(b, nil))
}

const providersJSON = `{
  "version": 1,
  "lastUsedProvider": "ollama",
  "providers": {
    "ollama": {
      "settings": {
        "provider": "ollama",
        "apiKey": "not-a-secret",
        "model": "model-b:q4",
        "contextWindow": 96000,
        "maxToolResultChars": 64000,
        "baseUrl": "` + fixtureEndpoint + `",
        "sampling": {"temperature": 0.8, "repeatPenalty": 1.05}
      }
    }
  }
}`

// stream builds a small event stream in the CLI's wrapped format.
func stream(model string, withResult bool) string {
	var b strings.Builder
	ts := 0
	line := func(s string) {
		ts++
		fmt.Fprintf(&b, `{"ts":"2026-08-17T05:42:%02d.000Z",%s}`+"\n", ts, s)
	}
	ev := func(s string) { line(`"type":"agent_event","event":{` + s + `}`) }
	line(`"type":"hook_event","hookEventName":"agent_start","agentId":"agent_1","parentAgentId":null`)
	ev(`"type":"iteration_start","iteration":1`)
	ev(`"type":"content_start","contentType":"reasoning","reasoning":"The","redacted":false`)
	ev(`"type":"content_start","contentType":"reasoning","reasoning":" file","redacted":false`)
	ev(`"type":"content_end","contentType":"reasoning","reasoning":"The file"`)
	ev(`"type":"content_start","contentType":"tool","toolName":"run_commands","toolCallId":"call_1","input":{"commands":["ls"]}`)
	line(`"type":"hook_event","hookEventName":"tool_call","agentId":"agent_1"`)
	ev(`"type":"content_update","contentType":"tool","toolName":"run_commands","toolCallId":"call_1","update":{"chunk":"x"}`)
	ev(`"type":"content_end","contentType":"tool","toolName":"run_commands","toolCallId":"call_1","output":[{"query":"ls","result":"a","success":true}],"durationMs":12`)
	line(`"type":"hook_event","hookEventName":"tool_result","agentId":"agent_1"`)
	ev(`"type":"usage","inputTokens":100,"outputTokens":10,"totalInputTokens":100,"totalOutputTokens":10`)
	ev(`"type":"iteration_end","iteration":1,"hadToolCalls":true,"toolCallCount":1`)
	ev(`"type":"iteration_start","iteration":2`)
	ev(`"type":"content_start","contentType":"tool","toolName":"editor","toolCallId":"call_2","input":{}`)
	ev(`"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"call_2","output":{"query":"edit"},"error":"{\"error\":\"Missing required argument ` + "`path`" + `. Send it and call again.\"}","durationMs":3`)
	ev(`"type":"content_start","contentType":"tool","toolName":"editor","toolCallId":"call_3","input":{}`)
	ev(`"type":"content_end","contentType":"tool","toolName":"editor","toolCallId":"call_3","output":{"query":"edit","result":"","error":"Editor operation failed"},"durationMs":4`)
	ev(`"type":"content_start","contentType":"text","text":"Done"`)
	ev(`"type":"content_end","contentType":"text","text":"Done."`)
	ev(`"type":"notice","noticeType":"status","message":"auto-compacting"`)
	ev(`"type":"error","error":"transient"`)
	ev(`"type":"usage","inputTokens":200,"outputTokens":20,"totalInputTokens":300,"totalOutputTokens":30`)
	ev(`"type":"iteration_end","iteration":2,"hadToolCalls":true,"toolCallCount":2`)
	if withResult {
		ev(`"type":"done","reason":"completed","text":"Done.","iterations":2`)
		line(`"type":"hook_event","hookEventName":"agent_end","agentId":"agent_1"`)
		line(`"type":"run_result","finishReason":"completed","iterations":2,"usage":{"inputTokens":300,"outputTokens":30},"aggregateUsage":{"inputTokens":300,"outputTokens":30},"durationMs":20000,"text":"Done.","model":{"id":"` + model + `","provider":"ollama","info":{"contextWindow":131072}}`)
	}
	return b.String()
}

type fixture struct {
	root    string
	archive string
}

// newFixture writes a small legacy root with every kind of run the importer
// has to handle.
func newFixture(t *testing.T) fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "legacy")
	archive := filepath.Join(t.TempDir(), "archive")
	run := func(set, dir string, files map[string]string) {
		for name, content := range files {
			writeFile(t, filepath.Join(root, set, dir, name), content)
		}
	}
	prompt := "check the page, it is not working.\n"
	oracleV1 := `{"ok":true,"error":null,"frames_run":30,"started":false}` + "\n"
	oracleV2Bad := `{"ok":false,"error":"SyntaxError: Unexpected token","frames_run":0,"started":false,"state":null,"player_frames":0,"reached_playing":false}` + "\n"
	oracleV3 := `{"ok":true,"error":null,"frames_run":400,"state":"PLAYING","reached_playing":true,"game_logic":{"ok":false,"passed":21,"total":23,"failed":[{"trap":"jump"},{"trap":"gem"}]}}` + "\n"
	expectV2 := `"ok":\s*true.*"frames_run":\s*[1-9][0-9]*.*"reached_playing":\s*true`

	// Task version 1: oldest build file, compressed stream, archived state.
	run("runs-native", "20260817-074236-0001", map[string]string{
		"build.txt":             "version=4.100.40\nsha=aaaaaaaaa\ndirty=0\nmodel=model-a:Q4\nmode=native max_changes=3 max_tx=6 run_timeout=21600s\noracle=node run_game.js page.html expect=\"ok\":\\s*true\n",
		"exit.txt":              "verdict=FIXED kept=1 discarded=0 wall=848s exit=0\n",
		"prompt.txt":            prompt,
		"oracle.txt":            oracleV1,
		"transactions.txt":      "TX-01 kept — the check passed.\nkept=1 discarded=0\n",
		"run.jsonl.gz":          gz(t, stream("model-a:Q4", true)),
		"run.stderr":            "",
		"ollama.log":            "server line\n",
		"page.before.html":      "<html>before</html>\n",
		"page.after.html":       "<html>after</html>\n",
		"tmp-scratch.tar.gz":    gz(t, "not really a tar"),
		"tmp/cline-tools.jsonl": "{}\n",
	})
	// Voided by the operator: directory renamed, verdict line commented.
	run("runs-native", "20260907-114615-0151-VOID-badquant", map[string]string{
		"build.txt":        "version=4.100.71\nsha=bbbbbbbbb\ndirty=0\nmodel=model-b:q4\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=3 max_tx=6 run_timeout=21600s\narm=proposed atomic=always propose_check=yes\n",
		"exit.txt":         "verdict=broken kept=0 discarded=0 empty=0 wall=4186s exit=1 arm=proposed proposals=1 adopted=0 run_checks=0\n",
		"VOID-stopped.txt": "stopped by the operator; the verdict line is a kill artefact\n",
		"prompt.txt":       prompt,
		"run.jsonl.gz":     gz(t, stream("model-b:q4", false)),
	})
	// Task version 2, first run; a stream that was cut.
	run("runs-native", "20260911-155111-0213", map[string]string{
		"build.txt":    "version=4.100.97\nsha=ccccccccc\ndirty=47\nmodel=model-b:q4\nendpoint=http://localhost:11434 (default)\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=oracle atomic=auto propose_check=n/a reconsider_after=0\noracle=node run_game.js page.html expect=" + expectV2 + "\noracle_sha=0123456789abcdef\n",
		"exit.txt":     "verdict=broken kept=0 discarded=0 empty=0 wall=2688s exit=0 arm=oracle proposals=0 adopted=0 run_checks=49 rethink_offered=0 broken_rejected=0\n",
		"prompt.txt":   prompt,
		"oracle.txt":   oracleV2Bad,
		"checks.txt":   "proposals=0 adopted=0 run_checks=49 rethink_offered=0 broken_rejected=0\n",
		"run.jsonl.gz": gz(t, stream("model-b:q4", false)+`{"ts":"2026-09-11T14:35:59.382Z","type":"run_aborted","reason":"stopped","message":"loop guard stopped the run"}`+"\n"),
	})
	// Listed in the harness's notes as invalid.
	run("runs-native", "20260912-150953-0267", map[string]string{
		"build.txt":  "version=4.100.98\nsha=ddddddddd\ndirty=0\nmodel=model-c:q6\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=oracle atomic=auto propose_check=n/a reconsider_after=0\noracle=node run_game.js page.html expect=" + expectV2 + "\n",
		"exit.txt":   "verdict=broken kept=0 discarded=0 empty=0 wall=4201s exit=0 arm=oracle proposals=0 adopted=0 run_checks=3 rethink_offered=0 broken_rejected=0\n",
		"prompt.txt": prompt,
		"oracle.txt": oracleV2Bad,
	})
	// Never finished: no exit.txt.
	run("runs-native", "20260913-214530-0296", map[string]string{
		"build.txt":  "version=4.100.98\nsha=ddddddddd\ndirty=0\nmodel=model-c:q6\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=oracle atomic=auto propose_check=n/a reconsider_after=0\noracle=node run_game.js page.html expect=" + expectV2 + "\n",
		"prompt.txt": prompt,
		"run.jsonl":  "",
	})
	// Task version 3, raw stream, state still on disk, everything appended.
	v3build := "version=4.100.220\nsha=eeeeeeeee\ndirty=37\nmodel=model-b:q4\nsource=some/dir/hard.html sha=5b6559abbb62\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=manual atomic=off propose_check=n/a reconsider_after=0\nmanual_prompt=new\ntail_arm=n/a\noracle=none expect=\noracle_version=v3 pinned=f13e481f2578\nexpert=none expert_num_ctx=default max_escalations=default max_follow_ups=default close_after=no\noracle_sha=fedcba9876543210\n"
	run("runs-native", "20260930-131610-0411", map[string]string{
		"build.txt":                     v3build,
		"exit.txt":                      "verdict=FIXED kept=0 discarded=0 empty=0 wall=1125s exit=0 arm=manual proposals=0 adopted=0 run_checks=0 rethink_offered=0 broken_rejected=0 oracle_runs=7 commands=7 check_files=4 escalate_calls=0 started=0 expert_configured=no oracle=v3 loads=yes logic=21/23 failed=jump,gem\n",
		"prompt.txt":                    prompt,
		"oracle.txt":                    oracleV3,
		"logic.txt":                     "loads=yes logic=21/23 failed=jump,gem\n",
		"checks.txt":                    "proposals=0 adopted=0 run_checks=0 rethink_offered=0 broken_rejected=0 oracle_runs=7 commands=7 check_files=4\n",
		"escalations.txt":               "escalate_calls=0 started=0 expert_replies=0\n",
		"transactions.txt":              "kept=0 discarded=0 empty=0\n",
		"run.jsonl":                     stream("model-b:q4", true),
		"server.log.gz":                 gz(t, "server log\n"),
		"compactions/001-1.json":        "{}\n",
		"state/settings/providers.json": providersJSON,
		"state/sessions/s1/s1.json":     "{}\n",
		"state/logs/cline.log":          "log\n",
	})
	run("runs-native", "20261011-112103-0430", map[string]string{
		"build.txt":        v3build,
		"exit.txt":         "verdict=TIMEOUT kept=0 discarded=0 empty=0 wall=7203s exit=0 arm=manual proposals=0 adopted=0 run_checks=0 reason=\"clock: 7203s vs 7200s cap, exit=0\" oracle=v3 loads=yes logic=13/23\n",
		"abort-reason.txt": "clock: 7203s vs 7200s cap, exit=0\n",
		"prompt.txt":       prompt,
		"oracle.txt":       oracleV3,
		"run.jsonl":        stream("model-b:q4", true),
	})
	run("runs-native", "20261011-132114-0431", map[string]string{
		"build.txt":        v3build,
		"exit.txt":         "verdict=ABORTED kept=0 discarded=0 empty=0 wall=2233s exit=143 arm=manual proposals=0 adopted=0 run_checks=0 reason=\"exit=143 (SIGTERM)\" oracle=v3 loads=no logic=none\n",
		"abort-reason.txt": "exit=143 (SIGTERM)\n",
		"prompt.txt":       prompt,
		// A compressed stream that is not valid gzip must not lose the run.
		"run.jsonl.gz": "this is not gzip",
	})
	// A bad run: the exit line cannot be read.
	run("runs-native", "20261012-000000-0999", map[string]string{
		"build.txt": v3build,
		"exit.txt":  "garbage without a verdict\n",
	})
	writeFile(t, filepath.Join(root, "runs-native", "stray-file.txt"), "not a run\n")

	// The atomic loop: one stream per transaction.
	atomicBuild := "version=4.100.40\nsha=fffffffff\ndirty=0\nmodel=model-a:Q4\nmode=atomic max_changes=3 max_tx=6 tx_timeout=7200s first_tx_timeout=7200s\nedit_verification=<host default>\n"
	run("runs-atomic", "20260814-211259-0001", map[string]string{
		"build.txt":                           atomicBuild,
		"exit.txt":                            "verdict=FIXED transactions=2 wall=21421s\n",
		"TX-01.prompt.txt":                    prompt,
		"TX-01.jsonl.gz":                      gz(t, stream("model-a:Q4", false)),
		"TX-01.oracle.txt":                    `{"ok":false,"error":"SyntaxError","frames_run":0,"started":false}` + "\n",
		"TX-01.diff.txt":                      "--- a\n+++ b\n",
		"TX-01.diffstat.txt":                  "page.html modified\n",
		"TX-01.stderr":                        "",
		"TX-02.prompt.txt":                    prompt,
		"TX-02.jsonl":                         stream("model-a:Q4", true),
		"TX-02.oracle.txt":                    oracleV1,
		"TX-02.diffstat.txt":                  "page.html modified\n",
		"state-TX-01/settings/providers.json": providersJSON,
		".tx-base/page.html":                  "<html>before</html>\n",
		".tx-base.manifest":                   "0000  ./page.html\n",
		"page.before.html":                    "<html>before</html>\n",
	})
	run("runs-atomic", "20260815-160854-0002", map[string]string{
		"build.txt":        atomicBuild,
		"exit.txt":         "verdict=ROLLBACK-FAILED transactions=1 wall=500s\n",
		"TX-01.prompt.txt": prompt,
		"TX-01.jsonl":      stream("model-a:Q4", false),
	})
	// The teams loop.
	run("runs-teams", "20260819-175024-0001", map[string]string{
		"build.txt":        "version=4.100.53\nsha=999999999\ndirty=0\nmodel=model-a:Q4\nagents_model=helper:cloud agents_num_ctx=model default parallel_sessions=3\nagents=auditor.md implementer.md \nendpoint=" + fixtureEndpoint + "\nmode=teams max_changes=3 max_tx=6 run_timeout=21600s\noracle=node run_game.js page.html expect=\"ok\":\\s*true\n",
		"exit.txt":         "verdict=broken kept=0 discarded=0 delegations=3 wall=4707s exit=0\n",
		"delegation.txt":   "agent auditor: 1\nagent implementer: 2\ndelegations=3\n",
		"transactions.txt": "kept=0 discarded=0\n",
		"prompt.txt":       prompt,
		"oracle.txt":       `{"ok":false,"error":"x","frames_run":0,"started":false}` + "\n",
		"run.jsonl":        stream("model-a:Q4", true),
	})
	// Discarded and abandoned runs.
	run("runs-discarded", "20260818-071005-0008", map[string]string{
		"build.txt":  "version=4.100.52\nsha=888888888\ndirty=1\nmodel=model-d:27b\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=3 max_tx=6 run_timeout=21600s\n",
		"exit.txt":   "verdict=broken kept=0 discarded=0 wall=254s exit=1\n",
		"prompt.txt": prompt,
		"run.jsonl":  stream("model-d:27b", false),
	})
	run("runs-abandoned-20260912-1031", "20260912-090403-0257", map[string]string{
		"build.txt":  "version=4.100.98\nsha=777777777\ndirty=0\nmodel=model-e:27b\nendpoint=" + fixtureEndpoint + "\nmode=native max_changes=6 max_tx=6 run_timeout=7200s\narm=oracle atomic=auto propose_check=n/a reconsider_after=0\noracle=node run_game.js page.html expect=" + expectV2 + "\n",
		"exit.txt":   "verdict=FIXED kept=1 discarded=0 empty=0 wall=392s exit=0 arm=oracle proposals=0 adopted=0 run_checks=1\n",
		"prompt.txt": prompt,
		"run.jsonl":  stream("model-e:27b", true),
	})

	writeFile(t, filepath.Join(root, "verdicts-native.txt"), strings.Join([]string{
		"20260817-074236-0001  FIXED  kept=1 discarded=0 848s",
		"20260907-114615-0151  broken  kept=0 discarded=0 empty=0 4186s  arm=proposed",
		"# 20260907-114615-0151 VOID — stopped by operator; the line above is a kill artefact",
		"20260911-155111-0213  broken  kept=0 discarded=0 empty=0 2688s  arm=oracle",
		"20260912-090403-0257  FIXED  kept=1 discarded=0 empty=0 392s  arm=oracle",
		"20260912-150953-0267  broken  kept=0 discarded=0 empty=0 4201s  arm=oracle",
		"20260930-131610-0411  FIXED  kept=0 discarded=0 empty=0 1125s  arm=manual prompt=new tail=n/a oracle=v3 loads=yes logic=21/23 failed=jump,gem",
		"20261011-112103-0430  TIMEOUT  kept=0 discarded=0 empty=0 7203s  arm=manual prompt=new tail=n/a oracle=v3 loads=yes logic=13/23",
		"20261011-132114-0431  ABORTED  kept=0 discarded=0 empty=0 2233s  arm=manual prompt=new tail=n/a oracle=v3 loads=no logic=none",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "verdicts-atomic.txt"),
		"20260814-211259-0001  FIXED  transactions=2 21421s\n20260815-160854-0002  ROLLBACK-FAILED  transactions=1 500s\n")
	writeFile(t, filepath.Join(root, "verdicts-teams.txt"),
		"20260819-175024-0001  broken  kept=0 discarded=0 delegations=3 4707s\n")
	writeFile(t, filepath.Join(root, "INVALID-RUNS.md"), "# Runs that must not be pooled\n\n## 0267 — broken tag\n\n```\n20260912-150953-0267  broken  4201s\n```\n\nDriver: a script.\n\n**Why invalid.** The tag swallowed its template,\nso the arm ran without its renderer.\n\n**Disposition.** Deleted.\n")
	writeFile(t, filepath.Join(root, "changes-20260911", "CHANGES.md"), "# Harness changes\n\nEverything below changed at once; the\n**boundary run is `20260911-155111-0213`** — the first with all of it.\n")
	writeFile(t, filepath.Join(root, "changes-20261011", "CHANGES.md"), "# A day of changes that names no run\n")

	tarBytes := stateTar(t, map[string]string{
		"state/settings/providers.json": providersJSON,
		"state/sessions/s9/s9.json":     "{}",
		"state/sessions/s9/s9.messages": "[]",
		"state/logs/cline.log":          "log line\n",
	})
	writeFile(t, filepath.Join(archive, "state", "20260817-074236-0001.tar.zst"), zst(t, tarBytes))
	writeFile(t, filepath.Join(archive, "state", "20200101-000000-0001.tar.zst"), zst(t, tarBytes)) // matches no run
	writeFile(t, filepath.Join(root, "state-archive", "20260912-150953-0267-state.tar.gz"), gz(t, string(tarBytes)))
	return fixture{root: root, archive: archive}
}

// treeDigest fingerprints a directory tree: every path with its type, size,
// mode, modification time and content hash. Two equal digests mean nothing
// under the tree was created, removed or changed.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		fmt.Fprintf(h, "%s|%v|%d|%d", rel, fi.Mode(), fi.ModTime().UnixNano(), fi.Size())
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			h.Write(sum[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
