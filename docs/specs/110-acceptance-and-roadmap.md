# 110 — Acceptance and roadmap

**Status:** draft 0.19, 2026-10-23. The milestone order in §3 was approved by the owner
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
does not name (presence penalty among them). Release 4.100.251 of the same
day corrects it (X8). An arm on 4.100.250 that states an effort level runs
on the tag's budget, and says so in a finding. Both releases are provisioned
side by side (040 §3.1).

**Five runs of the same arm on 4.100.251** (arm
`qwen36-mtp-27b-high-sdk-4.100.251`, a separate arm, the same day, the same
lane, endpoint, model tag and task version): what the arm states is what was
sent. `think: "high"` and `presence_penalty: 0` are in every agent request,
the server answered a thinking budget of 48,000 tokens, and no "stated, not
sent" finding remains. No reload, model whole on the GPU.

| | 4.100.250 (budget 24,000) | 4.100.251 (budget 48,000) |
|---|---|---|
| `PASS` | 5 of 5 | 5 of 5 |
| wall, median (least to most) | 12m 13s (6m 00s to 26m 22s) | 13m 26s (8m 14s to 18m 41s) |
| requests | 18 to 31 | 18 to 51 |
| tokens out, median (least to most) | 34,136 (13,886 to 73,050) | 28,822 (17,745 to 47,497) |
| of which reasoning, median | 27,300 | 19,611 |
| generation rate | 52.2 to 66.2 tok/s | 51.7 to 63.4 tok/s |
| peak share of the window | 28% to 44% | 26% to 48% |
| failed tool calls, per run | 0, 4, 2, 0, 0 | 1, 0, 2, 0, 9 |
| reasoning block of 10,000 characters or more | 4 of 5 runs | 5 of 5 runs |

The two are different arms and are not pooled. With five runs each, the
ranges overlap on every row: nothing here shows that the larger budget
changed the wall time, the token count or the verdict on this task. No run
on either build used its budget in one block (the longest reasoning block is
about 117,000 characters, on 4.100.251). The notes that remain on 4.100.251
are the two of the paragraph below (`num_predict` and `placement`) and the
long reasoning blocks.

The detector library (070 §3.2) over the same five runs: no looping detector
fired on any block; four of the five have at least one reasoning block of
10,000 characters or more (the largest 29,345). Over the 531 imported runs
the same library, version 1, gives: sentence repeat in the reasoning of 156
runs (76 of 313 `PASS`, 27 of 87 `FAIL`, 19 of 38 `TIMEOUT`, 21 of 60
`ABORTED`, 13 of 33 `INVALID`), short-unit repeat in 6, no letter run, and
three or more identical tool calls in a row in 107. A run of the old
harness is not a run of this arm, so these are two descriptions, not a
comparison.

**Library version 2 (D75), the same day:** the thresholds above for a long
block and for identical tool calls were this project's own and are
withdrawn; the SDK's guards, with the thresholds it measured, are read
instead (070 §3.2). Over the 531 imported runs: reasoning repetition in the
reasoning of 64 runs (28 of 313 `PASS`, 12 of 87 `FAIL`, 9 of 38 `TIMEOUT`,
14 of 60 `ABORTED`, 1 of 33 `INVALID`); a reasoning loop in 27 (10 `PASS`,
5 `FAIL`, 6 `TIMEOUT`, 6 `ABORTED`); five or more identical tool calls in a
row, where the SDK escalates, in 29 (17 of 87 `FAIL`, 8 of 313 `PASS`);
two or more, where it warns, in 292. On the ten real runs none of the
SDK's guards fires; one run has two identical calls in a row.

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
| old harness | 1 | 11 | 10 | 1 `ABORTED` | 9m 15s |
| old harness | 2 | 67 | 66 | 1 `ABORTED` | 15m 13s (3m 51s to 48m 23s) |
| automaticum, SDK 4.100.250 | 4 | 5 | 5 | none | 12m 13s (6m 00s to 26m 22s) |
| automaticum, SDK 4.100.251 | 4 | 5 | 5 | none | 13m 26s (8m 14s to 18m 41s) |

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

The second half of the criterion: every way a run was silently invalid in
the old harness's notes (100 §2 and §8).

