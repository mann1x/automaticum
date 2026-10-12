# automaticum — requirements from the omnimergekit evaluation protocol

**Status:** draft 0.1, 2026-10-12. Written by the omnimergekit session (the
second user of the tool) at the owner's request. It is a companion to
`automaticum-draft-spec.md` (draft 0.4), not a replacement: it states what the
model-evaluation side needs, and where the draft already covers it. Everything
marked **Open** is a question, not a decision. References of the form "draft
5.4" point into the draft spec; sections here are numbered `R1`…`R14` so the
two documents never collide.

**One sentence:** omnimergekit needs automaticum to run coding and
tool-calling suites (SWE-bench, BFCL, Terminal-Bench, our own multi-language
suite) with Cerebriline as the agent engine in place of each harness's own,
and to hand back a result that the omnimergekit protocol can put in a table
without anyone re-reading samples by hand.

**Source of every requirement:** `omnimergekit/eval/EVAL_PROTOCOL.md`,
`eval/omk_eval.py`, `eval/agentic/` and the incidents behind them. Where a
requirement names a number, the number was measured there.

---

## R1. Three ways omnimergekit will use it

| mode | who owns tasks and scoring | who owns the agent loop | who owns the model server |
|---|---|---|---|
| **A. Autonomous** | automaticum (a suite, draft 7) | Cerebriline, in a lane | an endpoint automaticum is given |
| **B. Engine for an external harness** | the external harness (SWE-bench, BFCL, Harbor, Inspect) | Cerebriline, called by the harness | an endpoint automaticum is given |
| **C. Backend of `omk_eval`** | automaticum, on behalf of an omnimergekit template | Cerebriline | omnimergekit serves the model and passes the endpoint |

Mode C is how automaticum becomes "part of the protocol": an omnimergekit
template says `backend: automaticum`, `omk_eval.py` starts or reuses a server,
calls automaticum, waits, and folds one result file into its own
`summary.json` (R3). Modes A and B are what runs underneath.

The owner's stated reason for mode B: the external harnesses' own agent
engines are poor, and the score they report is mostly the engine. Measured in
our protocol: the same model scored 60.47 / 59.20 / 53.73 on SWE-bench under
three scaffolds, 6.7 points from the scaffold alone. Two consequences:

- A score from mode B is a score of *model + Cerebriline build*. It is never
  comparable to a published number for the same suite under another engine,
  and every report must say so in its header.
- The harness build (draft 3.1, draft 5.4) is part of the basis (R4). Two runs
  on different Cerebriline builds are two arms, even with identical weights.

---

## R2. Terms this document needs

Proposed additions to the draft's vocabulary table (draft 2). They are listed
here, not yet added there; the automaticum session owns that table.

