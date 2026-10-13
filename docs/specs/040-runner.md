# 040 — The runner

**Status:** draft 0.8, 2026-10-13. Facts about the SDK in this file were read
from the Cerebriline source at release 4.100.247 and were not verified by
running it. The parity table in §6 was checked row by row by the Cerebriline
session on 2026-10-13 against its main branch (release 4.100.249 plus requests
1 to 3); §6.1 and §6.2 were not checked. Items under "Requests to Cerebriline" are dependencies on another
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
   built session and compared with the arm. A setting that did not survive is a
   gate failure, not a quiet default. This uses the SDK's resolved-configuration
   callback (§8.2), which is on the SDK's main branch but not yet in a release.
   With an SDK build that lacks it, the runner relies on layer 3 and the record
   says read-back was unavailable.
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
| Approval by category (read, edit, safe commands, all commands, web fetch, browser, MCP) | `requestToolApproval` on the runtime capabilities (not the session configuration); `toolPolicies` on the start input. A request carries tool name, input and policy; the extension approves by itself when the policy says auto-approve | approval policy in the arm; decisions by the scripted user |
| Denial with feedback text | `ToolApprovalResult` with `approved` and `reason` | the scripted user's denial uses the extension's exact sentences (below) |
| `ask_question` with suggested options | `toolExecutors.askQuestion` on the capabilities | answered by the scripted user |
| Plan mode, act mode and the switch | `mode`. A switch rebuilds the session with the **same session id** and the loaded messages as initial messages, cancelling a running turn first | scripted, or fixed per arm |
| `switch_to_act_mode` tool | added through `extraTools` in plan mode, removed in act mode | the runner does the same, or the plan-mode tool list differs from the extension's |
| Steering a running turn; queued messages | `send` with delivery `steer` or `queue`; `pendingPrompts` to list, update and delete. The extension's default for a message typed during a run is `queue`; `steer` is the explicit send-now | scripted; the default delivery is `queue` |
| Cancel and resume; resume after restart | `abort`, `stop`; `start` with session id, initial messages **and the initial compaction state**. Without the last, a resume drops the saved compaction | used for pause and resume (050 §4) and scripted |
| Checkpoint restore (task, workspace, both) and compare | `restore`, `compareCheckpoint` | scripted; also used for snapshots |
| Mistake-limit stop | `onConsecutiveMistakeLimitReached`; the extension answers stop, with a reason. The call says whether the stop was `forced` by a terminal guard | arm field; the run record says whether a stop was forced or a count of mistakes |
| Manual compaction | none that compacts. The extension reads the transcript, runs the compaction itself, then stores the result with `updateSessionCompactionState` | the runner runs the compaction, as the extension and the CLI each do; scripted |
| Slash commands (`/compact`, `/smol`, the built-ins) | none: the extension expands them before the prompt is sent | expanded by the runner, like mentions (§8, request 4) |
| Escalation and check approvals | `escalation.approve` and `atomicProtocol.approveCheck`, on the session configuration. Leaving `approveCheck` out is a behaviour of its own (a host with no user), not an approval | scripted user; the arm states whether the hook is supplied |
| MCP tools | `extraTools` | arm field: which servers, started inside the container |
| Browser tool | present only where a Chrome is found | arm field; the lane image provides the browser or the tool is absent |
| `render_image`, `transform_image`, `generate_icon` | extension only, through the same browser driver; the CLI registers none | the runner supplies the driver, or they are absent and the wire tool list shows it |
| `check_file` tool | registered by the extension | registered by the runner |
| Replaced executors | the extension supplies its own `editor`, `applyPatch`, `readFile` and `semanticSearch` executors, sharing one registry of read receipts | **the runner replaces none** and records that the SDK's own executors ran. If it ever replaces the reader it must share the registry, or edits are refused as "not read in this session" |
| Task progress (focus chain) | `taskProgress` | arm field |
| Sandbox | in the extension, delegated agents are sandboxed by default and the lead is not | arm field, same default |
| Bundled skills | three media skills are on by default | arm field (§8.2) |

**Denial sentences**, as the extension sends them: by default "This tool call
was rejected by the user and not executed."; for edit tools "The user denied
this edit. The file was NOT modified and still contains its original
content."; with feedback, that sentence followed by " The user provided the
following feedback:" and the feedback inside `<feedback>` tags. A pending
approval is also denied when the user types instead of answering, with "Tool
execution was cancelled because the user sent a follow-up message." The SDK
appends guidance of its own to every rejection, so the text on the wire is
longer than what the runner returns.

