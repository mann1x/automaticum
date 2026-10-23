# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-10-18

## User Preferences

- (2026-10-23) The client session starts its own batches: for the training pool, backup_models (omnimergekit) is the client and starts the runs; automaticum prepares the installation, arm and endpoint, hands over the exact commands and does not start a client's batch itself, nor ask the owner for a go on the client's behalf.

- [2026-10-23] Run behaviour the owner chooses (which guards stop a run, thresholds, sets) goes in the arm as configuration, never hard-coded in the runner or harness.

- When the owner lists setup steps, do them in the order given.
- GitHub repos go under the `mann1x` account.
- The repo is public. Tracked files use only the neutral host names, with no nicknames, private paths or addresses. The mapping is in docs/local/hosts.local.md (untracked).
- Data never goes on the dev NVMe or on the old harness's spool NVMe. Use the dedicated disk named in docs/local.
- C# is a primary language for the owner: dotnet leads the language layers.
- An **Open** item becomes a decision only when the owner says so. The owner answers open questions in batches.
- 2026-10-13: durations are always shown as hours, minutes and seconds ("8m 13s", "1h 02m 05s"), never as bare seconds. This applies to every report and to all automaticum output (CLI, TUI, web).
- 2026-10-13: every run must show its model (family and variant), GPU, host, inference point and sampling parameters. Reports lead with these.
- 2026-10-13: fix forward. Do not rework published results. Label which method produced each number.

## Key Learnings

- Language lane layers (2026-10-23) copy each toolchain from the oracle image's digest (`COPY --from`), so agent and oracle compilers match. Check the source image's Debian release first: gcc:14 is Debian 13 and cannot be copied onto the Debian 12 base (bug-032), so `cpp` starts from the gcc image and copies Node and the runner from `automaticum-lane:base`. `automaticum image build` needs `-repo .` when the config is `examples/config` (its repo points elsewhere).

- **Project:** automaticum is a Go test harness for coding agents on @cerebriline/sdk. The specs in docs/specs/000..110 plus decisions.md are the source of truth; docs/specs/archive/ is input only. The old harness is READ-ONLY.
- Do not rebuild the lane image or bin/, and do not restart the daemon, while a batch is running. Write and test code meanwhile, then deploy afterwards.
- Builds: the host GCC 10 cannot link DuckDB, so anything that imports `internal/store/duck` is built in the link image (`make build`). The CLI also builds with `CGO_ENABLED=0`, without store commands. New store commands go in `store_cgo.go`, with a stub in `store_nocgo.go`.
- The store holds the imported old corpus (531 runs, private strings). Never quote imported records in a tracked file. The spike's own corpus store is obsolete.
- Old streams are mostly deltas, and `content_end` repeats the full text. Search the non-delta lines. Single streams reach 5 GB, so any corpus walk is a daemon job, never work done inside an HTTP request. Both envelopes (old `agent_event` and new) are read by `internal/ingest`. About half of a stream is `chunk` events, which are skipped.
- Lance IVF_PQ needs a refine of at least 30. Real embeddings contain near-copies, so a hit counts when it is as close as the true k-th neighbour. For scale tests use real vectors plus noise.
- Arms: `template = true` (not inherited) marks an extend-only arm. `LoadArms` skips it.
- Go `flag` stops at the first positional argument, so commands use `parse()` from config_cmds.go.
- A run row is written only with a verdict. A run in flight lives in `lane_state`, which is what restart recovery reads (verified with KILL and restart). Verdict rules live only in `run.Decide`. Run tests with `-race`.
- A new method on `store.State` panics in the embedding test fakes, so extend `internal/daemon/daemon_test.go` and `internal/run/run_test.go`.
- `ReplaceFindings` replaces every finding of a run. A new ingest detector must be added to `owned`. Bump `ingest.AnalysisVersion` whenever a version in `ingest.Versions` changes. The basis records what was applied, not what fired.
- SDK builds live in `runner/sdk/<build>/`, with the lockfile made in the Node 22 container (`npm install --package-lock-only --ignore-scripts`). Then run `sdk add`, then commit the pinned hash. The bundle is mounted read-only at run time and images hold no SDK. Never put the `automaticum.sdk.release` label on an image without that SDK. `Run.Harness` holds only the release that was established, never the one an arm names.
- SDK 4.100.250 pins zod 4.4.3 (overrides) and installs the stock Ollama provider: every effort goes out as `think: true`, and presence_penalty and think_budget are dropped (X8). The read-back is not proof of what was sent: always check the wire of the first request. Agent requests are the `/api/chat` requests that carry `tools`.
- SDK behaviour:
  - It refuses an editor call on a file not read in the same session, so read before edit.
  - Every text-only reply gets one no-tool-call nudge, so a run ends on the second text-only reply.
  - Plan mode has no editor and no switch_to_act_mode, which is an extension extraTool.
  - With `interactive:false` the session is disposed at "ended".
  - `steer` aborts the request in flight.
  - Iterations restart per run of the session, so count lead `iteration_start` events.
  - Denial reasons get a suffix appended.
