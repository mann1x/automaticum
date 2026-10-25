# 000 — Overview, vocabulary and map of the spec set

**Status:** draft 0.6, 2026-10-25. Refined from two input documents kept in
[`archive/`](archive/): the draft specification 0.4 (from the Cerebriline
session, the first user) and the omnimergekit evaluation requirements 0.1 (from
the omnimergekit session, the second user). Items marked **Open** are questions,
not decisions; all of them are listed in [`decisions.md`](decisions.md).

## 1. What automaticum is

automaticum is a test harness that runs coding agents built on the Cerebriline
npm modules (`@cerebriline/sdk`) against tasks with a verdict, on lanes it
orchestrates itself, and answers the questions about those runs without a
throwaway script.

It replaces a harness of four bash loops, about fifty driver scripts and a pile
of one-off analysis scripts, in which a run could be invalid without anyone
noticing. Its job is to make each such failure impossible or loud, and to make
the results queryable.

### 1.1 The failures it exists to prevent

Each of these happened, and each maps to a gate, an `INVALID` verdict or a
finding (see [110](110-acceptance-and-roadmap.md) §1, criterion 3):

- A verdict word that meant four different things; a timeout that exited 0 and
  was recorded as a failure.
- An oracle that never left the game's countdown and scored broken code as
  fixed for weeks.
- A thinking flag that overrode every model tag's own budget, with no record of
  what was sent.
- A sampler written to the wrong settings field, never applied, in a run that
  looked normal.
- A stale SDK build that killed the CLI at import and exited 0.
- The only record of the session state deleted for all but a few runs.
- A grep for uppercase verdicts that dropped every lowercase one.
- Edits to a running loop script taking effect mid-campaign.
- Six hours of zeros measured on the wrong port.
- A score that was mostly the scaffold: one model scored 60.47, 59.20 and 53.73
  on SWE-bench under three agent engines.

### 1.2 Non-goals

- Not a model server. It talks to endpoints and does not load models. In mode C
  (§2) the caller owns serving entirely.
- Not a replacement for the VS Code extension's own tests.
- Not a general CI system. One purpose: agent runs with a verdict.
- Not a host for completion-form benchmarks (one prompt, one answer). Those
  stay in `omk_eval` and MultiPL-E; automaticum runs tasks with an agent loop.
  The one exception is the BFCL proxy cell ([061](061-suite-bfcl.md)).

## 2. Three ways it is used

| mode | who owns tasks and scoring | who owns the agent loop | who owns the model server |
|---|---|---|---|
| **A. Autonomous** | automaticum (a suite) | Cerebriline, in a lane | an endpoint automaticum is given |
| **B. Engine for an external harness** | the external harness (SWE-bench, BFCL, Harbor, Inspect) | Cerebriline, called by the harness through an engine adapter | an endpoint automaticum is given |
| **C. Backend of `omk_eval`** | automaticum, on behalf of an omnimergekit template | Cerebriline | omnimergekit serves the model and passes the endpoint |

A score from mode B or C is a score of *model + Cerebriline build*. It is never
comparable to a published number for the same suite under another engine, and
every report says so in its header.

## 3. Vocabulary

Use these terms exactly. A new term gets a row here before it is used anywhere.

| term | meaning |
|---|---|
| **task** | one problem with a workspace, a prompt and an oracle. |
| **suite** | a named set of tasks plus how to fetch, prepare and score them. |
| **arm** | one fully specified configuration under test. Two arms differ in at least one recorded field. |
| **run** | one arm on one task, once. The unit that gets a verdict. |
| **batch** | N runs of one arm on one task or suite. |
| **cell** | one suite, or named subset, scored for one arm: the unit that gets one headline number. |
| **cohort** | a named set of cells meant to be compared, with one declared variable. |
| **campaign** | an ordered set of batches, with gates and stop conditions between them. |
| **basis** | every recorded fact that must be equal for two cells to be compared ([020](020-run-record.md) §5). |
| **headline metric** | the one metric a suite subset declares as its score, with its direction. Declared in the suite, never chosen at report time. |
| **lane** | an execution slot: one container on one host, bound to one endpoint, running one run at a time. |
| **endpoint** | a model server, configured separately from lanes. |
| **oracle** | the harness's own check of the result, run by the harness after the agent stops, never by the agent. |
| **gate** | a precondition that must hold before a run starts. |
| **finding** | something a detector found in a run. |
| **cap hit** | a generation ended by a length limit, at any turn, not only the last. |
| **errored** | a task that produced no scoreable result because of the harness, the server or the environment: the union of `ABORTED` and `INVALID` in reports. |
| **engine adapter** | the piece that lets an external harness call the Cerebriline runner as its agent ([064](064-engine-adapters.md)). |
| **runner** | the Node program that drives the agent through the SDK inside a container ([040](040-runner.md)). |
| **agent mode** | the same Go binary running on a remote host under the daemon's control ([010](010-architecture.md) §3). Distinct from "the agent", which always means the coding agent under test. |

