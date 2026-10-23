# The runner: protocol 7

The runner is a Node program that runs **one** agent session for one run,
inside the lane's container, and exits. The harness (`internal/run`) writes
its job and reads what it prints. Spec: `docs/specs/040-runner.md`.

**State on 2026-10-13:** run against SDK release 4.100.250 and the stub model
server: a scripted session that reads, edits and checks a file ends
`completed` and the harness's oracle passes. `wire.mjs` and `session.mjs`
also have tests that need no SDK (`node --test`). zod is pinned in
`package.json` to the version the SDK was built with; see the note there.

**Protocol 2 (2026-10-14):** the job may carry the task's scripted user
(`user`), and the runner prints a `user` line for each of its answers and
actions (040 §6.2). A job of protocol 1 is refused by a protocol 2 runner
and the other way round, so an old image never plays a script by
ignoring it.

**Protocol 3 (2026-10-14):** stage 2 of the scripted user. Approval goes by
the extension's categories (the job's `user.categories`, filled by the
harness), with the extension's sentences for a denial; a script may switch
the mode, answer a plan, and restore a checkpoint's messages, which forks
the session (D86). The job may ask for the workspace to be a git
repository (`workspace_git`). A stage-1 runner would play such a script
wrongly, so the number went up.

**Protocol 4 (2026-10-23):** guard trips. The runner notes every trip of a
guard the SDK announces (`guard_trip`), and an arm with
`guards.stop_at_first_trip` ends the run at the first one (D99) with the
outcome `guard_stop`. A protocol 3 harness would not know that outcome.

**Protocol 5 (2026-10-23):** the arm says which trips stop the run:
`guards.stop_on` lists guards or guard.phase pairs (D100), in place of the
true/false `stop_at_first_trip`. A protocol 4 runner would not read the list
and would stop nothing.

**Protocol 6 (2026-10-23):** the arm's `thinking.reasoning_history`
(`auto`, `all`, `last` or `none`) and `thinking.reasoning_inline` go to the
SDK in the provider configuration (`reasoningHistory`, `reasoningInline`):
how much of the model's earlier reasoning the history sent back carries
(D103). The resolved arm leaves them out when they are `auto` and `true`,
which is what the SDK does unasked. A protocol 5 runner would ignore a
stated choice, so the number went up.

**Protocol 7 (2026-10-23):** the job may ask for the request capture
(`save_model_requests`, D104): the runner passes the SDK's
`saveModelRequests` with responses, writing one file per model request and
one per response under `requests/` in its output directory, as the plugin's
"Save model requests" does, and notes `capture`. The harness keeps the
directory as the `model_requests` blob. Off unless the batch was started
with it; an SDK before 4.100.261 writes nothing.

## In

`node runner.mjs /job/job.json`, with three mounts: `/job` (read-only),
`/workspace` and `/out`.

| field | meaning |
|---|---|
| `protocol` | 3 |
| `run_id`, `session_id` | the run, and the session id made beforehand |
| `arm` | the resolved arm (`automaticum arm show <arm> --json`) |
| `endpoint` | `name`, `kind`, `base_url` as the container reaches it, `api_key` if any |
| `prompt`, `default_answer` | the task's prompt; what the default user answers to a question |
| `user` | the task's scripted user (`[user]` of `task.toml`, 040 §6.2), with each rule's `times` stated and `categories`, the extension's approval categories and their tools; absent, every tool runs without asking and every question gets `default_answer` |
| `workspace_git` | make the workspace a git repository before the session starts, its seed one commit: the SDK's checkpoints are commits. True unless the task says `[workspace] git = false` (D87) |
| `workspace`, `out` | paths in the container |
| `wall_s` | the wall limit the runner enforces by stopping the agent |
| `sdk_release` | the version the installed SDK package must be: that of the build the arm names, mounted at the runner's `node_modules` (the image holds no SDK) |

## The hold

While a file named `hold` stands next to the job file, the runner waits
before its next model request. It looks before every request, through the
SDK's `beforeModel` hook: the request being answered has finished and its
tools have run, and nothing is cut. It writes a `runner` line with note
`held` when the wait begins, `holding` once a minute, and `released` with
`held_ms` when the file is gone. The wall limit does not run while the run
is held. Inside the process nothing is reset: every counter and guard of
the SDK is the same object before and after. The runner unloads nothing:
freeing the endpoint is the daemon's step.

## Out: standard output

One JSON object per line, each with a time `t` and a kind `k`.

