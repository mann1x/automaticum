# 050 — Orchestration: scheduling, gates, control

**Status:** draft 0.7, 2026-10-13.

## 1. Scheduling

- A queue of runs. The scheduler places a run on a lane that satisfies its
  labels and whose endpoint has capacity. One run per lane by default.
- **Interleaving:** A/B arms alternate run by run on the same lane (ABAB), so
  drift in the host does not land on one arm. One host generates at about half
  the speed of another because of a power cap; lane skew is real.
- A campaign is data (030 §6): batches, order, gates, stop conditions.
- Priorities, and reservations ("this lane is mine until 08:00").
- Other things use these GPUs. A lane can be marked busy by an external signal
  and the scheduler waits. It never preempts someone else's work. One heavy job
  per GPU.
- Concurrency asked for by an external harness is a request; lanes and endpoint
  capacity decide. The adapter queues rather than oversubscribing (064 §2).
- Large suites are sharded across lanes, with per-instance environment caching
  and resume of a half-finished evaluation (062).

## 2. Gates

A gate failure produces `SKIPPED` or `INVALID` with the reason, never a run
that looks normal. Gates are plugins with a name and a version, so a campaign
can add one and a report can say which gate stopped what.

**A refusing gate notifies** (mailbox, webhook), not only exits non-zero: a
gate that dies quietly looks like a queue that is merely slow.

`automaticum doctor` runs every gate on every lane, now, without starting a
run.

## 3. Built-in gates

### 3.1 Before every run

| gate | checks |
|---|---|
| model tag | the tag exists and its digest matches the pin |
| template | the tag's template, renderer and parser are intact (a quoted template can swallow the directives after it and fall back to the GGUF's own) |
| sampler | the arm's sampler parses, was written, and was read back |
| GPU residency | the model is fully on the GPU after load, and no named tensor is parked on the CPU |
| GPU ownership | the lane's device binding is explicit, and the process landed only on that device |
| thinking | the request's thinking level and the tag's budget agree, or the arm says which wins. **Decided by the owner (2026-10-13):** an arm value the provider does not send does not stop the run; it is a finding ("stated, not sent") and reports show the effective value |
| free | the lane and the endpoint are free. **Decided by the owner (2026-10-13):** a busy endpoint makes the run wait in the queue, with one notification; nothing is unloaded |
| expert | an expert model, if configured, answers a real request |
| harness build | the installed SDK is the one named; the session started from an empty data directory (040 §5) |
| server log level | the level diagnostics need |
| host | disk space against the floor, inotify limits, user-namespace sysctl, clock sync |
| oracle | the oracle verifies against its fixtures (060 §3) |
| workspace | pristine: restored from source and hash-checked. A run never inherits what the previous one left |
| bind address | the model server is not bound to all interfaces and is reachable from the lane |

### 3.2 Before a cell or batch

| gate | checks |
|---|---|
| context per slot | read from the server: at least prompt plus output budget for the suite's largest task. A too-small slot truncates silently |
| parse failures degrade | the serving library degrades on a tool-call parse failure instead of returning HTTP 500. An unpatched server loses the generation and scores a correct answer as a failure |
| scorer validation | for external suites, the official gold patch or gold answer passes the scorer on a sample of tasks. A check that gold fails is a broken check |
| detector self-test | every detector fires on its positive control and stays silent on its negative controls (070 §3). A failing detector marks its metric unavailable instead of reporting zero |
| disk for the suite | image and environment size for the task set against free space |
| credentials | gated datasets and endpoints: the token exists in the secret store, checked before starting |

### 3.3 The first-generations canary

After the first three to five finished requests of a batch: none empty, none
ended by length, no control tokens in visible text, median length within the
band the suite declares. Failure stops the batch. Twice, two hours ran at the
wrong sampler before anyone looked.

## 4. Control

`pause`, `resume`, `abort`, `drain`, `requeue`, `retry`, at the level of run,
batch, campaign, lane and whole daemon.

- **Graceful by default.** Stop means "finish the run in flight, analyse it,
  then stop".
- **Abort** kills in the right order: the run's own process tree by recorded
  id, then confirms on the server side that generation has stopped. A client
  that merely leaves does not always cancel the request on the server. An
  aborted run is still ingested and analysed with what it produced.
- **Pause** of a run in flight suspends between turns, using the SDK's stop and
  resume with the session's messages.
  **Decided by the owner (2026-10-12):** paused intervals are recorded and
  subtracted from wall-time metrics, and the run carries a `paused` flag so
  reports can filter it or show it apart.
- **Infrastructure errors** (endpoint down, 429, network) wait and retry and
  are recorded as such. They are never the model's failure.
- **Restart:** the daemon survives its own restart. Runs in flight are
  re-attached or marked `ABORTED(reason=daemon restart)`, never lost and never
  duplicated.
- **Immutability:** configuration is fixed once a run has started. Changing an
  arm file affects the next run only, and the interfaces say so.
- **Idempotency:** every mutating command accepts an idempotency key. A retried
  command does not start a second campaign; re-running a cell with the same key
  resumes it (065 §2).
- **Dry run** for every mutating command, returning exactly what would start.

**As built in milestone 2** (one lane, runs only; batches and pause come
later):

- `run stop` takes a queued run off the queue and records it `SKIPPED` with
  that reason. It refuses a run in flight, which finishes by itself or is
  ended with `run abort`.
- `run abort` kills the run's container by its recorded id. The run is still
  collected and the oracle still runs, for the record; the verdict is
  `ABORTED`. Confirming on the server side that generation stopped is not
  done yet: it needs agent mode on the endpoint's host.
- The daemon's first termination signal drains: the run in flight finishes and
  nothing else starts. A second signal aborts the run in flight, which is
  recorded `ABORTED(daemon stop)`.
- After a stop without warning, a run in flight is recorded
  `ABORTED(daemon restart)` from what the lane's state held, its container is
  removed by id, and it leaves the queue. A queued run that already has a
  record is not run again.
- The idempotency key names the batch: a repeated `run start` with the same
  key starts nothing and answers with the runs the first one made.
- The arm and the task are read when a run starts, not when it is queued.

## 5. Watchdogs

- A run whose event stream is silent for N minutes while its process lives.
- A batch whose driver vanished.
- A lane that produces only infrastructure failures: quarantine it.
- A run whose endpoint identity changed since the probe at start: `INVALID`.

## 6. Unattended operation

- A heartbeat with counts every ten minutes, to the log and on request.
- Server-side `wait` primitives so a caller does not poll (090 §5).
- A mailbox message and a webhook on campaign end, on any refusing gate, and
  on a stopped batch.
- A cell with any `SKIPPED` task is marked incomplete, and says so in its
  result file.