| term | meaning |
|---|---|
| **cell** | one suite (or named subset) scored for one arm: the unit that gets one headline number. A batch produces a cell when the suite has many tasks. |
| **cohort** | a named set of cells meant to be compared, with one **declared variable**. |
| **basis** | every recorded fact that must be equal for two cells to be compared (R4). |
| **headline metric** | the one metric a suite subset declares as its score, with its direction. Declared in the suite, never chosen at report time. |
| **cap hit** | a generation ended by a length limit (`finish_reason == length` or the server's equivalent), at any turn, not only the last. |
| **errored** | a task that produced no scoreable result because of the harness, the server or the environment. Counted apart from `FAIL` (draft 4.3 already separates `ABORTED`/`INVALID`; "errored" is their union in reports). |
| **engine adapter** | the piece that lets an external harness call the Cerebriline runner as its agent (R6). |

---

## R3. Mode C: the contract with `omk_eval`

`omk_eval` already drives four non-lm-eval backends the same way (`mrcr`,
`nolima`, `ruler_native`, `agentic`): a subprocess writes one JSON, and
`extract_canonical_score()` reads it. automaticum should fit that shape.

### R3.1 Invocation

One blocking command, safe to re-run:

```
automaticum cell run \
  --suite <suite[:subset]> --arm <arm file or id> \
  --endpoint-url http://<host>:<port>/v1 --served-model <name> \
  --out <dir> --idempotency-key <key> --json
```

- The endpoint is passed in, already serving. automaticum probes it (draft
  5.3) but does not start or stop it in this mode.
- Arm fields that `omk_eval` resolves (sampler, thinking, output budget,
  context) arrive as explicit values with `source = "omk template"`. The
  runner still reads them back from the built session and from the wire
  (draft 3.1, draft 8.3 "parameter drift").
- Re-running with the same key resumes: finished runs are not repeated,
  unfinished ones restart. This is the equivalent of lm-eval's `--use_cache`,
  which our protocol makes mandatory because every long run dies at least
  once.
- Exit codes are stable and distinguish: finished and clean; finished with
  warnings; gate refused (nothing ran); incomplete (resume possible); internal
  error. `omk_eval` maps them to its own.
- A line-oriented progress stream on stdout (`started`, `n_done/n_total`,
  `finished`) so the caller's log shows movement; our protocol requires a
  liveness check every ten minutes on long runs.

### R3.2 The result file

`<out>/automaticum_result.json`, written once, atomically, at the end. This is
the only file `omk_eval` reads for the score.

```json
{
  "schema": "automaticum.cell/1",
  "suite": "swe-bench", "subset": "verified-50", "suite_version": "…",
  "task_set_sha256": "…", "n_expected": 50,
  "arm_id": "…", "cohort": "…", "declared_variable": "…",
  "headline": {"metric": "resolved_rate", "value": 0.42, "higher_is_better": true},
  "counts": {"pass": 21, "fail": 26, "timeout": 2, "aborted": 1, "invalid": 0, "skipped": 0},
  "scores": {"resolved_rate": 0.42, "loop_rate": 0.06, "empty_patch_rate": 0.04},
  "basis": { "...": "R4, every field" },
  "token_stats": { "...": "R9.1" },
  "generation_caps": { "...": "R9.2" },
  "degeneration": { "...": "R9.3" },
  "tool_calls": { "...": "R9.5" },
  "warnings": ["…"],
  "runs": "runs.jsonl",
  "started_at": "…Z", "finished_at": "…Z", "duration_s": 0
}
```

Rules that come from incidents:

- The headline is computed over `n_expected`, and `counts` must sum to it. A
  score over a different denominator than its sibling is not a delta.
- `errored` tasks are never silently dropped and never silently counted as
  failures; the file says how each was counted.
- `warnings` is machine-readable and non-empty when a cap, a basis mismatch, a
  detector self-test failure or an incomplete set was seen. A warning is a
  reason not to tabulate, so it must be in the file, not only in a log.
- `runs.jsonl` has one row per run: task id, verdict, the per-run numbers of
  R9, and the run id to drill into. It is the equivalent of lm-eval's
  `samples_*.jsonl`, which our protocol requires us to inspect after every
  run.
- Timestamps in UTC with offset. Our hosts sit in two time zones.

### R3.3 Results are never deleted

Evaluation results, raw transcripts and adapters are permanent in this
project. A scorer or detector change must be able to re-score from stored raw
material (draft 8.3 `reanalyse` covers detectors; the same is needed for
scores). The store must therefore retain, per run, enough to re-run the
scorer without the model: the final workspace or patch, the tool-call trace,
and the model's raw output.

**Open:** whether `automaticum cell run` may also start the server itself in
mode C (asking an endpoint to load a model, draft 5.3), or whether `omk_eval`
always owns serving. Today omnimergekit owns it, because GPU placement, the
context per slot and the serving library are protocol-locked there.

---

## R4. Basis: what must be recorded for a number to be comparable

The draft records the arm (5.4) and the endpoint identity (4.1). The
evaluation side needs those gathered into one comparable object, with a hash,
and a report that refuses to pool across it. Each field below was the cause of
at least one invalid comparison.

| field | why |
|---|---|
| weights: repository or path, revision, sha256 of each file | a merge output was once bit-identical to its base |
| quantization, and whether an imatrix was used, read from the GGUF metadata | a filename is not provenance |
| chat template: sha256 and byte size as served | a tool-calling score grades the template too |
| sampler as sent: temperature, top-p, top-k, min-p, repeat penalty, seed, and its **name** (`template_default`, `recommended`, …) | a greedy cohort and a sampled cohort were once merged |
| thinking: on/off, effort, budget, and who owns it | a budget moves both score and length |
| output budget per request, and context per slot **as loaded** | the server's total context divided by its parallel slots is what a request gets |
| stop sequences | a stop word fired inside the thinking channel and halved a score |
| server kind, version, and the build time and hash of the serving **library** (for llama.cpp: `libllama.so`, not the thin binary) | two arms straddled a rebuild |
| reasoning parser / format flags, tool-call parser | flags decide whether control tokens leak |
| concurrency and batch composition | greedy is not bit-reproducible under batching |
| GPU, driver, residency (all layers on GPU), power cap | draft 4.1 has these |
| harness build: Cerebriline SDK version, commit, dirty flag, package sha | R1 |
| agent settings, fully resolved | draft 5.4 |
| suite version, task-set sha256, the exact task ids in order | a subset is a different bench |
| scorer identity: tool, version or commit, image digest | a scorer fix voids every earlier cell of that bench |
| detector versions | a loop rate from another detector is another number |
| seed set, when sampling | the seed set is part of the basis |

Requirements:

- `automaticum basis diff <cell A> <cell B>` prints exactly the differing
  fields. For a cohort with a declared variable, the diff must show that
  variable and nothing else, or the report labels the comparison confounded.
- Reports refuse to put cells with different bases in one table unless told
  to, and then label every column that differs (draft 8.4 "comparability" for
  task boundaries; this extends it to the whole basis).
- Greedy is the anchor for cross-arm tables in omnimergekit. A sampled cell is
  allowed, as a separately named sampler cohort; the report never mixes the
  two.
- A value is recorded *as probed or as sent*, never as configured.

---

## R5. Gates the evaluation side needs before a run

In addition to draft 6.2. A refusing gate must notify (draft 9.3 mailbox),
not only exit non-zero: a gate that dies quietly looks like a queue that is
merely slow.

1. **Sampler and thinking read-back** on the first request of every arm,
   compared to the arm. Mismatch → no run.
2. **Context per slot** read from the server, at least prompt + output budget
   for the suite's largest task. A too-small slot truncates silently.
3. **Tool-call parse failures must degrade, not throw.** An unpatched
   llama.cpp returns HTTP 500 on a parse failure and the generation is lost,
   which scores a correct answer as a failure and biases whichever arm emits
   more parser-stressing output. The gate checks the serving library for the
   degrade behaviour and refuses otherwise.
4. **Bind address.** Lanes are containers, so the model server must be
   reachable from a container; serve on the Docker bridge or a private
   interface. automaticum must refuse to use, and never start, a server bound
   to all interfaces: these servers have no authentication and our run hosts
   have public addresses.
5. **Scorer validation.** For external suites: the official gold patch or
   gold answer passes the scorer on a sample of tasks before the first cell
   (the SWE-bench harness and BFCL both ship gold). For our own suite this is
   draft 4.3 `oracle verify`. A check that gold fails is a broken check.
6. **Detector self-test** (R9.3) at start; a failing detector marks its
   metric unavailable instead of reporting zero.
7. **First-generations canary.** After the first three to five finished
   requests: none empty, none ended by length, no control tokens in visible
   text, median length within the band the suite declares. Fails → stop the
   batch. Twice we ran two hours at the wrong sampler before anyone looked.
8. **Disk.** Image and environment size for the suite's task set against free
   space and the host's floor (Terminal-Bench: 49 GB compressed for 89 tasks,
   about 160 GB on disk; SWE-bench is larger).
