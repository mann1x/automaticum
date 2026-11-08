# automaticum

@.wolf/OPENWOLF.md

Follow `.wolf/OPENWOLF.md` every session. Check `.wolf/anatomy.md` before reading files and `.wolf/cerebrum.md` before generating code.

## Status

- Milestone 1 (store spike and legacy importer) is approved and in progress; the application code so far is the store in `internal/store/`, the legacy importer in `internal/legacyimport/`, the configuration loader in `internal/config/`, the stub model server in `internal/stubmodel/`, the Docker Engine client in `internal/docker/`, the task loader in `internal/task/`, the run executor in `internal/run/`, the SDK builds in `internal/sdk/`, the formatter for values a person reads in `internal/human/`, the statistics in `internal/stats/`, the waits in `internal/wait/`, the detector library in `internal/detect/`, the gates in `internal/gate/`, the ingest in `internal/ingest/`, the export of training traces in `internal/export/`, the Terminal-Bench 2.0 importer in `internal/suite/tbench2/`, the daemon in `internal/daemon/` and `cmd/automaticumd/`, the runner in `runner/` and the command line in `cmd/automaticum/`. The source of truth is the spec set in `docs/specs/` (`000-overview.md` to `110-acceptance-and-roadmap.md`, plus `decisions.md`). The two input documents are in `docs/specs/archive/` and are not edited.
- automaticum is a test harness that runs coding agents against tasks with a verdict, on lanes it orchestrates itself.
- Executable code today: tooling in `.wolf/hooks/` (Node ESM) and `.claude/hooks/` (Shell), the build setup (`go.mod`, `go.sum`, `Makefile`, `build/`), the store spike in `spike/001-store/`, the store in `internal/store/`, the legacy importer in `internal/legacyimport/`, the configuration loader in `internal/config/`, the stub model server in `internal/stubmodel/`, the Docker Engine client in `internal/docker/`, the task loader in `internal/task/`, the run executor in `internal/run/`, the SDK builds in `internal/sdk/`, the formatter for values a person reads in `internal/human/`, the statistics in `internal/stats/`, the waits in `internal/wait/`, the detector library in `internal/detect/`, the gates in `internal/gate/`, the ingest in `internal/ingest/`, the export of training traces in `internal/export/`, the Terminal-Bench 2.0 importer in `internal/suite/tbench2/`, the daemon in `internal/daemon/` and `cmd/automaticumd/`, the runner in `runner/` (Node ESM), the lane images in `images/`, the tasks in `tasks/`, the example arms in `examples/config/arms/`, the model catalogue in `models/`, the example configuration in `examples/config/` and the command line in `cmd/automaticum/`.
- Do not invent Go packages or build commands outside a milestone plan the owner has approved: check `docs/specs/110-acceptance-and-roadmap.md` section 3 and `docs/specs/decisions.md` first.

## Owner decisions

- All owner decisions are in `docs/specs/decisions.md` (D1 onwards). Read it before proposing anything.
- Go daemon `automaticumd`; a host runs only the Go application; Node only inside containers.
- Lanes run in Docker; remote hosts use agent mode only; configuration is TOML; store is DuckDB + Lance + blobs on disk.
- Anything marked `**Open**` in a spec is a question, not a decision. Only the owner decides: ask, never close one by default.

## Vocabulary

- Use the terms of `docs/specs/000-overview.md` section 3 exactly: `task`, `suite`, `arm`, `run`, `batch`, `cell`, `cohort`, `campaign`, `basis`, `lane`, `endpoint`, `oracle`, `gate`, `finding`, `runner`.
- The `oracle` is run by the harness after the agent stops, never by the agent.
- A `gate` must hold before a `run` starts; a `lane` runs one `run` at a time.

## Commands

Spec navigation (grep before reading whole files):

```bash
grep -n '^#' docs/specs/0*.md docs/specs/1*.md
grep -n -F '**Open' docs/specs/*.md
```

OpenWolf hooks (wired in `.claude/settings.json`):

```bash
node .wolf/hooks/session-start.js
node --check .wolf/hooks/shared.js
openwolf designqc
```

