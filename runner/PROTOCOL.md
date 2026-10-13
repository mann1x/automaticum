# The runner: protocol 1

The runner is a Node program that runs **one** agent session for one run,
inside the lane's container, and exits. The harness (`internal/run`) writes
its job and reads what it prints. Spec: `docs/specs/040-runner.md`.

**State on 2026-10-13:** run against SDK release 4.100.250 and the stub model
server: a scripted session that reads, edits and checks a file ends
`completed` and the harness's oracle passes. `wire.mjs` and `session.mjs`
also have tests that need no SDK (`node --test`). zod is pinned in
`package.json` to the version the SDK was built with; see the note there.

## In

`node runner.mjs /job/job.json`, with three mounts: `/job` (read-only),
`/workspace` and `/out`.

| field | meaning |
|---|---|
| `protocol` | 1 |
| `run_id`, `session_id` | the run, and the session id made beforehand |
| `arm` | the resolved arm (`automaticum arm show <arm> --json`) |
| `endpoint` | `name`, `kind`, `base_url` as the container reaches it, `api_key` if any |
| `prompt`, `default_answer` | the task's prompt; what the scripted user answers to a question |
| `workspace`, `out` | paths in the container |
| `wall_s` | the wall limit the runner enforces by stopping the agent |
| `sdk_release` | the release the installed SDK must be |

## Out: standard output

One JSON object per line, each with a time `t` and a kind `k`.

| `k` | content |
|---|---|
| `event` | `event`: one SDK session event, unchanged, errors described by hand |
| `runner` | `note` and its fields: `prepared`, `config_resolved` (the SDK's own report), `ask_question`, `wall_limit`, ... |
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

| outcome | the harness's verdict | exit code |
|---|---|---|
| `refused`: never started (other SDK release, arm asks for what protocol 1 cannot do) | `SKIPPED` | 3 |
| `invalid`: the session is not the arm | `INVALID` | 4 |
| `wall_limit` | `TIMEOUT` unless the oracle passes | 5 |
| `error` | `ABORTED` | 1 |
| `completed` | the oracle decides | 0 |
| no last line at all | `ABORTED` | any |

## What protocol 1 does not do

Approval other than automatic, escalation, sub-agents and teams, the
emulation of editor behaviour (diagnostics after edits, mentions, the visible
terminal: 040 §6.1), and looking up the model's family on the server to
choose a prompt template. An arm that asks for one of the first three is
refused; the others are recorded differences.
