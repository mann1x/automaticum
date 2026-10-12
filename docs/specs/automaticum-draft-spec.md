# automaticum — draft specification

**Status:** draft 0.4, 2026-10-12. Written by the Cerebriline session (the first
user of the tool) from the harness we run today. To be refined with the
automaticum session and the owner. Everything marked **Open** is a question, not
a decision.

**One sentence:** automaticum is a test harness that runs coding agents built on
the Cerebriline npm modules against tasks with a verdict, on lanes it
orchestrates itself, and answers every question we now answer with a throwaway
script.

It is a spin-off of the manic_miner loop harness (`spool/manic-harness`, the
"scan-analysis" campaign checkout `cline-scan`) and is meant to replace it.

---

## 1. Why it exists

The current harness is four bash loops, about fifty `go-*.sh` / `chain-*.sh`
driver scripts, and a growing pile of one-off Python: 365+ runs, three hosts,
and no single place to ask "what happened". Its README is 325 lines, most of
them about ways a run was silently invalid. The facts that cost us days:

- A verdict called `broken` meant four different things (oracle said no, the
  clock, a crash, a replaced oracle). Five "broken" rows were a timeout that
  exited 0.
- The 30-frame oracle never left the countdown, so a file with an undefined
  function scored FIXED for weeks.
- `--thinking high` overrode every tag's own thinking budget in every arm, and
  nothing recorded what was actually sent.
- A sampler written to the wrong settings field was never applied; the run
  looked normal.
- A stale SDK `dist/` killed the CLI at import and exited 0.
- The only record of what was *sent* (`state/`) was deleted; 9 of 336 survived.
- A grep for uppercase verdicts dropped every lowercase `broken`.
- Edits to a running loop script took effect on the next invocation.
- Six hours of zeros were measured on the wrong port.

automaticum's job is to make each of these impossible or loud, and to make the
answers queryable without writing code.

### Non-goals

- Not a model server. It talks to endpoints; it does not load models (it may
  *ask* an endpoint to, see 5.3).
- Not a replacement for the VS Code extension's own tests.
- Not a general CI system. One purpose: agent runs with a verdict.

---

## 2. Vocabulary

| term | meaning |
|---|---|
| **task** | one problem with a workspace, a prompt and an oracle. manic_miner is one task; a SWE-bench instance is one task; a BFCL entry is one task. |
| **suite** | a named set of tasks plus how to fetch, prepare and score them (custom, BFCL, SWE-bench, …). |
| **arm** | one fully specified configuration under test: model, endpoint, sampler, thinking, agent settings, harness build. Two arms differ in at least one recorded field. |
| **run** | one arm on one task, once. The unit that gets a verdict. |
| **batch** | N runs of one arm on one task or suite. |
| **campaign** | an ordered set of batches (today: a `chain-*.sh`), with gates between them. |
| **lane** | an execution slot: one container on one host, bound to one endpoint, running one run at a time. Lanes are what is scarce. |
| **endpoint** | a model server (Ollama, xOllama, opencoti, llama.cpp, any OpenAI-compatible, cloud), configured separately from lanes. |
| **oracle** | the harness's own check of the result, run by the harness after the agent stops, never by the agent. |
| **gate** | a precondition that must hold before a run starts (see 6.2). |
| **finding** | something a diagnostic detected in a run (a loop, a guard firing, a leak). |

---

## 3. Architecture

**Decided by the owner (2026-10-12):**

- The main application and daemon are written in **Go**.
- **A host runs only the Go application.** No Node, no Python, no helper
  scripts on a host. Whatever else is needed runs in a container.
- The **Node runner lives in the Docker image**, never on the host.
- Lanes run in **Docker**, from a slim base image with one extra layer per
  language.
- **Gitea** provides git for tasks that need a remote.
- **LanceDB** is the database, with embedder support.

```
            ┌────────────────────────── automaticumd (Go) ──────────────────────────┐
  CLI  ───► │ HTTP/JSON API + SSE/WebSocket │ scheduler │ lane manager (Docker API) │
  web  ───► │ gate engine │ oracle runner │ ingest + diagnostics │ query + stats     │
  agent ──► │ store: LanceDB (+ blobs on disk) │ embedder client │ Gitea client       │
            └──────────────┬───────────────────────────────┬───────────────────────┘
                           │ docker                         │ http
                 ┌─────────▼──────────┐            ┌────────▼────────┐
                 │ lane container     │            │ endpoints       │
                 │  runner (Node/Bun) │──────────► │ ollama/xollama/ │
                 │  @cerebriline/sdk  │   model    │ opencoti/cloud  │
                 │  workspace (task)  │            └─────────────────┘
                 └────────────────────┘
```

- **automaticumd** owns all state and all decisions. Single static binary.
- **automaticum** (CLI) is a thin client of the daemon's API. Same binary, or a
  second one. Every dashboard action exists as a CLI command and an API call.
