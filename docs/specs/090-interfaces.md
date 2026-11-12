# 090 — Interfaces

**Status:** draft 0.35, 2026-11-12.

One API, four ways in. Everything the dashboard can do, the CLI and the API can
do; everything the CLI can do, the MCP server can do.

## 1. CLI

`automaticum <noun> <verb>`, with `--json` on every command and stable exit
codes.

```
automaticum status                          # lanes, queue, what is running
automaticum config check                    # the configuration, arms, models and tasks read as the daemon would at its start, with no daemon (D141)
automaticum config reload [--dry-run]       # the running daemon reads its configuration again and puts it in force, nothing stopped (D144)
automaticum config show                     # the configuration in force, whether it is watched, the last reload and why it was refused (D144)
automaticum sdk add|ls|verify <build>        # SDK builds: install, list, reproduce with no network (040 §3.1)
automaticum doctor [--lane L] [--arm A] [--task T] [--all]   # every gate, on every lane, now
automaticum lane ls|show|pause|resume|drain|quarantine
automaticum endpoint ls|probe|show|models
automaticum arm ls|show|diff|validate
automaticum basis show|diff
automaticum task ls|show|verify|build
automaticum task pin [--write] <suite/task | dir>   # build the artefacts, print or write the sha256s to pin (D140)
automaticum oracle verify <task>
automaticum suite import tbench2 -source D -commit C <task>...   # Terminal-Bench 2.0 tasks as tbench2, kept only if the oracle holds (066 §3)
automaticum run start --arm A --task T [--task T2 ...] [--suite S ...] [-n 5] [--lane L] [--wall-s S] [--idle-s S] [--capture-requests] [--no-preflight]
automaticum run server --url U --task T --extends A [-n 10] [--wall-s S] [--model TAG] [--sampler tag|arm] [--lane L] [--idempotency-key K] [--yes] [--dry-run]   # a server already running, in one step (030 §4.3, D146)
automaticum run ls|show|tail|stop|abort|pause|resume|retry|invalidate|identify|held
automaticum compare <batch> [<batch> ...] [--group AXIS] [--pool-versions] [--all]
automaticum batch|campaign start|ls|show|pause|resume|abort
automaticum cell run|show|ls
automaticum cohort ls|show [<name>]        # cells declared to differ in one variable; confounded when another fact differs (D120)
automaticum report run|batch|cell|ab|regression|census [...]
automaticum find "<query>" [--semantic] [--where ...]
automaticum logs <run> [--server|--runtime|--events|--wire] [--grep ...] [--turn N]
automaticum notices (<run> ... | --batch B ...) [--key K]   # the SDK's notices with their sentences (D116)
automaticum replay <run> --turn N [--endpoint E]
automaticum detect <file> [--runaway N] [--sdk R] [--first-match]   # the detector library on one text; --self-test runs its controls
automaticum detect [<detector>...] [<run>...] [--batch B] [--cohort C] [--campaign X] [--lane L] [--arm A] [--task T] [--suite S] [--model M] [--verdict V] [--since T] [--until T] [--all] [--metric M] [--group AXIS] [--firings] [--calls] [--quiet] [--fail-if "EXPR"] [--fail-scope S] [--record] [--finished] [--imported]   # every detector again on any set of runs, in flight included (070 §3.3, D133)
automaticum embed start|status|stop [--where ...]   # by hand only (080 §5.2)
automaticum reanalyse [--run R | --batch B] [--imported] [--detector D] [--where ...]
automaticum rescore <batch> [--out D] [--no-wait]   # the cell scored again with its tasks' current oracles (D121, 065 §4)
automaticum export run|cell|cohort|campaign ...
automaticum wait run|batch|cell|campaign|lane|idle [<id>] [--until "<condition>"] [--next|--each [--seen N]] [--any] [--timeout D]
automaticum agent                           # agent mode on a remote host
```

