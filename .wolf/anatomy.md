# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-10-12T21:20:53.095Z
> Files: 25 tracked | Anatomy hits: 0 | Misses: 0

## ../../../../root/.claude/plans/

- `functional-mapping-elephant.md` — Milestone 1: store spike and legacy importer (~2669 tok)

## ./

- `LICENSE` — Apache-2.0
- `Makefile` — container build targets; needs SPIKE_ROOT
- `go.mod` — module github.com/mann1x/automaticum
- `spike.local.toml` — untracked local paths for the spike
- `CLAUDE.md` — OpenWolf (~57 tok)
- `README.md` — Project documentation (~1599 tok)

## .claude/

- `settings.json` (~441 tok)

## .claude/rules/

- `openwolf.md` (~313 tok)

## docs/local/ (untracked)


## docs/specs/

- `000-overview.md` — purpose, three usage modes, vocabulary, map, traceability to both inputs (~2078 tok)
- `010-architecture.md` — fixed points, components, agent mode, native agents, access model, operating rules (~1694 tok)
- `020-run-record.md` — provenance, streams, verdict enum, task versions, the basis (~1856 tok)
- `030-configuration.md` — TOML rules, lanes, images, endpoints, arms, campaigns (~1412 tok)
- `040-runner.md` — SDK as library, build pinning, wire capture, clean session, extension parity, scripted user, SDK requests (~2163 tok)
- `050-orchestration.md` — scheduling, gates (built-in list), control, watchdogs, unattended operation (~1569 tok)
- `060-tasks-and-suites.md` — task directory, oracles, oracle sampling, suite interface, Gitea, private tasks (~1161 tok)
- `061-suite-bfcl.md` — BFCL: pinned scorer, proxy cell and agent cell (~551 tok)
- `062-suite-swe-bench.md` — SWE-bench: subsets, verdict mapping, gold validation, other-language sets (~610 tok)
- `063-suite-multi-language.md` — own agentic suite: languages, hidden tests, parallel tasks (~538 tok)
- `064-engine-adapters.md` — four adapter shapes for external harnesses (~963 tok)
- `065-cell-contract.md` — cell run, exit codes, automaticum_result.json, re-scoring, exports (~1177 tok)
- `070-diagnostics.md` — metrics, detectors with controls, findings, cap audit, statistics (~2651 tok)
- `080-store.md` — DuckDB + Lance + blobs, consistency, retention, store interface, spike (~1426 tok)
- `090-interfaces.md` — CLI, TUI, HTTP API, dashboard, MCP, search and replay, models in the interface (~1380 tok)
- `100-legacy-import.md` — old corpus figures, mapping, oracle boundaries, unrecoverable data, scripts replaced (~1469 tok)
- `110-acceptance-and-roadmap.md` — acceptance criteria, one-command questions, proposed milestones (~1121 tok)
- `decisions.md` — owner decisions D1-D26, session-settled items, Open list O1-O13, SDK dependencies X1-X6 (~1616 tok)

## docs/specs/archive/

- `automaticum-draft-spec-0.4.md` — input document, not edited (~10526 tok)
- `omnimergekit-evaluation-requirements-0.1.md` — input document, not edited (~8242 tok)

## build/

- `Dockerfile.build` — Debian 11 (glibc 2.31) image with Go, Rust, protoc, arm64 and MinGW cross toolchains

## spike/001-store/linkcheck/

- `l1/main.go` — T1 link check: DuckDB + lancedb-go (main, from source) in one process
- `l2/main.go` — T1 link check: DuckDB + lance-c through our own cgo wrapper and the Arrow C Data Interface