| `k` | content |
|---|---|
| `event` | `event`: one SDK session event, unchanged, errors described by hand |
| `runner` | `note` and its fields: `prepared`, `config_resolved` (the SDK's own report), `wall_limit`, `interactive`, `restored`, `user_action_failed`, `workspace_git` (`head`), `mode_switched` (`from`, `to`, `source` user or agent, `messages`, `compaction_carried`), `fork` (`from`, `to`, `checkpoint`, `checkpoint_ref`, `workspace`, `messages`, `restarted`), ... |
| `user` | the scripted user: `on` (approval, question, turn, elapsed, completion, plan, resume), `rule` (from 1; 0 for the default), `do`, and what it answered or sent |
| `end` | the last line, see below |

## Out: the output directory

- `wire.jsonl`: one line per HTTP request the SDK made, with the request and
  response bodies whole, their sizes, the response's sha256 and timing.
  Credentials in headers are replaced by `[set]`.
- `data/`: the SDK's data directory for this run.

## The last line

| field | meaning |
|---|---|
| `outcome` | `completed`, `refused`, `invalid`, `wall_limit` or `error` |
| `reason`, `problems` | why, and for `invalid` every setting that did not survive |
| `sdk` | `release` and `package_sha256` as found installed |
| `read_back` | `compared`, or why it was not |
| `agent_end` | how the agent ended, as the SDK says it |
| `claimed_success` | whether the agent ended its task by itself |
| `usage`, `iterations`, `requests`, `questions_answered` | counts |
| `agent_requests_answered`, `agent_error` | how many of the agent's requests (chat requests that offer tools) the server answered with a 2xx, and the agent's last error when it ended in error: none answered and an error end is `ABORTED` (D89) |
| `user` | with a script: `approvals`, `denials`, `questions`, `actions` |
| `guard_stop` | with the outcome `guard_stop`: the trip that ended the run (`guard`, `phase`, `iteration`, `message`) |

| outcome | the harness's verdict | exit code |
|---|---|---|
| `refused`: never started (other SDK release, arm asks for what this protocol cannot do) | `SKIPPED` | 3 |
| `invalid`: the session is not the arm | `INVALID` | 4 |
| `wall_limit` | `TIMEOUT` unless the oracle passes | 5 |
| `guard_stop`: a guard tripped that the arm's `guards.stop_on` names | `FAIL`; the oracle still runs and what it said is in the reason (D99, D100) | 6 |
| `error` | `ABORTED` | 1 |
| `completed` | the oracle decides, unless the agent ended in error with no request answered: `ABORTED` | 0 |
| no last line at all | `ABORTED` | any |

## Mode switches and forks

A switch is done as the extension does it: the runner reads the session's
live messages and compaction state, stops the session, and starts it again
with the same id in the other mode with those messages, then sends the
script's text (for a switch to act without one, the extension's sentence
"The user approved switching to act mode. Continue with the approved plan
now."). A fork restores the checkpoint's messages (and, with `both`, the
files) into a new session `<session_id>-fork<n>`, stops the old one and
follows the new one. One record of the files read
(`capabilities.readReceipts`) serves every session of the run, as the
extension's does, so the read-before-edit guard keeps it across a switch
and a fork. The `fork` note lists what the SDK started again from
nothing (`restarted`). Turns are counted by the runner over the whole run.

## switch_to_act_mode

In plan mode the session has the extension's `switch_to_act_mode` as an
extra tool (`plan.mjs`, ported word for word). When the agent calls it the
run ends, and before the scripted user is asked the runner switches the
session to act mode and sends the extension's continuation sentence, as
the extension does when the session goes idle. A session whose arm starts
in plan mode is interactive for this.

## Guard trips

A guard's action is a `notice` event of the SDK whose `metadata.kind` names
the guard and `metadata.phase` what it did; for a `nudge`, the phase is
`metadata.nudge`, which nudge it was (`guards.mjs` has the table). From
4.100.259 every guard announces its actions: `loop_detection` (`soft`,
`hard`, `stopped`), `reasoning_loop` (`cut`, `aborted`, `removed`, with a
`cause`), `tool_intent_overrun` (`cut`), `nudge` (`no_tool_call`,
`unparsed_tool_call`, `announced_intent`, `non_convergence`,
`silent_repetition`), the completion guards `edit_verification`,
`checklist_close_out` and `team_obligation` (`nudge`), and `struggle`
(`nudge`, `offer`). 4.100.258 announced only `reasoning_loop`,
`tool_intent_overrun` and `nudge`. Every action is a `guard_trip` note
(`guard`, `phase`, `iteration`, `message`, and `cause` or `tool`). The first
trip the arm's `guards.stop_on` names is also a `guard_stop` note, the
session is aborted, and the last line's outcome is `guard_stop`.

## What protocol 7 does not do

Escalation, sub-agents and teams, MCP approvals (no arm starts an MCP
server), the emulation of editor behaviour (diagnostics after edits,
mentions, the visible terminal: 040 §6.1), and looking up the model's
family on the server to choose a prompt template. An arm that asks for one
of the first is refused; the others are recorded differences.