## 4. Map of the set

| file | content |
|---|---|
| [010-architecture](010-architecture.md) | daemon, CLI, agent mode, host rules, access model, operating rules |
| [020-run-record](020-run-record.md) | what a run records, verdicts, task versions, the basis |
| [030-configuration](030-configuration.md) | TOML files for lanes, images, endpoints, arms |
| [040-runner](040-runner.md) | the runner, build pinning, wire capture, extension parity |
| [050-orchestration](050-orchestration.md) | scheduling, gates, control, recovery |
| [060-tasks-and-suites](060-tasks-and-suites.md) | task format, oracles, the suite interface, Gitea |
| [061-suite-bfcl](061-suite-bfcl.md) | BFCL |
| [062-suite-swe-bench](062-suite-swe-bench.md) | SWE-bench and SWE-bench-style sets |
| [063-suite-multi-language](063-suite-multi-language.md) | our own multi-language agentic suite |
| [064-engine-adapters](064-engine-adapters.md) | adapters for external harnesses |
| [065-cell-contract](065-cell-contract.md) | `automaticum cell run`, the result file, exports |
| [066-suite-terminal-bench](066-suite-terminal-bench.md) | Terminal-Bench 2.0, imported as the held-out suite `tbench2` |
| [070-diagnostics](070-diagnostics.md) | metrics, detectors, statistics |
| [080-store](080-store.md) | DuckDB, Lance, blobs, the spike |
| [090-interfaces](090-interfaces.md) | CLI, TUI, HTTP API, dashboard, MCP server, search |
| [100-legacy-import](100-legacy-import.md) | import of the old harness's corpus |
| [110-acceptance-and-roadmap](110-acceptance-and-roadmap.md) | acceptance criteria and milestones |
| [decisions](decisions.md) | decision log and the open list |

Cross-references use the file number and section: "050 §2".

## 5. Traceability

Every section of the two input documents has a home.

| draft 0.4 | here |
|---|---|
| 1 Why, non-goals | 000 §1 |
| 2 Vocabulary | 000 §3 |
| 3 Architecture | 010 |
| 3.1 Runner and SDK | 040 |
| 4 Run record, verdict | 020 |
| 5.1 Lanes, 5.2 Images, 5.3 Endpoints, 5.4 Arms | 030 |
| 6.1 Scheduling, 6.2 Gates, 6.3 Control | 050 |
| 6.4 Git and Gitea | 060 §6 |
| 7.1 Custom tasks, 7.4 Suite interface | 060 |
| 7.2 BFCL | 061 |
| 7.3 SWE-bench | 062 |
| 8.1–8.4 Diagnostics | 070 |
| 8.5 Logs, search, replay | 090 §6 |
| 9 Interfaces | 090 |
| 10, 10.1 Store and spike | 080; corpus import in 100 |
| 11 Acceptance | 110 |
| Appendix A (scripts replaced) | 100 §5 |
| Appendix B (one-command questions) | 110 §2 |
| Appendix C (open questions) | decisions |

| omnimergekit 0.1 | here |
|---|---|
| R1 Three modes | 000 §2 |
| R2 Terms | 000 §3 |
| R3 Contract with `omk_eval` | 065; retention in 080 §5 |
| R4 Basis | 020 §5 |
| R5 Evaluation gates | 050 §3 |
| R6 Engine adapters | 064 |
| R7 BFCL | 061 |
| R8 SWE-bench | 062 |
| R9 Diagnostics | 070 |
| R10 Comparing arms | 070 §6 |
| R11 Multi-language suite | 063 |
| R12 Operations | 010 §6, 050 |
| R13 Exports | 065 §5 |
| R14 Open questions | decisions |
