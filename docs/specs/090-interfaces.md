# 090 — Interfaces

**Status:** draft 0.14, 2026-10-13.

One API, four ways in. Everything the dashboard can do, the CLI and the API can
do; everything the CLI can do, the MCP server can do.

## 1. CLI

`automaticum <noun> <verb>`, with `--json` on every command and stable exit
codes.

```
automaticum status                          # lanes, queue, what is running
automaticum sdk add|ls|verify <build>        # SDK builds: install, list, reproduce with no network (040 §3.1)
automaticum doctor [--lane L] [--arm A] [--task T] [--all]   # every gate, on every lane, now
automaticum lane ls|show|pause|resume|drain|quarantine
automaticum endpoint ls|probe|show|models
automaticum arm ls|show|diff|validate
automaticum basis show|diff
automaticum task ls|show|verify
automaticum oracle verify <task>
automaticum run start --arm A --task T [-n 5] [--lane L]
automaticum run ls|show|tail|stop|abort|pause|resume|retry|invalidate|identify
automaticum compare <batch> [<batch> ...]
automaticum batch|campaign start|ls|show|pause|resume|abort
automaticum cell run|show|ls
automaticum cohort ls|show
automaticum report run|batch|cell|ab|regression|census [...]
automaticum find "<query>" [--semantic] [--where ...]
automaticum logs <run> [--server|--runtime|--events|--wire] [--grep ...] [--turn N]
automaticum replay <run> --turn N [--endpoint E]
automaticum detect <file|run> [--runaway N] # the detector library, standalone; --self-test runs its controls
automaticum embed start|status|stop [--where ...]   # by hand only (080 §5.2)
automaticum reanalyse [--run R | --batch B] [--imported] [--detector D] [--where ...]
automaticum rescore <cell>
automaticum export run|cell|cohort|campaign ...
automaticum wait run|batch|cell <id> [--until "<condition>"]
automaticum agent                           # agent mode on a remote host
```

Every mutating command has `--dry-run` and accepts `--idempotency-key`.

**Built in milestone 2:** `status`; `arm validate|show|diff`; `task
ls|show|build`; `oracle verify`; `lane ls`; `endpoint ls` (prints no address
unless asked); `run start|ls|show|tail|stop|abort|invalidate|identify` (`run identify <run>` adds where and on what a run ran to a record that lacks it, from today's configuration and catalogue, marked as added later, D74); `logs`
(`--events`, `--wire`, `--runner`, `--oracle`, `--kind`); `legacy import`;
`store check`. `task build` is new in this list: it builds a task's artefacts
and checks their pinned hashes. Of these, `--dry-run` and
`--idempotency-key` exist on `run start` so far.

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
  `wait run <id>`, `wait batch <id> --until "pass>=5 or fail>=3"`.
- A **`brief` report format** written for a reader with a context budget: the
  verdict, the three numbers that matter, the findings, and ids to drill into.
- **Notifications** through the mailbox the sessions already use, and
  webhooks: on campaign end, on a refusing gate, on a stopped batch.
- Dry run and idempotency keys, as for the CLI.

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
