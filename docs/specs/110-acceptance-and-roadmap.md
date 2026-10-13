# 110 — Acceptance and roadmap

**Status:** draft 0.8, 2026-10-13. The milestone order in §3 was approved by the owner
on 2026-10-12. Milestone 1 has been carried out: the store, the importer and
the spike's measurements exist (080 §7.4, 100 §7), and the owner has decided
the questions it raised (D46 to D51). Milestone 2 was planned and approved on
2026-10-13 (D53 to D58) and is in progress; its first real runs are on
`windows-2` (D55). Built and tested so far: configuration and arms, the
task with its proven oracle, the run lifecycle with the verdict rules, the
daemon with its API and the command line. Waiting for SDK release 4.100.250
(040 §3): the runner, the lane images, the run against the stub server and
the real runs.

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

## 3. Milestones

**Decided by the owner (2026-10-12):** this order is the roadmap, the store
spike runs first, and milestone 1 is the next thing planned in detail. Later
milestones may be reordered when they are planned.

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

Milestone 2 does not start until the spike has answered its first question:
whether both store engines link into one static binary. **Answered on
2026-10-13: yes** (080 §7.1.1), with glibc and the C++ runtime dynamic.
