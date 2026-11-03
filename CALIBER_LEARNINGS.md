# Caliber Learnings

Accumulated patterns and anti-patterns from development sessions.
Auto-managed by [caliber](https://github.com/caliber-ai-org/ai-setup) — do not edit manually.

- **[gotcha]** A new detector in `internal/detect` reports nothing until it is in the `available` list in `controls.go`. Ingest checks `detect.Available(name)` and skips the detector without an error or a metric. A new detector, or a widened rule, also needs:
- **[pattern]** In daemon tests, give a stored run an event stream with `g.d.Blobs = memBlobs{"h": stream}` plus `store.BlobRef{RunID: id, Kind: "events", Hash: blob.Hash("h")}` appended to `g.state.refs`. The field is `blob.Hash`, not string, so import `internal/store/blob`. Create the run itself with `putRun`. A run in flight needs two things:
- **[gotcha]** `.claude/rules/runner-protocol.md` states the current `AnalysisVersion` as `(now N)`. When you bump it in `internal/ingest/analyse.go`:
- **[correction]** On a shared lane host, do not leave an unattended watcher that swaps the build in as soon as the lane goes idle. The owner stopped one and set this order instead: the client running batches drains and pauses the lane; anyone else holding the host's GPU (seen in `claude-hooks-res status`) finishes and releases it; you claim that GPU resource `:exclusive`, check the lane is idle with 0 queued, mail the client, deploy, verify and release; then you mail the client DONE so they resume. Check mail on a recurring schedule while you wait (CronCreate, hourly).
- **[gotcha]** A tool call that the output limit cut off has input `{}` in the event stream, because the SDK drops what it cannot parse (`ev.Calls[i].Input == "{}"`, error "Tool call editor was cut off by the output budget"). Its arguments exist only in the wire's streamed deltas, which `ReadWire` keeps as `Request.Calls`. A detector over call arguments must read the wire's calls when the run was captured and fall back to `ev.Calls` otherwise. To read cut-off JSON, use `detect.StringArgs`.
- **[env]** A full `make test` takes close to 5 minutes, nearly all of it `internal/legacyimport` under `-race`. Pass a Bash timeout of 600000 ms and run it under `claude-hooks-res run <host>:shared -r "<why>" -- make test`. While iterating:
- **[gotcha]** A new detector in `internal/detect` reports nothing until it is in the `available` list in `controls.go`. Ingest checks `detect.Available(name)` and skips the detector without an error or a metric. A new detector, or a widened rule, also needs:
- **[pattern]** Set a detector threshold from the lane host's real runs, read-only, never from a guess:
- **[gotcha]** The daemon has no notification hook for tests: `d.notified` is a `map[string]bool` of waiting runs, not a callback. Every notification is logged, so to assert on one, set `g.d.Log = slog.New(slog.NewTextHandler(&logs, nil))` with the `lockedBuffer` from `mutate_test.go` and look for `msg="<kind> <status>"`. The other way is a `Notify.Command` that writes to a file, as in `TestNotifyCommand`. A new notification kind must also go into `config.NotifyEvents`, or `[notify] events` refuses it. Then update the comment in `examples/config/automaticum.toml` and the kind list in 050 §2.
- **[pattern]** A new daemon setting or background job touches six places:
- **[pattern]** The rows of the spec-vs-code audit (`docs/audit/2026-11-01-spec-vs-code.md`) marked NOT BUILT or PARTIAL were partly grep guesses: a re-check found 17 already built and 20 with wrong evidence. Before building an audit gap, verify it against the code, or hand a read-only re-check to a subagent. To update rows, use a script that:
- **[gotcha]** Go code appended with `cat >> file_test.go <<'EOF'` or written by a python patch is not gofmt'd, and the tests still pass. Before staging, run `gofmt -l internal cmd`, then `gofmt -w` on what it lists, and check `test -z "$(gofmt -l internal cmd)"`.
- **[gotcha]** The private-name scan before a commit also covers the tracked `.wolf/cerebrum.md` and `.wolf/memory.md`. A lane host's short alias once slipped into a Decision Log entry and a memory row. Write the neutral host names in `.wolf/` notes too. Grep both `git diff --cached` and the tracked files with your local private-string pattern before every commit, and grep `git show HEAD` after it.
- **[pattern]** In daemon tests, give a stored run an event stream with `g.d.Blobs = memBlobs{"h": stream}` plus `store.BlobRef{RunID: id, Kind: "events", Hash: blob.Hash("h")}` appended to `g.state.refs`. The field is `blob.Hash`, not string, so import `internal/store/blob`. Create the run itself with `putRun`. A run in flight needs two things:
- **[pattern]** Fixtures do not show what a real stream does. To try analysis, a detector or a view on a real run, add a temporary env-gated test such as `internal/ingest/zz_tmp_test.go` or `cmd/automaticum/zz_tmp_test.go`. The test:
- **[pattern]** When a client reports a finding missing from a real run, do three things on the lane host before reading code:
- **[pattern]** In daemon tests, give a stored run an event stream with `g.d.Blobs = memBlobs{"h": stream}` plus `store.BlobRef{RunID: id, Kind: "events", Hash: blob.Hash("h")}` appended to `g.state.refs`. The field is `blob.Hash`, not string, so import `internal/store/blob`. Create the run itself with `putRun`. A run in flight needs two things:
  - `g.d.flights = map[string]*flight{"lane-1": {runID: ..., started: ..., lines: ...}}`, set under `g.d.mu`;
  - a `g.state.Enqueue` of its queue item.
  Event lines need the double wrapping `{"k":"event","event":{"type":"agent_event","payload":{"event":...}}}`. Without a wire capture, `swallowed_argument` learns argument names only from the run's own calls, so the fixture needs one call that did pass the argument.
- **[pattern]** A new daemon setting or background job touches six places:
  1. a field in `config.Daemon` (`internal/config/local.go`) with its validation;
  2. a refusal case in the `cases` table of `config_test.go`;
  3. a commented line in `examples/config/automaticum.toml`;
  4. the `automaticum.toml` line in 030;
  5. for a loop: start it in `Serve` beside `go d.heartbeatLoop(canary)`, and give the `Daemon` an `<X>Every time.Duration` field so the test can tick in milliseconds;
  6. for a new dependency: wire it in `cmd/automaticumd/main.go`, which `make test` does not compile.
- **[pattern]** The rows of the spec-vs-code audit (`docs/audit/2026-11-01-spec-vs-code.md`) marked NOT BUILT or PARTIAL were partly grep guesses: a re-check found 17 already built and 20 with wrong evidence. Before building an audit gap, verify it against the code, or hand a read-only re-check to a subagent. To update rows, use a script that:
  - splits each row on unescaped `|`;
  - asserts that the first cell starts with the expected spec § for that line number;
  - then sets the status and evidence cells.
  Line numbers drift, and the assert catches a wrong row.
- **[pattern]** Fixtures do not show what a real stream does. To try analysis, a detector or a view on a real run, add a temporary env-gated test such as `internal/ingest/zz_tmp_test.go` or `cmd/automaticum/zz_tmp_test.go`. The test:
  - skips when `REAL_EVENTS` is empty;
  - reads the events (`ReadEvents`);
  - runs `Analyse(&store.Run{ID: "x", Harness: os.Getenv("REAL_SDK")}, ev, nil)`, the detector or the view's function, and prints the result.
  Run it with `-count=1 -v`: without `-v` a passing test prints nothing. When the rules depend on the release, compare under two `REAL_SDK` values. Delete it with `rm` on the same command line. Fetch the stream read-only from the lane host with `automaticum logs <run> --events -json`. To turn a client's run into a positive control, have the temporary test write `ev.Calls` as JSON into `internal/detect/testdata/` (`json.Unmarshal` back into `[]detect.Call` matches lowercase keys), and check that file for private strings. A large control, such as one cut-off call of 200k characters, goes in as `.json.gz` and is read with `gzip.NewReader`. To check a detector against a client's own scan, run both on the same runs and compare the counts.
- **[pattern]** When a client reports a finding missing from a real run, do these on the lane host before reading code:
  - run `automaticum detect <detector> <run> --json --firings`, which prints the firings now beside the `stored` count;
  - run `automaticum run show <run>`, which lists the metrics and findings;
  - if the metric is there (e.g. `cap.hits 1`) and the finding is missing even from the fresh `detect`, look for the bug in how the findings are put together, not in the detector;
  - for a text detector, cut the detector out of the question: rebuild each turn's text from the event stream's `content_start` deltas (their `text`/`reasoning` fields, turn set by `iteration_start`) with a short python script into files, build the CLI on the dev host with `CGO_ENABLED=0 go build -o <scratch>/am ./cmd/automaticum`, and run `am detect <file> --sdk <release>`. If the file fires and `detect <run>` does not, the bug is in how `internal/ingest/events.go` puts blocks together.
  The request-level wire comes from `automaticum logs <run> --wire -json`, one JSON line per request with `response_body` as the raw SSE text. Feed it to `ReadWire` and the function under test (e.g. `CapHits`) in a temporary env-gated zz test.
- **[gotcha]** The event stream has no `content_end` for a turn the output limit or the wall limit cut off. The SDK's retry after the output limit (`max_tokens_turn_recovery`) is not in the event stream at all: only the wire capture holds that answer. Any new reader of `content_start`/`content_end` must keep a block that never ended. `Events.Read` keeps these as `Block.Unfinished` (`scan.flush`). Before analysis version 16 they were silently dropped, and a 360k-character loop that ran into the cap got no finding.
- **[gotcha]** Synthetic text fixtures for the loop detectors must be longer than the detectors' minimum sizes (`repetitionMinChars`, `loopMinChars` = 4000 in `internal/detect/guards.go`). Forty repeats of one sentence fire nothing. Which detector fires also depends on `detect.Options.Rules`: under the default 4.100.250 rules, 200 repeats fired `reasoning_loop` (low-uniqueness), not `reasoning_repetition`. Either set `Rules` to match what you assert, or assert that any loop detector fired on the turn.
- **[gotcha]** `.claude/rules/runner-protocol.md` states the current `AnalysisVersion` as `(now N)`. When you bump it in `internal/ingest/analyse.go`:
  - update that rule with `sed -i 's/(now N)/(now N+1)/'`;
  - add the dated "From analysis version N" note in 070 §2 naming the new metrics.
  Stored runs keep their old metrics until `automaticum reanalyse` (or `automaticum detect --record`) runs on the lane host after the deploy.
- **[env]** A full `make test` takes close to 5 minutes, nearly all of it `internal/legacyimport` under `-race`. Pass a Bash timeout of 600000 ms and run it under `claude-hooks-res run <host>:shared -r "<why>" -- make test`. While iterating:
  - use `go test -race -count=1 -run <Test> ./internal/daemon/`;
  - use `CGO_ENABLED=0 go test` for `cmd/automaticum`.
  `make test` does not compile `cmd/automaticumd`, so after changing the daemon's wiring there run `go vet ./cmd/automaticumd/` (cgo, slow: timeout 300000 ms). When you pipe the output through `grep -v "^ok\|cached"`, read the result from `echo rc=${PIPESTATUS[0]}`, since the pipe's own exit status is grep's.
- **[gotcha]** A new detector in `internal/detect` reports nothing until it is in the `available` list in `controls.go`. Ingest checks `detect.Available(name)` and skips the detector without an error or a metric. A new detector, or a widened rule, also needs:
  - a `SelfTest` control in `controls.go`: a control that fails takes down every detector it names;
  - a bump of `detect.Version` (`const Version` in `detect.go`) and a "library version N" note in 070;
  - an entry in the detectors map in `internal/ingest/analyse.go`;
  - an entry in `mainMetrics` in `cmd/automaticum/compare_cmds.go`, if its metric is a headline.
- **[pattern]** Set a detector threshold from the lane host's real runs, read-only, never from a guess:
  - Cross-compile the temporary env-gated zz test as a binary: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -o <scratch>/measure.test ./internal/ingest`.
  - `scp` it to the lane host's work directory.
  - List the runs with `automaticum run ls --json --limit 2000` (fields `id`, `state`, `batch`, `task`, `arm`, `lane`, `started_at`; keep `state == "finished"`).
  - For each run, write `automaticum logs <id> --wire -json` to a file and run `nice measure.test -test.run TestZZ...` with the file path in an env var. Print one tagged line per item.
  - Copy the output back and take the maximum per arm. The threshold is the base-model and stub maximum; say what the failing arms reached.
  - Delete the binary and outputs on both ends.
  371 runs took about 3 minutes. Record the measurement (date, run count, maxima) in the constant's comment and in 070.
- **[gotcha]** When a client quotes a size, check what it counts before you write the number into a spec or a code comment. A client's "218,643 characters" was the length of the call's escaped JSON arguments. The decoded `new_text` was 215,471 Unicode characters, which is what the detector reports, and fixing it took a second commit. Check it with python: `len(text)`, `len(text.encode())` and the length of the raw arguments string.
- **[gotcha]** The private-name scan before a commit also covers the tracked `.wolf/cerebrum.md` and `.wolf/memory.md`. A lane host's short alias has slipped into a Decision Log entry and into memory rows, once through a `claude-hooks-res` resource name (`<alias>/gpu1`). Write the neutral host names and "the GPU" in `.wolf/` notes too. Grep both `git diff --cached` and the tracked files with your local private-string pattern before every commit, and grep `git show HEAD` after it. Make the scan fail closed: put `test -n "$P" || exit 9` before the grep, because an empty pattern finds nothing and looks clean.
- **[pattern]** `git commit` runs the Caliber refresh and the gitleaks pre-commit hooks, which take a while. Run the commit with `run_in_background`, chaining `echo rc=$?`, the `git show HEAD` private-string grep and `git log --oneline -1`. When it finishes, grep its output for `no leaks found`, `rc=0` and the grep's `rc=1` (no match) before you push.
