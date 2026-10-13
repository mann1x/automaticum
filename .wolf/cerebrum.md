# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-10-12

## User Preferences

<!-- How the user likes things done. Code style, tools, patterns, communication. -->
- When the owner lists setup steps, do them in the order given (repo creation before tool inits).
- GitHub repos go under the `mann1x` account.
- The repo is public: no host nicknames, private paths or addresses in tracked files.
- Never put spike or store data on the development NVMe (filling it blocks every project) or on the old harness's spool NVMe (heavily used). Use the dedicated disk recorded in docs/local/hosts.local.md.
- C# is a primary language for the owner: dotnet leads the language layer list.
- An **Open** spec item becomes a decision only when the owner says so; ask, do not decide by default. The owner is willing to answer open questions in batches when asked.

## Key Learnings

- **Project:** automaticum — Go test harness for coding agents built on @cerebriline/sdk; spec stage, no code yet.
- Source of truth: the spec set docs/specs/000..110 plus decisions.md (draft 0.5). Inputs, not edited: docs/specs/archive/ (draft spec 0.4 from the Cerebriline session, first user; omnimergekit requirements 0.1, second user).
- Host mapping, old-harness paths and mailbox aliases live in docs/local/hosts.local.md (untracked). Published files use neutral host names (linux-gpu-1, linux-gpu-2, windows-1, mac-1) and no absolute paths. The old harness is READ-ONLY.