Every mutating command has `--dry-run` and accepts `--idempotency-key`.

**Built in milestone 2:** `status`; `arm validate|show|diff`; `task
ls|show|build`; `oracle verify`; `lane ls`; `endpoint ls` (prints no address
unless asked); `run start|ls|show|tail|stop|abort|invalidate|identify` (`run identify <run>` adds where and on what a run ran to a record that lacks it, from today's configuration and catalogue, marked as added later, D74; `run held <run> --task T --n-decoded N --source S` records a held stream from the server's log and decides the run again, D136); `logs`
(`--events`, `--wire`, `--runner`, `--oracle`, `--kind`); `legacy import`;
`store check`. `task build` is new in this list: it builds a task's artefacts
and checks their pinned hashes.

**Built on 2026-11-04 (D140, D141):** `task pin [--write] <suite/task |
dir>` builds each artefact of a task whatever it pins, an empty `sha256`
included, and prints the sha256 each build gave; `--write` puts them into
the task's `task.toml` (the value replaced, or the key added after the
artefact's name, every other line kept) and loads the task to check. The
task is found by its directory under the repository's `tasks/` or the
private root, or given as a directory, so a task still being written
hides no other. `config check` reads the configuration directory as the
daemon does at its start (`automaticum.toml`, lanes and endpoints, strict
on unknown keys), the token, the `repo` with `tasks/` and `images/`,
`data_root`, `archive_root` and `private_tasks`, every arm (each on its
own, and the endpoint it names), the models and the tasks of both roots,
and says each problem; it changes nothing and exits 1 on any problem, so a
configuration edit is checked before a restart. Of these, `--dry-run` and
`--idempotency-key` exist on `run start` so far.

**Built on 2026-11-12 (D146):** `run start` asks each arm's endpoint
before queueing and refuses a batch its gates would skip run by run
(050 §4.1; `--no-preflight` queues regardless), and its answer carries
`warnings` for an endpoint that did not answer. `run server` runs tasks
against a server already running in one step: endpoint, arm, lane,
loopback forward (`forward_to`), proposed idempotency key, preflight
(030 §4.3). `config show` lists the forwards; a reload names those that
could not listen (`unforwarded`).

**Built on 2026-11-09 (D144):** the running daemon takes a changed
configuration without a restart. `config reload` (`POST /v1/config/reload`,
`?dry_run=1`; answered 422 with the reason when refused), SIGHUP, and, unless
`watch_config = false`, any change to `automaticum.toml`, `lanes/`,
`endpoints/` or the token files (read every two seconds, put in force once
two reads agree) read the directory again with the checks of the start and
put it in force for every reader at once. A directory that does not load
changes nothing: the configuration in force stays, whole. `config show`
(`GET /v1/config`, and `config` in `status`) says when it was loaded, how
many reloads, whether it is watched and what the last reload did or why it
was refused. The daemon's signals: SIGHUP reloads; SIGUSR1 logs the state
(configuration, lanes, queue and why each waiting run waits); SIGUSR2 logs
every goroutine's stack; the first SIGINT or SIGTERM drains and a second
aborts (050 §5); SIGPIPE is ignored. All are taken from the first moment of
the start, so none stops a daemon still starting.

**Built in milestone 4 so far:** `compare` (070 §6); `run start` with
several `--arm` and `--order`; `run pause|resume|retry`; `lane
drain|resume`; `batch show|pause|resume|stop|retry` (050 §3.3, §4); `basis show|diff` (020 §5); `cell run` (065 §3.1); `campaign start|ls|show|pause|resume|stop` (050 §1). `run retry` and
`batch retry` have `--dry-run`. The API routes are `POST
/v1/runs/{id}/retry`, `POST /v1/lanes/{lane}/drain|resume` and `GET /v1/basis?batch=...`, `GET /v1/batches/{batch}`, `GET /v1/batches/{batch}/cell`, `POST|GET /v1/campaigns`, `GET /v1/campaigns/{name}`, `POST /v1/campaigns/{name}/pause|resume|stop`, `POST
/v1/batches/{batch}/pause|resume|stop|retry|limits`.

