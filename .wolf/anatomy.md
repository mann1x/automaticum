# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-10-13T03:58:24.460Z
> Files: 66 tracked | Anatomy hits: 0 | Misses: 0

## ./

- `.gitignore` — Git ignore rules (~73 tok)
- `CALIBER_LEARNINGS.md` — Caliber Learnings (~2566 tok)
- `CLAUDE.md` — automaticum (~1671 tok)
- `go.mod` — Go module definition (~13 tok)
- `LICENSE` — Project license (~3029 tok)
- `Makefile` — Make build targets (~271 tok)
- `README.md` — Project documentation (~2348 tok)
- `spike.local.toml` — Untracked. Paths for the store spike on this installation. (~167 tok)

## .caliber/

- `.caliber-state.json` (~35 tok)
- `manifest.json` (~728 tok)
- `refresh-hook.log` (~0 tok)
- `score-history.jsonl` (~58 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/

- `CLAUDE.md` — OpenWolf (~57 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/.claude/rules/

- `openwolf.md` (~313 tok)

## .caliber/cache/

- `fingerprint.json` — Declares dim (~48822 tok)

## .caliber/learning/

- `current-session.jsonl` (~9466 tok)
- `finalize.log` (~0 tok)
- `roi-stats.json` (~5158 tok)
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
- `080-store.md` — 080 — The store (~3060 tok)
- `090-interfaces.md` — 090 — Interfaces (~1480 tok)
- `100-legacy-import.md` — 100 — Import of the old harness's corpus (~1464 tok)
- `110-acceptance-and-roadmap.md` — 110 — Acceptance and roadmap (~1154 tok)
- `decisions.md` — Decisions and open questions (~2169 tok)

## docs/specs/archive/

- `automaticum-draft-spec-0.4.md` — automaticum — draft specification (~10389 tok)
- `omnimergekit-evaluation-requirements-0.1.md` — automaticum — requirements from the omnimergekit evaluation protocol (~8226 tok)

## internal/legacyimport/

- `importer.go` — Struct: Options (~7326 tok)
- `mapping.go` — Struct: Boundary (~3263 tok)
- `parse.go` — Struct: KV (~3478 tok)
- `scan.go` — Struct: RunSource (~3551 tok)
- `stream.go` — Struct: StreamStats (~3877 tok)

## spike/001-store/linkcheck/

- `README.md` — Project documentation (~1941 tok)

## spike/001-store/linkcheck/l1/

- `go.mod` — Go module definition (~458 tok)
- `go.sum` — Go dependency checksums (~4313 tok)
- `main.go` — Command linkcheck-l1 is threshold T1 of the store spike for Lance candidate (~1417 tok)

## spike/001-store/linkcheck/l2/

- `go.mod` — Go module definition (~304 tok)
- `go.sum` — Go dependency checksums (~1202 tok)
- `main.go` — Command linkcheck-l2 is threshold T1 of the store spike for Lance candidate (~1947 tok)

## internal/store/

- `store.go` — the store interface: Run, BlobRef, ToolCall, Finding, LaneState, Chunk, Hit; State and Search interfaces
- `blob/` — content-addressed blob store: seekable zstd frames, two tiers, archive-by-age with verify, Check; format in `blob/FORMAT.md`
- `duck/duck.go` — store.State on DuckDB (autoload/autoinstall off, appender for derived tables)
- `lance/lance.go` — store.Search on lance-c via cgo and the Arrow C Data Interface; build tag `lance`; rank fusion in Go
