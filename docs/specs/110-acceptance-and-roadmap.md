# 110 — Acceptance and roadmap

**Status:** draft 0.11, 2026-10-13. The milestone order in §3 was approved by the owner
on 2026-10-12. Milestone 1 has been carried out: the store, the importer and
the spike's measurements exist (080 §7.4, 100 §7), and the owner has decided
the questions it raised (D46 to D51). Milestone 2 was planned and approved on
2026-10-13 (D53 to D58) and is in progress; its first real runs are on
`windows-2` (D55). Built and tested so far: configuration and arms, the
task with its proven oracle, the run lifecycle with the verdict rules, the
daemon with its API and the command line. Since SDK release 4.100.250 was published
on 2026-10-13: the lane images, the runner, whole runs against the stub
server, and the first five real runs (§3.1). Milestone 2 is
carried out. Milestone 3 was planned and approved on 2026-10-13 (D62 to
D65) and is built (§3.2): ingest with metrics, the comparison of every
request with the arm, the detector library, the gates with `doctor`, and
`reanalyse`. What it leaves open is listed there.

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

### 3.1 The first real runs (2026-10-13)

Arm `qwen36-mtp-27b-high` (D55, D59) on `manic_miner` version 4, five runs,
one lane on `linux-gpu-1`, the model served by xollama 0.40.3 on `windows-2`,
SDK release 4.100.250.

| run | verdict | wall | agent requests | tokens out | generation |
|---|---|---|---|---|---|
| 1 | `PASS` | 733 s | 18 | 34,136 | 57.5 tok/s |
| 2 | `PASS` | 828 s | 28 | 38,099 | 63.7 tok/s |
| 3 | `PASS` | 689 s | 31 | 29,306 | 61.8 tok/s |
| 4 | `PASS` | 360 s | 19 | 13,886 | 66.2 tok/s |
| 5 | `PASS` | 1,582 s | 26 | 73,050 | 52.2 tok/s |

Median 733 s. The owner's reference for this tag in VS Code with the
extension is about 12 minutes (720 s). Every run ended by the agent itself,
no limit fired, the oracle's copy in the workspace was untouched, the
endpoint's identity was the same before and after, and the model was fully
on the GPU with the 131072 window.

What the wire shows, on every one of the 122 agent requests: `num_ctx`
131072, temperature 1, top-k 20, repeat last-n 0, repeat penalty 1, as the
arm states. Two values of the arm did **not** reach the wire, although the
SDK reported them as resolved:

- **Thinking effort.** The request carries `think: true` and nothing else.
  The server answered with its own budget, the tag's (`medium`, 24,000
  tokens). The effort `high`, and the 48,000-token budget the SDK derived
  from it, changed nothing that was sent.
- **Presence penalty 0** is not among the options sent. The tag gives 0 too,
  so the effective value is the same.

So the read-back (040 §4, layer 2) is not proof of what was sent; only the
wire (layer 3) is. Since milestone 3 the comparison is automatic: ingest
compares every agent request with the arm and records each difference as a
`wire_arm` finding (070 §4.1), which `run show` prints.

**The thinking setting of these five runs is the tag's own (`medium`), not
`high`.** The cause, as the Cerebriline session reported it on 2026-10-13:
the SDK packages published as 4.100.250 install the stock Ollama provider
library, not the patched one the extension is built with. The stock library
sends every effort level as `think: true` and drops the options its schema
does not name (presence penalty among them). It is corrected in the SDK's
repository and not released (X8). Until a release carries it, an arm that
states an effort level runs on the tag's budget, and says so in a finding.

The detector library (070 §3.2) over the same five runs: no looping detector
fired on any block; four of the five have at least one reasoning block of
10,000 characters or more (the largest 29,345). Over the 531 imported runs
the same library, version 1, gives: sentence repeat in the reasoning of 156
runs (76 of 313 `PASS`, 27 of 87 `FAIL`, 19 of 38 `TIMEOUT`, 21 of 60
`ABORTED`, 13 of 33 `INVALID`), short-unit repeat in 6, no letter run, and
three or more identical tool calls in a row in 107. A run of the old
harness is not a run of this arm, so these are two descriptions, not a
comparison.

Two more things the comparison shows on all five runs, both as notes: the
output budget sent (`num_predict`) shrinks as the context fills, by the SDK's
own rule, since the arm leaves it to the SDK; and the first agent request
alone carries a `placement` field that asks for a window of 111,277 tokens,
smaller than the 131,072 of `num_ctx` in the same request.