9. **Credentials.** Gated datasets need a token; check before starting, read
   from the secret store, never written to a run record, a command line or a
   log.
10. **GPU ownership.** A GPU that is not ours is never used, even when idle;
    a lane's GPU binding is explicit, and the gate verifies the process landed
    only on that device.

---

## R6. Mode B: Cerebriline as the engine of an external harness

Each harness wants its agent in a different shape. automaticum should ship one
**engine adapter** per shape and keep its own run record for every task the
harness sends it, so diagnostics (R9) are the same whichever harness asked.

| shape | harness examples | what the adapter is |
|---|---|---|
| **Predictions file** | SWE-bench | automaticum runs the agent on each instance in mode A, writes the `predictions.jsonl` the official harness consumes (`instance_id`, `model_name_or_path`, `model_patch`), then runs the official evaluation images and ingests their reports |
| **Agent in the harness's container** | Harbor / Terminal-Bench, SWE-agent-style runners | an installable agent the harness launches inside its task container; it runs the Cerebriline runner there and streams events back to the daemon |
| **Solver / model inside a Python framework** | Inspect (`inspect_evals/bfcl`), lm-eval | a thin Python shim that forwards each sample to automaticum's API and returns the agent's final answer or tool calls in the framework's types |
| **OpenAI-compatible proxy** | any harness that only knows a chat endpoint and keeps its own loop | Cerebriline's provider layer and tool-call parser exposed as an endpoint; no agent loop of ours |

