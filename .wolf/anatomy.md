# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-10-14T13:49:43.947Z
> Files: 228 tracked | Anatomy hits: 0 | Misses: 0

## ./

- `.gitattributes` — Git attributes (~20 tok)
- `.gitignore` — Git ignore rules (~73 tok)
- `CALIBER_LEARNINGS.md` — Caliber Learnings (~7228 tok)
- `CLAUDE.md` — automaticum (~3399 tok)
- `go.mod` — Go module definition (~303 tok)
- `go.sum` — Go dependency checksums (~1248 tok)
- `LICENSE` — Project license (~3029 tok)
- `Makefile` — Make build targets (~701 tok)
- `README.md` — Project documentation (~2524 tok)
- `spike.local.toml` — Untracked. Paths for the store spike on this installation. (~243 tok)

## .caliber/

- `.caliber-state.json` (~35 tok)
- `learnings-archive.md` — Caliber Learnings Archive (~28655 tok)
- `manifest.json` (~728 tok)
- `refresh-hook.log` (~0 tok)
- `score-history.jsonl` (~959 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/

- `CLAUDE.md` — OpenWolf (~57 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/.claude/rules/

- `openwolf.md` (~313 tok)

## .caliber/cache/

- `fingerprint.json` — Declares usage (~95828 tok)

## .caliber/learning/

- `current-session.jsonl` (~12293 tok)
- `finalize.log` (~0 tok)
- `roi-stats.json` (~27022 tok)
- `state.json` (~41 tok)

## .claude/

- `settings.json` (~974 tok)
- `settings.local.json` (~35 tok)

## .claude/hooks/

- `caliber-check-sync.sh` — Don't block headless claude sessions spawned by caliber itself (e.g. during caliber refresh) (~722 tok)
- `caliber-session-freshness.sh` — Don't run inside a caliber-spawned headless session — the systemMessage would (~226 tok)

## .claude/rules/

- `openwolf.md` — OpenWolf protocol (~207 tok)
- `spec-conventions.md` — Spec conventions (~371 tok)
- `wolf-hooks.md` — Hook conventions (~250 tok)

## .claude/skills/add-wolf-hook/

- `SKILL.md` — Add Wolf Hook (~3583 tok)

## .claude/skills/draft-spec-section/

- `SKILL.md` — Draft Spec Section (~491 tok)

## .claude/skills/find-skills/

- `SKILL.md` — Find Skills (~593 tok)

## .claude/skills/log-wolf-bug/

- `SKILL.md` — log-wolf-bug (~4099 tok)

## .claude/skills/record-decision/

- `SKILL.md` — Record Decision (~522 tok)

## .claude/skills/save-learning/

- `SKILL.md` — Save Learning (~626 tok)

## .claude/skills/setup-caliber/

- `SKILL.md` — Setup Caliber (~1941 tok)

## arms/

- `base.toml` — The settings every arm here shares. It is not runnable by itself: it names (~422 tok)
- `omnimerge-v4-mtp-27b-high-sdk-4.100.251.toml` — OmniMerge v4 against the base model (owner, 2026-10-13): the same (~118 tok)
- `qwen36-mtp-27b-high-sdk-4.100.251.toml` — The owner's first real arm (D55, D59) on SDK release 4.100.251: the same (~124 tok)
- `qwen36-mtp-27b-high.toml` — First real arm of milestone 2 (decisions D55 and D59). Every value here is (~220 tok)
- `stub-sdk-4.100.251.toml` — The stub arm on SDK release 4.100.251: the same settings, another build of (~58 tok)
- `stub.toml` — An arm for the stub model server: used by the harness's own tests and for (~99 tok)

## cmd/automaticum/

- `client_cmds.go` — client (143 fields) (~7562 tok)
- `compare_cmds.go` — Declares list (~2036 tok)
- `config_cmds_test.go` — TestParseInterspersed (~116 tok)
- `config_cmds.go` — place (119 fields) (~3088 tok)
- `control_cmds.go` — lane drain|resume, batch pause|resume|stop|retry, run retry (~1300 tok)
- `image_cmds.go` (~1096 tok)
- `main.go` — Command automaticum is the command line of automaticum. (~1553 tok)
- `sdk_cmds.go` — SDKEnv (~1197 tok)
- `store_cgo.go` — go:build cgo (~1850 tok)
- `store_nocgo.go` — go:build !cgo (~128 tok)
- `stub_cmds.go` (~419 tok)

## cmd/automaticumd/

- `main.go` — Command automaticumd is the daemon: it owns the store, works the queue on (~1576 tok)

## docs/local/

- `hosts.local.md` — Local mapping (untracked) (~420 tok)

## docs/specs/

- `000-overview.md` — 000 — Overview, vocabulary and map of the spec set (~2073 tok)
- `010-architecture.md` — 010 — Architecture (~1615 tok)
- `020-run-record.md` — 020 — The run record, verdicts and the basis (~2483 tok)
- `030-configuration.md` — 030 — Configuration (~2326 tok)
- `040-runner.md` — 040 — The runner (~6120 tok)
- `050-orchestration.md` — 050 — Orchestration: scheduling, gates, control (~2997 tok)
- `060-tasks-and-suites.md` — 060 — Tasks, oracles and the suite interface (~1440 tok)
- `061-suite-bfcl.md` — 061 — Suite: BFCL (Berkeley Function-Calling Leaderboard) (~551 tok)
- `062-suite-swe-bench.md` — 062 — Suite: SWE-bench and SWE-bench-style sets (~610 tok)
- `063-suite-multi-language.md` — 063 — Suite: our own multi-language agentic suite (~536 tok)
- `064-engine-adapters.md` — 064 — Engine adapters: Cerebriline as the engine of an external harness (~962 tok)
- `065-cell-contract.md` — 065 — The cell contract and exports (~1280 tok)
- `070-diagnostics.md` — 070 — Diagnostics: metrics, detectors, statistics (~5748 tok)
- `080-store.md` — 080 — The store (~4404 tok)
- `090-interfaces.md` — 090 — Interfaces (~2087 tok)
- `100-legacy-import.md` — 100 — Import of the old harness's corpus (~3426 tok)
- `110-acceptance-and-roadmap.md` — 110 — Acceptance and roadmap (~4576 tok)
- `decisions.md` — Decisions and open questions (~4906 tok)

## docs/specs/archive/

- `automaticum-draft-spec-0.4.md` — automaticum — draft specification (~10389 tok)
- `omnimergekit-evaluation-requirements-0.1.md` — automaticum — requirements from the omnimergekit evaluation protocol (~8226 tok)

## examples/config/

- `automaticum.toml` — The daemon's own file. Copy this directory, edit it, and start the daemon (~228 tok)

## examples/config/endpoints/

- `local-ollama.toml` (~114 tok)
- `stub.toml` — The stub model server the daemon can start for tests (automaticum stub). (~46 tok)

## examples/config/lanes/

- `lane-1.toml` (~32 tok)

## images/base/

- `Dockerfile` — Docker container definition (~234 tok)

## images/layers/typescript/

- `Dockerfile` — Docker container definition (~98 tok)

## internal/config/

- `arm.go` — loads automaticum's configuration: arms, lanes, endpoints (~7474 tok)
- `config_test.go` — TestRepositoryArms, TestArmValidation, TestNothingIsDefaulted, TestExtendsAndIdentity + 3 more (~3694 tok)
- `local.go` — Local (77 fields); methods: Token, APIKey (~2391 tok)
- `model_test.go` — TestModels (~532 tok)
- `model.go` — ModelIdentity (25 fields); methods: Missing (~755 tok)
- `runner_fixture_test.go` — TestRunnerFixtureIsTheResolvedArm (~164 tok)

## internal/daemon/

- `api.go` — HTTP handlers: reply, fail (~10748 tok)
- `compare.go` — CompareView (75 fields) (~2458 tok)
- `canary.go` — first-generations canary (D84): CanaryRecord, canaryLoop/canaryTick reading RunsDir/<run>/out/wire.jsonl, canaryStop, controlTokens; BatchView + handleBatch (~1900 tok)
- `control.go` — control (drained lanes, held batches, retries; control.json), handleLane, handleBatchPause/Resume/Stop, handleRetry, askedOf, askedProvenance (~4300 tok)
- `control.go` — Struct: control (~4075 tok)
- `daemon_test.go` — memState (126 fields); methods: Metrics, Findings, ReplaceMetrics, ReplaceToolCalls (~8060 tok)
- `daemon.go` — is automaticumd without its main: the queue, one worker (~5222 tok)

## internal/detect/

- `controls.go` — Control (39 fields) (~2450 tok)
- `detect_test.go` — TestSelfTest, TestShortUnit, TestShortUnitAgainstPattern, TestCharRun + 5 more (~2754 tok)
- `detect.go` — is the detector library (spec 070 §3): degeneration and (~2624 tok)
- `guards.go` (~1753 tok)
- `reference_test.go` — TestReferenceFixtures, TestAgainstReference (~1596 tok)

## internal/docker/

- `docker_test.go` — TestDemux, TestRun, TestRunTimeoutAndCancel (~1085 tok)
- `docker.go` — is a small client for the Docker Engine API over the local (~4290 tok)

## internal/gate/

- `gate_test.go` — server (40 fields); methods: ImageInspect, Check, Path (~2872 tok)
- `gate.go` — holds the gates: what must hold before a run starts (spec (~4466 tok)

## internal/human/

- `human_test.go` — TestDuration, TestNumbers, TestMetric (~831 tok)
- `human.go` — writes values the way a person reads them: a duration in (~1413 tok)

## internal/ingest/

- `analyse.go` — Interface: Blobs (47 methods) (~3994 tok)
- `canary.go` — CanaryRequests = 5 (D84), CanaryVerdict, Finished, Canary (~700 tok)
- `canary_test.go` — TestCanary (~800 tok)
- `detectors.go` — Firing (23 fields) (~1014 tok)
- `events.go` — turns a run's stored raw material into what is queried: (~2544 tok)
- `ingest_test.go` — TestReadEvents, TestReadWire, TestCompareWireClean, TestCompareWire + 2 more (~8030 tok)
- `sample_test.go` — TestSample, TestDumpBlocks (~1010 tok)
- `sampler.go` — Setting (43 fields) (~1398 tok)
- `wire.go` — Request (63 fields) (~1317 tok)
- `wirearm.go` — Deviation (99 fields) (~3578 tok)

## internal/legacyimport/

- `doc.go` — reads the run corpus of the old test harness and maps (~201 tok)
- `fixture_test.go` — fixture (29 fields) (~5580 tok)
- `import_test.go` — TestDiscover, TestImportFixture (~9408 tok)
- `importer.go` — Options (122 fields) (~9530 tok)
- `mapping.go` — Boundary (112 fields) (~4322 tok)
- `parse_test.go` — TestParseExit, TestParseBuild, TestParseVerdictFile, TestParseTransactionsAndSummaries + 5 more (~3814 tok)
- `parse.go` — KV (135 fields); methods: Get, Map, Get, Counts (~3727 tok)
- `scan.go` — RunSource (133 fields) (~3852 tok)
- `secondpass_test.go` — run (57 fields) (~7262 tok)
- `secondpass.go` — Lane (67 fields) (~3088 tok)
- `sink.go` — Interface: Sink (8 methods) (~1357 tok)
- `stream_test.go` — TestStreamScanner, TestStreamScannerCutStream, TestStreamScannerSeveralStreams, TestStreamScannerOversizeLine + 2 more (~2310 tok)
- `stream.go` — StreamStats (112 fields); methods: Write, EndStream, Stats, ToolCalls (~4299 tok)
- `types.go` — The run record is the store's. The names are aliased here so the rest of (~269 tok)
- `verify.go` — FileCheck (71 fields); methods: OK, String, String (~2664 tok)

## internal/run/

- `identity_test.go` — m (29 fields) (~1462 tok)
- `probe.go` — Identity (122 fields); methods: Changed (~3110 tok)
- `protocol.go` — executes one run: an arm on a task, once, on a lane. It (~1671 tok)
- `run_test.go` — fakeState (75 fields); methods: UpsertRun, AddBlobRefs, Put, Check (~5958 tok)
- `run.go` — Interface: Engine (7 methods) (~8237 tok)
- `verdict.go` — Facts (20 fields) (~588 tok)

## internal/sdk/

- `sdk_test.go` — TestDefinitions, TestInstallRebuild (~1726 tok)
- `sdk.go` — provisions the SDK under test. A lane image holds no SDK: each (~5126 tok)

## internal/stats/

- `stats_test.go` — TestWilson, TestFisher, TestMannWhitney, TestSummarise (~581 tok)
- `stats.go` — holds the statistics reports print about runs (spec 070 (~1626 tok)

## internal/store/

- `store.go` — defines the one interface through which automaticum reaches (~2867 tok)

## internal/store/blob/

- `archive_test.go` — TestArchiveMove, TestArchiveRootMissing, TestArchiveInterrupted, TestArchiveLeftoverTemporary, TestArchiveRefusesCorruptSource (~4780 tok)
- `archive.go` — ArchiveReport (79 fields); methods: ArchiveOlderThan, Read (~1911 tok)
- `bench_test.go` — BenchmarkPut, BenchmarkReadRange (~592 tok)
- `blob_test.go` — failingReader (100 fields); methods: Read (~5881 tok)
- `blob.go` — Options (86 fields); methods: String, Close, Root, ArchiveRoot (~2684 tok)
- `check.go` — entry (68 fields); methods: Walk, OK, Check (~1761 tok)
- `crash_test.go` — slowReader (36 fields); methods: Read (~1321 tok)
- `doc.go` — is the content-addressed store for raw material: event (~347 tok)
- `format.go` — index (63 fields) (~2199 tok)
- `FORMAT.md` — Blob file format, version 1 (~1112 tok)
- `put.go` (~1319 tok)
- `read.go` — blobFile (92 fields); methods: Stat, Has, Reader, Read (~2231 tok)

## internal/store/duck/

- `duck_test.go` — TestRunRoundTrip, TestFiltersAndDerivedTables, TestLaneState, TestQueue, TestMetricsAndReads (~2747 tok)
- `duck.go` — implements store.State on DuckDB. (~5601 tok)

## internal/store/lance/

- `lance_test.go` — go:build lance (~1673 tok)
- `lance.go` — implements store.Search on the Lance format, through the (~5344 tok)

## internal/store/storetest/

- `crash_test.go` — go:build lance (~1657 tok)
- `doc.go` — holds tests that exercise the store's engines together, (~46 tok)

## internal/stubmodel/

- `stubmodel_test.go` — TestChatFollowsTheScript, TestProbesAndErrors (~1089 tok)
- `stubmodel.go` — is a model server that plays a script. It speaks enough (~2292 tok)

## internal/task/

- `exec.go` — Interface: Executor (4 methods) (~3851 tok)
- `task_test.go` — TestLoadAll, TestLoadRejects, TestHashChangesWithAnyFile, TestVerifyDemo + 2 more (~2726 tok)
- `task.go` — loads tasks from the tasks directory, builds what they need, (~3571 tok)

## models/

- `omnimerge-v4-mtp_tb-27b-q4km-128k.toml` — What this tag is, in the owner's words (030 §4.1), as named on 2026-10-13. (~83 tok)
- `qwen36-base-mtp_tb-27b-q4km-128k.toml` — What this tag is, in the owner's words (030 §4.1), as named on 2026-10-13: (~67 tok)
- `stub.toml` — The scripted stub model server of the harness's own tests. Not a model. (~44 tok)

## runner/

- `package.json` — Node.js package manifest (~103 tok)
- `PROTOCOL.md` — The runner: protocol 1 (~985 tok)
- `runner.mjs` — The runner: one agent session for one run, inside the lane's container. (~2805 tok)
- `session.mjs` — unsupported lists what an arm asks for that this runner cannot do. (~2455 tok)
- `wire.mjs` — createWire returns a fetch that appends one JSON line per request to (~1492 tok)

## runner/sdk/4.100.250/

- `package-lock.json` — npm lock file (~53902 tok)
- `package.json` — Node.js package manifest (~186 tok)
- `sdk.toml` — SDK release 4.100.250, from the registry. zod is held at the version the (~96 tok)

## runner/sdk/4.100.251/

- `package-lock.json` — npm lock file (~52402 tok)
- `package.json` — Node.js package manifest (~64 tok)
- `sdk.toml` — SDK release 4.100.251, from the registry: the patched Ollama provider (~74 tok)

## runner/test/

- `arm-stub.json` (~287 tok)
- `session.test.mjs` — stub: reportOf (~1906 tok)
- `wire.test.mjs` — Declares serve (~1056 tok)

## spike/001-store/

- `REPORT.md` — Store spike: report (~4157 tok)

## spike/001-store/bench/

- `corpus.go` — go:build lance (~4837 tok)
- `dedup.go` — go:build lance (~1645 tok)
- `embed.go` — go:build lance (~2628 tok)
- `fs_linux.go` — go:build lance (~62 tok)
- `main.go` — go:build lance (~800 tok)
- `rangeread.go` — go:build lance (~897 tok)
- `state.go` — go:build lance (~1735 tok)
- `vectors.go` — go:build lance (~2332 tok)

## spike/001-store/linkcheck/

- `README.md` — Project documentation (~2147 tok)

## spike/001-store/linkcheck/l1/

- `go.mod` — Go module definition (~458 tok)
- `go.sum` — Go dependency checksums (~4313 tok)
- `main.go` — Command linkcheck-l1 is threshold T1 of the store spike for Lance candidate (~1417 tok)

## spike/001-store/linkcheck/l2/

- `go.mod` — Go module definition (~304 tok)
- `go.sum` — Go dependency checksums (~1202 tok)
- `main.go` — Command linkcheck-l2 is threshold T1 of the store spike for Lance candidate (~1947 tok)

## spike/001-store/results/

- `embed-1024-search.json` (~525 tok)
- `embed-1024.json` (~493 tok)
- `embed-768.json` (~490 tok)
- `o8-dedup.json` (~326 tok)
- `t10-vec-1m-real.json` (~727 tok)
- `t10-vec-1m.json` (~597 tok)
- `t11-vec-5m-real.json` (~724 tok)
- `t11-vec-5m.json` (~605 tok)
- `t12-state.json` (~298 tok)
- `t13-range.json` (~185 tok)
- `t15-archive-half.json` (~93 tok)
- `t15-text-after-3.json` (~225 tok)
- `t15-text-after-coldarchive.json` (~224 tok)
- `t15-text-after.json` (~225 tok)
- `t15-text-before.json` (~191 tok)
- `t8-chunks-x10.json` (~144 tok)
- `t8-chunks.json` (~143 tok)
- `t8-text-live.json` (~190 tok)
- `t8-text-x10.json` (~228 tok)
- `t9-regex-1-skip.json` (~101 tok)
- `t9-regex-1.json` (~65 tok)
- `t9-regex-4-skip.json` (~101 tok)
- `t9-regex-4.json` (~84 tok)

## tasks/custom/manic_miner/

- `README.md` — Project documentation (~345 tok)
- `task.toml` — manic_miner: repair a broken browser game. (~809 tok)

## tasks/custom/manic_miner/fixtures/

- `known-bad-weak-jump.html` — Manic Miner - ZX Spectrum Classic (~4638 tok)
- `known-good.html` — Manic Miner - ZX Spectrum Classic (~4638 tok)

## tasks/custom/manic_miner/oracle/

- `build.mjs` — Builds the oracle the agent may run but not read: run_game.logic.js as a (~795 tok)
- `run_game.logic.js` — Everything a canvas context is asked to do, swallowed. (~8136 tok)

## tasks/custom/manic_miner/seed/

- `manic_miner.html` — Manic Miner - ZX Spectrum Classic (~3767 tok)

## tasks/demo/answer/

- `task.toml` — A made-up task for the harness's own tests. It needs no model that can (~218 tok)

## tasks/demo/answer/fixtures/

- `good.txt` (~5 tok)

## tasks/demo/answer/seed/

- `answer.txt` (~5 tok)