- Extension source for porting: the local Cline checkout, read only with `git show v4.100.25x:<path>`. Match the extension's session wiring (shared readReceipts) before blaming the SDK.
- Runner tests: `docker run --rm --network none -v $PWD/runner:/runner:ro -w /runner node@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392 sh -c 'node --test test/*.test.mjs'`. A new runner file goes on the COPY line of images/base/Dockerfile. Regenerate `runner/test/arm-stub.json` (`automaticum arm show stub --json`) when stub.toml or base.toml change.
- To try the harness by hand: `automaticum stub serve --listen <gw>:7412 --script <f>` plus `bin/automaticumd --config <dir>`. The first SIGTERM drains, the second aborts.
- A task needs `automaticum oracle verify <task>` for its current hash, or gate `oracle` refuses it. Gates are asked by `Daemon.Gates`, and `internal/gate` imports `internal/run`, never the other way round.
- Everything a person reads goes through `internal/human`. `--json` stays raw.
- Model identity is in `models/<file>.toml`, and the GPU is in the endpoint's `[hardware]`. Keep stated facts, server facts and weights-file facts apart (the server says `qwen35` for Qwen 3.6).
- The client's `do()` drops the error body, so use `fail()` with the reasons in the message.
- SKIPPED runs carry `provenance.asked`. Older ones cannot be retried.
- Mode C: read endpoints only through `Local.Endpoint` and `Local.MayUse`, and reload arms with `d.armFor`. On OpenAI-compatible endpoints the arm must own thinking: "tag" makes the SDK send `budgetTokens = 1024` and the run is INVALID.
- llama-server `/props` is at the server root. `n_ctx` there is per slot, so never derive it from `-c`/`-np`.
- Decoding JSON into a struct that already holds values keeps the old fields, so zero the struct first.
- The private-string check gives a false positive on a shared-directory prefix in package-lock: look at the matching lines, never skip the check.
- Owner reference: the D59 tag takes about 12 minutes on manic_miner in VS Code.

## Do-Not-Repeat
- 2026-10-23: an endpoint that is an opencoti (or xollama) server gets the SDK provider of that name, never openai-compatible: the providers differ in what they send back (reasoning history, pools, sessions). Read the SDK bundle's provider list before writing an arm's model.provider.

- [2026-10-23] Never take a sampler from another session's account of "the owner's config": omnimergekit's "8244" values (temp 0.7, rep 1.13/72, pres 0.25) were the owner's ANTI-loop sampler, so the Gemma loop test measured the fix. Ask the owner for the exact sampler of the condition under test, and show it before the first run. Owner's loop sampler: temp 1.0, min_p 0, rep 1 / last 0, pres 0.

- [2026-10-23] A batch id is not the run ids' prefix (runs 20261023-110717-819b65-NNN belonged to batch b20261023-110717-524ef9). Read the batch from `run show -json <run>` (field `batch`) before `batch pause`. First runs on a new llama-server: check /props chat_format and reasoning_format; "Content-only"/"none" means no tool calls or reasoning will be parsed, so pause and tell the server owner.