Requirements common to all four:

- The harness's task id maps one-to-one to an automaticum run; the harness's
  own result (reward, resolve status, score) is ingested as the oracle
  evidence, with the scorer's identity (R4).
- The harness's limits (per-task timeout, step limits, cost limits) are
  recorded as arm fields; a limit we set is not model behaviour and must be
  distinguishable in the verdict (`TIMEOUT` by the harness vs by the agent).
- The adapter must not give the agent anything the harness withholds. Aider's
  polyglot set hides the tests from the agent; SWE-bench hides the failing
  tests. A harness where the agent can see the oracle is a different bench.
- Concurrency is the harness's request, but lanes and endpoint capacity are
  automaticum's; the adapter queues rather than oversubscribing.
- Resume: a half-finished external evaluation continues from the finished
  instances (draft 7.3 asks this for SWE-bench; it applies to all).
- Harness result files are read by their real layout, verified on a known
  run. (Harbor writes a `result.json` for the job and one per trial, and its
  `pass_at_k` block is empty even on perfect runs; the score comes from
  per-trial rewards.)

**Open:** whether the "agent in the harness's container" shape can respect the
draft's hard constraint (Node only inside *our* lane image, draft 3), since
the container is the harness's, built per task. Options to weigh: install the
runner into the task container at start (what Harbor's installed agents do);
or keep the agent in our lane container and give it a remote shell into the
task container.

---

## R7. BFCL

What omnimergekit runs today and needs kept comparable:

- `inspect_evals/bfcl`, pinned **by commit** (the project re-cuts version tags
  over a moving main), category `multi_turn_base`, n = 200, plus
  `multi_turn_long_context` and `multi_turn_composite` for degeneration work.
- The V3 scorer compares the final state of the backend objects and the
  execution results with ground truth. No model judge. This must stay the
  scorer: it is why the cell is usable when an effect is a few points.
- The comparison between two arms is **paired** on sample ids (R10).

Draft 7.2 leaves **Open** whether the agent loop is in play or only the
provider and parser. Input from this side, not a decision: they are two
different cells and omnimergekit wants both, named apart.

| cell | loop | what it measures | comparable to |
|---|---|---|---|
| BFCL through the proxy shape | the harness's | model + chat template + Cerebriline provider and parser | our existing `agentic_bfcl_*` cells, if the parser is the only change |
| BFCL through the agent | Cerebriline's | the whole engine on stateful tool use | only itself across arms |

Also needed per entry: the raw output; how each call was obtained (native
tool call, repaired, read out of text, read out of reasoning — draft 7.2 has
this); the number of turns; cap hits at any turn; and the reasoning length per
turn. Known property to carry into reports: BFCL multi-turn reasoning is short
(median 252–480 characters measured), so it is a tool-use cell and a poor
degeneration cell. A null loop rate on BFCL says nothing about long-context
looping.

---

## R8. SWE-bench

- Subsets: Verified, Lite, and **fixed named subsets** with their instance-id
  list stored and hashed (a 50-instance cell is what fits a single GPU per
  arm). The subset is part of the basis; a subset chosen after seeing results
  is not a bench.
- Scoring only by the official harness images, version-pinned and recorded by
  digest. Per instance: `resolved`, `unresolved`, `empty_patch`,
  `patch_apply_failed`, `error`, with the test logs. Map: resolved → `PASS`;
  unresolved, empty patch, apply failed → `FAIL` with the reason as a finding;
  harness error → `ABORTED`. `empty_patch_rate` is reported next to the
  headline, because an agent that gives up scores like one that tries and
  fails.
- Gold-patch validation on the subset before the first cell (R5.5), and a
  record of instances whose gold patch does not resolve on this host (they
  exist; they must be excluded by a recorded list, not silently).
- Flakiness: an instance whose result differs between two evaluations of the
  same patch is flagged and excluded from paired comparisons.
- Budgets per instance: wall time, requests, tokens. Recorded as arm fields.
- Environment caching per instance and sharding across lanes (draft 7.3).
- The long transcripts here are where degeneration shows: every R9 detector
  must run per turn, and the headline for a degeneration cohort may be
  `loop_rate`, not `resolved_rate` (R9.3).

**Open:** SWE-bench-style sets for other languages (Multi-SWE-bench,
SWE-bench-java, …) — which, and whether they count as part of R11's own
suite or as external suites.

---

## R9. Diagnostics the protocol gathers on every cell

The draft's section 8 already lists most agent-side metrics. This is the list
the evaluation protocol treats as mandatory, with the definitions it uses, so
numbers from automaticum and from `omk_eval` mean the same thing.

### R9.1 Length and token statistics

Per request, from the server's own `usage` block whenever it exists (not
re-tokenized afterwards): prompt tokens, cached prompt tokens, completion
tokens, reasoning tokens, `finish_reason`, time to first token, generation
time. Where the server gives no split, the split method is recorded (draft
8.2 says this).

