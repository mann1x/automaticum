# automaticum

A test harness that runs coding agents against tasks with a verdict, on lanes it
orchestrates itself, and answers the questions about those runs without a
throwaway script.

> **Status: specification stage.** Nothing is implemented yet. The design is the
> spec set in [`docs/specs/`](docs/specs/000-overview.md) (draft 0.5). Items a
> spec marks **Open** are questions, not decisions; they are listed in
> [`docs/specs/decisions.md`](docs/specs/decisions.md).

## What it is for

automaticum runs agents built on the Cerebriline npm modules (`@cerebriline/sdk`)
against a model endpoint, on a task that has an oracle, and records enough to
prove what happened. It replaces a harness made of bash loops, about fifty driver
scripts and a pile of one-off analysis scripts, in which a run could be invalid
without anyone noticing: a setting that never reached the wire, an oracle that
passed broken code, a timeout that exited 0.

Its job is to make each of those failures impossible or loud, and to make the
results queryable.

It is used in three ways: on its own, as the agent engine behind an external
harness (SWE-bench, BFCL, Harbor, Inspect), and as a backend of omnimergekit's
`omk_eval`. A score from the last two is a score of model plus Cerebriline
build, and is not comparable to a published number under another engine.

It is not a model server, not a general CI system, not a host for
completion-form benchmarks, and not a replacement for the VS Code extension's
own tests.

## Core ideas

- **A run record that proves what was sent.** Every request body, the resolved
  configuration with the origin of each value, the build under test, the
  endpoint as probed at start, and each agent setting read back from the built
  session before the first request.
- **Verdicts are an enum.** `PASS`, `FAIL`, `TIMEOUT`, `ABORTED`, `INVALID`,
  `SKIPPED`. `FAIL` means the oracle ran and said no, and nothing else.
  `INVALID` marks a finished run whose result must not be counted.
- **The harness runs the oracle**, after the agent stops, from a copy the agent
  could not write. A task ships known-good and known-bad fixtures, and
  `automaticum oracle verify` must pass before the task can be used.
- **Gates before every run.** Model digest, template, sampler, GPU residency,
  SDK build, pristine workspace and more. A gate failure yields `SKIPPED` or
  `INVALID` with a reason, never a run that looks normal.
- **Diagnostics at ingest, no custom script.** Metrics and versioned detectors
  (loops, guard firings, unreadable tool calls, template leaks, parameter
  drift, server-side events, integrity) run on every run and can be recomputed
  over the whole corpus when a detector changes.
- **Statistics that state their limits.** Pass rates with intervals, A/B
  comparisons that say whether two arms are distinguishable at this n, and a
  refusal to pool across a task, oracle or scoring boundary.

## Vocabulary

| term | meaning |
|---|---|
| task | one problem: a workspace, a prompt and an oracle |
| suite | a named set of tasks plus how to fetch, prepare and score them |
| arm | one fully specified configuration under test |
| run | one arm on one task, once; the unit that gets a verdict |
| batch | N runs of one arm on one task or suite |
| cell | one suite or subset scored for one arm; gets one headline number |
| cohort | a named set of cells to compare, with one declared variable |
| basis | every recorded fact that must be equal for two cells to be compared |
| campaign | an ordered set of batches with gates between them |
| lane | an execution slot: one container on one host, bound to one endpoint |
| endpoint | a model server, configured separately from lanes |
| oracle | the harness's own check of the result |
| gate | a precondition that must hold before a run starts |
| finding | something a detector found in a run |

## Architecture

```
            ┌────────────────────────── automaticumd (Go) ──────────────────────────┐
  CLI  ───► │ HTTP/JSON API + SSE/WebSocket │ scheduler │ lane manager (Docker API) │
  web  ───► │ gate engine │ oracle runner │ ingest + diagnostics │ query + stats     │
  agent ──► │ store (+ blobs on disk) │ embedder client │ Gitea client               │
            └──────────────┬───────────────────────────────┬───────────────────────┘
                           │ docker                         │ http
                 ┌─────────▼──────────┐            ┌────────▼────────┐
                 │ lane container     │            │ endpoints       │
                 │  runner (Node)     │──────────► │ ollama/xollama/ │
                 │  @cerebriline/sdk  │   model    │ opencoti/cloud  │
                 │  workspace (task)  │            └─────────────────┘
                 └────────────────────┘
```