- **runner** is the only non-Go part: a small Node/Bun program inside the lane
  image that drives the agent through `@cerebriline/sdk` and streams events
  back. It must be a *library consumer*, not a wrapper around the CLI's argv,
  so that every setting the extension has is reachable and typed.
- **Remote hosts** (linux-gpu-2, windows-1, mac-1): since a host runs only the
  Go application, a remote host runs the same binary in an agent mode
  (`automaticum agent`), which manages the local Docker engine, fetches the
  local server's logs and reports host facts to the daemon. **Open:** whether
  the daemon may also drive a bare Docker engine over SSH/TLS with no agent.
  Windows lanes (windows-1) are needed: some bugs are Windows-only (rename with
  open handles, PowerShell commands, path separators).
- Consequence: everything the old harness did with host-side Python and bash
  (analysis, log collection, archiving, tag checks) is Go code in the daemon
  or the agent. Suite scorers that exist only in Python (BFCL's evaluator,
  the SWE-bench harness) run in their own containers.

### 3.1 Why the runner uses the SDK directly

Today `apps/cli` runs from source and imports the SDK from a gitignored
`dist/`. The harness therefore tests "whatever was last built there". The
runner instead pins `@cerebriline/sdk@<version>` (or a tarball built from a
named commit) per **harness build**, and the build is part of the arm.

Requirements on the runner:

- Install any published version, or a local tarball from a commit (`bun pack`).
- Record version, commit sha, dirty flag, and the sha256 of the installed
  package in the run record. Refuse to run when the installed package does not
  match what the arm names.
- Expose every agent setting as typed configuration (see 5.4), and **read each
  one back from the built session config** before the first request. A setting
  that did not survive is a gate failure, not a quiet default.
- Stream the SDK's event stream unchanged (no dropping of fields; today we
  strip `accumulated` only to save disk — do that at storage, not at source).
- Run under **Node** for anything that talks HTTP to a model: Bun's `fetch`
  returned empty worker bodies in live harnesses.

**Open:** which store. LanceDB is the owner's starting point, but its Go
support was not checked when it was chosen. Section 10.1 lists what was found
and what to put in a spike. Blobs (event streams, logs, workspace snapshots)
may live in the store or on disk, content-addressed; that is part of the same
decision.

---

## 4. What a run records (the contract)

Nothing below is optional. Each item is here because its absence invalidated
or blinded a real campaign.

### 4.1 Identity and provenance

- run id (sortable by time), batch, campaign, arm id, task id, lane, host.
- **Arm, fully resolved:** every field in 5.4 with the value *as sent*, plus
  where each value came from (arm file, default, model tag, endpoint).
- Harness build (4 fields above), runner version, lane image digest, language
  layer digest, automaticum version.
- Endpoint identity *as probed at start*: server kind and version, build info,
  feature list, the model's digest, its `capabilities`, template/renderer/
  parser, parameters, context length loaded, GPU residency (fully on GPU or
  not, how many layers), quantization and imatrix file read from the GGUF.
- Task identity: suite, version, sha256 of every input file, the exact prompt
  text sent, the oracle's sha256.
- Host facts: GPU, driver, power cap, free VRAM at start.

### 4.2 The streams

- The agent event stream, complete, compressed, never pruned.
- Every request body and response as they crossed the wire (or enough to
  rebuild them): rendered system prompt, message list, tool schemas, sampler,
  thinking fields, `num_ctx`, `num_predict`. Today this is `state/` and is the
  single most valuable artefact.
- The runtime's own log (output caps, discarded turns, compaction, retries).
- The server's log for the window of the run, fetched from the endpoint's host
  (local journal, ssh, or an API), with debug level verified *before* the run.
- Workspace before and after, and a diff. Optionally a snapshot per
  transaction/compaction.
- The oracle's command, output, exit code and duration.
- stdout/stderr of the runner, exit code, signal.

### 4.3 The verdict

Exactly one of, with the evidence stored beside it:

| verdict | meaning |
|---|---|
| `PASS` | the oracle passed. Outranks the clock. |
| `FAIL` | the oracle ran and said no. Nothing else. |
| `TIMEOUT` | the wall clock ended it, whether by an outer kill or the agent's own limit exiting 0 near the cap. |
| `ABORTED` | no verdict could be produced: crash, OOM, operator kill, empty event stream, tampered oracle. |
| `INVALID` | the run finished but a gate or an integrity check says its result must not be counted (wrong build, setting not applied, model not GPU-resident, endpoint changed mid-run). |
| `SKIPPED` | never started; reason recorded. |

Rules:

- Verdicts are an enum in one casing. No free text in the verdict field.
- The oracle is run by the harness, after the agent stops, from a copy the
  agent could not write. Its hash is checked before its output is read. A
  changed oracle is `ABORTED`.
