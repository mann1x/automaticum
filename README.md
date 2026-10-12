# automaticum

A test harness that runs coding agents against tasks with a verdict, on lanes it
orchestrates itself, and answers the questions about those runs without a
throwaway script.

> **Status: specification stage.** Nothing is implemented yet. The design lives
> in [`docs/specs/automaticum-draft-spec.md`](docs/specs/automaticum-draft-spec.md)
> (draft 0.4) and is still being refined. Items the spec marks **Open** are
> questions, not decisions.

## What it is for

automaticum runs agents built on the Cerebriline npm modules (`@cerebriline/sdk`)
against a model endpoint, on a task that has an oracle, and records enough to
prove what happened. It replaces a harness made of bash loops, about fifty driver
scripts and a pile of one-off analysis scripts, in which a run could be invalid
without anyone noticing: a setting that never reached the wire, an oracle that
passed broken code, a timeout that exited 0.

Its job is to make each of those failures impossible or loud, and to make the
results queryable.

It is not a model server, not a general CI system, and not a replacement for the
VS Code extension's own tests.

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
- Remote hosts run the same binary in an agent mode.

Preferred, to be confirmed by a spike (spec section 10.1): **DuckDB** for state,
configuration, metrics and statistics, **Lance** for text chunks, vectors and
full-text search, and a content-addressed blob store on disk for streams and
logs.

## Suites

- **Custom tasks**: a directory with the workspace seed, prompt, oracle and
  fixtures. `manic_miner` is the first to port.
- **BFCL** (Berkeley Function-Calling Leaderboard), scored with its own
  evaluator, version-pinned.
- **SWE-bench** (Verified, Lite, full), evaluated with the official harness
  images.

A suite is a plugin: fetch, prepare, prompt, run, collect, score.

## Interfaces

- **CLI** for everything, with `--json` and stable exit codes.
- **HTTP API and live dashboard**: lanes, queue, live transcripts, A/B boards,
  log viewer, search.
- **MCP server** for AI sessions: small paged outputs, server-side
  wait-until primitives, dry-run and idempotency keys.
- **Search**: full-text, regex and semantic search over transcripts, reasoning,
  tool arguments and logs, plus replay of a recorded request.

Planned command shape (from the spec, subject to change):

```
automaticum status
automaticum run start --arm A --task T [-n 5] [--lane L]
automaticum run show <id>
automaticum arm diff A B
automaticum oracle verify <task>
automaticum report ab ...
automaticum find "<query>" [--semantic]
automaticum doctor
```

## Repository layout

```
docs/specs/    specifications (start here)
```

## Documentation

- [Draft specification](docs/specs/automaticum-draft-spec.md): requirements,
  the run record contract, orchestration, diagnostics, store candidates,
  acceptance criteria and open questions.
- [omnimergekit evaluation requirements](docs/specs/omnimergekit-evaluation-requirements.md):
  what the model-evaluation side needs (result contract with `omk_eval`,
  comparability basis, SWE-bench and BFCL with Cerebriline as the engine, a
  multi-language suite, length/cap/looping diagnostics).
