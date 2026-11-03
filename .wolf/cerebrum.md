# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-11-01

## User Preferences

- Only the owner turns an **Open** item into a decision. Carry a recommendation; ask in batches.
- Do the owner's setup steps in the order listed. GitHub repos go under `mann1x`.
- Public repo: neutral host names only, no nicknames, private paths or addresses. Mapping: docs/local/hosts.local.md (untracked).
- Data never goes on the dev NVMe or the old harness's spool NVMe; use the disk named in docs/local.
- C# is a main language, so dotnet leads the language layers.
- Show durations as "8m 13s" or "1h 02m 05s", never bare seconds, in all reports and output.
- Every run shows model (family, variant), GPU, host, inference point and sampler. Reports start with these.
- Fix forward: don't rework published results; label each number's method.
- Run behaviour the owner chooses (stopping guards, thresholds) is arm config, never hard-coded.
- (2026-10-29) This session only develops automaticum. Client sessions (e.g. backup_models) write their own arms, models, endpoints and lanes, confirm their gates and start their own batches.

## Key Learnings

- **A turn cut by the output or wall limit has no `content_end` (2026-11-03, bug-076):** the SDK streams text/reasoning as `content_start` deltas and closes with `content_end`; a cut turn never closes, and its retry (`max_tokens_turn_recovery`) is NOT streamed as events at all, only the wire capture holds it. `Events.Read` keeps open partials as `Block.Unfinished` (`scan.flush`). Any reader of the event stream must handle a block with no end.

- [2026-11-03] A tool call cut by the output limit has input "{}" in the event stream (the SDK drops what it cannot parse); its arguments exist only in the wire's streamed deltas, now Request.Calls. Detectors over call arguments must read the wire when captured.
- [2026-11-03] To set a detector threshold from data: cross-compile a throwaway `go test -c` measurement binary (CGO_ENABLED=0), scp it to the lane host, pipe `automaticum logs <run> --wire -json` per run into it, read-only; delete it after.

- [2026-11-03] `ingest.Load` + `ingest.Derive` are the analysis without writes; `ingest.Run` = Load + Derive + write. Reuse them for any read-only rerun of detectors (`daemon/detect.go`). A run in flight has no wire capture until it ends, so swallowed_argument learns argument names only from its own calls.
- [2026-11-03] Tool-call error classes already exist (070 §2, `ErrorClass` in events.go, on `store.ToolCall.ErrorClass`); never write a second classifier.

- (2026-11-02) The 2026-11-01 audit's NOT BUILT/PARTIAL rows were partly grep guesses: a re-check found 17 already built and 20 with wrong evidence. Before building an audit gap, verify it against the code (or delegate a read-only re-check).
- (2026-11-02) Mutating API calls carry `Automaticum-Client` (CLI `--client`, default user@host); the daemon logs `msg=change client=...` for every non-GET, non-dry-run call. compareView takes `pool`: runs off the latest task version are Excluded unless pooled; the cell passes pool=true.
- 2026-11-02: a regex for an argument name before a colon must allow a closing quote between (JSON writes `"start_line":`); backup_models' scan pattern did not, but its data held the unquoted form (`,start_line:90`).
- 2026-11-02: Docker's GET /containers/{id}/changes lists a container's mount points (and the directories above them) as added/changed even though writes under a mount are not changes; filter them (docker.notMounts) before reporting files written outside the workspace.
- 2026-11-02: "the batch's baseline" (070 §1.1) is undefined since a batch is one arm; carried as O40, not guessed.
- 2026-11-02: before writing "the SDK emits no event for X", search the bundle for notice kinds (`kind:"..."`); one run without the feature on proves nothing. The change protocol has atomic_transaction/atomic_empty_attempt/atomic_plan notices.
- 2026-11-02: the SDK's struggle detector (core class Pa, observer Tl) is identical 4.100.250-269; it is only attached to the main agent when escalation tools or spawn_agent exist, and inspects only at content_end/iteration_end.
- (2026-11-01) encoding/json decodes into an existing map by merging keys: a test that polls into one struct keeps stale keys. Decode into a fresh value each time.
- (2026-11-01) The SDK already waits and retries infrastructure errors (llms stream wrapper 3 attempts, 250 ms / 2 s; agents provider_error_retry with backoff; turn_fault_recovery). automaticum's job is the verdict: `run.Fault` (ported from llms `Kr`) makes such an end ABORTED unless the oracle passed.
- (2026-11-01) Daemon tests: the fakeRunner's records carry no `Arm` JSON, so a retry of a run it ended is refused ("the record does not say which arm it was"); only runs that never started (SKIPPED, with `provenance.asked`) retry in the rig. `batch pause` answers 202 when it holds a run in flight. `RunView.Verdict` is a string: compare with `string(store.Pass)`.
- (2026-11-01) `Executor.Release` is a no-op on a run that is not held, and also cancels a hold asked for that the runner has not reached: safe to call on every run a drain touches (D119).
- (2026-11-01) The regression view is `compareView(..., split="build")`: groups by arm NAME + task + the basis "harness build" fact, because an SDK change changes the arm id.