Decided so far:

- The application and daemon are written in **Go**. A host runs only the Go
  binary; anything else runs in a container.
- Lanes run in **Docker**, from a slim base image with one layer per language
  toolchain.
- The **runner** is the only non-Go part: a small Node program inside the lane
  image that drives the agent through the SDK as a library and streams its
  events back unchanged.
- **Gitea** provides a git remote for tasks that need one.
- Remote hosts run the same binary in agent mode. Windows and macOS get a
  native agent later.
- The runner aims at full parity with the VS Code extension, with a scripted
  user for approvals, questions and steering.
- Configuration is TOML; tasks live in this repository.

Preferred, to be confirmed by a spike ([080](docs/specs/080-store.md)): **DuckDB** for state,
configuration, metrics and statistics, **Lance** for text chunks, vectors and
full-text search, and a content-addressed blob store on disk for streams and
logs.

## Suites

- **Custom tasks**: a directory with the workspace seed, prompt, oracle and
  fixtures. `manic_miner` is the first to port.
- **BFCL**, scored with its own evaluator, as two cells: one through
  Cerebriline's provider and parser only, one through the whole agent.
- **SWE-bench** (Verified, Lite, full, fixed subsets), evaluated with the
  official harness images; SWE-bench-style sets for other languages, C/C++
  first.
- **A multi-language agentic suite** of our own: C#, C/C++, Python, TypeScript,
  Go, Rust first.

A suite is a plugin: fetch, prepare, prompt, run, collect, score. External
harnesses reach the agent through an engine adapter.

## Interfaces

- **CLI** for everything, with `--json` and stable exit codes, and a full
  terminal dashboard.
- **HTTP API and live dashboard**: lanes, queue, live transcripts, A/B boards,
  log viewer, search.
- **MCP server** for AI sessions: small paged outputs, server-side
  wait-until primitives, dry-run and idempotency keys.
- **Search**: full-text, regex and semantic search over transcripts, reasoning,
  tool arguments and logs, plus replay of a recorded request.

Planned command shape (from [090](docs/specs/090-interfaces.md), subject to change):

```
automaticum status
automaticum run start --arm A --task T [-n 5] [--lane L]
automaticum run show <id>
automaticum arm diff A B
automaticum cell run --suite S --arm A --out DIR
automaticum oracle verify <task>
automaticum report ab ...
automaticum find "<query>" [--semantic]
automaticum doctor
```

## Repository layout

```
docs/specs/            the spec set (start at 000-overview.md)
docs/specs/archive/    the two input documents the set was refined from
```

## Documentation

| spec | content |
|---|---|
| [000 overview](docs/specs/000-overview.md) | purpose, usage modes, vocabulary, map and traceability |
| [010 architecture](docs/specs/010-architecture.md) | daemon, agent mode, access, operating rules |
| [020 run record](docs/specs/020-run-record.md) | what a run records, verdicts, the basis |
| [030 configuration](docs/specs/030-configuration.md) | lanes, images, endpoints, arms |
| [040 runner](docs/specs/040-runner.md) | SDK use, wire capture, extension parity |
| [050 orchestration](docs/specs/050-orchestration.md) | scheduling, gates, control |
| [060 tasks and suites](docs/specs/060-tasks-and-suites.md) | task format, oracles, suite interface |
| [061 BFCL](docs/specs/061-suite-bfcl.md) · [062 SWE-bench](docs/specs/062-suite-swe-bench.md) · [063 multi-language](docs/specs/063-suite-multi-language.md) | suites |
| [064 engine adapters](docs/specs/064-engine-adapters.md) | Cerebriline as the engine of an external harness |
| [065 cell contract](docs/specs/065-cell-contract.md) | `cell run`, the result file, exports |
| [070 diagnostics](docs/specs/070-diagnostics.md) | metrics, detectors, statistics |
| [080 store](docs/specs/080-store.md) | DuckDB, Lance, blobs, the spike |
| [090 interfaces](docs/specs/090-interfaces.md) | CLI, TUI, API, dashboard, MCP, search |
| [100 legacy import](docs/specs/100-legacy-import.md) | the old harness's corpus |
| [110 acceptance and roadmap](docs/specs/110-acceptance-and-roadmap.md) | acceptance criteria, proposed milestones |
| [decisions](docs/specs/decisions.md) | decision log and open questions |