- [2026-10-13] Old event streams are almost all streaming deltas: 30 GB of stream holds 0.45 GB of distinct text, and each block's closing event (`content_end`) repeats its text in full. Chunk, search and regex over the non-delta lines; skip lines whose first 200 bytes contain `"type":"content_start","contentType":"reasoning"` or `..."text"`.
- [2026-10-13] Lance IVF_PQ needs a refine step: without it recall@10 is 0.1 to 0.6 on every set tried and more probes do not help. Use refine 30 or more.
- [2026-10-13] Real chunk embeddings are full of near-copies (mean cosine 0.95 to the nearest neighbour), so recall by identical ids tops out near 0.92 even for an exact scan. Count a hit as correct when it is at least as close as the true k-th neighbour.
- [2026-10-13] Synthetic vectors as evenly spread isotropic clusters are a worst case for PQ and unlike real embeddings. Draw scale sets as real vectors plus noise (`bench vectors -base`).
- [2026-10-13] The host compiler (GCC 10) cannot link DuckDB, so anything importing `internal/store/duck`, including `cmd/automaticum`, is built in the link image (`make build`). Pure-Go packages (`blob`, `legacyimport`) build and test on the host.
- [2026-10-13] The imported store holds private strings from the old corpus (endpoint addresses, absolute paths, host nicknames in invalid-run notes). Never quote imported records in a tracked file.
- The command line builds two ways: with cgo (link image) it has the store commands; with `CGO_ENABLED=0` (host) it has everything that reads configuration and tasks. New store-touching commands go in `cmd/automaticum/store_cgo.go` with a stub in `store_nocgo.go`.
- An arm that exists only to be extended carries `template = true` (not inherited). `LoadArm` returns it unvalidated with no ID; `LoadArms` leaves it out. `arms/base.toml` is one.
- Go's `flag` stops at the first positional argument; commands use `parse()` in `config_cmds.go` so `automaticum oracle verify <task> -repeats 2` works.
- A run row is only written with a verdict (`UpsertRun` refuses others). A run in flight is known through `lane_state` (run id, container id in `detail`), which is what restart recovery reads.
- `internal/run` is pure Go: it reaches the store through `store.State` and a `Blobs` interface, so its tests run on the host with fakes. Verdict rules live only in `run.Decide`.
- Run tests with `-race`: `Execute` stores the runner's two output streams from two goroutines.
- 2026-10-13: `@cerebriline/sdk` 4.100.250 is neither tagged nor on the registry; the SDK main branch is at "start 4.100.250". Runner install and real runs wait for it (D54: no fallback).
- `Run.Harness` holds the SDK release only once established (image label, then the runner's own finding). The release an arm names is in the resolved arm and in `provenance.sdk.named`. Never show a named release as the run's release.
- The daemon runs on the host as `bin/automaticumd --config <dir>` (built by `make build` in the link image; glibc of manylinux 2.28 runs on the host). First SIGTERM drains, the second aborts. Find its pid with `pidof automaticumd`.
- No test image may carry the `automaticum.sdk.release` label unless that SDK is really installed in it: the label is what a run's record trusts before the runner confirms.
- Runner tests run in the pinned Node image, never with a host Node: `docker run --rm --network none -v $PWD/runner:/runner:ro -w /runner node@sha256:86f07bc9… node --test test/`. `runner/test/arm-stub.json` must be rewritten with `automaticum arm show stub --json` whenever `arms/stub.toml` or `arms/base.toml` changes (a Go test fails otherwise).
- The SDK source keeps the `@cline/*` names; a pack script renames the five packages to `@cerebriline/*` at one version. `@cerebriline/sdk` re-exports core. The system prompt builder is exported from core only as `getClineDefaultSystemPrompt`. `ask_question` is absent unless the host supplies an executor. (From a sub-agent's read of the SDK main branch; unverified until the runner runs.)
- SDK 4.100.250 needs zod 4.4.3 exactly (pinned by `overrides` in `runner/package.json`); regenerate the lockfile with the lane's Node 22 image, not Node 26.
- The SDK refuses an `editor` call on a file not read in the session, and nudges for `check_file` after an edit: a stub script must read, edit, check, then answer. Stub turns are chosen by the count of assistant messages in the request.
- To try the harness by hand: `automaticum stub serve --listen <bridge gateway>:7412 --script <file>` and `automaticumd --config <dir>`; stop both with SIGTERM (`pidof automaticum`, `pidof automaticumd`).
- First wire of 4.100.250: `num_ctx` follows `contextWindow`; `think: true` is sent even when the arm leaves thinking to the model tag (the stub advertises the thinking capability); `num_predict` and `max_output_tokens` are sent by the SDK's own rule when the arm states 0.
- The private-string check matches registry URLs in `runner/package-lock.json` (packages whose name is the word shared, between slashes): a false positive. Look at the matching lines before deciding, do not skip the check.
- Verified on the real daemon 2026-10-13: `kill -KILL` mid-run, restart, the run is `ABORTED (daemon restart)` once, its container removed, the next queued run runs once.
- Owner's reference (2026-10-13): this tag, configured as in D59, takes about 12 minutes on `manic_miner` in VS Code with the Cerebriline extension on the owner's desktop. Compare real runs with that.
- With a reasoning effort and no budget stated, the SDK derives a thinking budget (high and xhigh give tens of thousands of tokens); the runner records it under `derived` in its `config_resolved` note.
- The read-back is not proof of what was sent. On 4.100.250 with xollama, `reasoningEffort` (and the budget the SDK derives from it) is reported as resolved but the request carries only `think: true`, and the server applies the tag's own think budget; a sampler value of 0 (`presencePenalty`) is reported but not sent. Always check the wire of the first request.
- Since 2026-10-13 the daemon's store also holds the imported old corpus (531 runs); the spike's separate corpus store still exists and is no longer the one to query.

- 2026-10-13: SDK 4.100.250 from the registry installs the stock Ollama provider library (the extension uses a patched one): every effort level goes out as `think: true` and options the stock schema does not name (presence_penalty, think_budget) are dropped. Fixed upstream, unreleased (X8). The five first real runs ran on the tag's budget (medium), not high.
- 2026-10-13: half of a run's event stream is `chunk` events that repeat agent events as text; ingest skips them unparsed. Agent requests on the wire are the `/api/chat` requests that carry `tools`.
- 2026-10-13: `ReplaceFindings` replaces all findings of a run; ingest keeps findings of detectors it does not own (`internal/ingest` `owned`). A new detector written by ingest must be added there.
- 2026-10-13: adding a method to `store.State` compiles in test fakes that embed the interface but panics at run time when called; extend the fakes (`internal/daemon/daemon_test.go`, `internal/run/run_test.go`).

- 2026-10-13: the old corpus holds single event streams of up to 5 GB (deltas that repeat the accumulated text); anything that walks the corpus takes minutes and must be a job inside the daemon, never work done inside an HTTP request.
- 2026-10-13: the short-unit detector follows the reference pattern `(.{2,40}?)\1{7,}` exactly: shortest unit at the leftmost place, letters rule on that unit, a failing repeat passed over whole. Check a change against that pattern in Python before trusting a test's expectation.
- 2026-10-13: the older harness's stream is `{"ts","type":"agent_event","event":{...}}` with the same inner SDK events; `internal/ingest` reads both envelopes.

- 2026-10-13: a run now needs its task's oracle proven for the task's current hash: `automaticum oracle verify <task>` writes the proof under the data root (`artefacts/proven/<hash>.json`); any change to a task's files voids it and gate `oracle` refuses until it is run again.
- 2026-10-13: gates are asked by the daemon (`Daemon.Gates`, set in `cmd/automaticumd`), not by `run.Executor`, which keeps its own checks; `internal/gate` imports `internal/run` (probe), never the other way.
- 2026-10-13: decoding JSON into a struct that already holds values keeps the fields the new document omits; zero the struct first (a stale `waiting` in a daemon test).

- 2026-10-13: lane images hold no SDK. A build is `runner/sdk/<build>/` (package.json, package-lock.json, sdk.toml with the pinned bundle hash); `automaticum sdk add <build>` installs it under the data root and archives the downloaded packages; the run mounts the bundle read-only at the runner's node_modules. `automaticum sdk verify <build>` reinstalls with no network and compares. Never put an SDK back into an image.
- 2026-10-13: a new SDK release = a new directory under `runner/sdk/` with its lockfile made in the Node container (`npm install --package-lock-only --ignore-scripts`), then `sdk add`, then commit the pinned hash. An arm picks it with `[harness] sdk_release`.

## Do-Not-Repeat
- [2026-10-13] Built the SDK into the lane image with a label, so one image carried one SDK and a new release meant rebuilding images. The owner: "we need to support multiple sdk versions at the same time. the images should be sdk agnostic and support provisioning of a specific sdk version at runtime. we need the testing to be reproducible on demand". Anything that is a variable of an arm must not be baked into an image.

<!-- Mistakes made and corrected. Each entry prevents the same mistake recurring. -->
<!-- Format: [YYYY-MM-DD] Description of what went wrong and what to do instead. -->
- [2026-10-12] Ran the tool inits before creating the GitHub repo although the owner listed the repo first. Follow the listed order.
- [2026-10-13] Wrapped a container job in `/usr/bin/time`, which the link image lacks; the job died with 127 and the `docker wait` task still said exit 0. Have each job write its own exit code to a result file and read that file.
- [2026-10-13] Wrote a blind `until` waiter (success condition only) and a waiter that tested a result file created empty by shell redirection. Give every waiter a failure exit and a bound, and test for the job's own "exit" line.

- [2026-10-13] NEVER fill an arm from the old harness's values when the owner has named the value. "128k" in the model tag the owner chose means a 128k window (131072, what the tag declares), and the sampler is the owner's (temperature 1, top-k 20, repeat last-n 0, repeat penalty 1, presence penalty 0). I had copied 96000, effort xhigh and temperature 0.8 from the old manual arm and was corrected three times (thinking effort is `high`). When a value is not named, read the tag's own parameters (`/api/show`) and ask, do not reach for the old harness.
- [2026-10-13] The owner's second GPU host for xollama is a WINDOWS machine with an RTX 3090; its neutral name is `windows-2`. I had mapped it to `linux-gpu-2`. Check the host mapping against what the owner says before writing a neutral name.
- [2026-10-13] A host with two addresses: write the address that answers on the service port (`curl -w %{remote_ip}`), not the first one the resolver returns.

- 2026-10-13: do not slice-and-splice a Go file between two text markers without checking their order in the file; it duplicated two type declarations in `internal/daemon/api.go`.

## Decision Log

<!-- Significant technical decisions with rationale. Why X was chosen over Y. -->
- [2026-10-12] Owner: Go daemon; hosts run only the Go binary; Node runner inside the lane image; Docker lanes (slim base + language layers); Gitea for git tasks.
- [2026-10-12] Owner: spec-only outcome for now; agent mode only for remote hosts; TOML with tasks in this repo; one operator plus assistants; verdicts PASS/FAIL/TIMEOUT/ABORTED/INVALID/SKIPPED; raw streams as content-addressed blobs on disk; runner at full extension parity; first language layers dotnet (C#), node, python, go, rust, c/c++.
- [2026-10-12] Owner prefers store shape 3: DuckDB (state, config, metrics, stats) + Lance via lancedb-go (chunks, vectors, FTS). To be confirmed by the spike in spec 10.1.
- [2026-10-12] Owner: paused time excluded from timing; oracle sampling optional and off by default; no default for guards or compaction (arm must state each); runner installed into external harness task containers; omk_eval always owns serving in mode C; BFCL as two cells (proxy, agent); completion-form tasks stay out of automaticum; other-language SWE-bench sets are external suites with C/C++ first; detector library in Go in automaticum; VS Code-only behaviour emulated where an SDK hook exists; full TUI dashboard.
- [2026-10-12] First languages recorded as C#, C/C++, Python, TypeScript, Go, Rust. This is a reading of two answers (C# named primary, then not ticked in the language question); see decisions.md D15 note.
- [2026-10-12] Full list with ids lives in docs/specs/decisions.md; keep it and this log in step.
- [2026-10-12] Owner (second pass): SDK fixes approved on the Cerebriline side (X1-X3 first); store spike first; milestone order approved, milestone 1 planned next; one Gitea per daemon; blobs archived automatically by age; plugins compiled in only; TUI has all four view groups; runner only observes endpoint-error retries (X5 withdrawn); native agent's Node question deferred to milestone 7.
- [2026-10-12] Owner: archived logs must stay searchable like the episodic memory in claude-hooks (index stays in the database, only raw blobs move, transparent open of archived blobs, embedder version per chunk). Spec 080 section 5.1.
- [2026-10-12] Owner (milestone 1 plan): Apache-2.0; Lance binding chosen by the spike (lancedb-go v0.1.2 is unusable as released); both engines static with dynamic glibc, built on Debian 11; store required on Linux only, Windows gets agent mode and CLI without it; thresholds T1-T15 (T1-T3 hard gates); spike data on a dedicated NVMe (never the dev or spool disk), archive on the large array.
- [2026-10-12] Cerebriline SDK: X1-X3 are on its main branch, unreleased (resolved-config callback that can refuse a session, context window field, process-wide data dir option, ignore stored provider profile). No switch for the global settings file; three bundled media skills are on by default.
- [2026-10-13] Owner, after link gate T1 (both candidates passed on linux amd64+arm64): Lance binding is lance-c behind our own cgo wrapper (lancedb-go main is the fallback); build base is manylinux_2_28 because DuckDB's prebuilt archive cannot link with Debian 11's GCC 10.
- [2026-10-13] Milestone 1 result: T1-T3 pass; spike supports DuckDB + Lance (lance-c) + blobs. Open with the owner: O14 confirm D6, O8 dedup by message part, O15 regex over non-delta lines, O16 refine default 30, O17 chunk locator, O18 earlier campaigns. Not decided by me.
- [2026-10-13] Owner, after the spike: store shape confirmed with a review of the Lance binding at milestone 6 (D46, not the plain "confirm" I recommended); wire captures deduplicated by message part, sizes confirmed on real captures in milestone 2 (D47); regex skips streaming deltas by default, raw mode kept (D48); vector refine factor at least 30 (D49); every chunk carries blob hash and byte offset (D50); earlier campaigns imported only on demand (D51).
- [2026-10-13] Owner: embeddings are updated manually; with this amount of data it cannot be scheduled or automatic (D52). Ingest writes chunks without vectors; the operator starts an embedding pass.
- [2026-10-13] Owner (milestone 2 plan): manic_miner public and whole in the repo (D53, against my recommendation of a private directory); SDK release 4.100.250 and every run records the SDK release version (D54); first real runs on xollama, windows-2 (not linux-gpu-2: my mapping error), model qwen36-base-mtp_tb:27b-q4km-128k (D55); old corpus: re-map contradicted verdicts and import the two sibling lanes, no boundary for the defective build (D56); config directory by --config, lanes and endpoints untracked (D57); plan proposals approved (D58).
- [2026-10-13] Owner: first real arm uses a 128k window, thinking effort high and sampler temperature 1, top-k 20, repeat last-n 0, repeat penalty 1, presence penalty 0 (D59); compaction and all guards are on by default unless an arm says otherwise (D60); real runs on windows-2 must be stoppable at once, gracefully or not, because the owner may need the host (D61).
- [2026-10-13] Owner: milestone 3 plan approved (D62). A value the provider does not send does not stop the run: run and record a finding (D63, against my recommendation to refuse). A busy endpoint makes the run wait, with one notification (D64). The old corpus gets the text detectors only (D65).
- 2026-10-13 D66: several SDK builds at once; images SDK-agnostic; the arm's build provisioned at run time; reproducible on demand.
- 2026-10-13 D67: prebuilt bundle mounted read-only; tracked lockfile plus an archive of every downloaded package.
- 2026-10-13 D68: releases and local builds (`<release>+<commit>`) are provisionable.
