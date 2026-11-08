# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-11-08

## User Preferences

- Only the owner turns an **Open** item into a decision. Carry a recommendation and ask in batches. Never quietly bend a decision to fit an import: record it and open an O item.
- Do the owner's setup steps in the order listed. GitHub repos go under `mann1x`.
- Public repo: neutral host names only, with no nicknames, private paths or addresses. The mapping is in docs/local/hosts.local.md (untracked).
- Data never goes on the dev NVMe or the old harness's spool NVMe. Use the disk named in docs/local.
- C# is a main language, so dotnet leads the language layers.
- Write durations as "8m 13s" or "1h 02m 05s", never bare seconds.
- Every run and report starts with model (family, variant), GPU, host, inference point and sampler.
- Fix forward and label each number's method.
- Arm, sampler, guard and threshold values come from the owner. They are arm config: never hard-coded and never baked into an image. Show them before the first run. "128k" means 131072.
- (2026-10-29) This session only develops automaticum. Client sessions write their own arms, models, endpoints and lanes in their configuration directory (D118) and start their own batches.
- Every spec requirement is either built, marked "not built" with its milestone, or dropped by a decision. Re-audit with docs/audit/ after each milestone. A noticed gap is a design flaw: name it and fix it.
- Ship ready-made waiters (`automaticum wait ...`, `--each`, `--next --seen N`). Never make a client write its own polling loop.

## Key Learnings

- Go harness for coding agents on @cerebriline/sdk. The truth is docs/specs plus decisions.md. archive/ is input only and the old harness is READ-ONLY. During a batch: no lane image or bin/ rebuild and no daemon restart.
- Host GCC 10 can't link DuckDB, so packages that import `internal/store/duck` use `make build`. A store command goes in `store_cgo.go` with a stub in `store_nocgo.go`. The old corpus has private strings (never quote it) and streams of up to 5 GB, so corpus walks are daemon jobs.
- Config: `template = true` arms are extend-only. Use `parse()`, because Go `flag` stops at the first positional. Drop a field from Resolved() if a false value must not change the arm's identity. Decode JSON into a fresh value, because encoding/json merges keys into an existing map.
- Runs: a run row needs a verdict. In-flight runs live in `lane_state`. Verdict rules live only in `run.Decide`. A new `store.State` method must also go into the fakes in the daemon and run tests. `run.Fault` (ported from llms `Kr`): the SDK already retries infrastructure errors, so a run that ends on one is ABORTED unless the oracle passed.
- Daemon: the regression view is `compareView(split="build")`. `pool` excludes runs on an older task version unless pooled. Mutating calls carry `Automaticum-Client`.
- Ingest: add new detectors to `owned`, and bump `AnalysisVersion` when `Versions` changes. `ingest.Load`+`Derive` is the analysis without writes, so reuse it for read-only reruns. Error classes already exist (`ErrorClass`): never write a second classifier.
- Event stream: a turn cut by the output or wall limit has no `content_end` (`Block.Unfinished`; it ends at its last delta). Its retry exists only in the wire (`WireAnswers`, bug-076/077). A cut tool call has input "{}", and its arguments exist only in the wire's `Request.Calls`. A run in flight has no wire capture yet.
- To set a threshold from data, use the learned-detector-measurement skill: a throwaway `go test -c` binary run read-only on the lane host and deleted afterwards.
- Docker:
  - `/changes` lists mount points, so filter them (`docker.notMounts`).
  - A tmpfs takes its target's owner and mode, so oracle `writable` paths use `exec,mode=1777`.
  - CapDrop ALL blocks a root chroot, so `run_as="root"` adds the default caps.
  - Local images have no repo digest, so an imported suite's oracle uses the source image's digest.
  - Lane layers `COPY --from` the oracle image's digest: check its Debian release first (bug-032).
  - Use MinAPIVersion and never pin one API version (bug-027).
- SDK: it is the owner's fork, the cline monorepo beside this one. Fix it on `main` with a Vitest and release it per `docs/protocols/BUILD-RELEASE-DEPLOY.md`. Releasing is outward-facing, so ask the owner first. Then pin it with learned-new-sdk-release and check that the ported guard code is unchanged (`git diff --stat v<old> v<new> -- sdk/packages`).
- SDK behaviour:
  - A run ends on the second text-only reply.
  - The SDK spawns `rg` and `git`.
  - The reasoning guard ends a run after `maxConsecutiveTrips` unless the host answers `continue`.
  - The struggle detector is attached only when escalation tools or spawn_agent exist.
  - Grep the bundle for `kind:"..."` before claiming the SDK sends no event for something.
  - Use notices and never match guard sentences.