- An oracle that exits 0 either way is judged by a declared pattern, and the
  pattern is part of the task.
- **Oracle validation is a first-class command:** every task ships known-good
  and known-bad fixtures, and `automaticum oracle verify <task>` proves the
  oracle passes the first and fails the second. A task without both cannot be
  used in a campaign. (The 30-frame oracle would have failed this on day one.)
- Secondary outcome, separate from the verdict: how the agent ended
  (`completed`, `aborted`, `max_iterations`, `stopped`), and whether it
  *claimed* success. "Claimed fixed, oracle says no" is a finding.
- A change to a task, its oracle or the scoring creates a new **task version**
  and an explicit comparability boundary. Reports refuse to pool across a
  boundary unless told to.

---

## 5. Configuration

Declarative files (YAML or TOML, **Open**), versioned, validated on load, and
resolved into the run record. No environment-variable knobs.

### 5.1 Lanes

Name, host, Docker context, image (base + language layers), resource limits,
which endpoints it may use, concurrency (default 1), labels (`gpu=a6000`,
`os=windows`). A lane has a state: `idle`, `running`, `paused`, `draining`,
`offline`, `quarantined` (repeated infrastructure failures).

### 5.2 Images

- One slim base: runner, Node, git, the common shell tools the agent's tools
  call, and the sandbox prerequisites (user namespaces on Linux need a sysctl
  on some hosts).
- One layer per language toolchain, composable: node, python, go, rust, java,
  dotnet, c/c++, php, ruby, … "as many as possible". A task declares the
  layers it needs; the daemon builds or pulls the composition and records the
  digest.
- Images are built by automaticum from definitions in the repo, reproducibly.
- **Open:** Windows containers, or a native Windows agent for windows-1-style
  lanes; macOS has no containers, so a native agent there too.

### 5.3 Endpoints

Configured separately from lanes: kind, base URL, credentials (by reference to
a secret store, never in the run record), log source, how to probe it, how to
ask it to load/unload a model, capacity (how many sessions it serves at once).

- A capability probe per kind (`/api/show`, `/props`, `/v1/models`, …) and a
  real generation probe: a successful *pull* of a cloud tag proves nothing; one
  token does.
- Detection of what the endpoint did to us: admission refusals (429 with
  Retry-After), context-size errors, evictions, a server restart mid-run, an
  auto-update that changed the binary.
- Endpoint health must never be probed in a way that disturbs a busy server.
- An endpoint may be shared by lanes on different hosts; the scheduler respects
  its capacity.

### 5.4 Arms: every parameter we have had to vary or verify

Model and provider:
`provider`, `model`, `baseUrl`, model digest pin, context window, output
budget / `num_predict`, thinking level and budget (and whether the request or
the tag owns it), sampler (temperature, top-p, top-k, min-p, repeat penalty,
repeat-last-n, presence penalty, seed) **with ownership stated**: "tag's own"
(send nothing) or "ours" (send these, verify they were sent).

Agent:
prompt template family, system prompt variant, tool selection, tool result
cap, read limit, max iterations, retries, timeouts (run, idle, per request),
compaction (strategy, thresholds, council on/off, continuation), change
protocol (`off`/`static`, max changes, check proposal, reconsider-after), edit
verification, task-progress interval, guards (each one individually: reasoning
loop, tool-intent, struggle detector, stalled-check, no-tool-call nudge,
unreadable-call repair), escalation (expert model, limits, close-after),
sub-agents (enabled, nodes, window share, swarm, teammates), sandbox mode,
skills (which bundled ones are on), media tools, Jev, library, memory.

Harness:
SDK build, runner version, image, oracle version, task version.

An arm file may **extend** another and override fields; the resolved arm is
what is stored. `automaticum arm diff A B` prints exactly the fields that
differ, which is the first question about any two results.

---

## 6. Orchestration

### 6.1 Scheduling

- Queue of runs; the scheduler places a run on a lane that satisfies its
  labels and whose endpoint has capacity. One run per lane by default.
- Interleaving: A/B arms alternate run by run on the same lane (ABAB) so drift
  in the host does not land on one arm. Today a "chain" runs A then B.
- A campaign is data: batches, order, gates, stop conditions (stop after N
  consecutive PASS, stop on first INVALID, stop at a time).
- Priorities, and reservations ("this lane is mine until 08:00").
- Other things use these GPUs. A lane can be marked busy by an external signal
  and the scheduler waits; it never preempts someone else's work.

### 6.2 Gates (before every run)

A gate failure produces `SKIPPED` or `INVALID` with the reason, never a run
that looks normal. Built in:

- the model tag exists and its digest matches the pin;
- the tag's template / renderer / parser are intact (a quoted `TEMPLATE` can
  swallow the directives after it and fall back to the GGUF's Jinja);