| way an old run was invalid | what catches it now | test that injects it |
|---|---|---|
| a template that swallowed its directives | gate `template` refuses a tag that no longer offers tool calls; the tag's digest can be pinned in the arm (gate `model_tag`); template hash, renderer and parser are recorded before and after, and a change makes the run `INVALID` | `internal/gate` ("no tools", "digest pinned and other"), `internal/run` `TestOperatorAbortAndEndpointChange`. **A template that lost directives and still offers tools is only caught by a pinned digest** |
| a run voided by hand with a marker file | `automaticum run invalidate --reason`: `INVALID`, the earlier verdict kept beside it | `internal/daemon` `TestStreamAbortStopInvalidate` |
| partial states and operator kills recorded as failures | an operator's end is `ABORTED`, with its own place in the precedence; a daemon restart makes the run in flight `ABORTED` | `internal/run` `TestVerdicts`, `TestOperatorAbortAndEndpointChange`; `internal/daemon` `TestRecover` |
| the CLI's own clock recorded as a failure | `TIMEOUT`, with which limit fired and who owned it | `internal/run` `TestVerdicts` |
| a lane script four days stale (old oracle, other limits) | there is no lane script: one daemon whose version is in every run; the lane image's SDK release is a gate; the oracle must be proven for the task as it is now; limits come from the arm and the task and are recorded with their owner | `internal/gate` (image faults, "task changed since the proof") |
| a model unloaded mid-run by another model's load | gate `free` makes the run wait while the endpoint holds another model; finding `model_reloaded` when the server loads the model again after the run's first request (read from the server's own load times on the wire) | `internal/gate` ("another model is in memory"), `internal/ingest` `TestModelReloaded`. **A finding, not a verdict: whether such a run is `INVALID` is not decided** |
| a workspace that was not pristine | every run gets a fresh directory copied from the task's files, with the hash of what it started from recorded; nothing is inherited | `internal/task` (workspace manifest); gate `workspace` |

Not done in milestone 3, and carried forward:

- **Criterion 8:** the library passes its own controls; reproducing the
  firings of omnimergekit's reference implementation on **its** fixture set
  waits for that set (asked for on 2026-10-13).
- `reanalyse --detector` and `--where`; the timeline and the baseline
  highlighting of the per-run report (070 §1.1); a mailbox notifier; the
  detectors near-duplicate rounds, edit oscillation and re-read loop.
- The gates that need an agent on the endpoint's host (milestone 7), and the
  parts of `free`, `gpu_residency`, `host` and `bind_address` that 050 §3.1.1
  lists as not yet looked at.

**Criterion 8, 2026-10-14:** the detector library and the reference code
(omnimergekit `loop_detect.py`, version 2, commit `01f5ed3`) give the same
flags on every block: 907 blocks of fixtures, controls and runs of both
harnesses, and the reference owners' two real cohorts of 9,611 blocks with
their expected output for both versions. No difference (070 §3.2).

### 3.3 Batch D81: base Qwen 3.6 27B against OmniMerge v4 (2026-10-13 and 14)

Both arms: `manic_miner` version 4, SDK 4.100.251, thinking `high`, the
same sampler (temperature 1, top-k 20, repeat last-n 0, repeat penalty 1),
128k window, on `windows-2` (NVIDIA GeForce RTX 3090, 24 GB, xOllama) from
lane `lane-1` on `linux-gpu-1`, interleaved. Ten runs per arm in two
batches: `k-d81-base-vs-omnimerge-v4` (five each, server 0.40.3-xollama.3;
its other ten runs were stopped before they started, for a release of the
server) and `k-d81-base-vs-omnimerge-v4-part2` (five each, server
0.40.3-xollama.4). Every number below is from `automaticum compare` over
both batches; no conclusion is drawn (D78).

| | base Qwen 3.6 27B | OmniMerge v4 |
|---|---|---|
| runs counted | 10 | 10 (run 8 counted again under D85, below) |
| `PASS` (95% interval) | 10 (72.2% to 100%) | 10 (72.2% to 100%) |
| wall time, median (least to most) | 11m 45s (5m 27s to 24m 49s) | 16m 41s (8m 10s to 41m 23s) |
| output tokens, median | 31,152 | 42,660 |
| reasoning tokens, median | 24,604 | 33,933 |
| tokens per solved task | 32,869 | 45,302 |
| requests, median (least to most) | 30 (16 to 42) | 36.5 (25 to 115) |
| first token | 13s | 9.3s |
| generation rate, median | 58.2 tok/s | 56.4 tok/s |
| compactions, reloads | 0, 0 | 0, 0 |
| window peak, median (most) | 35.4% (50.2%) | 34.9% (70.4%) |
| runs with 2 or more identical calls in a row | 0 of 10 | 1 of 10 |

Mann-Whitney, two-sided: first token p < 0.001, tool time p ≈ 0.008,
visible text tokens p = 0.011; every other metric p ≥ 0.089. Every run has
one failed request: the SDK's tokenizer probe (`/api/engine?...endpoint=
tokenize`) answers 404 while the model is not loaded yet, by design of
xOllama; the SDK's next probe, after the first generation, gets 200. It is
not a request of the agent's loop. The SDK makes it only for a side call
with no conversation behind it (a title or summary), to book that call's
prompt exactly; on a failure that one call goes unbooked, nothing is
estimated or retried, and no guard, compaction or context share uses it
(the Cerebriline session, from the SDK's source).

**Pause, first real test.** Run 8 (OmniMerge v4) was paused at 14:59:17
because another project needed the GPU: held two seconds later between two
requests, the model dropped, the device empty. It was resumed at 15:25:39;
the next request loaded the model again in 26.5 s, fully on the GPU, and
the run went on to `PASS` by the oracle with 17m 56s of wall time (26m 23s
held, not counted). The endpoint rule (D73) then made it `INVALID`
("the model was loaded again during the run"): it cannot tell a reload the
pause caused from one nobody asked for. The previous verdict, `PASS`, is
kept in the record. **Decided by the owner the same day, D85:** a reload
after a pause keeps the verdict. Run 8 was analysed again: `PASS`, the
reload a note (`after_pause`), and the numbers above count it.

**Found on the way (bug-016):** the two server releases list a tag's
parameters in another order; `compare` called that a change. Fixed: the
parameters are compared as values.

**Analysed again under analysis version 3 (2026-10-14):** every run of
this harness was analysed again (`automaticum reanalyse`: 69 runs; the 14
`SKIPPED` ones have nothing to analyse). For batch D81 no verdict, metric,
finding or cap audit changed: the basis's `detectors` fact now reads
"analysis 3" for both arms, and three metrics of the scripted user appear,
0 in every run. Across all 69 runs: no verdict changed; the two false
`wire_arm` warnings of the first mode C cell went (bug-019); three
`demo/scripted` runs count the request a steer cut as `finish.failed`
instead of `finish.none` (bug-020). The numbers above are those of
analysis 3.

### 3.4 Milestone 5, step 0: the installation on blackswan-2 (2026-10-23)

At the owner's request, blackswan-2 has a stable installation of its own
(D93, D95), separate from the main host's and not reached through it:

- the daemon runs as a systemd service that restarts on failure and waits
  for the data disk and the container engine (`examples/systemd/`);
- the binaries are the main host's build of the same commit (glibc 2.28
  baseline), and a clone of this repository at that commit is the
  installation's `repo`;
- the SDK bundles 4.100.250 and 4.100.251 were copied with their archived
  packages, and `automaticum sdk verify` reproduced both with no network, at
  their pinned hashes;
- the lane images were built there; every task's oracle was proven there with
  `automaticum oracle verify`, and the rebuilt `run_game` has the pinned hash;
- `demo/answer` on the stub passed on both SDK builds.

The engine there (Docker 29.1) refuses API versions below 1.44, which the
client pinned at 1.41; the client now uses the engine's minimum when it is
newer (bug-027). The installation is updated by checking out a newer commit
and copying that commit's binaries, with the lane idle and the queue empty.

The same day the installation was made ready for the loop test of D98:
SDK release 4.100.258 provisioned there and here (pinned bundle, reproduced
offline on both), the runner at protocol 4 with the stop at the first guard
trip (D99), and the probe of a llama-server endpoint reading `/props` (050
§3), so that each run records the template and the weights file of the
server it ran on. The owner then narrowed the test to one server: the
BF16 file with Google's chat template and end token 106, with its two
guard arms (`arms/g4-128e-bf16-*`), 10 runs each. With SDK 4.100.258 an OpenAI-compatible request carries
nothing for "thinking on" (only an effort level is sent, as
`reasoning_budget_tokens`), so these arms leave thinking to the server, which
starts with it on; `thinking.blocks` shows it per run.


### 3.5 Milestone 5, next after the loop batch: runs exported as conversations (D102, D104)

**Decided by the owner (2026-10-23):** after the Gemma-4 loop batch,
automaticum gives omnimergekit's recovery trainer the runs' model requests
as training traces (D102). **Revised the same day (D104):** the traces are
the SDK's own "Save model requests" capture, the plugin's switch, not an
export automaticum rebuilds from its logs. Reasoning scope, inlined
`<think>` blocks and compaction all reshape what is sent, so only the
capture taken at the point of sending is faithful, and its layout is the
plugin's, the same for every host.

- The SDK release that carries it (after 4.100.260) saves the exact body
  of every generation request and, beside it, the raw response stream, so
  the reasoning is whole as generated; the file writer is in core, with a
  `ClineCoreOptions` switch. 4.100.260 has the request half only
  (`Llms.setModelRequestCapture`).
- The runner turns the switch on in every run; the capture is kept in the
  run record as a blob, with the run's verdict, arm, provider, sampler,
  template and the resolved prior-reasoning scope beside it.
- `automaticum export conversations --batch <id> [--run <id>] --out <dir>`
  hands out those files and that metadata; it does not rebuild turns.
  Loop spans (character offsets of the repeats, the second occurrence on)
  come from the detector library (070) only, as agreed with omnimergekit.
- `manic_miner` runs are the held-out gate and are never exported as
  training data; the training pool is the multi-language suite (063) once
  it has tasks.