**Values the extension sets and the old harness carried by hand:** at most 6
changes, task-progress interval 2, tool results capped at 64,000 characters,
compaction `agentic`, edit verification `nudge`, 6 retries. Each is an arm
field. Those that are guards or compaction settings have no default (030
§5.1): the arm states them, and these are the values that reproduce the
extension. The Cerebriline session proposed the extension's values as
defaults for all of them; that would contradict 030 §5.1 for guards and
compaction, so it is not adopted here.

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
the error-retry prompt for authentication and balance errors (§8.1).

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

**Decided by the owner (2026-10-12):** the SDK fixes go ahead on the Cerebriline
side. Costs and order below are that session's estimates from reading its
code, not from trying. Each request is tracked in `decisions.md` section 4.

| id | request | status |
|---|---|---|
| X1 | A public way to read the **resolved session configuration** before the first request, including where each value came from. | **on Cerebriline main, unreleased** (§8.2) |
| X2 | Configuration fields for the **data directory** and for the **lead agent's context window**. | **on Cerebriline main, unreleased** (§8.2) |
| X3 | A **strict mode** that refuses to read stored provider profiles and global settings. | **partly: provider profiles only** (§8.2) |
| X4 | The extension's **mention expansion** as an SDK function. | expensive: it lives in the extension and leans on the editor. Worked around at first (§8.1) |
| X5 | A hook for the **error-retry** decision. | **withdrawn** (owner, 2026-10-12): the runner observes only (§8.1) |
| X6 | Confirmation of the parity inventory in §6, row by row. | done on 2026-10-13 for §6: 11 rows confirmed, 2 corrected, 6 added. §6.1 and §6.2 not checked |

`INVALID` depends on X1 to X3, which is why they come first. More requests are
expected while the runner is written, and are sent in batches.

### 8.2 What landed (reported by the Cerebriline session, 2026-10-12)

On the SDK's main branch, to ship with the release after 4.100.249. Until that
release the runner pins a tarball built from the commit (§3). The session
reports unit tests for each item; a live request showing the context window on
the wire from the new field was **not** tested there, so the runner's wire
check (§4) is the first real proof.

- **Resolved configuration (X1).** A callback on the session configuration,
  called once the configuration is final and before any request, and awaited.
  **Throwing from it stops the session from starting**, which is how a gate
  refuses before anything is spent. The same object is returned by start. It
  holds provider, model, base URL, context window, output budget, thinking,
  sampling, the provider and session configuration as plain data with secrets
  masked, the system prompt as the model is given it, and a `sources` block.
- **`sources`** says whether a stored provider profile was merged, ignored or
  absent, **which fields were inherited from it**, the data directory in use,
  and whether a global settings file exists. An isolated run must show no
  inherited field; anything else is `INVALID` (§5).
- **Context window (X2)** is a session configuration field and wins over a
  stored profile.
- **Data directory (X2)** is an option when the core is created. It is
  **process-wide**, so one run per runner process stays the rule. Narrower
  environment variables still override parts of it, so the lane image must not
  set them, and the harness-build gate checks they are unset.
- **Stored provider settings (X3)** can be set to `ignore`, in which case the
  profile is not read at all.
- **Not covered:** the global settings file has no switch. With a fresh data
  directory it does not exist, and the report lets the runner assert that.
- **Only the local runtime** calls the callback. The runner uses it; the hub
  and remote runtimes are out of scope.
- **Bundled skills are on by default** in the SDK (three media skills), so
  every session has the `skills` tool unless global settings turn them off.
  Which bundled skills are on is therefore an arm field (030 §5) that the
  runner writes into the fresh data directory's global settings, and the wire
  check verifies the tool list.

### 8.1 Consequences until the requests land

- **One session per runner process.** The data directory is a process-global
  setting in the SDK today, so two sessions in one process share it. The runner
  starts one process per run, and a sub-agent's session is assumed to share the
  lead's directory.
- **Mentions:** the scripted user sends already-expanded text, and the run
  record says that mention expansion was not exercised.
- **Error retry:** the runner observes every retry the SDK makes (from the
  event stream and the wire) and records it as infrastructure, never as the
  model's failure (050 §4). The arm's retry limits are passed as configuration.
  **Decided by the owner (2026-10-12):** the runner **observes only**. The
  SDK's own retry ladder runs; no hook is requested. The extension's retry
  prompt stays a known difference (§6.1).
