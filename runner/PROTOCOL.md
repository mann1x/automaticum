# The runner: protocol 2

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

## In

`node runner.mjs /job/job.json`, with three mounts: `/job` (read-only),
`/workspace` and `/out`.

| field | meaning |
|---|---|
| `protocol` | 2 |
| `run_id`, `session_id` | the run, and the session id made beforehand |
| `arm` | the resolved arm (`automaticum arm show <arm> --json`) |
| `endpoint` | `name`, `kind`, `base_url` as the container reaches it, `api_key` if any |
| `prompt`, `default_answer` | the task's prompt; what the default user answers to a question |
| `user` | the task's scripted user (`[user]` of `task.toml`, 040 §6.2), with each rule's `times` stated; absent, every tool runs without asking and every question gets `default_answer` |
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
| `runner` | `note` and its fields: `prepared`, `config_resolved` (the SDK's own report), `wall_limit`, `interactive`, `restored`, `user_action_failed`, ... |
| `user` | the scripted user: `on` (approval, question, turn, elapsed, completion, resume), `rule` (from 1; 0 for the default), `do`, and what it answered or sent |
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
| `user` | with a script: `approvals`, `denials`, `questions`, `actions` |

| outcome | the harness's verdict | exit code |
|---|---|---|
| `refused`: never started (other SDK release, arm asks for what this protocol cannot do) | `SKIPPED` | 3 |
| `invalid`: the session is not the arm | `INVALID` | 4 |
| `wall_limit` | `TIMEOUT` unless the oracle passes | 5 |
| `error` | `ABORTED` | 1 |
| `completed` | the oracle decides | 0 |
| no last line at all | `ABORTED` | any |

## What protocol 2 does not do

Approval by category, mode switches, a restore of the messages,
escalation, sub-agents and teams, the
emulation of editor behaviour (diagnostics after edits, mentions, the visible
terminal: 040 §6.1), and looking up the model's family on the server to
choose a prompt template. An arm that asks for one of the first three is
refused; the others are recorded differences.