Caliber config sync (uses the hooks in `.claude/hooks/`):

```bash
caliber --version
caliber skills --query "go docker"
bash .claude/hooks/caliber-check-sync.sh
```

Native (cgo) builds run only in the build containers (`Makefile`; `SPIKE_ROOT` is a local directory for caches and sources, never stored in a tracked file). `make build` builds `cmd/automaticum` into `bin/automaticum` and `cmd/automaticumd` into `bin/automaticumd`, and `make bench` builds `spike/001-store/bench` into `bin/bench`, all in the link image. `make test` runs the pure-Go packages on the host (`internal/store/blob`, `internal/legacyimport`, `internal/config`, `internal/stubmodel`, `internal/run`, `internal/sdk`, `internal/human`, `internal/stats`, `internal/detect`, `internal/gate`, `internal/ingest`, `internal/export`, `internal/suite`, `internal/wait` and `internal/daemon` with `-race`; `internal/task` and `internal/docker` with `-short`; `cmd/automaticum` with `CGO_ENABLED=0`); `make test-store` runs `internal/store/...` in the link image and needs the archive from `make lance-c` first, as does `make bench`:

```bash
make images
make lance-c SPIKE_ROOT=<dir>
make build SPIKE_ROOT=<dir>
make bench SPIKE_ROOT=<dir>
make test
make test-store SPIKE_ROOT=<dir>
make shell SPIKE_ROOT=<dir>
```

## Layout

