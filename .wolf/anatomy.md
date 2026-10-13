# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-10-13T07:18:25.684Z
> Files: 106 tracked | Anatomy hits: 0 | Misses: 0

## ../../../../root/.claude/plans/

- `functional-mapping-elephant.md` — Milestone 2: walking skeleton (~3114 tok)

## ./

- `.gitignore` — Git ignore rules (~73 tok)
- `CALIBER_LEARNINGS.md` — Caliber Learnings (~2935 tok)
- `CLAUDE.md` — automaticum (~1879 tok)
- `go.mod` — Go module definition (~297 tok)
- `go.sum` — Go dependency checksums (~1202 tok)
- `LICENSE` — Project license (~3029 tok)
- `Makefile` — Make build targets (~493 tok)
- `README.md` — Project documentation (~2348 tok)
- `spike.local.toml` — Untracked. Paths for the store spike on this installation. (~167 tok)

## .caliber/

- `.caliber-state.json` (~35 tok)
- `learnings-archive.md` — Caliber Learnings Archive (~497 tok)
- `manifest.json` (~728 tok)
- `refresh-hook.log` (~0 tok)
- `score-history.jsonl` (~116 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/

- `CLAUDE.md` — OpenWolf (~57 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/.claude/rules/

- `openwolf.md` (~313 tok)

## .caliber/cache/

- `fingerprint.json` — Declares dim (~69754 tok)

## .caliber/learning/

- `current-session.jsonl` (~112551 tok)
- `finalize.log` (~0 tok)
- `roi-stats.json` (~6364 tok)
- `state.json` (~41 tok)

## .claude/

- `settings.json` (~974 tok)
- `settings.local.json` (~35 tok)

## .claude/hooks/

- `caliber-check-sync.sh` — Don't block headless claude sessions spawned by caliber itself (e.g. during caliber refresh) (~722 tok)
- `caliber-session-freshness.sh` — Don't run inside a caliber-spawned headless session — the systemMessage would (~226 tok)

## .claude/rules/

- `openwolf.md` — OpenWolf protocol (~207 tok)
- `spec-conventions.md` — Spec conventions (~368 tok)
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

## docs/local/

- `hosts.local.md` — Local mapping (untracked) (~380 tok)

## docs/specs/

- `000-overview.md` — 000 — Overview, vocabulary and map of the spec set (~2073 tok)
- `010-architecture.md` — 010 — Architecture (~1598 tok)
- `020-run-record.md` — 020 — The run record, verdicts and the basis (~1855 tok)
- `030-configuration.md` — 030 — Configuration (~1412 tok)
- `040-runner.md` — 040 — The runner (~3109 tok)
- `050-orchestration.md` — 050 — Orchestration: scheduling, gates, control (~1567 tok)
- `060-tasks-and-suites.md` — 060 — Tasks, oracles and the suite interface (~1185 tok)
- `061-suite-bfcl.md` — 061 — Suite: BFCL (Berkeley Function-Calling Leaderboard) (~551 tok)
- `062-suite-swe-bench.md` — 062 — Suite: SWE-bench and SWE-bench-style sets (~610 tok)
- `063-suite-multi-language.md` — 063 — Suite: our own multi-language agentic suite (~536 tok)
- `064-engine-adapters.md` — 064 — Engine adapters: Cerebriline as the engine of an external harness (~962 tok)
- `065-cell-contract.md` — 065 — The cell contract and exports (~1280 tok)
- `070-diagnostics.md` — 070 — Diagnostics: metrics, detectors, statistics (~2998 tok)
- `080-store.md` — 080 — The store (~3330 tok)
- `090-interfaces.md` — 090 — Interfaces (~1480 tok)
- `100-legacy-import.md` — 100 — Import of the old harness's corpus (~1464 tok)
- `110-acceptance-and-roadmap.md` — 110 — Acceptance and roadmap (~1154 tok)
- `decisions.md` — Decisions and open questions (~2233 tok)

## docs/specs/archive/

- `automaticum-draft-spec-0.4.md` — automaticum — draft specification (~10389 tok)
- `omnimergekit-evaluation-requirements-0.1.md` — automaticum — requirements from the omnimergekit evaluation protocol (~8226 tok)

## cmd/automaticum/

- `main.go` — command table and usage (~600 tok)
- `config_cmds.go` — `arm validate|show|diff`, `task ls|show|build`, `oracle verify`, `lane ls`, `endpoint ls`; pure Go (~3300 tok)
- `config_cmds_test.go` — flags among arguments (~150 tok)
- `store_cgo.go` — `legacy import`, `store check`; needs cgo, built in the link image (~2700 tok)
- `store_nocgo.go` — the same two commands in a build without cgo: an error (~120 tok)

## internal/config/

- `arm.go` — the arm format: every setting stated, `extends` with origins, `Validate`, identity hash, `Diff` (~5000 tok)
- `local.go` — the untracked side: `Local` (daemon, lanes, endpoints), `LoadLocal`, token and API key files (~2500 tok)
- `config_test.go` — arm inheritance, switching off an inherited feature, rejections, local config loading (~3500 tok)

## internal/stubmodel/

- `stubmodel.go` — model server that plays a script over the Ollama chat protocol (chat, show, ps, tags, version); for tests only (~2300 tok)
- `stubmodel_test.go` — script order, retried request, non-streamed answer, probes, status turn (~1200 tok)

## arms/

- `base.toml` — every guard, compaction and limit stated; other arms extend it (~600 tok)
- `stub.toml` — arm for the stub model server; measures nothing (~150 tok)

## examples/config/

- `automaticum.toml`, `lanes/lane-1.toml`, `endpoints/local-ollama.toml`, `endpoints/stub.toml` — neutral example of the untracked configuration directory (~400 tok)

## internal/docker/

- `docker.go` — small Docker Engine API client over the local socket: images (inspect, pull, build), containers (create, start, kill, remove, inspect, wait, logs), `Run` (~3900 tok)
- `docker_test.go` — stream demux; runs, timeout and cancellation against the local engine (skipped with -short) (~1100 tok)

## internal/task/

- `task.go` — `Task` and its parts, strict `Load`, `LoadAll`, `Find`, validation, hash of every task file (~3200 tok)
- `exec.go` — `Env`: `Build` (artefact, pinned hash), `PrepareWorkspace`, `RunOracle` (on a copy, no network), `Verify` (fixtures) (~3600 tok)
- `task_test.go` — loader rejections, demo task, oracle timeout, manic_miner build, fixtures and tamper detection (~2900 tok)

## tasks/custom/manic_miner/

- `task.toml` — task version 4: prompt, limits, the `run_game` artefact with its pinned hash, the oracle and three fixtures (~900 tok)
- `README.md` — what the task and its files are (~450 tok)
- `seed/manic_miner.html` — the broken page (CRLF, does not parse) (~4000 tok)
- `oracle/run_game.logic.js` — the oracle's source, never placed in a workspace (~8000 tok)
- `oracle/build.mjs` — reproducible build of the oracle as a masked single executable (~900 tok)
- `fixtures/known-good.html`, `fixtures/known-bad-weak-jump.html` — a working page, and the same with a jump too weak (~5000 tok each)

## tasks/demo/answer/

- `task.toml`, `seed/answer.txt`, `fixtures/good.txt` — made-up task for the harness's own tests (~250 tok)

## internal/legacyimport/

- `doc.go` — reads the run corpus of the old test harness and maps (~197 tok)
- `fixture_test.go` — Struct: fixture (~5208 tok)
- `import_test.go` — TestDiscover, TestImportFixture (~7125 tok)
- `importer.go` — Options (120 fields) (~7874 tok)
- `mapping.go` — Boundary (117 fields) (~3470 tok)
- `parse_test.go` — TestParseExit, TestParseBuild, TestParseVerdictFile, TestParseTransactionsAndSummaries, TestParseOra (~3560 tok)
- `parse.go` — KV (135 fields); methods: Get, Map, Get, Counts (~3727 tok)
- `scan.go` — RunSource (133 fields) (~3791 tok)
- `secondpass.go` — Struct: Lane (~2852 tok)
- `sink.go` — Interface: Sink (7 methods) (~1158 tok)
- `stream_test.go` — TestStreamScanner, TestStreamScannerCutStream, TestStreamScannerSeveralStreams, TestStreamScannerOve (~2156 tok)
- `stream.go` — StreamStats (112 fields); methods: Write, EndStream, Stats, ToolCalls (~4302 tok)
- `types.go` — Run (52 fields); methods: Valid (~628 tok)
- `verify.go` — FileCheck (56 fields); methods: OK, String, String (~2079 tok)

## internal/store/

- `store.go` — defines the one interface through which automaticum reaches (~2295 tok)

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

- `duck_test.go` — TestRunRoundTrip, TestFiltersAndDerivedTables, TestLaneState (~1709 tok)
- `duck.go` — implements store.State on DuckDB. (~4167 tok)

## internal/store/lance/

- `lance_test.go` — go:build lance (~1314 tok)
- `lance.go` — implements store.Search on the Lance format, through the (~5180 tok)

## internal/store/storetest/

- `crash_test.go` — go:build lance; TestKillDuringWrites, threshold T2 (~1900 tok)
- `doc.go` — conformance tests that need both engines (~80 tok)

## spike/001-store/

- `REPORT.md` — Store spike: report (~3697 tok)

## spike/001-store/bench/

- `corpus.go` — on the imported corpus: chunks (T4/T8), text (T8/T15), regex (T9), archive-half (T15) (~5000 tok)
- `dedup.go` — O8: by-part against content-defined deduplication on simulated request bodies (~1900 tok)
- `embed.go` — real embeddings through an Ollama-compatible server, search on them (~2600 tok)
- `fs_linux.go` — free space of a filesystem (~80 tok)
- `main.go` — go:build lance; throwaway measurement program, shared helpers (~800 tok)
- `rangeread.go` — T13: range reads from a multi-GB blob, live and archive tier (~1100 tok)
- `state.go` — T6/T7/T12: DuckDB statistics and census queries, lane-state updates under load (~2100 tok)
- `vectors.go` — T10/T11: synthetic clustered vectors, index build, recall and latency (~2200 tok)

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
