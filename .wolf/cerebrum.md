# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-10-25

## User Preferences

- Only the owner turns an **Open** item into a decision. Carry a recommendation, and ask the questions in batches.
- When the owner lists setup steps, do them in that order. GitHub repos go under `mann1x`.
- The repo is public. Tracked files use only the neutral host names, with no nicknames, private paths or addresses. The mapping is in docs/local/hosts.local.md, which is untracked.
- Data never goes on the dev NVMe or on the old harness's spool NVMe. Use the dedicated disk named in docs/local.
- C# is one of the owner's main languages, so dotnet leads the language layers.
- (2026-10-13) Show durations as hours, minutes and seconds ("8m 13s", "1h 02m 05s"), never as bare seconds. This applies to every report and to all automaticum output.
- (2026-10-13) Every run shows its model (family and variant), GPU, host, inference point and sampler. Reports start with these.
- (2026-10-13) Fix forward: do not rework published results. Label which method produced each number.
- (2026-10-23) Run behaviour the owner chooses (which guards stop a run, thresholds) goes in the arm configuration. Never hard-code it.
- (2026-10-23) The client session (for example backup_models) starts its own batches. automaticum prepares the installation, the arm and the endpoint, and hands over the exact commands. It does not start the batch, and it does not ask the owner for a go on the client's behalf.

## Key Learnings

- (2026-10-25) The SDK's reasoning-loop guard ends a run by itself after `reasoningLoopDetection.maxConsecutiveTrips` consecutive cut turns (default 3; a clean turn resets it), unless the host answers `onReasoningLoopLimitReached` with `{action: "continue"}`. The arm's `stop_on` never sees that decision, because the runner passes `{}`. Whether an arm states it is the owner's call.
- (2026-10-25) The export (`internal/export`) gives spans in code points. The SDK guard's `verdict.chars` counts UTF-16 units, so convert it. The guard's lines are split on "\n", trimmed, with blanks dropped, and a last line with no "\n" is not yet a line (`detect.CycleStart`). The capture blob is a tar of `run-<id>/<stem>.json` and `<stem>.response.json`, and a response body is the raw SSE stream.

- **Project:** a Go harness for coding agents on @cerebriline/sdk. The source of truth is docs/specs/000..110 plus decisions.md. archive/ is input only, and the old harness is READ-ONLY.
- While a batch is running, do not rebuild the lane image or bin/, and do not restart the daemon. Write and test code in the meantime.
- Builds: the host's GCC 10 cannot link DuckDB, so anything that imports `internal/store/duck` builds in the link image (`make build`). The CLI also builds with `CGO_ENABLED=0`. A new store command goes in `store_cgo.go`, with a stub in `store_nocgo.go`.
- The store holds the imported old corpus (531 runs, with private strings). Never quote it in a tracked file.
  - Old streams are mostly deltas, so search the lines that are not deltas (`content_end` repeats the full text).
  - About half of each stream is `chunk` events, which are skipped.
  - A stream can reach 5 GB, so a walk over the corpus is a daemon job.
  - `internal/ingest` reads both envelopes.
- Lance IVF_PQ needs a refine of at least 30. A hit counts when it is as close as the true k-th neighbour.
- Config:
  - An arm with `template = true` is extend-only, and `LoadArms` skips it.
  - Go `flag` stops at the first positional argument, so use `parse()` from config_cmds.go.
  - Zero a struct before decoding JSON into it.
  - Drop an arm field from Resolved() when its false value must not change the arm's identity.
- Runs:
  - A run row is written only with a verdict. A run in flight lives in `lane_state`, which restart recovery reads.
  - Verdict rules live only in `run.Decide`. Run the tests with `-race`.
  - A new `store.State` method panics in the test fakes, so extend `internal/daemon/daemon_test.go` and `internal/run/run_test.go`.
- Ingest:
  - `ReplaceFindings` replaces every finding of a run.
  - Add each new detector to `owned`.
  - Bump `ingest.AnalysisVersion` whenever a version in `ingest.Versions` changes.
  - The basis records what was applied, not what fired.