- [2026-10-23] In SDK 4.100.258 only reasoning_loop (cut/aborted/removed) and tool_intent_overrun (cut) announce a trip (notice metadata.kind/phase). Tool-call loop detection puts its soft warning in the tool result text and a hard one in the mistake tracker: no event. Never match guard sentences; ask the SDK for a notice. A new arm field whose false value must not change arm identity is dropped from Resolved() when false.

- [2026-10-23] Docker 29.1 refuses Engine API versions below 1.44; never pin one API version for every engine. The client now takes the engine's MinAPIVersion when newer than v1.41 (bug-027). A fresh host also needs the pinned node/debian images pulled before `sdk verify` and `oracle verify`, and `task build custom/manic_miner` before its verify.

- [2026-10-16] A `;`-chained commit after the private-string check pushed a hit. Gate the commit on the check, and include `\bbs2\b` in the pattern.
- [2026-10-14] A detector that expects Ollama `options.*` must check `Request.OpenAI`. Bump the detector version and AnalysisVersion when its output changes (bug-019).
- [2026-10-14] Decide "no filter given" before a loop that deletes from the `want` set (bug-021).
- [2026-10-14] A failed request is not a missing finish reason. Keep the cap audit consistent with CapHits (bug-020).
- [2026-10-13] Never invent a detector, guard or limit number. Use the SDK's tuned thresholds (packages/agents, core/src/runtime/safety) or ask.
- [2026-10-13] Never bake an arm variable (such as the SDK) into an image. Several SDKs run side by side and are provisioned at run time.
- [2026-10-13] Never fill an arm from old-harness values. "128k" means 131072. The sampler, effort (`high`) and other values are the owner's. If a value is not named, read `/api/show` and ask.
- [2026-10-13] The xollama GPU host is `windows-2` (RTX 3090), not linux-gpu-2. Check the mapping against what the owner says.
- [2026-10-13] For a host with two addresses, write the one that answers on the service port.
- [2026-10-13] The link image has no `/usr/bin/time`. Jobs write their own exit code to a file. Every waiter needs a failure exit and a bound, and must test the job's own "exit" line.
- [2026-10-13] Check the order of the markers before slice-and-splicing a Go file (duplicated types in api.go).

## Decision Log

Full list with ids is in docs/specs/decisions.md. Keep this log in step.

- 2026-10-23 D106: export loop spans come from the SDK guard's cuts too, not only the detector library (widens D102); each span has source guard|detector; guard span = cut response (complete:false), reasoning field, end verdict.chars, start at the second of the trailing verdict.cycles repeats of verdict.sample.