**Built on 2026-10-24 (D107):** `run start` takes `--task` several times and
`--suite` (every task of a suite), and `--wall-s` and `--idle-s`, the
batch's own limits; `batch limits <batch> [--wall-s S] [--idle-s S]` sets
them for the batch's queued runs (`POST /v1/batches/{batch}/limits`).

**Built on 2026-10-25 (D102, D104, D106):** `export conversations --out
<dir> [--batch B ...] [--run R ...]` writes each run's capture into
`<dir>/conversations/<run>/` and adds its lines to `<dir>/loop_spans.jsonl`
and `<dir>/manifest.jsonl`, its record to `<dir>/runs/<run>.json` (110
§3.5); `--batch` also takes a pattern (`'k-pool-*'`). The route is
`GET /v1/runs/{id}/export`, a tar of `meta.json`, `spans.jsonl` and the
capture's files; it refuses a run of a task that is not a training task
(403; a private training task exports, D137), an INVALID run (422) and a run without a capture (404), and the
command names each one it passes over (110 §3.5). So a batch of held-out
tasks (`custom/manic_miner`, `tbench2/*`) exports nothing, by design (D105):
its runs are read with `run show -json` instead.

**Built on 2026-10-27:** `run ls` takes `--task` as `suite/task`, as every
other command names a task (a bare task id still matches it in any suite;
before, `suite/task` matched no finished run), and `--suite`, `--model`,
`--arm` and `--arm-id`. `--arm` is the arm as its file in the checkout is
now, so runs of an earlier version of it, another identity, are left out.
The table has an ARM column, the arm's name as the record says it. The
route is `GET /v1/runs?task=&suite=&model=&arm_id=`. (Until 2026-11-09 a
queued or running run was left out whenever an arm or a model was asked;
see below.)

**Built on 2026-11-09:** a run not finished is found by its arm and model
as a finished one is. A running run is of the arm it started with, and a
queued run of the arm it would start with now, read from its file (the arm
is fixed when a run starts). So `run ls --arm A` lists a batch in flight,
not only what has finished. `run ls --state queued|running|finished`
(`?state=`) keeps the runs in one state. `arm ls` lists, beside each arm in
the configuration, its runs RUNNING and QUEUED and the lanes they run on.
`arm ls --active` lists only the arms with runs running or queued, and
`arm ls --running` only those with a run in flight; both come from the
daemon (`GET /v1/arms[?running=1]`: each arm's id, model, endpoint, runs in
flight, lanes, batches, counts and since when). An arm whose file changed
while a run of it was in flight is two entries: the run keeps the arm it
started with, and its queued runs take the new one. An arm that can no
longer be read is listed with why, since its queued runs will be skipped
when they start.

**Built on 2026-11-09, later:** `run ls --arm-name N` (`?arm=N`) keeps the
runs started with the arm named N, whatever its content: every version of
it, queued and running runs included, and never a run of another arm that
resolves alike (two such arms share an id, so `--arm` and `--arm-id` find
both). The name is stored with each run (080 §6, schema version 4).
`--where arm=N` does the same for `reanalyse` and the reports.

**Built on 2026-11-01 (§5):** `wait` and the route `GET
/v1/wait/{kind}?id=&until=&seen=&hold=`; `run tail` of a queued run waits
for it to start (before, the daemon answered 410 "has finished"); a
static-gate refusal of a start answers 422 with `gate` beside `error`.

**Built on 2026-11-01 (the CLI basics, after the spec audit):**