Per run and per cell:

- completion tokens and characters: n, sum, p10, p50, p90, max;
- the same for reasoning alone and for visible text alone;
- `finish_reasons` histogram; `empty_completions` (nothing visible and no tool
  call); turns per run; requests per run;
- prompt growth: prompt tokens at first and last request, peak context
  occupancy, share of prompt served from cache;
- **conditioned on the verdict**: token p50 and p90 over `PASS` runs only, and
  tokens per solved task = total completion tokens of the cell ÷ number of
  `PASS`. A median over all runs rewards a model that gives up early; the
  per-solved figure cannot be gamed in either direction.
- throughput: generation tokens per second per request, with the concurrency
  it was measured at. A rate from the easy first tasks is not the rate of the
  suite; report it over the whole cell.

Sanity failures that become warnings: n ≠ expected; empty completions above
5 %; p10 of visible characters under a suite-declared floor.

### R9.2 Generation-cap audit

- Count cap hits per run **at any turn**, not just the last: in an agent loop
  the mid-loop cap is the failure.
- Per cell: capped runs, capped requests, and of the capped runs how many
  still passed.
- A pile-up test for servers that do not report `finish_reason` honestly: two
  or more completions within a few tokens of the longest, at a plausible
  limit, on at least a minimum number of rows. Small samples say "unverified",
  never "capped".
- Verdict per cell: `CLEAN`, `CAPPED`, `UNVERIFIED`. A `CAPPED` cell is not
  comparable to a cell at another budget.
- Between two arms: if cap hits differ by 1.5× or more, the report refuses to
  call the score difference clean; the bench has become a length meter.
- The reasoning-budget regime is recorded (unlimited, budgeted, off), because
  a budget relocates rumination rather than removing it.

### R9.3 Degeneration and looping

One detector library, versioned, imported everywhere, never re-declared in a
report script. Evaluated per assistant block (reasoning and visible text
separately), per run, per cell. The detectors we use and the lessons paid
for:

| detector | definition used today | lesson |
|---|---|---|
| short-unit repeat | a 2–40 character unit repeated ≥ 8 times | must require word content (≥ 3 letters): on code-bearing transcripts every firing was indentation or a number printout |
| character run | one character repeated ≥ 41 times | same rule |
| sentence repeat | an identical unit of ≥ 60 characters repeated ≥ 3 times **and** making up ≥ 30 % of the block's units, or repeated ≥ 8 times outright | character detectors saw 1 and 9 blocks where this saw 29 and 89; the worst block repeated one sentence 402 times in 421 and the character detectors called it clean. Count alone is not enough either: 3–6 repeats with thousands of new characters between them is iterative drafting |
| runaway | a single reasoning block over a declared size (10,000 characters by default) | per suite, since healthy length differs |
| repeated tool call | identical name + arguments N times | draft 8.3 |
| near-duplicate rounds | draft 8.3 "changing ordinal" | draft 8.3 |

Requirements:

- **Positive and negative controls**, per domain. Each detector ships a
  fixture it must fire on and healthy long text (prose, code, tool output) it
  must stay silent on; the self-test runs at start (R5.6) and its result is
  in the result file. A zero from a detector with no proven non-zero floor is
  not a measurement.
- `loop_rate` = runs with at least one firing ÷ runs, reported with its
  components (which detector, reasoning or text, turn index).