- the sampler in the arm parses, was written, and was read back;
- the model is fully GPU-resident after load (and a named tensor is not parked
  on the CPU);
- the request's thinking level and the tag's budget agree, or the arm says
  which wins;
- the lane and the endpoint are free;
- an expert model, if configured, answers a real request;
- the SDK build installed is the one named, and `dist` is not stale;
- the server's log level is the one diagnostics need;
- disk space, inotify limits, user-namespace sysctl, clock sync;
- the oracle verifies against its fixtures (6.2 references 4.3);
- the workspace is pristine: restored from source, hash-checked. A run must
  never inherit what the previous one left (this happened on windows-1).

Gates are plugins with a name, so a campaign can add one and a report can say
which gate stopped what.

### 6.3 Control

`pause`, `resume`, `abort`, `drain`, `requeue`, `retry`, at the level of run,
batch, campaign, lane and whole daemon.

- **Graceful by default.** Stop means "finish the run in flight, analyse it,
  then stop"; that is what `touch STOP-NATIVE` does today.
- **Abort** kills in the right order (loop, driver, the run's process tree,
  then confirms the endpoint has actually stopped generating — a client that
  merely leaves does not always cancel the request server-side). An aborted
  run is still ingested and analysed with what it produced.
- **Pause** of a run in flight: **Open** — the SDK can suspend between turns;
  whether a paused run still counts for timing statistics must be defined.
- Infrastructure errors (endpoint down, 429, network) wait and retry and are
  recorded as such. They are never the model's failure.
- The daemon survives its own restart: runs in flight are re-attached or
  marked `ABORTED(reason=daemon restart)`, never lost and never duplicated.
- Configuration is immutable once a run has started. Changing an arm file
  affects the next run only, and the dashboard says so.
- Watchdogs: a run whose event stream is silent for N minutes while its
  process lives; a batch whose loop vanished; a lane that produces only
  infrastructure failures (quarantine it).

### 6.4 Git and Gitea

For tasks that need a remote (clone, branch, push, open a PR, review): a Gitea
instance managed by automaticum. Per run: a fresh repository or fork seeded
from the task, a scoped token, and afterwards the pushed refs, PRs and comments
collected as artefacts and available to the oracle. Torn down or archived per
policy.

---

## 7. Suites

### 7.1 Custom tasks (what we do today)

A task is a directory: `task.toml`, the workspace seed, the prompt, the oracle
(command + expectation pattern or exit code), fixtures (known-good,
known-bad), required language layers, limits, and optional per-task hooks.
manic_miner is the first one to port, with its history (oracle v1/v2/v3
boundaries) so old and new results can be laid side by side.

Needed beyond today: several oracles per task (primary verdict + secondary
checks such as "did not touch files outside X", "tests still pass"), partial
scores, and tasks with multiple turns (a scripted user who answers questions
or steers mid-run, since `ask_question`, steering and queued messages are
product features we cannot test unattended today).

### 7.2 BFCL (Berkeley Function-Calling Leaderboard)

Run its categories through the Cerebriline provider layer and tool-call
parsing, so the score reflects *our* stack on a given model and endpoint:
single/multiple/parallel calls, multi-turn, irrelevance, and the executable
categories. Record per-entry result, the raw model output, how the call was
parsed (native tool call, repaired, read out of text or out of reasoning),
and score with BFCL's own evaluator, version-pinned. **Open:** whether the
agent loop is in play or only the provider + parser.

### 7.3 SWE-bench

Verified / Lite / full, and SWE-bench-style sets for other languages. The
agent works in the instance's repository in a lane container; the patch is
evaluated by the official harness images, version-pinned. Record the patch,
the test logs, and resolve status per instance. Needs: instance-level
caching of environments, sharding across lanes, resume of a half-finished
evaluation, and cost/time budgets per instance.

### 7.4 Suite interface

fetch → prepare(task) → prompt(task) → run → collect → score. A suite is a
plugin so further ones (Aider polyglot, Terminal-Bench, our own tool-quiz
cases mined from real sessions) fit without touching the core.

---

## 8. Diagnostics, built in

**The rule: no custom script.** Everything below runs automatically at ingest,
is stored as findings and metrics on the run, and is available as a CLI
command, an API call and a dashboard view. Each maps to a script we wrote.

### 8.1 Per-run report (`automaticum run show <id>`)

