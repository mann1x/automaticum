# 010 — Architecture

**Status:** draft 0.8, 2026-10-13.

## 1. Fixed points

**Decided by the owner (2026-10-12):**

- The application and daemon are written in **Go**.
- **A host runs only the Go application.** No Node, no Python, no helper script
  on a host. Whatever else is needed runs in a container.
- **Node runs only inside containers.** The runner lives in the lane image; for
  external harnesses that launch the agent in their own task container, the
  engine adapter installs the pinned runner into that container (064 §3).
- Lanes run in **Docker**, from a slim base image with one layer per language.
- **Gitea** provides git for tasks that need a remote.
- The store is **DuckDB + Lance + blobs on disk** (080). The spike has run and
  supports it (080 §7.4); the owner's confirmation is open.
- Remote hosts use **agent mode only**. There is no agentless path over SSH.
- One operator plus assistants. No accounts, roles or per-user ownership.

## 2. Components

```
            ┌────────────────────────── automaticumd (Go) ──────────────────────────┐
  CLI  ───► │ HTTP/JSON API + SSE/WebSocket │ scheduler │ lane manager              │
  TUI  ───► │ gate engine │ oracle runner │ ingest + detectors │ query + statistics │
  web  ───► │ store: DuckDB + Lance + blobs │ embedder client │ Gitea client         │
  MCP  ───► │ engine adapters │ suite plugins │ mailbox + webhooks                   │
            └──────┬──────────────────────┬──────────────────────────┬──────────────┘
                   │ agent protocol       │ docker (local)           │ http
        ┌──────────▼─────────┐   ┌────────▼───────────┐     ┌────────▼────────┐
        │ automaticum agent  │   │ lane container     │     │ endpoints       │
        │ (remote host, Go)  │   │  runner (Node)     │────►│ model servers   │
        │  local Docker      │   │  @cerebriline/sdk  │     └─────────────────┘
        │  host facts, logs  │   │  workspace (task)  │
        └────────────────────┘   └────────────────────┘
```

- **automaticumd** owns all state and all decisions. One binary, built as
  described in 080 §7.1. It runs on Linux; Windows and macOS hosts run agent
  mode and the CLI.
- **automaticum** is the CLI and TUI: a thin client of the daemon's API. Every
  dashboard action exists as a CLI command and an API call.
- **runner**: the only non-Go part (040).
- **Suite plugins, gates, detectors and engine adapters** are plugins with a
  name and a version.
  **Decided by the owner (2026-10-12):** plugins are **compiled into the
  binary** only. A plugin is a Go package in this repository; adding one means
  a new build. Nothing is loaded at run time.
- Work that the old harness did with host-side Python and bash (analysis, log
  collection, archiving, tag checks) is Go code in the daemon or in agent mode.
  Scorers that exist only in Python (BFCL's evaluator, the SWE-bench harness)
  run in their own containers.

## 3. Remote hosts: agent mode

A remote host runs the same binary as `automaticum agent`. It:

- manages the local Docker engine on the daemon's instruction;
- reports host facts (GPU, driver, power cap, free VRAM, disk, kernel limits);
- fetches the local model server's log for the window of a run;
- relays the runner's event stream to the daemon, buffering to disk if the link
  drops, so a network gap never loses events.

Requirements:

- The daemon and its agents authenticate each other. An agent accepts
  instructions from one daemon only.
- An agent started with a different binary version than the daemon is refused,
  with the two versions in the message.
- An agent never decides: placement, gates and verdicts are the daemon's.
- Paths are never assumed to be POSIX. Windows is a first-class target for the
  binary even before native lanes exist.

## 4. Native agents (later phase)

Docker lanes need Linux. Two kinds of host cannot offer one, and both matter:
some agent bugs are Windows-only (rename with open handles, PowerShell commands,
path separators).

**Decided by the owner (2026-10-12):** Windows and macOS get a **native agent**
that runs the runner without a Linux container. It is specified after the Linux
path is proven (110 §3, milestone 7).

**Open** (the owner defers this to the planning of milestone 7): how the native agent meets the rule "a host runs only the Go
application", since the runner needs Node. Candidates: Windows containers on
Windows; a Node runtime the agent unpacks into its own private directory and
removes afterwards; a lightweight VM on macOS.

## 5. Access model

- One **write token** for the operator and for assistants, and a **read-only
  mode** to share a result. Tokens are stored hashed.
- The API listens on loopback by default. Any other bind address is an explicit
  setting, and the daemon refuses to start on a non-loopback address without a
  token configured.
- Every mutating call records who made it (a client name the caller supplies),
  so a campaign started by an assistant session is attributable.
- Secrets (endpoint credentials, dataset tokens, Gitea tokens) are held by
  reference to a secret store. Nothing secret appears in a run record, an
  export, a command line, a log or a mailbox message.

## 6. Operating rules

These apply to every component and come from incidents.

- **GPUs are shared and partly not ours.** A lane binds to named devices.
  automaticum never uses a device it was not given, even when idle, never
  preempts someone else's work, and waits on an external busy signal.
- **Never kill by process name.** Stop only what automaticum started, by the
  ids it recorded. A stale orphan with a matching name has both blocked and
  fooled liveness checks.
- **Readiness is a predicate, not a sleep.** An endpoint is ready when a
  one-token generation returns and the model identity matches.
- **Never probe a busy endpoint in a way that disturbs it.**
- **Never use, and never start, a model server bound to all interfaces.** These
  servers have no authentication. Lanes reach the server over the Docker bridge
  or a private interface.
- **Large data goes to a configured data root with a free-space floor.** Never
  to a RAM-backed temporary directory.
- **Never skip silently.** A task not run has `SKIPPED` with a reason and its
  cell is marked incomplete.
- **Timestamps are UTC with offset.** Hosts sit in more than one time zone.
- **Configuration is immutable once a run has started** (050 §4).