- SDK builds live in `runner/sdk/<build>/`, with the lockfile from the Node 22 container. Bundles are mounted read-only. `Run.Harness` holds only the release confirmed at run time.
- Wire: a settings read-back proves nothing; check the first `/api/chat` request carrying `tools`. On OpenAI-compatible endpoints the arm owns thinking. opencoti and xollama servers get that SDK provider. Check `Request.OpenAI` before expecting `options.*` (bug-019).
- Export spans count code points, while guard `verdict.chars` counts UTF-16 units.
- Runner: a new file goes on the COPY line of images/base/Dockerfile. Regenerate `runner/test/arm-stub.json` after changing stub.toml or base.toml. Run tests with `--network none`.
- Gates: run `oracle verify` per task hash on each installation (bug-038). `internal/gate` imports `internal/run`, never the reverse.
- Human-read output goes through `internal/human`; `--json` stays raw. Use `fail()`, because `do()` drops error bodies.
- llama-server: `/props` is at the root and `n_ctx` is per slot. On a new server, check chat_format and reasoning_format.
- Verify an audit gap against the code before building it. A batch id is not its runs' prefix: read `batch` from `run show -json`.

## Do-Not-Repeat

- 2026-11-08: a tool hint that names an edit or a verdict must be proved by the authority it names (the parser, the oracle), never by a heuristic of its own. Its tests must use real code that passes that authority after the fix. The SDK's delimiter scan called a bracket-count edit 'checked' and deleted a correct } on the manic_miner seed (bug-082); its fixtures never parsed. vitest runs under Node and the CLI under Bun: test runtime-dependent code on both.
- 2026-11-07: in a stub smoke test, `run start` prints the batch id first. Take the run id with an anchored `grep -oE '^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}'`, check the stub's `/api/version` first, and stop the stub only after `wait run` returns 0.
- 2026-11-05: never chain `git push` after the `git show HEAD` gate. Run the gate, read it, then push in a separate call.
- 2026-11-04: before telling a client a config key, read `examples/config/automaticum.toml`. Its keys are top-level; there is no `[daemon]` table (bug-080).
- 2026-11-02: when deploying to a lane host, copy binaries to `bin/<name>.new` and `mv` them after the idle check. Never scp into `bin/`.
- 2026-10-29: the private-string gate fails closed:
  - Use NUL-separated names and accept only grep exits 0, 1 or 123.
  - Never chain the commit with `;` and never write patterns to tracked files.
  - Read false positives.
  - Caliber rewrites files after the staged gate, so rerun the gate over `git show HEAD` before pushing.
- 2026-10-25: "must declare" means a config declaration, not a heuristic. Grow the tool; never drop tasks.
- 2026-10-25: in hand-written TOML, put top-level keys before the first table. Don't rewrite catalogue family or size from a model description: use `notes` and ask.
- 2026-10-25: rebuild the base on a host before a stub run on a new layer (bug-041). Never pkill or pgrep a pattern that appears in the same command.
- 2026-10-13: never invent detector, guard or limit numbers. xollama's GPU host is `windows-2`. Job waiters need an exit-code file, a failure exit and a time bound.

## Decision Log

The full list is in docs/specs/decisions.md; keep this log in step with it.

- D1-D101 (10-12 to 10-23): Go on hosts, Node in lanes; Docker and TOML; DuckDB, Lance and blobs; automaticum never launches model servers.
- D102-D117: export of conversations, request capture, the public training pool, batch limits, `stop_on`, tbench2 held out, the runner under /opt/automaticum, held-out batches, export spans.
- 2026-10-29 D118: arms and models live in `<config>/arms` and `<config>/models`; the repo ships examples only.
- 2026-11-01 D119: a drain releases a held run. D120-D125: cohorts, rescore, `requires`, `busy_command`, `quarantine_after`, priority and reserve. D126-D129: struggle signals, edit without a check, turn pathologies, the ported call guard.
- 2026-11-02 D130: the baseline is the campaign's or cohort's baseline arm, else the parent arm. D131: swallowed_argument. D132: tokens are stored hashed.
- 2026-11-03 D133: `detect` over any set of runs. D134: duplicate_definitions (more than 3, from data). D135: the repetition guard, an observe-only detector with an arm opt-in to stop. D136: on a held stream the oracle decides.
- 2026-11-04 D137: private training tasks can be exported. D138: the idle limit counts arriving bytes. D139: per-request first byte and endpoint meta. D140: `task pin`. D141: `config check`.
- 2026-11-05 D142: the default SDK release is 4.100.274.