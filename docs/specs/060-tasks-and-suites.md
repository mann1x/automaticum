# 060 — Tasks, oracles and the suite interface

**Status:** draft 0.5, 2026-10-12.

## 1. Scope

**Decided by the owner (2026-10-12):** automaticum runs tasks with an **agent
loop**. Completion-form benchmarks (one prompt, one answer, scored by hidden
tests) stay in `omk_eval` and MultiPL-E. The only run without our agent loop is
the BFCL proxy cell (061 §3).

## 2. A task

A task is a directory under `tasks/<suite>/<task>/`:

| part | content |
|---|---|
| `task.toml` | id, version, problem id, labels (language, tier, family), required language layers, limits, the prompt, the oracles, declared default answer for questions |
| workspace seed | the files the agent starts from, restored and hash-checked before every run |
| visible tests | optional; a file set the agent may read and run |
| oracle | command plus an expectation: an exit code, or a declared pattern when the command exits 0 either way |
| hidden tests | outside the workspace, never mounted into the agent's container |
| fixtures | a **known-good** and a **known-bad** result |
| scripted user | optional (040 §6.2) |
| hooks | optional per-task prepare and collect steps, run by the harness |

Needed beyond the old harness:

- **Several oracles per task:** one primary (the verdict) and secondary checks
  ("did not touch files outside X", "the other tests still pass") recorded as
  findings.
- **Partial scores**, recorded and never the verdict.
- **Multi-turn tasks** with a scripted user, since questions, steering and
  queued messages are product features that could not be tested unattended.
- **Limits per task**, calibrated from the reference solution's run time on the
  lane, not one global limit. A global limit is a language filter.

## 3. Oracles

- The harness runs the oracle after the agent stops, from a copy the agent
  could not write, and checks its hash before reading its output (020 §3).
- **Oracle validation is a first-class command.** `automaticum oracle verify
  <task>` proves the oracle passes the known-good fixture and fails the
  known-bad one. A task without both cannot be used in a campaign. The
  reference is run several times to reject flaky tests.
- For an external suite the equivalent is gold validation (050 §3.2).

### 3.1 Periodic oracle sampling

**Decided by the owner (2026-10-12):** optional per task, **off by default**.
A task may declare a trigger (after each edit transaction, after each
compaction, or an interval). The oracle then runs on a **snapshot** of the
workspace, never in the live one, and its results feed the "correct at some
point, not at the end" finding (070 §4). The cost is recorded per run.

## 4. The suite interface

A suite is a plugin: **fetch → prepare(task) → prompt(task) → run → collect →
score**. It declares:

- its source and pinned version (by commit where a project re-cuts its tags);
- named **subsets**, each with a stored and hashed list of task ids in order.
  A subset is part of the basis, and a subset chosen after seeing results is
  not a bench;
- per subset, the **headline metric** and its direction, and the band of
  healthy output length used by the canary (050 §3.3);
- its scorer: tool, version or commit, image digest;
- how its results map to verdicts.

New suites (Aider polyglot, Terminal-Bench, tool-quiz cases mined from real
sessions) fit without touching the core.

**What the suite withholds, the agent never gets.** Aider's polyglot set hides
the tests from the agent; SWE-bench hides the failing tests. A setup where the
agent can see the oracle is a different bench.

## 5. Suites in this set

| suite | spec | kind |
|---|---|---|
| custom tasks (the old harness's use) | this file | ours |
| BFCL | 061 | external, two cells |
| SWE-bench and SWE-bench-style sets | 062 | external |
| multi-language agentic suite | 063 | ours |

`manic_miner` is the first custom task to port, with its history, so old and
new results can be laid side by side (100 §3).

## 6. Git and Gitea

For tasks that need a remote (clone, branch, push, open a pull request,
review): a Gitea instance managed by automaticum.

Per run: a fresh repository or fork seeded from the task, a scoped token, and
afterwards the pushed refs, pull requests and comments collected as artefacts
and available to the oracle. Torn down or archived per policy.

**Open:** one long-lived Gitea instance per daemon, or one per campaign; and
where it runs when lanes are on a remote host.

## 7. Private tasks

Tasks carry a canary string and a `private` flag. Export, the semantic-search
embedder and the chat box (090 §7) must not send private task text to a cloud
endpoint.