One page: verdict and evidence; arm (with the fields that differ from the
batch's baseline highlighted); timeline; the numbers in 8.2; findings in 8.3;
links to every artefact. Replaces `analyse.py` + `exit.txt` + `build.txt`.

### 8.2 Metrics per run (replaces `run-metrics.py`, `think_stats.py`, `stats.py`)

- Wall time, time to first token, time per turn, time in tools vs in the model,
  idle gaps.
- Requests, iterations/turns, tool calls by tool, tool failures by tool and by
  error class.
- Tokens: input, cached/reused, fresh, output; output split into reasoning,
  text and tool input, with the method used to split; tok/s for prompt and
  generation, per request and overall.
- Thinking: blocks, tokens per block (median, p90, max), budget hits, turns
  with no thinking, thinking-only turns.
- Context: peak occupancy vs window, compactions (count, before/after size,
  duration, tokens spent, what triggered each), overflow errors, prompt
  re-reads (cache misses) per request with the cause when known.
- Edits: count, files, churn (lines written vs lines surviving), edits to the
  same region, reverts, edit-without-check sequences.
- Change protocol: transactions kept/discarded/empty, checks proposed and what
  happened to them, rollbacks.
- Escalation: offers, calls, expert cost, whether it helped.
- Sub-agents: spawned, per-agent verdict and spend, wait time for placement,
  retries, agents that reported nothing.
- Cost in tokens and, for priced endpoints, money.

### 8.3 Findings (detectors)

Each has a severity, the evidence (event ids, quoted text) and a count.

- **Looping:** repeated reasoning passages (paragraph-level, non-contiguous),
  repeated identical tool calls, repeated rounds hidden behind a changing
  ordinal, edit/revert oscillation, reading the same range again and again,
  search loops after a "no match".
- **Guards:** every guard that fired, when, what it cut or said, and what the
  model did next; guards that *should* have fired by their own rule and did
  not (replay the rule over the transcript). False-positive review queue.
- **Tool calls:** unreadable calls, repaired calls, calls written inside the
  reasoning or as text, truncated calls (output cap), wrong tool names,
  schema violations, arrays sent as strings, an `intent` used as scratch space.
- **Template leaks:** control tokens or channel markers in visible text or in
  history; thinking that stopped after a leak.
- **Turn pathologies:** thinking-only turns, empty turns, a report after work
  that ended the run early, a plan written to reasoning with an empty reply,
  premature "done", refusal to continue.
- **Parameter drift:** any request whose sampler, thinking, `num_ctx` or
  `num_predict` differs from the arm; values the server reported differently
  from what was sent; a tag parameter that silently won or lost.
- **Server side:** `done_reason` per request, truncations, slot/session
  reuse, checkpoint restores, evictions, admission refusals, load/unload
  events, restarts, GPU residency changes, errors in the window of the run.
- **Integrity:** oracle tampering, workspace not pristine, files written
  outside the workspace, sandbox escapes or denials, the agent changing its
  environment to get past an obstacle (editing PATH, chmod, deleting a guard).
- **Struggle signals:** the triggers from the escalation study, evaluated on
  every run, with recall and false-alarm rate per trigger across a set
  (`triggers.py`: "would this have fired, and would it have been right?").
- **Outcome mismatches:** claimed success vs oracle; fixed then broken again;
  correct at some point during the run but not at the end (needs periodic
  oracle sampling, **Open**: at what cost).

A detector is a plugin with a version. When a detector changes, findings can
be recomputed over the whole corpus (`automaticum reanalyse`), which is how we
re-read 365 runs after learning something.

### 8.4 Across runs (replaces `stats.py`, `ab-prompt-report.py`, `scan.py`, the census scripts)

- Pass rate per arm with a confidence interval (Wilson), streaks, and "how
  many more runs to tell these two arms apart".
- A/B: paired or unpaired comparison of any metric, with the test used stated
  (Fisher for rates, Mann-Whitney / bootstrap for times and counts), and a
  plain statement of whether the difference is distinguishable at this n.
- Medians and ranges by default, not means; distributions on request.
- Group by any arm field or finding: "pass rate by quant", "loops by sampler",
  "tool failures by model and tool", "time to first compaction by window".
- Tool failure census across the corpus, by tool, model and error class.
- Regression view across harness builds: what changed between two SDK
  versions, on the same arm, for every metric and finding.
- Comparability: refuses to pool across task/oracle/scoring boundaries, and
  says which runs were excluded and why.
- Lane skew: the same arm on two hosts, to separate the model from the machine
  (one host generates at about half the speed of another because of a power
  cap).

### 8.5 Logs: view, search, analyse

- A transcript viewer that shows what the model saw and what it produced,
  turn by turn: system prompt, messages, reasoning, text, tool calls with
  arguments, results, guard notes, compaction boundaries, and the raw request
  for any turn. Collapsible, linkable to a turn, diffable between two runs.
- Server log and runtime log aligned on the same timeline as the transcript.
- Full-text search and regex over transcripts, reasoning, tool arguments, tool
  results and logs, across the corpus, with filters on any run field.
- **Semantic search** through the embedder: "runs where the model decided the
  oracle was wrong", "reasoning that mentions giving up".
- Saved queries, and queries as report columns.
- Replay: re-send a recorded request to an endpoint (same bytes) to reproduce
  a behaviour offline; the gemma4 leak needed exactly this, and 23 cold runs
  reproduced nothing.
- Export of any slice (JSONL, CSV, Parquet) and a self-contained HTML report.

---

## 9. Interfaces

### 9.1 CLI — "full commandeering"

Everything, with `--json` on every command and stable exit codes. Sketch:

```
automaticum status                          # lanes, queue, what is running
automaticum lane ls|show|pause|resume|drain|quarantine
automaticum endpoint ls|probe|show|models
automaticum arm ls|show|diff|validate
automaticum task ls|show|verify
automaticum oracle verify <task>
automaticum run start --arm A --task T [-n 5] [--lane L]
automaticum run ls|show|tail|stop|abort|retry|invalidate
automaticum batch|campaign start|ls|show|pause|resume|abort
automaticum report run|batch|ab|regression|census [...]
automaticum find "<query>" [--semantic] [--where ...]
automaticum logs <run> [--server|--runtime|--events] [--grep ...] [--turn N]
automaticum replay <run> --turn N [--endpoint E]
automaticum reanalyse [--detector D] [--where ...]
automaticum export ...
automaticum doctor                          # every gate, on every lane, now
```

Rich terminal output: live tables, progress, a `tail` that renders the
transcript as it streams, and a TUI dashboard (**Open:** how far).

### 9.2 HTTP API and live dashboard

- REST/JSON for everything the CLI does; SSE or WebSocket for live events.
- Dashboard: lanes and queue; a run in flight with its live transcript,
  tokens, tok/s, context bar and findings as they appear; batch and campaign
  progress with the running pass rate and interval; A/B boards; the log
  viewer; the search box; management (start, pause, abort, requeue, edit a
  campaign's remaining steps).
- Authentication, and a read-only mode to share a result.

### 9.3 Assistant-optimized control interface

The primary user is often an AI session (this one). Requirements:

- An **MCP server** exposing the same operations as tools, with small,
  predictable outputs: every list is paged and summarised, every object has a
  `brief` form, and nothing returns a megabyte of transcript unless asked for
  a range.
- "Wait for" primitives that block server-side until a run/batch ends or a
  condition holds, so an assistant does not poll: `wait run <id>`,
  `wait batch <id> --until "pass>=5 or fail>=3"`.
- A `brief` report format written for a reader with a context budget: the
  verdict, the three numbers that matter, the findings, and ids to drill into.
- Notifications through the mailbox the sessions already use, and webhooks.
- Dry-run for every mutating command, returning exactly what would start.
- Idempotency keys, so a retried command does not start a second campaign.

### 9.4 Models inside the interface

"Support for Ollama models to search and query on the interface":

- Embedders (Ollama `/api/embed`, or any OpenAI-compatible) for semantic
  search over transcripts, reasoning, findings and reports. The embedder must
  be on an endpoint that is *not* the one under test, or scheduled around it.
- A chat/query box where a model answers questions about the corpus using the
  same tools the assistant interface exposes ("why did arm B fail more?"),
  with citations to runs and turns. Its answers are labelled as a model's.
- Optional model-written run summaries and model-assisted finding triage,
  always stored separately from measured facts.

---

## 10. Store

- **DuckDB** for runs, arms, tasks, lane and queue state, metrics, findings,
  statistics and reports; **Lance** (through `lancedb-go`) for text chunks,
  their vectors and full-text search. This is the owner's preferred shape, to
  be confirmed by the spike in 10.1.
- The run id is the join key between the two. A run is written to DuckDB
  first; its chunks are derived data in Lance and can always be rebuilt from
  the stored streams, so a failure between the two writes loses nothing.
- Content-addressed blob store on disk for streams, logs, snapshots and
  request bodies, compressed (zstd), deduplicated (system prompts and tool
  schemas repeat thousands of times).
- Nothing is pruned by default. Retention is an explicit policy with an
  archive tier (the 19 TB disk today), and a deletion happens only behind a
  verified archive.
- Never write large data to tmpfs. Never put the store on the RAID that
  stalled under 25 MB of log per 1000 s.
- **Import of the existing corpus**: the `runs-native/`, `runs-atomic/`,
  `runs-teams/` directories, the archived `state/` tars, the windows-1 session
  archive, and the measurement memories in pgvector, mapped to the new schema
  with their original ids, so old findings stay checkable.
- Schema migrations, backup, and a consistency check.

---

### 10.1 Choosing the store: candidates for a spike

Checked on 2026-10-12 against each project's repository and documentation.
Version numbers and statuses are as read that day.

**What the store has to do**

- Hold everything: run logs and event streams, resolved configurations, lane
  and queue state, metrics, findings, statistics and reports.
- Scale to thousands of runs, compressed. A run writes about 25 MB of log per
  1000 s today, so a few thousand long runs is tens to hundreds of GB raw.
  zstd got about 30x on the archived `state/` folders.
- Vector search over text chunks (likely millions), full-text and regex search,
  and analytical queries (group by any arm field, percentiles, joins).
- Be simple to run: no separate database server to administer (rules out
  Postgres + pgvector), and not fall over under load (rules out sqlite-vec).
- Work from Go, on Linux and Windows, with only the Go app on the host.
- One writer (the daemon), many readers, and survive a kill during a write.

**Candidates worth testing**

| | LanceDB | DuckDB | ClickHouse, embedded as chDB |
|---|---|---|---|
| Go path | `lancedb/lancedb-go`, official, CGO binding to the Rust core. v0.1.2 (Sep 2025), last push May 2026, 61 stars | `duckdb/duckdb-go`, official `database/sql` driver, CGO. v2.10506.0 (Sep 2026), pushed daily. The older `marcboeker/go-duckdb` is archived | `chdb-io/chdb-go` v2.0.1 (Jul 2026), no CGO: it loads `libchdb` at run time (purego). Or a ClickHouse server in a container with `clickhouse-go` |
| Shape | columnar, versioned, append-first | columnar, single file, SQL | columnar MergeTree, SQL |
| Compression | good (Lance format) | very good, automatic per column | the strongest of the three for logs, with per-column codecs |
| Vectors | native, built for it: IVF/HNSW indexes on disk, incremental | `vss` extension, HNSW. Persistence is still behind `hnsw_enable_experimental_persistence`; the docs warn of index corruption after a crash, and the index must fit in RAM. Exact search with no index always works | vector similarity index (HNSW, quantised); the docs page read today still labels it experimental, for 25.8 and later |
| Full-text | native index, hybrid search with RRF | `fts` extension with BM25, but "the FTS index will not update automatically when the input table changes": it must be rebuilt | text indexes and fast substring/regex scans over compressed columns |
| Analytics and statistics | basic filters; heavy aggregation is not its purpose | its home ground: percentiles, window functions, joins, reads Parquet directly | its home ground, at larger scale |
| Small frequent updates (lane state) | each update is a new table version; needs compaction | fine (transactional) | poor fit: updates are mutations or replacing merges |
| Concurrency | multiple readers and writers by design | one process writes; readers share that process | many readers and writers |
| Main risk | the Go SDK is young and little used | vectors and full-text are extensions with the limits above | a large native library to ship; awkward for mutable state; more to learn |

**Looked at and not proposed for the spike**

- **Qdrant**: excellent for vectors, but vectors and payload only; logs,
  statistics and configuration would need a second store.
- **VictoriaLogs, Quickwit**: built for exactly this volume of logs, but no
  vectors and no relational queries; again a second store.
- **Bleve** (Go, full-text plus vectors) and **chromem-go** (pure Go, in
  memory): fine for search over a small corpus, not a database for the rest.
- **sqlite-vec**: ruled out by the owner; also no push since May 2026.
- **Turso** (SQLite-compatible rewrite in Rust, with vectors): very active but
  still presenting itself as experimental in places.
- **Postgres + pgvector**: ruled out as too complex to run.

**Owner's preference (2026-10-12): shape 3 below**, DuckDB for state,
configuration, metrics and statistics, with Lance for text chunks and vectors.
The spike is still run, now to confirm that shape and to find its limits
rather than to choose between three: shapes 1 and 2 are the fallbacks if one
of the two engines fails its part.

**What this suggests**

No single candidate is best at all four jobs (mutable state, logs at volume,
vectors, statistics). Three shapes to compare:

1. **LanceDB for everything** (the original plan). Risk concentrated in the Go
   SDK and in the lane-state update pattern.
2. **DuckDB for everything**, with vectors as plain columns searched exactly at
   first, or with the HNSW index rebuilt from the table at start (which avoids
   trusting its persistence). Risk: full-text needs scheduled rebuilds; vector
   search cost at millions of chunks.
3. **DuckDB for state, configuration, metrics and statistics, plus Lance for
   text chunks and vectors.** Two engines in one binary, each doing what it is
   good at. More moving parts, the least risk in each part.

ClickHouse/chDB is the one to pick only if the spike shows the log volume
defeats the others.

**The spike**

Same data for every candidate: import the existing corpus (365+ runs with
their event streams and logs, plus the archived `state/` tars), then scale it
synthetically to 5,000 runs. Measure:

- ingest rate, and size on disk against the raw and the zstd figures;
- a batch statistics query (pass rate and percentiles grouped by three arm
  fields) and a "tool failures by model and tool" census over all events;
- regex and full-text search across all transcripts, cold and warm;
- vector search over 100 k, 1 M and 5 M chunks at 768 and 1024 dimensions:
  latency, recall against exact search, memory, index build time;
- 10 updates per second to a lane-state table for an hour while the queries
  above run: latency, file growth, what compaction costs;
- `kill -9` during a write, twenty times: does it reopen, and is anything lost;
- the build: a static binary on Linux amd64/arm64 and Windows amd64, and what
  cross-compiling takes;
- memory at rest and at peak.

The daemon should reach the store through one small interface so the choice
can be revisited; the spike's test set becomes that interface's test suite.

## 11. Acceptance: when it replaces the old harness

1. manic_miner ported; `oracle verify` passes on its fixtures.
2. One historical arm re-run and its pass rate, times and token figures agree
   with the old harness's records within stated tolerance; the old corpus is
   imported and queryable.
3. Every item in the README's "ways a run was silently invalid" is either a
   gate, an `INVALID` verdict or a finding, each with a test that injects the
   fault.
4. An unattended overnight A/B campaign across two hosts, paused and resumed
   once, one lane killed on purpose, ends with a report nobody had to script.
5. The questions in Appendix B are each answered by one command.
6. A BFCL category and a SWE-bench Lite shard run end to end and match the
   official scorer.

---

## Appendix A — what exists today, and what replaces it

| today | what it does | automaticum |
|---|---|---|
| `native.sh`, `atomic.sh`, `teams.sh`, `harness.sh` | the loops | scheduler + runner |
| ~40 `go-*.sh` | one arm each, with gates in bash | arm files + gate engine |
| ~12 `chain-*.sh`, `launch-*.sh` | sequences of arms | campaigns |
| `check-tag.sh`, `check_27b_config.py` | tag intact, params exact | gates |
| `analyse.py` | one run: tool sequence, budget, logs | run report |
| `run-metrics.py`, `think_stats.py` | comparable numbers per run | metrics |
| `stats.py` | rates, streaks, medians | batch statistics |
| `ab-prompt-report.py`, `ab-autoscore.sh` | A/B scoring | A/B report |
| `tx_watch.py` | live transaction and stall watch | live events + watchdogs |
| `tool-analysis/scan.py`, `shell.py`, `oracle.py` | tool failure census | census |
| `escalation-study/*` | trigger recall / false alarms | struggle detectors |
| `struggle-calib/extract.py`, `replay.py` | replay a rule over transcripts | reanalyse |
| `v9-census/census.py`, `leak.py`, `misspath.py` | leaks, missing paths, per-model census | findings |
| `serverlog-e2g.sh`, `SERVER_LOG_HOOK` | fetch the server's log | endpoint log source |
| `archive-state.sh`, `archive-sidecar.sh` | keep what was sent | store |
| `snap-compactions.sh`, `snapshot-tools.sh` | snapshots at compaction, tool schema dumps | artefacts |
| `strip-accumulated.py` | shrink the stream | storage compression |
| `opencoti-run-recorder.py` | record a server during a run | endpoint recorder |
| `grab.ps1` on windows-1 | copy a live log through a shared handle | Windows agent |
| `CHANGES.md` per change day | comparability boundaries | task/harness versions |

## Appendix B — questions it must answer in one command

- Which runs of this arm looped, where, and on what text?
- Did the sampler I set reach the wire in every request of this batch?
- What differs between these two arms? Between these two builds?
- Which guard fired in the runs that failed, and what happened after?
- Show every tool call that could not be read, by model.
- Did thinking stop after a template leak in any run this week?
- How many requests re-read the whole prompt, and why?
- Is B better than A, and how many more runs until I can say?
- Which runs were on a model that was not fully on the GPU?
- Show me turn 41 of run X exactly as the model saw it, and the server's log
  for those seconds.
- Find runs where the model concluded the checker was wrong.
- What did the last 20 FAILs have in common that the PASSes did not?

## Appendix C — open questions for the owner

1. Config format (YAML vs TOML), and one repo for tasks or tasks as their own
   repos.
2. Remote hosts: the Go binary in agent mode is assumed; is agentless Docker
   over SSH also wanted? Windows and macOS lanes (no Linux containers there
   without a VM).
3. Store (see 10.1): the owner prefers DuckDB with Lance for vectors. Still
   open inside that: where raw event streams and logs live (DuckDB columns,
   Lance, or compressed blobs on disk indexed by both), and how a run's rows
   in the two engines are kept consistent.
4. Does a paused run count in timing statistics?
5. Periodic oracle sampling during a run: worth the cost?
6. How much of the extension's behaviour that the CLI lacks should the runner
   emulate (the 20-request pause, approvals, the scripted user)?
7. Multi-user, or single operator plus assistants?
8. Which language layers first.
9. Name of the verdicts: keep `FIXED`/`broken` for continuity or move to
   `PASS`/`FAIL` (this draft moves).
