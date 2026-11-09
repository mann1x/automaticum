# 050 — Orchestration: scheduling, gates, control

**Status:** draft 0.24, 2026-11-09.

## 1. Scheduling

- A queue of runs. The scheduler places a run on a lane that satisfies its
  labels and whose endpoint has capacity. One run per lane by default.
- **Interleaving:** A/B arms alternate run by run on the same lane (ABAB), so
  drift in the host does not land on one arm. One host generates at about half
  the speed of another because of a power cap; lane skew is real.
- A campaign is data (030 §6): batches, order, gates, stop conditions.
  **As built (milestone 4):** `automaticum campaign start <name>` reads
  `campaigns/<name>.toml` (030 §6), checks that every batch could be queued,
  and keeps the definition as read: an edit to the file affects the next
  start only. The daemon then starts each batch, under the key
  `<campaign>-<batch>` (so a batch is never queued twice), when the one
  before it has ended, and every few seconds applies the stop conditions;
  a stopped batch's queued runs are recorded `SKIPPED` with the condition,
  and its run in flight finishes. Each run records its campaign. The state
  is in `control.json` and survives a restart. Starting a campaign again
  starts nothing and shows where it is. `campaign pause` holds the current
  batch as `batch pause` does (its run in flight held, the endpoint freed),
  `campaign resume` lets it go on, `campaign stop` ends it gracefully. Its
  end, finished or stopped, is notified as a refusing gate is.
  `automaticum campaign show <name>` gives the state of each batch and, below
  it, each batch compared (070 §6): the campaign's report. Not built yet:
  gates of a campaign's own, priorities and reservations, and running one
  campaign across two hosts (agent mode, milestone 7).
- **A batch's tasks and limits (D107, 2026-10-24).** A batch takes several
  tasks (`run start --task A --task B`) and whole suites (`--suite
  train-ml`, every task of it once, a task named twice runs once); each
  round runs every arm on every task in turn, each task in its own lane
  image. A run's wall and idle limits are the lower of the arm's and the
  task's, unless the batch sets its own: `run start --wall-s S --idle-s S`,
  `wall_s` and `idle_s` of a campaign batch, or `automaticum batch limits
  <batch> --wall-s S` for the runs still queued (the run in flight keeps
  its limits). A batch's limit replaces both and may be longer; the run
  records it as set by the batch (020 §1). A campaign batch also takes
  `suites` and `capture_requests` (D104). The idle limit runs while the
  runner's stream is silent; from protocol 10 the bytes of a model
  response still arriving keep it from running (040 §7.1, D138).
- Priorities, and reservations ("this lane is mine until 08:00").
- Other things use these GPUs. A lane can be marked busy by an external signal
  and the scheduler waits. It never preempts someone else's work. One heavy job
  per GPU.
- Concurrency asked for by an external harness is a request; lanes and endpoint
  capacity decide. The adapter queues rather than oversubscribing (064 §2).
- Large suites are sharded across lanes, with per-instance environment caching
  and resume of a half-finished evaluation (062).

**As built (2026-11-01, scheduling):** a run starts only while its arm's
endpoint serves fewer runs than its `capacity` (030 §4); a run held back
says `endpoint <name> serves its capacity of N` in `status`. A mode C
cell's endpoint is the caller's and is not counted (065 §3.1).

**Decided by the owner (2026-11-01), D122, D123, D125, as built:**

- **Labels (D122).** A task and an arm may say `requires = ["key=value",
  ...]`. A batch runs on one lane, so a run queued without `--lane` goes to
  the first lane, by name, whose `labels` hold every requirement of its
  arms and tasks and that may use the arm's endpoint; when none does, the
  start is refused naming the labels. A `--lane` without them is refused
  too. An arm's `requires` is in its resolved form but not its identity.
- **Busy signal (D123).** A lane may set `busy_command`, run on the
  daemon's host before each start with a time limit (`busy_timeout_s`, 10 s
  unless set). Exit 0 means something else uses the lane's devices: the run
  waits as for the `free` gate, notified once (gate `busy`), nothing
  unloaded, and its output is the reason `status` shows. A command that
  does not answer in time, or cannot be run, counts as busy, so a broken
  command never starts work beside someone else's.
