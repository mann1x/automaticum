# 064 — Engine adapters: Cerebriline as the engine of an external harness

**Status:** draft 0.7, 2026-10-25.

## 1. Why

External harnesses ship their own agent engines, and the score they report is
mostly the engine. An **engine adapter** lets a harness call the Cerebriline
runner as its agent (mode B, 000 §2). automaticum keeps its own run record for
every task the harness sends, so diagnostics are the same whichever harness
asked.

## 2. Rules common to every adapter

- The harness's task id maps one-to-one to an automaticum run. The harness's
  own result (reward, resolve status, score) is ingested as the oracle
  evidence, with the scorer's identity in the basis.
- The harness's limits (per-task timeout, step limits, cost limits) are
  recorded as arm fields. `TIMEOUT` says whether the harness or the agent hit
  the limit.
- The adapter never gives the agent anything the harness withholds (060 §4).
- Concurrency is the harness's request; lanes and endpoint capacity are
  automaticum's. The adapter queues rather than oversubscribing.
- **Resume:** a half-finished external evaluation continues from the finished
  instances.
- Harness result files are read by their real layout, verified on a known run.
  (Harbor writes a result file for the job and one per trial, and its
  pass-at-k block is empty even on perfect runs; the score comes from per-trial
  rewards.)

## 3. The four shapes

### 3.1 Predictions file

*Harness examples:* SWE-bench.
automaticum runs the agent on each instance in mode A, writes the predictions
file the official harness consumes, runs the official evaluation images, and
ingests their reports (062).

### 3.2 Agent in the harness's container

*Harness examples:* Harbor / Terminal-Bench, SWE-agent-style runners.
The harness launches the agent inside its own per-task container.

**Decided by the owner (2026-10-12):** the adapter is an installable agent that
**installs the pinned runner into the task container at start**, runs it there,
and streams events back to the daemon. The hard rule is therefore "Node never
on a host, only in containers" (010 §1).

Requirements:

- The installed runner is the same build, by hash, as the lane image's; the
  harness-build gate (050 §3.1) applies inside the task container.
- The install adds a Node runtime and the runner under a private prefix and
  changes nothing else in the task's environment. What it added is recorded, so
  a task that happens to test for Node is identifiable.
- The task container's image digest is recorded in place of the lane image
  digest.
- Events are streamed out over a connection the adapter opens to the daemon or
  its agent; if the harness forbids network access during a task, events are
  written to a file the adapter collects afterwards. **Open:** which harnesses
  need the offline path.

Terminal-Bench 2.0 does not need the adapter at run time: its tasks are
imported as ordinary tasks (066), and the runner is added to each task's image
when its lane layer is built, so events take the lane's usual path. What that
layer adds sits under the runner's own prefix, off the agent's PATH (D113,
040 §1), so the task's environment is otherwise the image's (2026-10-25).

### 3.3 Solver inside a Python framework

*Harness examples:* Inspect (`inspect_evals/bfcl`), lm-eval.
A thin Python shim, living in the framework's own container, forwards each
sample to automaticum's API and returns the agent's final answer or tool calls
in the framework's types. The shim holds no logic of its own.

### 3.4 OpenAI-compatible proxy

*Harness examples:* any harness that only knows a chat endpoint and keeps its
own loop.
Cerebriline's provider layer and tool-call parser exposed as an endpoint, with
no agent loop of ours. Runs in a lane container (the provider layer is Node).
Each request through the proxy is recorded as in 020 §2, and one harness sample
is one run. This is the shape behind the BFCL proxy cell (061 §3).

## 4. What a report says

Every report on a cell produced through an adapter states, in its header: the
harness and its pinned version, the adapter shape, and that the score is model
plus Cerebriline build and is not comparable to a published number under
another engine.