- **`--dry-run` on every command that changes something in the daemon:**
  `run stop|abort|pause|resume|invalidate|identify|retry`, `lane
  drain|pause|quarantine|abort|resume`, `batch pause|resume|stop|abort|
  retry|limits`, `campaign pause|resume|stop|abort|retry`, `cell run`,
  `reanalyse`, and `POST /v1/drain` (`?dry_run=1`). A dry run changes
  nothing and answers with what would be done (`dry_run: true`, and the
  run in flight's action where there is one). The commands that change
  something on the host without the daemon have one too: `image build`,
  `sdk add`, `suite import`, `task build` (each artefact present and as
  pinned, present and different, or missing), `legacy import` and every
  `export`.
- **`--idempotency-key K` on every such command**, sent as the
  `Idempotency-Key` header. The daemon keeps each answer below 500 for 30
  days in its control file. The same key on the same request answers the
  same, with `Idempotent-Replay: true`, and does nothing again. The same key
  on another request is refused (422). Requests with a key are served one
  at a time. The commands that do not talk to the daemon take no key: there
  is nothing to remember it, and each is repeatable by construction (an
  image or an artefact built again is the same; an SDK build is checked
  against its pin; an import at the same commit writes the same files; an
  export refuses or replaces whole files).
- **`--client NAME` on every command that talks to the daemon**, sent as
  the `Automaticum-Client` header (010 §5); without it the CLI sends
  `<user>@<host>`, and a request with no header is `unnamed`. The daemon
  logs every change (`msg=change client=… method=… path=… status=…`; not a
  read, not a dry run), keeps who started each batch (`started_by`, shown
  by `batch show`) and each campaign (`campaign show`), and starts a
  campaign's batches in its starter's name.
- **Exit codes** every command keeps: 0 done, 1 failed, 2 usage, 3 refused
  by the daemon, 6 the daemon does not answer, 7 not found. `wait` adds 4
  (timeout) and 5 (ended without the condition), and `cell run` keeps
  0/10/20/30/70 (065 §2.1).
- **`--json` on every command**, unformatted (§1.1).
- **Lanes:** `lane pause` (drained, the run in flight held, the endpoint
  freed unless `--keep-model`), `lane quarantine --reason R` (drained, shown
  quarantined with its reason; each queued run says `lane quarantined:
  R`), `lane abort` (the run in flight; the lane otherwise as it was), and
  `lane show <lane> [--recent N]` (its configuration, state, queue, last
  runs and since when it is drained, paused or quarantined). `--all` acts on
  every lane. A drain releases a held run (D119).
- **Batches and campaigns:** `batch abort` (`batch stop --abort`), `batch ls
  [--campaign C] [--limit N]` (`GET /v1/batches`: runs by state and verdict,
  arms, tasks, lanes, first and last times), `batch start` (`run start`),
  and `campaign abort|retry`.
- **Cells:** `cell show <batch> [--runs]` and `cell ls`. The batch a `cell
  run` starts carries its suite (the `Automaticum-Cell` header), and the
  cell view's `suite` and `subset` are filled from it.
- **Configuration:** `arm ls [--active|--running]` (each arm with its
  runs running and queued, 2026-11-09), `endpoint show|models|probe`, and `task
  verify` (the artefacts' pinned hashes, and whether the environment is
  proven).
- **Reading:** `run tail` renders the transcript (turns, reasoning, text,
  tool calls with how they ended, notices, the runner's notes, the scripted
  user and the end; `--raw` for the lines, `--max N` to cut long blocks).
  `logs` takes `--grep RE`, `--turn N`, `--render` and `--max`. `run
  timeline [-requests] <run>` is the per-run timeline of 070 §1.1 (`-requests` adds each model request, D139). `status
  --watch`. `run ls --lane L`.
- **`reanalyse --where`** (`suite=, task=, arm=, arm_id=, model=, verdict=, lane=,
  batch=, since=, until=`) and `--detector D`.
- **Reports (070 §6):** `report run` (`run show`), `report cell` (`cell
  show`), `report batch <b>` and `report ab <a> <b>` (`compare`), `report
  regression <batch>...` (`GET /v1/compare?split=build`), `report census
  [<batch>...] [--where ...]` (`GET /v1/report/census`), `report struggle
  [<batch>...] [--where ...]` (`GET /v1/report/struggle`, D126) and `report
  guards [<batch>...] [--kind unconfirmed|missed] [--where ...]` (`GET
  /v1/report/guards`) and `report skew <batch>...` (`GET
  /v1/compare?split=lane`). `compare --group arm.<field>|label.<name>|finding.<detector>`
  groups by that axis (`?split=`).
- **Exports (065 §5):** `export cell <batch> --out D [--format jsonl|csv]`
  writes a batch's cell files as `cell run` does, and with csv also
  `runs.csv`, one row per run with every per-run number as a column. `export
  campaign <name> --out D` writes every started batch as `<D>/<batch>/` and
  the campaign as `campaign.json`; the directory can be copied whole to
  another host. `export run <run>... --out D [--kind K]` writes each run's
  record (`run.json`) and its stored artefacts under `<D>/<run>/`.
- Not built yet: `--format parquet`, to be written by the daemon's DuckDB
  (no new library); the omnimergekit layout
  (`<results>/<suite>/<served-model>/`); slices and the HTML report (065
  §5). `requeue` is not adopted (O32).
- **2026-11-01, D122 to D125:** `run start --priority N`; `lane reserve
  <lane> --for <batch|campaign> --until T|8h` and `lane unreserve <lane>`
  (`POST /v1/lanes/{lane}/reserve|unreserve`); `status` shows a lane's
  reservation and the reason a run waits (`busy: ...`, `lane reserved for
  ...`, `lane quarantined: the daemon: ...`).
- **2026-11-01, D120 and D121:** `cell run --cohort N --declared-variable
  V`; `cohort ls`, `cohort show <name>` (`GET /v1/cohorts`, `GET
  /v1/cohorts/{name}`); `export cohort <name> --out D [--format jsonl|csv]`;
  `rescore <batch> [--out D] [--no-wait] [--dry-run]` (`POST|GET
  /v1/batches/{batch}/rescore`), `status` shows a lane `rescoring`.

**Decided by the owner (2026-10-27, D116):** `notices (<run> ... | --batch B
...) [--key K]` prints the SDK's notices of some runs, each with its run,
turn, time, key and sentence, read from the runs' event streams through
`GET /v1/runs/{id}/blobs/events`. The key is the one a report prints for
the notice's metric (070 §2), with or without `notices.`; a shorter key
takes every phase under it (`status.loop_detection`). The metrics leave
the sentence out; this is where it is read.

**Decided by the owner (2026-10-26, D115):** `run start --heldout` starts a
batch held out, kept by batch in the daemon's control file like
`--capture-requests`. Its runs, though of training tasks, are passed over by
`export conversations` unless it is given `--heldout`
(`GET /v1/runs/{id}/export?heldout=1`; without it the route answers 403). A
held-out task stays unexportable even then. A training export never names
`--heldout`.

The API is HTTP and JSON on a loopback address. Everything that changes
something needs the token; reading from the same host does not. A run in
flight is streamed as server-sent events.

Live output in plain commands: `status --watch`, progress bars, and a `run
tail` that renders the transcript as it streams.

### 1.1 What a person reads

**Decided by the owner (2026-10-13):** the command line, the terminal
interface and the web interface write values in formats a person reads
(D69). As built, all of them write through one formatter, `internal/human`,
so the same value reads the same everywhere:

| value | written as |
|---|---|
| a length of time | hours, minutes and seconds: `1h 02m 05s`, `8m 13s`, `42s`; one decimal under ten seconds, two under one. Never bare seconds for a minute or more, never milliseconds |
| a moment | the reader's own zone with its offset: `2026-10-13 14:56:00 +02:00` |
| a count | thousands apart, with what is counted where the name says it: `987,358 tokens` |
| a size | binary units: `143 MiB`, `15.9 GiB` |
| a share | a percentage: `36.4%` |
| a rate | `51.7 tok/s` |

A metric is written by what its name says it is (a name ending in `_ms` or
`_s` is a time, one holding `share` is a share). **`--json` and the API are
never formatted:** they keep the stored numbers and units, for machines.
The terminal and web interfaces do not exist yet (milestone 6); they are
bound by this section when they do.

## 2. TUI

**Decided by the owner (2026-10-12):** a **full terminal dashboard**, in
addition to the web dashboard: lanes and queue, a run in flight with its live
transcript and numbers, batch and campaign progress, and the controls of
050 §4. It is the interface for work over SSH.

The TUI is a client of the same API as everything else and holds no logic of
its own, so it cannot disagree with the web dashboard.

**Decided by the owner (2026-10-12):** the TUI has all four groups of views:

1. lanes, queue, a run in flight, batch and campaign progress, controls;
2. gate status per lane, the reason for every refusal, and the doctor run;
3. A/B boards and reports, with the basis diff above every comparison;
4. the transcript viewer with the aligned server log, and full-text and regex
   search.

They are built in that order (a proposal; the order is not an owner decision).

## 3. HTTP API

- REST and JSON for everything the CLI does; SSE or WebSocket for live events.
- Authentication by token, with a read-only mode (010 §5).
- Every list is paged. Every object has a `brief` form.
- Errors are structured: a code, a message, and the gate or check that refused.

## 4. Web dashboard

- Lanes and queue.
- A run in flight: live transcript, tokens, tokens per second, context bar,
  findings as they appear.
- Batch, cell and campaign progress with the running pass rate and interval.
- A/B boards, with the basis diff above every comparison.
- The log viewer and the search box (§6).
- Management: start, pause, abort, requeue, edit a campaign's remaining steps.
- A read-only mode to share a result.

## 5. Assistant-optimized control

The primary user is often an AI session. Requirements:

- An **MCP server** exposing the same operations as tools, with small,
  predictable outputs: every list is paged and summarised, and nothing returns
  a megabyte of transcript unless asked for a range.
- **"Wait for" primitives** that block server-side until a run, batch or cell
  ends or a condition holds, so an assistant does not poll:
  `wait run <id>`, `wait batch <id> --until "pass>=5 or fail>=3"`. Built
  2026-11-01 (§5.1).
- A **`brief` report format** written for a reader with a context budget: the
  verdict, the three numbers that matter, the findings, and ids to drill into.
- **Notifications** through the mailbox the sessions already use, and
  webhooks: on campaign end, on a refusing gate, on a stopped batch. Built
  2026-11-01 (§5.1, 050 §2).
- Dry run and idempotency keys, as for the CLI.

### 5.1 `wait` as built (2026-11-01)

`internal/wait` decides; the daemon holds `GET /v1/wait/{kind}` until the
condition holds, what is followed ends without it, or `hold` (at most 55 s)
runs out, and reads the state again at every change of the queue or of a
run in flight, and every 2 s. The command asks again until it has an
answer, so a wait has no length of its own unless `--timeout` gives one. A
daemon that does not answer, a restart for one, is waited for: the wait
goes on once it is back. A daemon older than the route (a plain 404) is
read through `GET /v1/runs`, `/v1/campaigns/{name}` and `/v1/status` every
`--poll` (5 s) and decided in the command, with the same answers.

| kind | follows | ends by default when | counts | facts |
|---|---|---|---|---|
| `run <id>...` | runs, queued or not | `done`: finished | `pass` `fail` `timeout` `aborted` `invalid` `skipped` `finished` `queued` `running` `total` | `started`, `done`, `finished` |
| `batch <id>` | every run of a batch | `done`: none queued, none in flight | as `run` | `started`, `done` |
| `cell <batch>` | a cell (065), as a batch; then prints its counts and headline | `done` | as `run` | as `batch` |
| `campaign <name>` | a campaign (§1 of 050) | `done`: finished or stopped | `batches` `batches_finished` `batches_stopped` `batches_running` `batches_waiting` | `running` `paused` `stopped` `finished` `done` |
| `lane <lane>` | one lane | `idle`: no run in flight, none queued for it | `running` `queued` | `idle`, `free` (no run in flight), `drained` (also when quarantined), `quarantined`, `paused` |
| `idle` | every lane | `idle`: nothing in flight or queued | `running` `queued` | `idle`, `free` |

A condition joins counts compared with a number (`>=`, `>`, `<=`, `<`,
`==`, `!=`) and facts with `and` and `or` (`&&`, `||`), in parentheses where
needed; `and` binds tighter. A name the kind does not have is refused when
the command starts. Every finished record counts once, a retried run's first
attempt too, so the counts only grow.

- `wait batch <b> --next [--seen N]` ends when the batch has more than N
  finished runs (N defaults to those finished when it starts), prints each
  run finished since, and the `--seen` for the next wait: nothing is missed
  between two waits.
- `wait batch <b> --each [--seen N]` prints one line (with `--json`, one
  JSON object) per run as it finishes, and the batch's counts when it has
  finished: one command for the whole batch, never armed again.
- `wait run <a> <b> ...` waits for every run named; `--any` for the first.
- `run tail <run>` of a queued run waits for it to start, then streams it.

Exit codes: **0** the condition holds; **4** `--timeout` ran out (the last
state read is printed); **5** what is followed ended without the
condition, so it never will (a batch done before `pass>=5`); **1** an error
(an unknown id, a condition the kind cannot have). `--json` prints the
answer: the snapshot (`kind`, `id`, `state`, `counts`, `facts`), `until`,
`met`, `over`, and `new`, the runs finished past `seen`.

**Notifications as built (050 §2):** the log, `[notify] webhook` and
`[notify] command` get a gate's refusal or wait, a canary stop, a
campaign's end, a batch's end or stop, and, when `[notify] events` names
`run`, each run's end. The sessions' mailbox is reached through
`[notify] command`, which the installation sets to its own mailbox client.

## 6. Logs: view, search, replay

- A **transcript viewer** showing what the model saw and what it produced, turn
  by turn: system prompt, messages, reasoning, text, tool calls with arguments,
  results, guard notes, scripted-user actions, compaction boundaries, and the
  raw request for any turn. Collapsible, linkable to a turn, diffable between
  two runs.
- Server log and runtime log **aligned on the same timeline** as the
  transcript.
- **Full-text and regex search** over transcripts, reasoning, tool arguments,
  tool results and logs, across the corpus, with filters on any run field.
- **Semantic search** through the embedder: "runs where the model decided the
  oracle was wrong", "reasoning that mentions giving up". It covers what has
  been embedded, which the operator decides (080 §5.2), and every result says
  how much of its scope that is.
- Search covers archived runs exactly as live ones (080 §5.1).
- Saved queries, and queries as report columns.
- **Replay:** re-send a recorded request to an endpoint, byte for byte, to
  reproduce a behaviour offline. One template leak needed exactly this, and 23
  cold runs reproduced nothing.
- Export of any slice and a self-contained HTML report (065 §5).

## 7. Models inside the interface

- **Embedders** (Ollama's embed API, or any OpenAI-compatible) for semantic
  search over transcripts, reasoning, findings and reports. The embedder must
  be on an endpoint that is not the one under test, or be scheduled around it.
- A **chat box** where a model answers questions about the corpus using the
  same tools the MCP server exposes ("why did arm B fail more?"), with
  citations to runs and turns. Its answers are labelled as a model's.
- Optional model-written run summaries and model-assisted finding triage,
  **always stored separately from measured facts**.
- Private task text is never sent to a cloud endpoint (060 §7).