- **Project:** Go harness for coding agents on @cerebriline/sdk. Truth: docs/specs plus decisions.md. archive/ is input only; the old harness is READ-ONLY.
- During a batch: no lane image or bin/ rebuild, no daemon restart.
- Host GCC 10 can't link DuckDB: importers of `internal/store/duck` use `make build`. New store command: `store_cgo.go` plus stub in `store_nocgo.go`.
- The store's old corpus has private strings; never quote it in tracked files. Streams reach 5 GB, so corpus walks are daemon jobs.
- Config: `template = true` arms are extend-only. Use `parse()`, since Go `flag` stops at the first positional. Zero a struct before decoding JSON. Drop a field from Resolved() if false must not change the arm's identity.
- Runs: a run row needs a verdict; in-flight runs live in `lane_state`. Verdict rules live only in `run.Decide`. A new `store.State` method must be added to the fakes in the daemon and run tests.
- Ingest: add new detectors to `owned`; bump `AnalysisVersion` when `Versions` changes.
- SDK builds: `runner/sdk/<build>/`; lockfile from the Node 22 container, then `sdk add`, commit the hash. Bundles are mounted read-only, never in images. `Run.Harness` holds only the release confirmed at run time.
- Wire: settings read-back proves nothing; check the first `/api/chat` request carrying `tools`. On OpenAI-compatible endpoints the arm must own thinking.
- SDK: a run ends on the second text-only reply. It spawns `rg` and `git`, so foreign lane images need both. The reasoning guard ends a run after `maxConsecutiveTrips` cut turns unless the host answers `continue`. Read extension source with `git show v4.100.25x:<path>`.
- Export spans are code points; guard `verdict.chars` is UTF-16 units.
- Runner: a new file goes on the COPY line of images/base/Dockerfile. After stub.toml or base.toml changes, regenerate `runner/test/arm-stub.json`. Run tests with `--network none` on the pinned Node image.
- Gates: `automaticum oracle verify <task>` per task hash. `internal/gate` imports `internal/run`, never the reverse.
- Human-read output goes through `internal/human`; `--json` stays raw. Use `fail()`, because `do()` drops error bodies.
- llama-server: `/props` is at the root; `n_ctx` is per slot.
- Docker: a tmpfs takes the target directory's owner and mode, so oracle `writable` paths use `exec,mode=1777`. CapDrop ALL stops even uid 0 chrooting, so `run_as = "root"` adds the default caps. Local images have no repo digest, so an imported suite's oracle uses the source image by digest.
- Lane layers `COPY --from` the oracle image digest; check its Debian release first (bug-032).

## Do-Not-Repeat
- 2026-11-02: deploying to a lane host, copy binaries to `bin/<name>.new` and swap with `mv` after the idle check; never scp straight into `bin/` (it overwrote the CLI with no `.prev` copy; the busy daemon refused the write).

