# 040 — The runner

**Status:** draft 0.5, 2026-10-12. Facts about the SDK in this file were read
from the Cerebriline source at release 4.100.247 and were not verified by
running it. Items under "Requests to Cerebriline" are dependencies on another
project.

## 1. What it is

The runner is a small Node program inside a container. It drives the coding
agent through `@cerebriline/sdk` **as a library consumer**, not by wrapping the
CLI's arguments, so that every setting the extension has is reachable and
typed. It streams the SDK's events back to the daemon and captures what crosses
the wire.

It runs under **Node** (22 or later), never Bun: Bun's `fetch` returned empty
worker bodies in live harnesses. The SDK needs no native addon; its sandbox is
a separate binary that the lane image ships.

## 2. Why not the CLI

The old harness ran `apps/cli` from source, importing the SDK from a gitignored
build directory, so it tested "whatever was last built there". The CLI also:

- writes its own wrapped event format, adds its own events and drops teammate
  events except usage;
- offers one approval policy for all tools, and withholds `ask_question` when
  there is no terminal;
- has no resume, no steering and no checkpoint restore in its non-interactive
  path.

## 3. Build pinning

The harness build is part of the arm and of the basis.

- Install any published `@cerebriline/sdk` version, or a tarball built from a
  named commit. Building the SDK needs Bun, so commit builds happen in a
  **build container**, never on a host; the resulting tarball is stored by
  hash.
- Record version, commit, dirty flag and the sha256 of the installed package.
- **Refuse to run** when the installed package does not match what the arm
  names. This is a gate (050 §3).

## 4. Proving what was sent

Three layers, each recorded:

1. **Typed configuration in.** The runner builds the SDK session configuration
   from the resolved arm. Nothing is passed through environment variables or
   settings files.
2. **Read-back.** Before the first request, each setting is read back from the
   built session. A setting that did not survive is a gate failure, not a quiet
   default. *This is not possible with the SDK today:* the resolved session
   configuration is private. See §8, request 1. Until it exists, the runner
   relies on layer 3 and the record says read-back was unavailable.
3. **The wire.** The runner supplies the SDK's `fetch` option and records every
   request and response body untruncated, with timing. The first request of a
   run is compared to the arm (sampler, thinking, context, output budget, stop
   sequences); every later request is compared too, and a difference is a
   parameter-drift finding (070 §4).

The SDK's environment-driven request capture is not used: it truncates bodies
at 64 KB unless raised, and it is configured by environment.

## 5. A clean session, every time

The SDK merges a stored provider profile under the session configuration and
reads a global settings file. A leftover file therefore changes the arm
silently.

- Every run gets a **new, empty data directory** inside the container.
- The runner writes nothing to it before the SDK starts, and verifies after
  start that no provider profile or global settings file was read from
  anywhere else. Failure is `INVALID`.
- The data directory is collected as an artefact after the run.
- The lead agent's context window has no direct configuration field today (the
  CLI injects it through the known-models table). The runner does the same and
  verifies it on the wire. See §8, request 2.

## 6. Extension parity

**Decided by the owner (2026-10-12):** the runner reaches full parity with what
a user gets in the VS Code extension, so unattended runs exercise the same
behaviour.

Inventory of extension behaviour absent from the CLI's non-interactive path,
and how the runner provides it:

| behaviour | SDK hook | runner |
|---|---|---|
| Approval by category (read, edit, safe commands, all commands, web fetch, browser, MCP) | `requestToolApproval`, `toolPolicies` | approval policy in the arm; decisions by the scripted user |
| Denial with feedback text | `ToolApprovalResult.reason` | the scripted user's denial carries feedback in the extension's format |
| `ask_question` with suggested options | `toolExecutors.askQuestion` | answered by the scripted user |
| Plan mode, act mode and the switch | `mode`; a switch restarts the session with its messages | scripted, or fixed per arm |
| Steering a running turn; queued messages | `send` with delivery `steer` or `queue`; pending prompts | scripted |
| Cancel and resume; resume after restart | `abort`, `stop`; `start` with session id and initial messages | used for pause and resume (050 §4) and scripted |
| Checkpoint restore (task, workspace, both) and compare | `restore`, `compareCheckpoint` | scripted; also used for snapshots |
| Mistake-limit stop | `onConsecutiveMistakeLimitReached` | arm field; the extension's behaviour is to stop |
| Manual compaction | compaction state update | scripted |
| Escalation and check approvals | `escalation.approve`, `atomicProtocol.approveCheck` | scripted user |
| MCP tools | `extraTools` | arm field: which servers, started inside the container |
| Browser tool | available in both | arm field |
| Task progress (focus chain) | `taskProgress` | arm field |

The **request-count pause** (asking the user to continue after about twenty
auto-approved requests) no longer exists in the extension: the setting is a
dead field. It is not emulated. If it returns, it is one more scripted-user
event.

### 6.1 Behaviour that depends on VS Code

**Decided by the owner (2026-10-12):** emulate where a hook exists, as an arm
field, and record that it was emulated.

| extension behaviour | emulation |
|---|---|
| Diagnostics after edits | language servers from the lane's language layers, injected through the session hooks |
| VS Code terminal, "proceed while running" | the SDK's background executor, which the extension itself falls back to when it runs standalone; auto-detach after the same interval |
| `@problems` mention | the same diagnostics |
| `@terminal`, `@git-changes`, commit, URL and folder mentions | expanded by the runner before the prompt is sent (see §8, request 4) |

The system prompt names the host IDE. The arm states which value is sent; the
default is the value the extension sends, since that is what users run.

Not emulated, and listed as known differences: the webview's own behaviour, and
the error-retry prompt for authentication and balance errors (no hook found;
§8, request 5).

### 6.2 The scripted user

A task may ship a script that plays the user. It is declarative and part of the
task version.

- **Rules** match an event (a tool approval request by category or by pattern,
  an `ask_question` by text pattern, a plan presented, a completion claim, a
  turn count, elapsed time) and give a response (approve, deny with feedback,
  answer, pick option N, send a steering message, queue a message, switch mode,
  restore a checkpoint, cancel and resume).
- A default rule is mandatory: what happens to an event no rule matches. An
  unanswered question never hangs a run.
- Every scripted-user action is an event in the run record, so the transcript
  shows what the "user" did and when.
- With no script, the policy is auto-approve for every category and
  `ask_question` is answered by the task's declared default answer. That is the
  unattended behaviour of the old harness, made explicit.

## 7. What the runner returns

- The SDK event stream unchanged, including teammate and sub-agent events.
- The wire capture (§4), the session data directory, the runtime log.
- Its own stdout, stderr, exit code and signal.
- A final record: how the agent ended, whether it claimed success, usage as the
  SDK reports it.

The runner never runs the oracle and never sees it (060 §3).

## 8. Requests to Cerebriline

Needed from the SDK; each is tracked in `decisions.md` as an external
dependency.

1. A public way to read the **resolved session configuration** after it is
   built and before the first request.
2. A configuration field for the **lead agent's context window**, and one for
   the **data directory**, so neither needs an environment variable or a
   side table.
3. A way to **disable the merge** of stored provider profiles and global
   settings, or to be told which files were read.
4. The extension's **mention expansion** as an SDK function, so the runner and
   the extension cannot drift.
5. A hook for the **error-retry** decision.
6. Confirmation of the parity inventory in §6.