- SDK builds:
  - Each build lives in `runner/sdk/<build>/`. Make its lockfile in the Node 22 container (`npm install --package-lock-only --ignore-scripts`), run `sdk add`, then commit the pinned hash.
  - Bundles are mounted read-only. Images hold no SDK, and never get the `automaticum.sdk.release` label.
  - `Run.Harness` holds only the release that was confirmed at run time, never the one the arm names.
- Wire checks:
  - Reading the settings back does not prove what was sent. Check the wire of the first request: agent requests are the `/api/chat` requests that carry `tools`.
  - In 4.100.250, every effort goes out as `think: true`, and presence_penalty and think_budget are dropped (X8).
  - On an OpenAI-compatible endpoint the arm must own thinking: "tag" sends `budgetTokens = 1024`, which makes the run INVALID.
- SDK behaviour:
  - Read a file before editing it, in the same session.
  - A run ends on the second text-only reply.
  - Plan mode has no editor; switch_to_act_mode is an extension extraTool.
  - With `interactive:false` the session is disposed at "ended".
  - `steer` aborts the request in flight.
  - Iterations restart on each run of the session, so count the leading `iteration_start` events.
- To read the extension's source, use the local Cline checkout with `git show v4.100.25x:<path>`. Match its session wiring (shared readReceipts) before blaming the SDK.
- Runner tests:
  - Run them with `docker run --rm --network none -v $PWD/runner:/runner:ro -w /runner node@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392 sh -c 'node --test test/*.test.mjs'`.
  - A new runner file goes on the COPY line of images/base/Dockerfile.
  - When stub.toml or base.toml changes, regenerate `runner/test/arm-stub.json` with `automaticum arm show stub --json`.
- To try the harness by hand, run `automaticum stub serve --listen <gw>:7412 --script <f>` with `bin/automaticumd --config <dir>`. The first SIGTERM drains and the second aborts.
- Gates: a task needs `automaticum oracle verify <task>` for its current hash. `internal/gate` imports `internal/run`, never the other way round.
- Everything a person reads goes through `internal/human`; `--json` stays raw. The client's `do()` drops the error body, so use `fail()`.
- Identity:
  - The model is in `models/<file>.toml` and the GPU is in the endpoint's `[hardware]`.
  - Keep stated facts, server facts and weights-file facts apart.
  - Mode C reads endpoints only through `Local.Endpoint` and `Local.MayUse`, and reloads arms with `d.armFor`.
- llama-server's `/props` is at the server root. Its `n_ctx` is per slot, so never derive it from `-c`/`-np`.
- Lane layers copy each toolchain from the oracle image's digest (`COPY --from`):
  - Check the source image's Debian release first: gcc:14 is Debian 13 (bug-032), so `cpp` starts from the gcc image.
  - `automaticum image build` needs `-repo .` when the config is `examples/config`.

## Do-Not-Repeat

- (2026-10-24) The private-string gate must fail closed:
  - Pass file names NUL-separated (`git ls-files -z | … | xargs -0 grep -IlniE "$P"`), because train-sh seed files have spaces in their names.
  - Commit only when nothing is listed and grep's exit code is 0, 1 or 123. Any error line means stop.
  - Never chain the commit after the check with `;`.
  - Never write the patterns or any real path fragment into a tracked file.
  - A package-lock false positive still needs its lines read, never a skipped check.
  - For tasks/train-sh, use the narrower pattern: its made-up server paths are pinned task data.
- (2026-10-24) A gap you notice (for example, the API takes several tasks but the CLI only one) is a design flaw, not out of scope. Name it and fix it, now or right after the current task.
- (2026-10-23) Never take a sampler or arm value from another session or from the old harness. omnimergekit's "8244" values were the owner's anti-loop sampler.
  - Ask the owner for the values and show them before the first run.
  - The owner's loop sampler is temp 1.0, min_p 0, rep 1 / last 0, pres 0.
  - "128k" means 131072.