- **Priorities and reservations (D125).** `run start --priority N`: higher
  starts first, and equal priorities keep the queue's order and so a
  batch's A/B interleaving; `batch ls` shows it. `automaticum lane reserve
  <lane> --for <batch|campaign> --until T` (a time, or a length such as
  `8h`): only that batch, or the batches of that campaign, start on the
  lane until T; others say `lane reserved for X until T`; a run in flight
  finishes. `lane unreserve` ends it. Both are kept in `control.json`.

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

### 3.1.1 As built (milestone 3)

The gates are `internal/gate`, asked by the daemon before every run and by
`automaticum doctor`. Each has a name and a version and gives one of four
answers: **pass**, **refuse** (the run is `SKIPPED` with
`gate <name>: <reason>`), **wait** (the run stays queued, the lane shows
why, and the gates are asked again every half minute) and **unchecked** (the
gate could not look and says why; it does not stop a run and is never shown
as a pass). Every gate's answer is recorded in the run (020 §1, `gates`).
The gates ask the endpoint what it is and what it holds; **no gate generates
text, loads a model or unloads one**.

| gate | what it looks at now | not yet |
|---|---|---|
| `lane` | the lane exists, may use the arm's endpoint, the arm is not a template | |
| `free` | the endpoint holds no other model in memory; if it does, **wait** (D64) | requests in flight for someone else: needs the agent |
| `model_tag` | the endpoint has the tag; its digest equals the arm's pin when the arm pins one | |
| `template` | the server lists tool calls among what the tag can do; template size and hash, renderer and parser are recorded. On llama-server (kind `llamacpp`, since 2026-10-23) the facts come from `/props`: the build, the template's hash, `chat_template_caps`, the default sampler and window, the weights file and its end token; a server that gives no `chat_template_caps` leaves the gate unchecked | the template's text is not parsed; a weights file is named, not hashed |
| `sampler` | the arm is complete; the tag's own sampler values are listed next to it | "written and read back" is the wire comparison after the run (070 §4.1) |
| `thinking` | who owns thinking and the tag's budget; never refuses (D63) | |
| `gpu_residency` | a loaded model is fully on the GPU; unchecked when it is not loaded. After the run the same look is a metric and, when partly off the GPU, a finding | named tensors parked on the CPU: needs the server log |
| `harness_build` | the lane image is there; the SDK build the arm names is defined, installed, and the bundle its definition pins (D54, D66; 040 §3.1). The run hashes the bundle's files again and the runner checks the installed package | |
| `host` | free space against the floor, inotify watches | clock sync, the user-namespace setting |
| `oracle` | the task's artefacts are built and `automaticum oracle verify` proved the oracle for the task **as it is now** (the proof is kept by the task's hash) | |
| `workspace` | the runs directory is writable and not on a RAM-backed file system; the seed is there. The copy and its hash check are the run's | |
| `bind_address` | the endpoint's address names a server and answers from this host | whether the server listens on every interface of its host: needs the agent |
| `gpu_ownership`, `server_log_level`, `expert` | nothing: unchecked, with the reason | milestone 7 |

**Notification.** A refusal is notified every time, a wait once per run: one
line in the daemon's log and, when `[notify] webhook` is set in
`automaticum.toml`, one JSON object by POST (`source`, `kind`, `status`,
`gate`, `detail`, `lane`, `run`, `arm`, `task`, `batch`, `counts`, `time`).
A webhook that fails is logged and stops nothing.

**As built (2026-11-01, 090 §5):** the kinds are `gate` (refuse, wait),
`canary` (stop), `campaign` (finished, stopped; `gate` holds its name),
`batch` (`finished` when its last run ends, once for each number of
finished runs, with `counts` by verdict; `stopped` by `batch stop` or the
canary, naming the run in flight), `run` (its verdict, at its end) and
`archive` (`unavailable` when a pass moved nothing because the archive
could not be reached, `failed` when blobs stayed on the live tier; 080 §5).
`[notify] events` lists the kinds sent; empty sends all but `run`, which a
batch of many runs would make noisy. The log has every kind always.
`[notify] command = ["program", "arg", ...]` runs once per notification with
the JSON object on its standard input and `AUTOMATICUM_NOTIFY_KIND`,
`_STATUS`, `_RUN`, `_BATCH`, `_LANE` and `_GATE` in its environment, for at
most 30 s; a failure is logged with its first 300 characters of output and
stops nothing. This is how the sessions' mailbox is reached: the
installation names its own mailbox client there, so the public repository
holds no mailbox of its own. A static-gate refusal of a whole start is the
caller's 422, which names the gate (`gate`), and is not notified.

`automaticum doctor [--lane L] [--arm A] [--task T] [--all]` asks every gate
for every lane, every arm the lane may run and every task, and fails when a
gate refuses.

**Built (2026-10-25):** the static gates, `harness_build` and `oracle`,
answer from the arm, the task and what the installation has (the lane image,
the SDK build, the task's artefacts and its oracle's proof): waiting changes
nothing. `run start` (and a campaign's batch, and a dry run) asks them for
every arm and task first, and a refusal refuses the start (`422`, "nothing
was queued", each arm, task and gate named) instead of queueing runs that
are `SKIPPED` at once. The other gates are still asked when each run starts.

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

**Decided by the owner (2026-10-14), D84:** the canary judges the first 5
finished requests of a batch. The length of the answers matters for specific
kinds of evaluation, not in general: a band is checked only where a task or
suite declares one, and otherwise the check is reported unchecked.

**As built (milestone 4):** while a batch has a run in flight and no
verdict, the daemon reads that run's wire capture every few seconds and
takes the finished requests of the agent's loop (the SDK's own probes and
requests that failed on the way are not generations), across the batch's
runs in order, until it has five. Each is checked for an empty answer, an
answer the server ended by length (`done_reason` `length`), and any of the
model's own control tokens in the visible text: the tokens its tokenizer
marks as control (type 3), read once from the endpoint's `/api/show`, such
as `<|im_start|>` and `<|im_end|>`. Tokens of other types, such as
`<think>` and `<tool_call>`, are not control tokens. An endpoint that cannot
list the tokens leaves that check unchecked, never passed. One problem stops
the batch: the run in flight is aborted ("the canary stopped the batch: ..."),
the queued runs are recorded `SKIPPED` with the canary's words, and a
notification goes out as for a refusing gate. The verdict, sound or not, is
kept in `control.json` and shown by `automaticum batch show <batch>`.

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
  **Decided by the owner (2026-10-13), D77:** pause inside a run is added
  when the SDK supports it; until then pause works between runs only.
  **Decided by the owner (2026-10-13), D83:** a pause is there to release
  the endpoint, when the GPU is needed for something else. As built:
  `automaticum run pause <run>` asks the runner to hold before its next
  model request (the request being answered finishes first, which can take
  minutes), and on an Ollama or xOllama endpoint the daemon then asks the
  server to drop every model it holds. `automaticum run resume <run>`
  releases the hold; the next request makes the server load the model
  again. The run's process and container stay: every counter and guard of
  the SDK is untouched. Each hold is in the record with its two times,
  time held is taken out of the wall time, limits do not run while a run
  is held, and the run carries the `paused` flag. Not offered: stopping the
  container during a pause. The SDK cannot resume a session in a new
  process as the same run (it would need one more user message, and its
  guards start from nothing).
- **Infrastructure errors** (endpoint down, 429, network) wait and retry and
  are recorded as such. They are never the model's failure.
  **As built (2026-11-01):** the SDK waits and retries them itself: three
  attempts in its stream wrapper (250 ms after a server error, 2 s after a
  network one), then the agent's provider-error retries with backoff, and
  a turn the server dropped or queued is waited out; each retry is a
  notice, stored as a `notices.*` metric (D116). An agent that ends in
  error with a fault the SDK calls `refusal` or `transport` (its own
  patterns, ported to `run.Fault`) has exhausted them: the run is `ABORTED`
  ("the server failed after the SDK's retries"), never `FAIL` or `TIMEOUT`.
  A pass outranks the fault as it outranks a limit. Before, such a run
  went to the oracle and could be counted `FAIL`.
  **Decided by the owner (2026-11-03), D136:** a held stream is not such a
  fault. The SDK gave the server up on its silence ceiling, but the server
  was decoding all along: its parser held back the model's malformed
  output (070 §4, `held_stream`). That run goes to its oracle like any
  other: `PASS` if the oracle passes, else `FAIL`. Its reason is "held
  stream: model output unparseable for <silence> (n_decoded <N>)", next to
  the oracle's answer. The proof is the readings of the endpoint's
  `/slots` (its `n_decoded` still grew during the silence), or the server's
  log recorded by an operator with `automaticum run held <run> --task T
  --n-decoded N --source S`. That command decides a stored run again from
  its record, its oracle's answer included, and keeps the old verdict
  beside the new one. The SDK's message alone stays `ABORTED`. This rule is
  interim: once the server sends keepalives while it holds, such a request
  runs on to its output limit like any other.
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
  recorded `ABORTED(daemon stop)`. A service manager must not kill the
  daemon on its own while it drains: the example unit
  (`examples/systemd/automaticumd.service`) has `TimeoutStopSec=infinity`
  and `SendSIGKILL=no` since 2026-11-01. Before, it had 120 s and SIGKILL,
  so a restart with a run in flight killed the run after 2 minutes and the
  next start recorded it `ABORTED(daemon restart)`. To restart without
  waiting for a run: `lane drain` each lane, `automaticum wait idle --until
  free`, restart, `lane resume`. Nothing queued is lost: the queue, the
  drained lanes and the batches' limits are stored.
- **Decided by the owner (2026-11-01), D119:** a drain releases a held run.
  When a lane or the daemon is drained (`lane drain`, `lane quarantine`,
  `POST /v1/drain`, the first termination signal) while its run in flight
  is held by `run pause`, `batch pause` or `lane pause`, the drain resumes
  that run, the run finishes normally (oracle, ingest, verdict), and the
  lane then stays drained: a drain never waits on a hold and never aborts.
  A lane paused that is drained is shown drained; a batch paused keeps its
  queued runs held. The answer of `lane drain` says `released from its
  hold` (also under `--dry-run`). A held run whose endpoint was freed loads
  the model again on its next request.
- A run still held when the daemon stops without a drain (a second signal,
  a kill) is not resumed by the next daemon (D83: a resume in a new process
  is not the same run) and is recorded `ABORTED(daemon restart)`.
  Re-attaching a run in flight to a new daemon is not built.
- After a stop without warning, a run in flight is recorded
  `ABORTED(daemon restart)` from what the lane's state held, its container is
  removed by id, and it leaves the queue. A queued run that already has a
  record is not run again.
- The idempotency key names the batch: a repeated `run start` with the same
  key starts nothing and answers with the runs the first one made.
- The arm and the task are read when a run starts, not when it is queued.

**As built in milestone 4 (control, 2026-10-14):**

- `automaticum lane drain <lane>`: the run in flight finishes and is
  analysed (released first if it is held, D119), then the lane starts
  nothing until `automaticum lane resume <lane>`. `status` shows the lane `drained` and each queued run on it
  `lane drained`.
- `automaticum batch pause <batch>`: none of the batch's queued runs starts,
  and its run in flight is paused as `run pause` does (held before its next
  model request, then the endpoint freed of every model, unless
  `--keep-model`). Runs of other batches on the same lane are not held.
  `automaticum batch resume <batch>` releases the run in flight and lets the
  queued runs start.
- `automaticum batch stop <batch>`: every queued run of the batch leaves the
  queue and is recorded `SKIPPED` ("batch stopped by the operator before it
  started"). Graceful: the run in flight finishes, unless `--abort`. A run
  in flight that is held says so and is left to `run resume` or `run abort`.
- `automaticum run retry <run>` and `automaticum batch retry <batch>` queue
  a new run with the same arm, task and lane, in the same batch, for each
  run that ended `SKIPPED`, `ABORTED` or `INVALID`. A counted verdict
  (`PASS`, `FAIL`, `TIMEOUT`) is never run again, since a retry would replace
  a result; nor is an imported run, nor a run already retried. The old
  record is not changed; `run show` says `retries` on the new run and
  `retried by` on the old one, and both stay in the batch for `compare`.
  `--dry-run` says what would be queued.
- A run that never started now keeps in its record what it was asked to be
  (`provenance.asked`: arm, task, lane), so that it can be retried. Records
  written before this have no such field, and a retry refuses them with that
  reason.
- Drained lanes, paused batches and retry links are kept in `control.json`
  next to the database and survive a restart. A control file that cannot be
  read drains every lane, so nothing the operator stopped starts by
  accident.
- **Decided by the owner (2026-11-01, O32):** `requeue` is not adopted:
  `retry` runs again, `lane drain` and `run start --lane` move work. Pause
  at campaign level: `campaign pause|resume|stop` (§1).

**As built (2026-11-01, control):** the matrix's other cells. `lane pause`
(drained, its run in flight held, the endpoint freed unless
`--keep-model`), `lane quarantine --reason R` (drained, shown
`quarantined`, each queued run on it stopped by `lane quarantined: R`),
`lane abort` (its run in flight; the lane is otherwise left as it is), all
kept in `control.json`; `batch abort`; `campaign abort` (each started batch
stopped with its run in flight aborted) and `campaign retry` (each batch's
retry). Every one of these, and every other mutating request, takes a dry
run (`?dry_run=1`) and an idempotency key (the `Idempotency-Key` header,
answers kept 30 days, 090 §1). Daemon-level pause and resume are lane
pause and resume with `--all`. Not built: a quarantine set by the daemon
itself after repeated infrastructure failures (item 2 of the audit).

## 5. Watchdogs

- A run whose event stream is silent for N minutes while its process lives.
- A batch whose driver vanished. **Decided by the owner (2026-11-01,
  O37):** not adopted: the daemon drives every batch, a stopped `cell run`
  leaves its cell running (065 §3.1), and a daemon stop is the restart
  case (§4).
- A lane that produces only infrastructure failures: quarantine it.
  **Decided by the owner (2026-11-01), D124, as built:** when the last 10
  runs in a row on a lane ended `ABORTED` for the harness's or the server's
  reason (the container engine failed, the runner failed or ended without
  its last line, an empty event stream, the oracle's container could not
  run, a server fault after the SDK's retries, an agent that ended before
  the server answered), the daemon quarantines the lane, with the count and
  the last reason, and notifies (kind `lane`, each run and its reason).
  Any other end resets the count; an operator's abort, a daemon's stop, a
  container out of memory, a tampered oracle and an oracle that timed out
  are not counted. The number is `quarantine_after` in `automaticum.toml`
  or the lane's file (0 turns it off). Only `lane resume` lifts it. The
  count is kept in `control.json` and is checked before the lane takes its
  next run.
- A run whose endpoint identity changed since the probe at start: `INVALID`.

## 6. Unattended operation

- A heartbeat with counts every ten minutes, to the log and on request.
  **Built 2026-11-01:** every ten minutes the daemon logs `heartbeat` with
  its lanes by state, the runs in flight and queued, and the runs that
  ended since the heartbeat before, by verdict; the last one is in
  `status` and `GET /v1/heartbeat` (`?now=1` counts now).
- Server-side `wait` primitives so a caller does not poll (090 §5). **Built
  2026-11-01:** `automaticum wait` and `GET /v1/wait/{kind}` (090 §5).
- A mailbox message and a webhook on campaign end, on any refusing gate, and
  on a stopped batch. **Built 2026-11-01** through `[notify] webhook` and
  `[notify] command` (§2), with a batch's end and each run's end besides.
- A cell with any `SKIPPED` task is marked incomplete, and says so in its
  result file.
- **Decided by the owner (2026-11-09, D144):** the daemon takes a changed
  configuration while it runs, and nothing stops it unless that cannot be
  avoided. **Built 2026-11-09:**
  - A reload (SIGHUP, `config reload`, or the watcher, 090 §1) puts new,
    changed and removed lanes and endpoints, `[notify]`, `free_floor_gb`,
    `quarantine_after`, `archive_after_days`, `private_tasks`,
    `watch_config`, the token and `listen` in force at once. A new `listen`
    is bound before the old address is let go; one that cannot be bound
    refuses the reload. Arms and models were already read at each use.
  - `data_root`, `archive_root`, `docker_socket` and `repo` are held open
    by the running daemon: a change to them keeps the old value until the
    next start and is named (`restart` in `config show` and `status`).
  - Runs in flight keep the lane and endpoint they started with. A lane
    removed under a run finishes it, shown `removed` in `status`, and
    starts nothing more. A queued run whose lane or endpoint the
    configuration no longer has waits, with that reason, and starts once a
    reload brings it back; it is never SKIPPED for it.
  - A fault (a Go panic) in a run's handling ends that run ABORTED with
    `harness fault: …`; the lane and the daemon go on. A fault in the
    canary, campaign, heartbeat or archive loop is logged with its stack
    and the loop starts again.