- Always cross-tabulated with the verdict and with cap hits: loops among
  passes, loops among failures, loops that ended in a cap. A loop rate alone
  cannot tell a harmless tic from a task-wrecking one.
- A suite subset may declare `loop_rate` as its headline metric. For a
  degeneration hypothesis the pass rate is a downstream proxy that moves only
  when a loop is severe enough to wreck the task.
- Agent-side interventions change this measurement and must be arm fields
  with ownership (draft 5.4 has guards and compaction): a reasoning-loop guard
  or history compaction that hides a loop is a different arm, and the report
  says which runs had a guard fire. For a test of what the **model** does, the
  arm runs with those off.

### R9.4 Output hygiene

Per run, counted and quoted: empty turns; visible text under five characters;
control tokens or channel markers in visible text or history (draft 8.3
"template leaks"); thinking that never closed; reasoning emitted as text or
text emitted as reasoning; markdown fences where the scorer executes raw code;
answers present only inside the reasoning; a stop sequence that fired inside
the reasoning.

### R9.5 Tool calls

Calls per run and per tool; parse path per call (native, repaired, from text,
from reasoning); schema violations; unknown tool names; calls truncated by a
cap; server parse errors (HTTP 5xx on a generation) counted apart from model
errors; share of turns with no call and no final answer.

### R9.6 Reasoning carried between turns

Whether prior-turn reasoning is re-sent in later requests is a property of the
chat template and of the engine, and it changes fresh reasoning length
(measured: median 252 vs 467 characters). Record per request whether history
contains earlier reasoning, and how many tokens of it.

---

## R10. Comparing arms

Draft 8.4 covers intervals and A/B tests. Required specifics:

- Two arms on the same task ids are **paired**: McNemar on the discordant
  pairs (exact binomial), reported with the two discordant counts. Errored
  tasks are excluded from the pairs and listed.
- A repeat of the same arm on the same basis gives the jitter band; a
  difference inside the band is reported as not distinguishable, whatever its
  sign. One repeat per cohort is the minimum to state a band.
- A zero or a null result carries its power: n, the smallest difference this
  n could have shown.
- The report states the basis diff (R4), the cap comparison (R9.2) and the
  denominator of each arm above any difference it prints.
- Attribution of a change across categories (language, difficulty, task
  family) is done on balanced axes, with per-category n shown.

---

## R11. Our own multi-language coding suite

The owner wants a suite of our own across programming languages. Requirements
on automaticum so that such a suite can exist; the tasks themselves are
omnimergekit's to write.

- **Two task forms** in one suite, scored apart:
  - *completion*: one prompt, one answer, scored by hidden tests (what
    HumanEval and MultiPL-E do). No agent loop; this goes through the proxy
    shape or a no-tools arm. Our protocol runs code benches of this form with
    thinking off; that is an arm field, not a suite constant.
  - *agentic*: a small repository, an instruction, hidden tests; the agent
    edits and may run the visible tests only.
- **Language layers** (draft 5.2) pinned by digest, one per language, with the
  compiler or interpreter version in the basis. Per-language time limits
  calibrated from the reference solution's run time on the lane, not one
  global limit (a global limit is a language filter).
- **Hidden tests** live outside the workspace and are run by the harness
  (draft 4.3). Visible tests, when given, are a separate file set.
- **Reference solution and a known-bad solution per task per language**; the
  suite is admitted only when `oracle verify` passes for all, and the
  reference is run several times to reject flaky tests.
- **Parallel tasks**: the same problem in several languages shares a problem
  id and has its own task id. Reports show both per-language and per-problem
  views. The same problem in two languages is two draws, not a repeat.
- **Tiers and families** as task labels (difficulty, kind: algorithmic,
  refactor, bug fix, API use, build/config), so that a score change can be
  attributed.
- **Partial credit** recorded (tests passed ÷ tests) but never as the
  headline; the headline is all-tests-pass.
- **Versioning**: any change to a task, test, limit or toolchain is a new task
  version and a comparability boundary (draft 4.3).
- **Leakage**: tasks carry a canary string and a private flag; export and the
  semantic-search embedder must not send private task text to a cloud
  endpoint.
- Diagnostics of R9 apply unchanged; additionally compile errors vs test
  failures vs timeouts per language.