Before these, five runs of an earlier draft of the arm were recorded
`INVALID` within seconds by a fault in the runner's read-back (a thinking
budget derived by the SDK was taken for a deviation). No request reached the
model. They stay in the store.

**Against the old harness** (same model tag, from the imported corpus, which
was brought into the daemon's store the same day):

| | task version | runs | `PASS` | other | median wall of `PASS` |
|---|---|---|---|---|---|
| old harness | 1 | 11 | 10 | 1 `ABORTED` | 555 s |
| old harness | 2 | 67 | 66 | 1 `ABORTED` | 913 s (231 to 2,903) |
| automaticum | 4 | 5 | 5 | none | 733 s (360 to 1,582) |

This is not a like-for-like comparison and must not be pooled: the task
version, the SDK build, the lane (a container against a host process), the
context window and the sampler all differ, and the old runs' thinking
setting was not recorded. What it does show is that the new harness produces
verdicts and times in the range the old one did for this tag, and close to
the owner's reference in the editor.

### 3.2 Milestone 3 as built, against criterion 3 (2026-10-13)

Built: metrics and tool calls at ingest (070 §4.1), the comparison of every
request with the arm, the detector library with its controls (070 §3.2), the
gates with `doctor` and notification (050 §3.1.1), `reanalyse` over stored
material including the imported corpus, and `run show` with metrics and
findings.

Criterion 3 asks that every failure of 000 §1.1 be a gate, an `INVALID`
verdict or a finding, each with a test that injects the fault:

| failure (000 §1.1) | what catches it | test that injects it |
|---|---|---|
| a verdict word with four meanings; a timeout that exited 0 | six verdicts with one precedence; an exit 0 at the cap is `TIMEOUT` | `internal/run`: `TestDecide`, `TestVerdicts` |
| an oracle that scored broken code as fixed | fixtures that must fail, `oracle verify`; gate `oracle` refuses a run whose oracle is not proven for the task as it is now | `internal/task` (fixtures), `internal/gate` ("oracle not proven", "task changed since the proof"), `internal/run` `TestTamperedOracle` |
| a thinking flag that overrode the tag's budget, with no record of what was sent | the wire capture; finding `wire_arm` (stated and not sent, sent and not stated) with the value in effect | `internal/ingest`: `TestCompareWire`, `TestCompareWireTagOwns` |
| a sampler written to the wrong field, never applied | the runner's read-back (`INVALID`) and finding `wire_arm` on the wire | `runner/test/session.test.mjs`, `internal/ingest` `TestCompareWire` |
| a stale SDK build that died at import and exited 0 | gate `harness_build` and the run's own check (`SKIPPED`, D54); an empty stream is `ABORTED` | `internal/gate` (three image faults), `internal/run` `TestVerdicts` |
| the session state deleted | the data directory is stored as a blob of every run; `store check` finds a missing blob | `internal/store/blob` (check); **no test deletes a run's session blob and asks the run record** |
| a grep that dropped lowercase verdicts | verdicts are one typed column; nothing is counted by text search | by construction; the importer's mapping of old words is tested in `internal/legacyimport` |
| edits to a running script taking effect mid-campaign | the arm and the task are read once at a run's start and fixed; an arm's identity is the hash of its resolved content | per run only. **Across a batch or campaign: milestone 4** |
| six hours of zeros on the wrong port | gates `model_tag` and `bind_address`: the endpoint must answer and have the model; its identity is probed before and after | `internal/gate` ("endpoint down", "model not on the endpoint"), `internal/run` `TestOperatorAbortAndEndpointChange`. The first-generations canary (050 §3.3) is milestone 4 |
| a score that was mostly the scaffold | the agent engine as part of the arm (064) | **milestone 5** |

So seven of the ten are caught with an injected-fault test now, one is
caught without such a test, and two belong to milestones 4 and 5.

Not done in milestone 3, and carried forward:

- **The second half of criterion 3:** "every way a run was silently invalid
  in the old harness's notes" is not yet tabulated against gates and
  findings one by one.
- **Criterion 8:** the library passes its own controls; reproducing the
  firings of omnimergekit's reference implementation on **its** fixture set
  waits for that set (asked for on 2026-10-13).
- `reanalyse --detector` and `--where`; the timeline and the baseline
  highlighting of the per-run report (070 §1.1); a mailbox notifier; the
  detectors near-duplicate rounds, edit oscillation and re-read loop.
- The gates that need an agent on the endpoint's host (milestone 7), and the
  parts of `free`, `gpu_residency`, `host` and `bind_address` that 050 §3.1.1
  lists as not yet looked at.