- (2026-10-23) An opencoti or xollama server gets the SDK provider of that name, never openai-compatible. Read the bundle's provider list first.
- (2026-10-23) A batch id is not the prefix of its run ids. Read `batch` from `run show -json <run>` before `batch pause`.
- (2026-10-23) On a new llama-server, check chat_format and reasoning_format in /props. If they say "Content-only" or "none", pause and tell the server's owner.
- (2026-10-23) Never match guard sentences. Use the SDK's notices (metadata.kind/phase), and ask for one where a guard has none.
- (2026-10-23) Never pin a single Docker API version: Docker 29.1 refuses anything below 1.44, so use the engine's MinAPIVersion (bug-027). On a fresh host, pull the pinned node and debian images before running `sdk verify` or `oracle verify`.
- (2026-10-14) A detector that expects Ollama `options.*` must check `Request.OpenAI` (bug-019). Decide "no filter given" before a loop that deletes from `want` (bug-021). A failed request is not a missing finish reason (bug-020).
- (2026-10-13) Never invent a detector, guard or limit number: use the SDK's thresholds or ask. Never bake an arm variable, such as the SDK, into an image.
- (2026-10-13) The xollama GPU host is `windows-2` (RTX 3090). On a host with two addresses, write the one that answers on the service port.
- (2026-10-13) A job waiter needs an exit-code file, a failure exit and a time bound. Check the order of the markers before slice-and-splicing a Go file.

## Decision Log

- (2026-10-25) D108 (closes O23): the arm's stop_on alone decides whether the SDK's reasoning-loop limit ends a run. Every arm states guards.reasoning_loop.max_consecutive_trips; the base has the SDK's 3, which is dropped from Resolved() so arm ids stay the same. The runner answers onReasoningLoopLimitReached with stop only when stop_on names reasoning_loop.aborted (protocol 8). SDK core up to 4.100.262 does not forward the host's hook (X9, asked in mail 1925), so a run the SDK ends against the arm is INVALID (guard_limit_breach).

The full list is in docs/specs/decisions.md; keep this log in step with it.

- 2026-10-12: Go daemon on the hosts, Node only in lanes. Docker lanes, TOML config, agent mode for remote hosts. Verdicts are PASS/FAIL/TIMEOUT/ABORTED/INVALID/SKIPPED.
- 2026-10-13 D46-D52: store is DuckDB + Lance (own cgo wrapper over lance-c) + blobs; refine at least 30.
- 2026-10-13 D53-D65: manic_miner is public. D59 arm: 128k, effort high, temp 1, top-k 20, repeat last-n 0, repeat penalty 1, presence 0. A value that is not sent runs with a finding.
- 2026-10-13 D66-D83: SDK-agnostic images. A reload or partial GPU makes the run INVALID unless `[accepts]`. D81: OmniMerge v4 against base, 10 runs per arm, interleaved.
- 2026-10-14 D84-D86: canary on the first 5 requests. Checkpoint restore and fork are recorded.
- 2026-10-16 D87-D97: workspaces are git. An agent error before any answered request is ABORTED. M5 is BFCL. automaticum never launches model servers.
- 2026-10-23 D98-D101: Gemma-4 26B-A4B loop test, stop-at-first-trip arm against shipped guards. `guards.stop_on` is arm config (D100). The opencoti provider with effort high (D101).
- 2026-10-23 D102-D104: `automaticum export conversations` (110 §3.5). Arm `thinking.reasoning_history`, default auto (D103). Training traces come from the SDK's opt-in request capture (D104).
- 2026-10-23 D105 (revised): training-pool tasks are public, in tasks/<suite>/ with training = true.
- 2026-10-23 D106: export loop spans also come from the SDK guard's cuts (source guard|detector).
- 2026-10-23 Gemma loop batch: waits for the SDK's reasoningHistory probe fix. Before restarting, render a tool turn and a nudge turn on /apply-template, with request capture ON.
- 2026-10-24 D107: a batch's own wall/idle limits override the arm's and the task's. A batch takes several tasks and suites.