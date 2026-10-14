# 020 — The run record, verdicts and the basis

**Status:** draft 0.9, 2026-10-14.

Nothing in this file is optional. Each item is here because its absence
invalidated or blinded a real campaign. A value is recorded **as probed or as
sent, never as configured**.

## 1. Identity and provenance

- Run id (sortable by time), batch, cell, cohort, campaign, arm id, task id,
  task version, lane, host.
- **The arm, fully resolved:** every field of 030 §5 with its value as sent,
  plus where each value came from (arm file, parent arm, default, model tag,
  endpoint, caller such as an `omk_eval` template).
- **Harness build:** SDK version, commit, dirty flag, sha256 of the installed
  package; runner version; lane image digest; language layer digests;
  automaticum version.
- **Endpoint identity as probed at start:** server kind and version; build time
  and hash of the serving library (for llama.cpp the shared library, not the
  thin binary); feature list; reasoning and tool-call parser flags; the model's
  digest, capabilities, template (sha256 and byte size as served), renderer,
  parser, parameters; context loaded and context per slot; parallel slots; GPU
  residency (layers on GPU out of total); quantization and imatrix read from
  the GGUF metadata.
- **Weights:** repository or path, revision, sha256 of each file.
- **Task identity:** suite, suite version, sha256 of every input file, the
  exact prompt text sent, the oracle's sha256, the scorer's identity (tool,
  version or commit, image digest).
- **Host facts:** GPU, driver, power cap, free VRAM at start.
- **Limits in force:** wall time, idle time, requests, tokens, steps, and who
  set each (arm, task, external harness).

### 1.1 Where and on what the model ran

**Decided by the owner (2026-10-13):** every run records the inference
point, the model's family and variant, the GPU and the host it ran on, and
its sampling parameters (D70 to D72). As built, the run's record holds an
`inference` object, each fact with where it comes from:

| fact | source |
|---|---|
| endpoint name, kind, server version, engine | the server |
| the endpoint's host and the lane's host, by neutral name | the configuration |
| GPU and its memory | **declared** in the endpoint's `[hardware]` table (030 §4.2); `measured` stays empty until an agent on that host reads the device (milestone 7) |
| share of the model on a GPU after the run | the server |
| the model as served at the start of this run: digest, quantization, parameter size, template hash, tag parameters | the server, asked before every run (D82): a served model can change under the same name, and `automaticum compare` shows every distinct answer per arm and says when one changed between runs |
| `added_later`: when, and from what | set only when the facts were added to a finished run's record by `automaticum run identify` (D74); the server's answers in it are still the run's own |
| model tag and digest | the arm and the server |
| model family, size, variant, lineage | **stated** by the owner in the model catalogue (030 §4.1); what is not stated is listed as such |
| family, parameter size, quantization, format | the server's own words for the tag |
| base model, size label, parameter count, who quantized it | the weights file's own metadata, as the server shows it |

The server's word and the stated identity are both kept and never merged: a
server calls a Qwen 3.6 tag `qwen35`, and no server knows a merge's name.

A run whose tag has no catalogue entry, or an incomplete one, runs all the
same and carries the finding `model_identity`, which names what is not
stated (D71).

**Sampling and thinking as in effect.** `run show` prints one table per
run: for every parameter, the value that was in effect and why (sent on the
wire, the model tag's own value because nothing was sent, or the server's
answer), next to what the arm states, what was sent and what the tag sets.
It is derived from the arm, the tag's parameters as served before the run,
and the `wire_arm` findings (070 §4.1), so it can be recomputed.

Not yet: these facts as columns to group reports by. They are in the
record's JSON under stable names; columns come with the reports of
milestone 4.

## 2. The streams

- The agent event stream as the SDK emits it (`CoreSessionEvent`), complete,
  compressed, never pruned. No field is dropped at source; size is handled at
  storage (080 §4).
- **Every request and response as they crossed the wire**, captured by the
  runner at the HTTP layer (040 §4): rendered system prompt, message list, tool
  schemas, sampler, thinking fields, context and output-budget fields, stop
  sequences, and the server's `usage` block. This is the proof of what was sent.
- The SDK's session state for the run (messages, compaction state, rounds).
- The runtime's own log (output caps, discarded turns, compaction, retries).
- The server's log for the window of the run, fetched from the endpoint's host,
  with the log level verified before the run.
- Workspace before and after, and a diff. Optionally a snapshot per transaction
  or compaction.
- For every oracle: command, output, exit code, duration.
- The runner's stdout and stderr, exit code and signal.
- Enough to **re-run the scorer without the model**: the final workspace or
  patch, the tool-call trace and the model's raw output (080 §5).

## 3. The verdict

**Decided by the owner (2026-10-12).** Exactly one of, with the evidence stored
beside it:

| verdict | meaning |
|---|---|
| `PASS` | the oracle passed. Outranks the clock. |
| `FAIL` | the oracle ran and said no. Nothing else. |
| `TIMEOUT` | a limit ended it: an outer kill, the agent's own limit exiting 0 near the cap, or an external harness's limit. Which one is recorded. |
| `ABORTED` | no verdict could be produced: crash, out of memory, operator kill, empty event stream, tampered oracle, scorer error. |
| `INVALID` | the run finished but a gate or an integrity check says its result must not be counted (wrong build, setting not applied, model not GPU-resident, endpoint changed mid-run). |
| `SKIPPED` | never started; reason recorded. |