- **Specs** (`docs/specs/`): `000-overview.md` · `010-architecture.md` · `020-run-record.md` · `030-configuration.md` · `040-runner.md` · `050-orchestration.md` · `060-tasks-and-suites.md` · `061-suite-bfcl.md` · `062-suite-swe-bench.md` · `063-suite-multi-language.md` · `064-engine-adapters.md` · `065-cell-contract.md` · `066-suite-terminal-bench.md` · `070-diagnostics.md` · `080-store.md` · `090-interfaces.md` · `100-legacy-import.md` · `110-acceptance-and-roadmap.md` · `decisions.md`
- **Inputs** (`docs/specs/archive/`): `automaticum-draft-spec-0.4.md` · `omnimergekit-evaluation-requirements-0.1.md`
- **Build** (repo root and `build/`): `go.mod` · `go.sum` · `Makefile` · `LICENSE` · `.gitattributes` · `.gitleaks.toml` (secret scan, run fail-closed by the pre-commit hook on the staged diff) · `build/Dockerfile.build` · `build/Dockerfile.cross`
- **Store spike** (`spike/001-store/`): `REPORT.md` · `results/` · `linkcheck/README.md` · `bench/` (`main.go` · `state.go` · `vectors.go` · `rangeread.go` · `fs_linux.go` · `corpus.go` · `dedup.go` · `embed.go`)
- **Store** (`internal/store/`): `store.go` · `blob/` (`doc.go` · `blob.go` · `put.go` · `read.go` · `archive.go` · `check.go` · `format.go` · `FORMAT.md`) · `duck/duck.go` · `lance/lance.go` · `storetest/` (`doc.go` · `crash_test.go`)
- **Legacy importer** (`internal/legacyimport/`): `doc.go` · `importer.go` · `scan.go` · `secondpass.go` · `parse.go` · `stream.go` · `mapping.go` · `sink.go` · `verify.go` · `types.go` · tests (`fixture_test.go` · `import_test.go` · `parse_test.go` · `secondpass_test.go` · `stream_test.go`)
- **Configuration** (`internal/config/`): `arm.go` · `campaign.go` · `cell.go` · `local.go` · `model.go` · `config_test.go` · `campaign_test.go` · `cell_test.go` · `model_test.go` · `runner_fixture_test.go`
- **Stub model server** (`internal/stubmodel/`): `stubmodel.go` · `stubmodel_test.go`
- **Docker client** (`internal/docker/`): `docker.go` · `docker_test.go`
- **Task loader** (`internal/task/`): `task.go` · `exec.go` · `user.go` · `pin.go` (`LoadUnpinned`, `Locate`, `WritePins`: a task pinned by `task pin`, D140) · `task_test.go` · `user_test.go` · `pin_test.go`
- **Run executor** (`internal/run/`): `run.go` · `protocol.go` · `probe.go` · `props.go` · `verdict.go` · `rescore.go` (`Rescore`: a verdict decided again from the recorded facts, D121; `Untar`) · `fault.go` (`Fault`: the SDK's refusal and transport patterns, ported; a run ending on one is ABORTED, 050 §4) · `held.go` (`HeldStream`, `Held.Proven`: a held stream is decided by the oracle, D136) · `held_test.go` · `slots.go` (`ReadSlots`, `watchSlots`: the endpoint's `/slots` read every 10 s during a run, the `slots` blob, for `held_stream`) · `slots_test.go` · `run_test.go` · `identity_test.go` · `props_test.go` · `fault_test.go` · `rescore_test.go`
- **SDK builds** (`internal/sdk/`): `sdk.go` · `sdk_test.go`
- **Human formatter** (`internal/human/`): `human.go` · `human_test.go`
- **Statistics** (`internal/stats/`): `stats.go` · `stats_test.go`
- **Detector library** (`internal/detect/`): `detect.go` · `guards.go` · `controls.go` · `spans.go` (`Spans`, `CycleStart`: where a hit's repeats stand) · `callguard.go` (`CallGuard`: the SDK's failed, refused and same-result call rules, D129) · `guards258.go` (`RulesFor`, the 4.100.258 reasoning-loop rules: block cycles, what the model was given) · `struggle.go` (`Struggle`: the SDK's struggle detector replayed, `StruggleFailure`, D126) · `swallow.go` (`SwallowedArgs`: an argument written inside another's string, D131; the newline form from library version 5) · `duplicates.go` (`DuplicateDefinitionCalls`: a string argument defining names again in their class more than once, measured, D134; `StringArgs` reads cut-off JSON) · `restated.go` (`RestatedText`: the SDK's restated-lines measure, long lines written a third time) · `repguard.go` (`GuardText`, `GuardReasoning`, `GuardArgument`: the repetition guard's first would-trip point per output, D135) · `testdata/swallow_newline_calls.json` (a client's 18 swallowed calls, the positive control) · `testdata/duplicate_definitions_call.json.gz` (the cut-off 215,471-character editor call, D134's positive control) · `detect_test.go` · `reference_test.go` · `spans_test.go` · `callguard_test.go` · `guards258_test.go` · `struggle_test.go` · `swallow_test.go` · `duplicates_test.go` · `restated_test.go`
- **Export** (`internal/export/`): `export.go` (capture responses with their tool calls, guard cuts from the event log, loop spans with `field` `reasoning`, `content` or `tool_calls[<i>].function.arguments`, capped requests, D102/D104/D106/D117) · `export_test.go`
- **Gates** (`internal/gate/`): `gate.go` · `gate_test.go`
- **Waits** (`internal/wait/`, 090 §5.1): `wait.go` (`Parse` a condition such as `pass>=5 or fail>=3` for a kind; `Runs`, `Lane`, `Idle`, `Campaign` make a `Snapshot`; `Over` when it never will hold) · `wait_test.go`
- **Ingest** (`internal/ingest/`): `analyse.go` · `canary.go` · `cap.go` · `detectors.go` · `events.go` · `sampler.go` · `wire.go` · `wirearm.go` · `render.go` (the transcript for `run tail` and `logs --render`) · `oracle.go` (`PartialScore`, `secondary_oracle` and `claimed_success_oracle_no` findings) · `turns.go` (turn pathologies, hygiene, guard trips and replay, re-reads, edits, D127/D128) · `struggle.go` (`struggle_signals`, `StruggleTriggers`, the change protocol's `transactions.*`, D126) · `integrity.go` (`outside_workspace`: what the agent's container wrote outside its mounts) · `swallow.go` (`swallowed_argument` with the schemas from the wire, D131; `Schemas`, `SwallowFirings`) · `duplicates.go` (`duplicate_definitions` from the wire's calls, else the event stream's, D134) · `answers.go` (`WireAnswers`: an agent answer the event stream never had, such as the SDK's retry after the output limit, read from the wire as a block, by time, bug-077) · `repguard.go` (`repetition_guard`: where the repetition guard would have tripped, observe-only, D135; `GuardFires`) · `held.go` (`held_stream`: the SDK gave the server up as silent while it still decoded, its parser holding the model's output; from the record, the `slots` blob or the SDK's message, D136) · `outcomes.go` (`Outcomes`: a run's calls by tool, failed by error class, refused, unreadable, not offered, control-token argument names, for `detect`) · tests (`answers_test.go` · `held_test.go` · `repguard_test.go` · `duplicates_test.go` · `outcomes_test.go` · `ingest_test.go` · `turns_test.go` · `struggle_test.go` · `integrity_test.go` · `oracle_test.go` · `sample_test.go` · `canary_test.go` · `cap_test.go` · `openai_test.go`)
- **Terminal-Bench 2.0 importer** (`internal/suite/tbench2/`, 066): `tbench2.go` (reads a source task; writes `task.toml`, the lane layer and `NOTES.md`) · `import.go` (pins the image by digest, copies `/app` out as the seed, vendors the pytest wheels, pins the verifier, makes the gold fixture with `solve.sh`, keeps a task only if `Verify` holds) · `verifier/` (`main.py` · `build.py`: the zipapp the oracle runs, embedded) · `tbench2_test.go`
- **Daemon** (`internal/daemon/`): `daemon.go` · `api.go` · `compare.go` · `basis.go` · `campaign.go` · `cell.go` · `control.go` · `canary.go` · `modec.go` · `export.go` (`GET /v1/runs/{id}/export`; a batch started held out, D115, only with `?heldout=1`) · `wait.go` (`GET /v1/wait/{kind}`: held until the condition holds, at most 55 s; `batchRuns`, `statusSnapshot`) · `mutate.go` (dry runs, the `Idempotency-Key` middleware) · `lists.go` (`GET /v1/batches`, `GET /v1/lanes/{lane}`) · `report.go` (`GET /v1/report/census`, `GET /v1/report/struggle` (D126), `GET /v1/report/guards`: the guards' review queue) · `heartbeat.go` (every ten minutes to the log, `GET /v1/heartbeat`, 050 §6) · `detect.go` (`GET|POST /v1/detect`: every detector again on any set of runs, in flight included, streamed as JSON lines; POST records, D133) · `archive.go` (blobs older than `archive_after_days` moved to the archive tier at start and daily, D31) · `cohort.go` (cohorts, D120: `cohortFacts`, `cohort ls|show`, the confounded label) · `rescore.go` (`automaticum rescore`, D121: queued per lane, one oracle at a time, records beside the run, the cell's latest scoring) · `baseline.go` (`run show`'s arm against its campaign's baseline or its parent, D130) · `guardwatch.go` (`watchGuard`: an arm whose `guards.stop_on` names `repetition_guard` is stopped by the daemon at the guard's first trip, as a failed run, D135) · `lanerules.go` (automatic quarantine after `quarantine_after` infrastructure failures, D124; the lane's `busy_command`, D123; `lane reserve|unreserve`, D125) · `daemon_test.go` · `detect_test.go` · `guardwatch_test.go` · `held_test.go` (`run held`, D136) · `wait_test.go` · `notify_test.go` (`[notify] command`, run and batch notices) · `modec_test.go` · `export_test.go` · `mutate_test.go` (dry runs, keys, lane pause, a drain releasing a held run, D119) · `lists_test.go` · `report_test.go` (census, regression, endpoint capacity, heartbeat, automatic quarantine, busy command, lane labels, priorities and reservations) · `cell_numbers_test.go` (verdict-conditioned cell numbers, streaks) · `rescore_test.go` · `cohort_test.go`
- **Daemon command** (`cmd/automaticumd/`): `main.go`
- **Runner** (`runner/`, Node ESM, runs only inside the lane container, started as `/opt/automaticum/bin/node`, D113): `PROTOCOL.md` · `guards.mjs` · `package.json` · `plan.mjs` · `runner.mjs` · `session.mjs` · `tools.mjs` (routes the SDK's own `rg` calls to `/opt/automaticum/bin`, off the agent's PATH) · `user.mjs` · `wire.mjs` · `sdk/` (`4.100.250/`, `4.100.251/`, `4.100.258/`, `4.100.259/`, `4.100.260/`, `4.100.262/`, `4.100.266/`, `4.100.268/`, `4.100.269/`, `4.100.274/` and `4.100.275+da478ecc8a/` (a local build from the release's packed packages, not on the registry), each with `package.json` · `package-lock.json` · `sdk.toml`) · `test/` (`arm-stub.json` · `guards.test.mjs` · `plan.test.mjs` · `session.test.mjs` · `tools.test.mjs` · `user.test.mjs` · `wire.test.mjs`)
- **Lane images** (`images/`): `base/Dockerfile` · `layers/{typescript,python,go,rust,cpp,dotnet}/Dockerfile` (each toolchain copied from its oracle image by digest; `cpp` starts from the Debian 13 gcc image, so rebuild the base first) · `layers/tb2-<task>/Dockerfile` (written by `suite import tbench2`: the task's image by digest with Node, the runner and the base's static ripgrep added under `/opt/automaticum`, and nothing else; rebuild the base first)
- **Tasks** (`tasks/`): `custom/manic_miner/` (`task.toml` · `README.md`) · `demo/answer/` (`task.toml`) · `demo/scripted/` (`task.toml`, a scripted user) · `demo/cancel/` (`task.toml`, cancel and restore) · `demo/plan/` (`task.toml`, plan then a switch to act) · `demo/plan-switch/` (`task.toml`, the agent's switch_to_act_mode) · `demo/fork/` (`task.toml`, a fork of the session, D86) · the training pool (public, `training = true`, D105; the loader refuses a `train-*` task without it and a training task outside a `train-*` suite): `train-ml/` (`c_parser_bugfix` · `cpp_matrix_feature` · `cs_buildfix` · `cs_inventory_refactor` · `go_kvstore_feature` · `go_workerpool` · `py_ledger` · `py_reportgen` · `rust_cli_buildconfig` · `rust_tokenizer_bugfix` · `ts_cache_refactor` · `ts_scheduler`) · `train-sh/` (`sh_backup_rotate` · `sh_config_migrate` · `sh_logtriage` · `sh_user_audit`) · `train-game/space_invaders/`, each with `task.toml` · `checker.json` · `checker/` · `fixtures/` · `oracle/` (`build.mjs` · `run_tests.logic.js`) · `seed/` and usually `NOTES.md` · the held-out suite `tbench2/` (066: Terminal-Bench 2.0 imported by `automaticum suite import tbench2`, never edited by hand; `circuit-fibsqrt` · `feal-linear-cryptanalysis` · `regex-chess` · `schemelike-metacircular-eval`, each with `task.toml` · `NOTES.md` · `seed/` · `fixtures/gold/` · `oracle/` (`main.py` · `build.py` · `requirements.txt` · `tests/` · `wheels/`, and `built.txt` where a package had no wheel); `LICENSE` is the source's; `import.toml`, the subset and what each task declares, is the one file written by hand) · a private tasks root named by `private_tasks` in the local configuration (`examples/config/automaticum.toml`) is read beside `tasks/` in the same `<suite>/<task>/task.toml` layout; every task there says `private = true` and carries a `canary`
- **Campaigns** (`campaigns/`): `stub-demo.toml`
- **Example configuration** (`examples/config/`): `automaticum.toml` · `endpoints/local-ollama.toml` · `endpoints/stub.toml` · `lanes/lane-1.toml` · `examples/systemd/automaticumd.service` (the daemon as a systemd service) · `arms/` (the harness's own: `base.toml`, the template every arm extends, whose `[harness] sdk_release` is the default release `4.100.274` (D142; an arm that states its own keeps it), and the stub arms `stub*.toml`, also used by the tests) · `models/stub.toml`. An installation's own arms and models live in its configuration directory (`<config>/arms`, `<config>/models`), never in the repo (D118)
- **Command line** (`cmd/automaticum/`): `main.go` (`automaticum version` and the `<noun> <verb>` dispatch) · `config_cmds.go` (`automaticum arm validate` · `automaticum arm show` · `automaticum arm diff` · `automaticum task ls` · `automaticum task show` · `automaticum task build` · `automaticum oracle verify` · `automaticum lane ls` · `automaticum endpoint ls`) · `image_cmds.go` (`automaticum image build` · `automaticum image ls`) · `sdk_cmds.go` (`automaticum sdk add` · `automaticum sdk ls` · `automaticum sdk verify`) · `check_cmds.go` (`automaticum task pin [--write]`, D140 · `automaticum config check`, D141) · `check_cmds_test.go` · `stub_cmds.go` (`automaticum stub serve`) · `suite_cmds.go` (`automaticum suite import tbench2`) · `client_cmds.go` (`automaticum status` · `automaticum run start` · `automaticum run ls` · `automaticum run show` · `automaticum run tail` · `automaticum run stop` · `automaticum run abort` · `automaticum run pause` · `automaticum run resume` · `automaticum run invalidate` · `automaticum run held` (D136) · `automaticum run identify` · `automaticum logs` · `automaticum notices` · `automaticum doctor` · `automaticum reanalyse`; these talk to the daemon) · `detect_cmds.go` (`automaticum detect`: a text file, `--self-test`, or every detector again on any set of runs with totals and `--fail-if`, D133; talks to the daemon) · `detect_cmds_test.go` · `compare_cmds.go` (`automaticum compare` · `automaticum basis show|diff`; talk to the daemon) · `campaign_cmds.go` (`automaticum campaign start|ls|show|pause|resume|stop|abort|retry`; talk to the daemon) · `cell_cmds.go` (`automaticum cell run`; talks to the daemon) · `control_cmds.go` (`automaticum lane drain|pause|quarantine|abort|resume` · `automaticum batch show|pause|resume|stop|abort|retry|limits` · `automaticum run retry`; talk to the daemon) · `cohort_cmds.go` (`automaticum cohort ls|show` · `automaticum export cohort`, D120; talk to the daemon) · `rescore_cmds.go` (`automaticum rescore <batch> [--out D]`, D121; talks to the daemon) · `export_cmds.go` (`automaticum export conversations`: `<out>/conversations/<run>/` · `loop_spans.jsonl` · `manifest.jsonl` · `runs/<run>.json`; `--heldout` also exports batches started `run start --heldout`, D115; talks to the daemon) · `wait_cmds.go` (`automaticum wait run|batch|cell|campaign|lane|idle`, `--until`, `--next`, `--each`, exit 0/4/5; against a daemon without the route it reads the state every `--poll`; talks to the daemon) · `wait_cmds_test.go` · `show_cmds.go` (`lane show` · `batch ls` · `cell show|ls` · `arm ls` · `endpoint show|models|probe` · `task verify`) · `timeline_cmds.go` (`automaticum run timeline`, 070 §1.1; `-requests`, D139) · `timeline_cmds_test.go` · `report_cmds.go` (`report run|cell|batch|ab|regression|census|struggle|guards|skew` · `export cell|campaign|run`) · `store_cgo.go` (`automaticum legacy import` · `automaticum store check`; cgo builds only) · `store_nocgo.go` · `config_cmds_test.go` · `exit_test.go` · `report_cmds_test.go`
- **Audit** (`docs/audit/`): `2026-11-01-spec-vs-code.md` (every spec item against the code at 180cc97: built, partial, not built, scheduled, open)
- **Local, untracked** (`docs/local/`): host mapping and private paths
- **OpenWolf state** (`.wolf/`): `OPENWOLF.md` · `anatomy.md` · `cerebrum.md` · `memory.md` · `buglog.json` · `identity.md` · `config.json` · `token-ledger.json` · `cron-manifest.json` · `cron-state.json` · `suggestions.json` · `designqc-report.json` · `reframe-frameworks.md` · `daemon.log`
- **OpenWolf hooks** (`.wolf/hooks/`): `session-start.js` · `pre-read.js` · `post-read.js` · `pre-write.js` · `post-write.js` · `stop.js` · `shared.js` · `package.json`
- **Claude config** (`.claude/`): `settings.json` · `rules/openwolf.md` · `rules/runner-protocol.md` (runner protocol, guards and `[guards]` arm settings) · `rules/wire-arm.md` (wire detector: `ReadWire`, `CompareWire`, `WireArmVersion`) · `rules/task-loader.md` (task loader: `task.Roots`, private tasks, `train-*` suites and `training = true`, D105) · `rules/lane-images.md` (lane image layers: digests, offline toolchains, `cpp` from the gcc image) · `rules/spec-conventions.md` · `rules/wolf-hooks.md` · `hooks/caliber-check-sync.sh` · `hooks/caliber-session-freshness.sh`
- **Skills** (`.claude/skills/`): `add-wolf-hook/` · `draft-spec-section/` · `find-skills/` · `learned-new-host-install/` (bring up automaticum on a fresh host, D93/D95) · `learned-lane-host-deploy/` (put a new build on a running lane host: stage, idle-gated swap, unit, repo checkout, rollback) · `learned-new-sdk-release/` (provision, pin, verify and adopt a new `@cerebriline/sdk` release under `runner/sdk/<ver>/`) · `log-wolf-bug/` · `record-decision/` · `save-learning/` · `setup-caliber/`

## Conventions

- Add an entry to `.wolf/anatomy.md` whenever you create, rename or delete a file.
- Append one row to `.wolf/memory.md` after each significant action.
- Read `.wolf/buglog.json` before a fix and append to it after.
- Record owner decisions in `docs/specs/decisions.md` and in `.wolf/cerebrum.md` under `## Decision Log`, with the date.
- The repo is public: tracked files use the neutral host names (`linux-gpu-1`, `linux-gpu-2`, `windows-1`, `windows-2`, `mac-1`, `blackswan-2`) and no absolute paths.
- Do not hand-edit `.claude/settings.json`; hooks there are managed by OpenWolf and Caliber.

## Tooling constraints

- Caliber must use the `claude-cli` provider; never the Anthropic API provider (no API key exists).
- `caliber init` and `caliber regenerate` are token-expensive: confirm timing with the user first.
- MCP servers available: `pgvector` (persistent memory), `context7` (library docs), `gitnexus` and `code-graph` (code analysis).
- Never write large files to `/tmp` (tmpfs); use persistent disk.
- Ask before unloading `ollama` models or freeing GPU VRAM; the user may be mid-session.

## Before Committing

**IMPORTANT:** Before every git commit, you MUST ensure Caliber syncs agent configs with the latest code changes.

First, check if the pre-commit hook is already installed:
```bash
grep -q "caliber" .git/hooks/pre-commit 2>/dev/null && echo "hook-active" || echo "no-hook"
```

- If **hook-active**: the hook handles sync automatically — just commit normally. Tell the user: "Caliber will sync your agent configs automatically via the pre-commit hook."
- If **no-hook**: run Caliber manually before committing:
  1. Tell the user: "Caliber: Syncing agent configs with your latest changes..."
  2. Run: `caliber refresh && git add CALIBER_LEARNINGS.md CLAUDE.md .claude/ 2>/dev/null`
  3. After it completes, briefly tell the user what Caliber updated. Then proceed with the commit.

**Valid `caliber refresh` options:** `--quiet` (suppress output) and `--dry-run` (preview without writing). Do not pass any other flags — options like `--auto-approve`, `--debug`, or `--force` do not exist and will cause errors.

**`caliber config`** takes no flags — it runs an interactive provider setup. Do not pass `--provider`, `--api-key`, or `--endpoint`.

If `caliber` is not found, tell the user: "This project uses Caliber for agent config sync. Run /setup-caliber to get set up."
## Session Learnings

Read `CALIBER_LEARNINGS.md` for patterns and anti-patterns learned from previous sessions.
These are auto-extracted from real tool usage — treat them as project-specific rules.
## Context Sync

This project uses [Caliber](https://github.com/caliber-ai-org/ai-setup) to keep AI agent configs in sync across Claude Code, Cursor, Copilot, and Codex.
Configs update automatically before each commit via `caliber refresh`.
If the pre-commit hook is not set up, run `/setup-caliber` to configure everything automatically.