- 2026-11-01: owner: "it can't be something is in the specs and not developed!" A spec requirement is built, or the spec says plainly "not built" with its milestone, or a decision drops it. Never leave a requirement (090 §5 wait, the mailbox) in the spec with no milestone and no code. Re-audit with docs/audit/ after each milestone.
- 2026-11-01: automaticum must ship ready-made waiters for client sessions (`automaticum wait ...`, `--each` for a Monitor, `--next --seen N` for re-arming). Never make a session write its own polling loop.
- 2026-10-29: Caliber's hook rewrites CALIBER_LEARNINGS.md after the staged gate; run the private-string pattern over `git show HEAD` after every commit and fix hits before pushing.
- 2026-10-29: installation data goes in the installation's configuration directory, never a second "private" location beside the repo.
- 2026-10-25: "must declare" means a config declaration, not a heuristic. Grow the tool for a gap; never recommend leaving tasks out.
- 2026-10-25: in hand-written TOML, write top-level keys before the first table.
- 2026-10-25: don't rewrite catalogue family or size from a model "description"; use `notes` and ask.
- 2026-10-25: oracle proofs are per installation; verify on each lane host too (bug-038).
- 2026-10-25: rebuild the base on a host before a stub run on a new layer (bug-041).
- 2026-10-25: don't quietly bend an owner decision to fit an import; record it and open an O item.
- 2026-10-25: never pkill/pgrep a pattern that appears in the same command.
- 2026-10-24: the private-string gate fails closed: use NUL-separated names, accept grep exits 0, 1 or 123 only, never chain the commit with `;`, never write patterns to tracked files, read false positives.
- 2026-10-24: a noticed gap is a design flaw; name and fix it.
- 2026-10-23: sampler and arm values come only from the owner; show them before the first run. "128k" means 131072.
- 2026-10-23: opencoti or xollama servers get that SDK provider, never openai-compatible.
- 2026-10-23: a batch id is not its run ids' prefix; read `batch` from `run show -json`.
- 2026-10-23: on a new llama-server, check chat_format and reasoning_format in /props; tell the server's owner if "Content-only" or "none".
- 2026-10-23: never match guard sentences; use SDK notices.
- 2026-10-23: never pin one Docker API version; use MinAPIVersion (bug-027).
- 2026-10-14: check `Request.OpenAI` before expecting `options.*` (bug-019). A failed request is not a missing finish reason (bug-020).
- 2026-10-13: never invent detector, guard or limit numbers. Never bake an arm variable into an image.
- 2026-10-13: xollama's GPU host is `windows-2`. Job waiters need an exit-code file, a failure exit and a time bound.

## Decision Log

- [2026-11-03] D134: duplicate_definitions (tool-call string argument redefining names > 3 times). Owner: "build it with your recommendation" = threshold from data: max over base-model + stub runs on blackswan-2 (371 runs) was 3; trained broke at 6/18/33/492.

- [2026-11-03] D133: `automaticum detect` over any set of runs (070 §3.3). Owner: "there should be a command in automaticum to do it at any time ... single run or scanning multiple results, cohorts, lanes" and "make detect broad and covering all possible use cases". Read-only by default, POST records; in-flight runs partial.

The full list is in docs/specs/decisions.md; keep this log in step with it.

- D1-D101 (2026-10-12 to 10-23): Go on hosts, Node in lanes; Docker, TOML; DuckDB, Lance, blobs; automaticum never launches model servers.
- D102-D106: export conversations, SDK request capture, public training pool.
- D107-D108: batch limits override; `stop_on` alone decides the reasoning-loop limit.
- D109-D114: tbench2 held out; root by declaration; collect paths; task limits; runner under /opt/automaticum; vendor anything.
- D115-D117: held-out batches; notice metric keys; tool-call export spans.
- 2026-10-29 D118: arms and models live in `<config>/arms` and `<config>/models`; the repo ships examples only.
- 2026-11-01 D119 (was O30): a drain (lane, quarantine, daemon, SIGTERM) releases a held run so it finishes with a verdict; the lane then stays drained.
- 2026-11-01 D120-D125 (O31, O33-O36, O38, the recommendations): cohort in the campaign file `[cohort] name/variable` + `cell run --cohort`; rescore queued on the cell lane, records beside runs, old result file kept; `requires` labels on task and arm; lane `busy_command`; auto quarantine after 10 infrastructure ABORTs in a row (`quarantine_after`, 0 off); batch `--priority`, `lane reserve`. Not adopted: requeue (O32), driver watchdog (O37), level-based statistics (O39).

- 2026-11-01: D126 struggle signals = SDK signals + owner study (2nd compaction) + SDK struggle detector replayed; D127 edit-without-check = run_commands after the edit; D128 turn pathologies: build what data defines; D129 edit oscillation & re-read loop = ported SDK call guard on writing/other tools, near-duplicate rounds held.
- 2026-11-02: D130 (O40): the per-run report's baseline is the campaign's or cohort's baseline arm, else the parent arm. D131: swallowed-argument detector (a schema argument of the tool missing from the call, named with a colon inside a string argument), suggested by backup_models.
- 2026-11-02: D132: tokens stored hashed: daemon token_file holds the token or sha256:<hex>, the daemon keeps only the hash; the CLI reads client_token_file, else a plain token_file.