**Open:** which languages first (draft Appendix C item 8 asks the same for
language layers; this should be one answer).
**Open:** whether completion-form tasks belong in automaticum at all, or stay
in `omk_eval`/MultiPL-E with automaticum only running the agentic form.

---

## R12. Operations in full autonomy

- **GPUs are shared and partly not ours.** Lanes bind to named devices;
  automaticum never uses a device it was not given, never preempts, and waits
  on an external busy signal (draft 6.1 has this). One heavy job per GPU.
- **Never kill by process name.** Stop what automaticum started by the ids it
  recorded. A stale orphan with a matching name has both blocked and fooled
  our liveness checks.
- **Readiness is a predicate, not a sleep**: an endpoint is ready when a
  one-token generation returns and the model identity matches (draft 5.3).
- **Unattended progress**: a heartbeat with counts every ten minutes to the
  log and on request; `wait` primitives (draft 9.3); a mailbox message on
  campaign end, on any refusing gate, and on a stopped batch.
- **Never skip silently**: a task not run has `SKIPPED` with a reason, and the
  cell is marked incomplete.
- **Disk**: large artefacts go to a configured data root with a floor; never
  to a RAM-backed temp directory.
- **Secrets** by reference only (draft 5.3). Nothing secret in a run record,
  an export, an argv or a mail.
- **Backup**: a per-campaign export (R13) that can be mirrored to another
  host; our rule is that every result exists on the master host too.

---

## R13. Exports omnimergekit will consume

- `automaticum_result.json` + `runs.jsonl` per cell (R3.2).
- A `STACK.txt`-equivalent per cell: the basis in plain text, so that the diff
  of two arms' files is the audit of a cohort.
- Raw per-run artefacts by run id on request: transcript, requests as sent,
  patch or final workspace diff, scorer logs.
- `automaticum export cohort <name> --format parquet|jsonl` with one row per
  run and every R9 number as a column, for analysis outside the tool.
- A layout option matching omnimergekit's result tree,
  `<results>/<suite>/<served-model>/`, so existing tabulators find the files.

---

## R14. Open questions raised here

To be mirrored into the draft's Appendix C by the automaticum session.

1. Mode C serving: may automaticum start the model server, or does `omk_eval`
   always own it? (R3)
2. The "agent in the harness's container" shape against the Node-only-in-our-
   image constraint. (R6)
3. BFCL: one cell or two (proxy shape and agent shape)? This side asks for
   both, named apart. (R7; same question as draft 7.2)
4. Other-language SWE-bench-style sets: which, and under which suite. (R8)
5. Own suite: which languages first; whether completion-form tasks live in
   automaticum. (R11)
6. Whether the vocabulary takes `cell`, `cohort`, `basis`, `headline metric`,
   `cap hit`, `errored`, `engine adapter` as proposed in R2, or other names.
7. Which loop detectors are the shared library's first version, and whether
   that library lives in automaticum (Go) with omnimergekit calling it, or in
   omnimergekit (Python, `eval/agentic/loop_detect.py` today) with automaticum
   porting it. Two implementations of one detector will drift.
8. Whether guards and compaction default **off** for evaluation arms (to
   measure the model) and on for product arms (to measure the product). (R9.3)

---

## Appendix — where the draft already covers a requirement

| requirement here | draft section |
|---|---|
| arm fully resolved, values as sent, ownership of sampler and thinking | 4.1, 5.4 |
| endpoint identity, GPU residency, quantization read from the GGUF | 4.1, 5.3 |
| harness-run oracle with known-good and known-bad fixtures | 4.3 |
| verdict enum separating fail, timeout, aborted, invalid | 4.3 |
| task versions as comparability boundaries | 4.3 |
| per-run tokens, thinking, context, tool metrics | 8.2 |
| loop, tool-call, leak, drift, server-side findings | 8.3 |
| intervals, A/B tests, grouping, refusal to pool | 8.4 |
| re-analysis after a detector change | 8.3 |
| wait primitives, mailbox, idempotency, dry run | 9.3 |

What this document adds on top: the cell-level result file and its contract
with `omk_eval` (R3); the basis as one hashed object and its diff (R4); the
evaluation gates (R5); engine adapters for external harnesses (R6); the
length, cap and degeneration definitions the protocol already uses (R9); the
paired-comparison rules (R10); and the requirements for a multi-language suite
(R11).