Rules:

- Verdicts are an enum in one casing. No free text in the verdict field.
- The oracle is run by the harness, after the agent stops, from a copy the
  agent could not write. Its hash is checked before its output is read. A
  changed oracle is `ABORTED`.
- An oracle that exits 0 either way is judged by a declared pattern, and the
  pattern is part of the task.
- A limit we set is not model behaviour: `TIMEOUT` records which limit fired
  and who owned it.
- **Secondary outcome**, separate from the verdict: how the agent ended
  (`completed`, `aborted`, `max_iterations`, `stopped`, `mistake_limit`) and
  whether it claimed success. "Claimed success, oracle says no" is a finding.
- **Partial score**, where a task has one (tests passed ÷ tests), is recorded
  and is never the verdict.
- A run that was paused carries a `paused` flag and its paused intervals
  (050 §4).
- An operator may mark a finished run `INVALID` with a reason
  (`automaticum run invalidate`). The original verdict is kept beside it.

## 4. Task versions and comparability

- A change to a task, its oracle, its limits, its toolchain or its scoring
  creates a new **task version** and an explicit comparability boundary.
- A change to a scorer voids the comparability of every earlier cell of that
  suite until those cells are re-scored from stored material.
- Reports refuse to pool across a boundary unless told to, and say which runs
  were excluded and why.

## 5. The basis

The basis is one object per cell, gathering every recorded fact that must be
equal for two cells to be compared. It has a canonical serialisation and a
sha256.

| field | why it is in the basis |
|---|---|
| weights: source, revision, sha256 per file | a merge output was once bit-identical to its base |
| quantization and imatrix, from GGUF metadata | a filename is not provenance |
| chat template: sha256 and size as served | a tool-calling score grades the template too |
| sampler as sent, including its **name** | a greedy cohort and a sampled cohort were once merged |
| thinking: on/off, effort, budget, owner | a budget moves both score and length |
| output budget per request; context per slot as loaded | total context divided by parallel slots is what a request gets |
| stop sequences | a stop word fired inside the thinking channel and halved a score |
| server kind, version, serving-library build | two arms straddled a rebuild |
| reasoning and tool-call parser flags | flags decide whether control tokens leak |
| concurrency and batch composition | greedy is not bit-reproducible under batching |
| GPU, driver, residency, power cap | one host generates at about half the speed of another |
| harness build | a score is model + engine |
| agent settings, fully resolved | including every guard and compaction setting (030 §5) |
| suite version, task-set sha256, the task ids in order | a subset is a different bench |
| scorer identity | a scorer fix changes every number |
| detector versions | a loop rate from another detector is another number |
| seed set, when sampling | part of what was measured |

Requirements:

- `automaticum basis diff <cell A> <cell B>` prints exactly the differing
  fields. `automaticum arm diff A B` does the same for two arms.
- For a cohort with a declared variable, the diff must show that variable and
  nothing else; otherwise the report labels the comparison **confounded**.
- Reports refuse to put cells with different bases in one table unless told to,
  and then label every column that differs.
- Greedy is the anchor for cross-arm tables. A sampled cell is allowed as a
  separately named sampler cohort; a report never mixes the two.
- A plain-text rendering of the basis is exported with every cell (065 §5), so
  the diff of two arms' files is the audit of a cohort.

**As built (milestone 4, 2026-10-14):** until cells exist, a basis belongs to
one arm on one task over some batches, over the runs `compare` counts. Each
run's facts are read from its record; a fact whose value differs between
the runs of the group keeps every value and is marked mixed. The canonical
form is one `name=value` line per fact in the order of the table (mixed
values sorted and joined), and its sha256 is the basis hash. The facts and
where they come from:

| fact | from |
|---|---|
| weights | the model's digest as served, and the weights' `general.finetune` |
| quantization | the server's quantization level and the weights' file type |
| imatrix | not recorded: the weights' quantization metadata is not read through the server |
| template, parser | the template's hash and size, renderer and parser, as served |
| sampler | the arm's sampler section, with its name (the wire check, 070 §4.1, says whether it was sent) |
| tag parameters | the tag's own values, compared as sorted lines (stop sequences are among them) |
| thinking | the arm's thinking section and the budget the server applied |
| output limit | the limit the requests declared |
| context | the context as the server loaded the model |
| server | kind, version and engine |
| concurrency | one run on the lane, no other model on the endpoint (the free gate) |
| GPU | the GPU declared in the configuration, and the share of the model on it |
| GPU driver and power | not recorded: needs agent mode on the endpoint's host |
| harness build | SDK release and package hash, runner protocol, lane image, automaticum version |
| agent settings | the hash of the arm's agent, guards, compaction, limits and protocol |
| task | suite, task, version and the hash of its files |
| scorer | the oracle and its image |
| detectors | the analysis version a run was analysed with (metric `analysis.version`), and the detector versions it means, whether they found anything or not |
| seed | the sampler's seed, or none set |

`automaticum basis show <batch> ... [--arm A]` prints each basis;
`automaticum basis diff <batch[/arm]> <batch[/arm]>` prints exactly the facts
in which two differ; `automaticum compare` prints, above every number, the
basis diff of each pair and every fact in which an arm's own runs differ.
Not built yet: the cohort's declared variable and the **confounded** label,
the refusal to put different bases in one table, and the export (065 §5):
they come with cells and cohorts.