- 2026-10-12: Go daemon on hosts and Node only in lanes. Docker lanes with slim base plus language layers. TOML config. Agent mode for remote hosts. Verdicts are PASS/FAIL/TIMEOUT/ABORTED/INVALID/SKIPPED. Apache-2.0. Gitea per daemon. Plugins are compiled in.
- 2026-10-13 store: DuckDB + Lance (lance-c behind our own cgo wrapper) + blobs. Build base manylinux_2_28. D46-D52: binding review at M6, dedup by message part, regex skips deltas, refine at least 30, chunk locator, earlier campaigns imported on demand, embeddings run manually.
- 2026-10-13 D53-D61: manic_miner public in the repo. SDK release recorded per run. First runs on windows-2 with qwen36-base-mtp_tb:27b-q4km-128k. D59 arm: 128k window, effort high, temperature 1, top-k 20, repeat last-n 0, repeat penalty 1, presence penalty 0. Compaction and guards are on by default. Runs on windows-2 can be stopped at once.
- 2026-10-13 D62-D65: a value that is not sent runs with a finding. A busy endpoint makes the run wait. The old corpus gets text detectors only.
- 2026-10-13 D66-D83: SDK-agnostic images with read-only bundles, releases and local builds. Human-readable formats. Run identity (catalogue, GPU declared). A reload or partial GPU makes the run INVALID unless `[accepts]`. Detector thresholds are the SDK's own and every match is used. Reports give no "distinguishable" verdict. `unload_others` per endpoint. The model is queried at each run start. D81: OmniMerge v4 against base, 10 runs per arm, interleaved. Pause releases the endpoint.
- 2026-10-14 D84-D86: canary on the first 5 requests, with control tokens from the tokenizer. A reload after a pause keeps the verdict. Checkpoint restore and fork are recorded and metrics restart there.
- 2026-10-16 D87-D89: workspaces are git by default. Zero user.* metrics are hidden. An agent error before any answered request is ABORTED.
- 2026-10-16 D90-D97: M5 focuses on BFCL (SWE seeded 50 from Lite, env-only image cache). BFCL and SWE run on blackswan-2 with its own daemon and its data root on the data disk, using both GPUs when free and never unloading others. SWE disk floor is 200 GB. automaticum never launches model servers.
- 2026-10-23 D98-D99: Gemma-4 26B-A4B loop test with omnimergekit on blackswan-2: thinking on, ctx 256000, 10 runs per arm, 1h wall / 15m idle, SDK 4.100.258. Two arms: guards.stop_at_first_trip = true (first guard trip ends the run as FAIL, oracle still runs and is in the reason) vs guards as shipped. Every run counts guard_trips.<guard>.<phase>.
- 2026-10-23 D100: which guard trips stop a run is ARM config (guards.stop_on, guard or guard.phase; owner: "this is obviously a configuration for the runs"), not runner code. Gemma stop arm: loop_detection, reasoning_loop, tool_intent_overrun, nudge.non_convergence, nudge.silent_repetition. Loop test moved to SDK 4.100.259 (every guard announces).
- 2026-10-23 D101: Gemma loop arms use the SDK `opencoti` provider (owner: "why openai-compatible? cerebriline has opencoti provider") with thinking effort high on the arm. The opencoti provider's reasoning-history scope is `none` (openai-compatible is `all`), so it changes whether reasoning is re-sent. Check the SDK's provider list before mapping a server to openai-compatible.
- 2026-10-23 D102: `automaticum export conversations` approved, to build after the Gemma loop batch (110 §3.5). Format agreed with backup_models (mails 1829-1831).
- 2026-10-23 D103: arm `thinking.reasoning_history` (auto|all|last|none) + `thinking.reasoning_inline`, base = auto/true (the plugin's 'Prior reasoning' and 'Inline thinking when the template drops it'); runner passes them in providerConfig (SDK reads reasoningHistory/reasoningInline there); runner protocol 6. Owner: the default MUST be auto like the plugin. SDK 4.100.259 auto probe for opencoti hits {baseUrl}/apply-template = /v1/apply-template -> 404 -> falls back to provider default none.
- 2026-10-23 D104: training traces come from the SDK's 'Save model requests' capture, OPT-IN per run/batch/campaign (owner: full requests are big and mostly not needed; not part of arm identity), (owner asked Cerebriline to add it, requests+raw responses, core writer, ClineCoreOptions switch), not from automaticum exporters rebuilding from logs; export conversations just hands out the capture + run metadata (110 §3.5).
- 2026-10-23 D105 (REVISED by owner same evening: "create our tests and mark them as training, not keeping them private"): training-pool tasks are PUBLIC in repo tasks/<suite>/ with training = true. The private root (private_tasks) exists but is not used for the pool.
- 2026-10-23: SDK 4.100.260 last-inline on Gemma-4 (Google template ae53464b) renders '<tool_response|><think>R</think><turn|>\n<|channel>thought\n' -> next turn starts headerless; owner stopped the loop batch until the SDK fixes inlining. Check a new history/inline mode by rendering a tool turn on the server's /apply-template before a batch.
- 2026-10-23: owner: the Gemma loop batch restarts on SDK 4.100.262 (adds preserve_thinking), not .261. Before restarting: resolved reasoningHistory last/native/measured, and render a tool turn + a nudge turn on the server's /apply-template. Restart batch with request capture ON (training material).
- 2026-10-23: owner: the Gemma loop test waits for the SDK release with the reasoningHistory probe fix; arms stay `auto`, which must resolve to `last` (not `all`) on opencoti. Verify on the wire + ResolvedSessionConfig.reasoningHistory (scope last, decidedBy measured) before restarting.