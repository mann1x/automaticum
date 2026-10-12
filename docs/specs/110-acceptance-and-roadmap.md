# 110 — Acceptance and roadmap

**Status:** draft 0.5, 2026-10-12. The milestone order is a proposal: the owner
chose a spec-only outcome for now and has not approved an implementation
milestone.

## 1. Acceptance: when it replaces the old harness

1. `manic_miner` is ported and `oracle verify` passes on its fixtures.
2. One historical arm is re-run and its pass rate, times and token figures
   agree with the old harness's records within a stated tolerance; the old
   corpus is imported and queryable, and the import reproduces the old verdict
   counts exactly (100 §6).
3. Every failure in 000 §1.1, and every way a run was silently invalid in the
   old harness's notes, is a gate, an `INVALID` verdict or a finding, **each
   with a test that injects the fault**.
4. An unattended overnight A/B campaign across two hosts, paused and resumed
   once, with one lane killed on purpose, ends with a report nobody had to
   script.
5. Each question in §2 is answered by one command.
6. A BFCL category (both cells) and a SWE-bench Lite shard run end to end and
   match the official scorer.
7. `omk_eval` runs one cell through `automaticum cell run`, kills it halfway,
   resumes it with the same key, and tabulates the result file without anyone
   reading samples by hand.
8. Every detector passes its positive and negative controls, and the Go library
   reproduces the firings of omnimergekit's reference implementation on its
   fixture set.

## 2. Questions it must answer in one command

- Which runs of this arm looped, where, and on what text?
- Did the sampler I set reach the wire in every request of this batch?
- What differs between these two arms? Between these two builds? Between these
  two cells' bases?
- Which guard fired in the runs that failed, and what happened after?
- Show every tool call that could not be read, by model.
- Did thinking stop after a template leak in any run this week?
- How many requests re-read the whole prompt, and why?
- Is B better than A, and how many more runs until I can say?
- Which runs were on a model that was not fully on the GPU?
- Show me turn 41 of run X exactly as the model saw it, and the server's log
  for those seconds.
- Find runs where the model concluded the checker was wrong.
- What did the last 20 `FAIL`s have in common that the `PASS`es did not?
- How many tokens did this arm spend per solved task, and was any cell capped?

## 3. Proposed milestones

Each milestone ends with something that runs and with tests for the failures it
is meant to prevent.

| # | milestone | contains | depends on |
|---|---|---|---|
| 1 | **Store spike and importer** | the store interface; the importer for the old corpus; the spike's measurements (080 §7), first of all the static build with both engines | none |
| 2 | **Walking skeleton** | daemon and CLI; one Docker lane on the main host; base image and one language layer; the runner with wire capture and an empty data directory; `manic_miner` with `oracle verify`; the six verdicts; the full run record | 1; SDK requests 1 to 3 (040 §8) help but do not block |
| 3 | **Gates, metrics, detectors** | the gate engine and built-in gates; `doctor`; metrics at ingest; the detector library with controls; `reanalyse`; per-run report | 2 |
| 4 | **Batches, cells, statistics** | scheduler, interleaving, campaigns, control and restart recovery; the basis and its diff; `cell run` and the result file; paired and unpaired statistics; the cap audit | 3 |
| 5 | **Suites and adapters** | the suite interface; SWE-bench (predictions file); BFCL (proxy and shim); the in-container adapter; the multi-language suite's requirements; Gitea | 4 |
| 6 | **Interfaces** | HTTP API hardening; web dashboard; TUI; MCP server; search (full-text, regex, semantic); replay; the chat box | 4; can overlap with 5 |
| 7 | **Remote and native** | agent mode on a second Linux host; the native agent for Windows and macOS; extension-parity features that need them | 4 |

Extension parity (040 §6) is built across milestones: the scripted user and
approvals in 2 and 3, steering, resume and checkpoints in 4, emulated
diagnostics with the language layers in 5.

Acceptance criteria map to milestones: 1 and 2 need milestone 2; 3 needs 3;
4 needs 4 and 7; 5 needs 6; 6 and 7 need 5; 8 needs 3.

**Open:** whether milestone 1 runs before or alongside milestone 2. The store
interface lets the skeleton start on DuckDB alone, at the risk of rework if the
spike sends us to a fallback.
