# Decisions and open questions

**Status:** current as of 2026-10-12.

Only the owner decides. An **Open** item becomes a decision when the owner says
so; until then the specs carry it as Open.

## 1. Decided by the owner

All dated 2026-10-12.

| id | decision | where |
|---|---|---|
| D1 | The application and daemon are written in Go. | 010 §1 |
| D2 | A host runs only the Go application. No Node, Python or helper script on a host. | 010 §1 |
| D3 | Node runs only inside containers: the lane image, and an external harness's task container when the adapter installs the runner there. | 010 §1, 064 §3.2 |
| D4 | Lanes run in Docker, from a slim base image with one layer per language. | 010 §1, 030 §3 |
| D5 | Gitea provides git for tasks that need a remote. | 060 §6 |
| D6 | Store: DuckDB for state, configuration, metrics and statistics; Lance for text chunks, vectors and full-text. A preference, to be confirmed by the spike. | 080 §2 |
| D7 | This phase produces specifications and a roadmap only. No implementation milestone is approved yet. | 110 |
| D8 | Remote hosts use agent mode only; no agentless path over SSH. Windows and macOS get a native agent, later. | 010 §3, §4 |
| D9 | Configuration is TOML. Tasks live in this repository; large suites are fetched. | 030 §1 |
| D10 | One operator plus assistants: one write token and a read-only share mode. No accounts. | 010 §5 |
| D11 | Verdicts are `PASS`, `FAIL`, `TIMEOUT`, `ABORTED`, `INVALID`, `SKIPPED`. Imports keep the original word in a separate field. | 020 §3 |
| D12 | Raw event streams, wire captures and logs are content-addressed blobs on disk, referenced from DuckDB. | 080 §2 |
| D13 | The runner reaches full parity with the VS Code extension. | 040 §6 |
| D14 | Published files use neutral host names. | — |
| D15 | First languages, for layers and for our own suite: C#, C/C++, Python, TypeScript, Go, Rust. C# is a primary language. *See the note below.* | 030 §3, 063 §2 |
| D16 | Paused intervals are recorded and excluded from timing; the run is flagged. | 050 §4 |
| D17 | Periodic oracle sampling is optional per task and off by default. | 060 §3.1 |
| D18 | No default for guards or compaction: an arm must state each one. | 030 §5.1 |
| D19 | For harnesses that run the agent in their own container, the adapter installs the runner there. | 064 §3.2 |
| D20 | In mode C, `omk_eval` always owns the model server. | 065 §2 |
| D21 | BFCL runs as two separately named cells: proxy and agent. | 061 §3 |
| D22 | Completion-form tasks do not live in automaticum. It runs agentic tasks only. | 060 §1, 063 §1 |
| D23 | SWE-bench-style sets for other languages are external suites; C/C++ first. | 062 §6 |
| D24 | The shared detector library is written in Go, in automaticum. | 070 §3 |
| D25 | VS Code-only behaviour is emulated where an SDK hook exists, as arm fields, and recorded as emulated. | 040 §6.1 |
| D26 | A full terminal dashboard, in addition to the web dashboard. | 090 §2 |

**Note on D15.** The owner said "add C# as primary language", and later, asked
which languages come first, selected C/C++, Python + TypeScript and Go + Rust
without selecting C#. This set records the union, with C# first. If C# was
meant to be left out of the first set, D15 changes.

## 2. Settled in the planning session

Not owner decisions; recorded so they can be overturned.

| id | item | where |
|---|---|---|
| S1 | The vocabulary takes `cell`, `cohort`, `basis`, `headline metric`, `cap hit`, `errored` and `engine adapter` as omnimergekit proposed, plus `runner` and `agent mode`. | 000 §3 |
| S2 | The request-count pause is not emulated, because it no longer exists in the extension. | 040 §6 |
| S3 | The old oracle's history is numbered as task versions 1, 2 and 3. | 100 §3 |

## 3. Open

| id | question | where | recommendation |
|---|---|---|---|
| O1 | How the native agent on Windows and macOS meets "a host runs only the Go application", given the runner needs Node. | 010 §4 | decide when milestone 7 is planned |
| O2 | Whether out-of-tree plugins (suites, gates, detectors, adapters) are ever needed. | 010 §2 | no; compile them in |
| O3 | Configuration directory layout and file names. | 030 §1 | as proposed there |
| O4 | Gitea: one instance per daemon or per campaign, and where it runs for remote lanes. | 060 §6 | one per daemon |
| O5 | Which published C/C++ SWE-bench-style set, at which version; the order of languages after it. | 062 §6 | pick when milestone 5 is planned |
| O6 | Which external harnesses need the offline event path. | 064 §3.2 | find out per harness |
| O7 | Exit code numbers for `cell run`. | 065 §2.1 | as proposed; confirm with omnimergekit |
| O8 | Deduplication scheme for wire captures. | 080 §4 | measure in the spike |
| O9 | The archive tier's location and the policy for cold blobs. | 080 §5 | owner's call |
| O10 | Pass thresholds for the store spike. | 080 §7 | set before it runs |
| O11 | Which views the TUI has beyond the core ones, and their order. | 090 §2 | lanes, queue, live run, controls first |
| O12 | Whether the store spike runs before or alongside the walking skeleton. | 110 §3 | before; the static build is the largest risk |
| O13 | The milestone order as a whole. | 110 §3 | as proposed |

## 4. External dependencies

Requests to the Cerebriline SDK (040 §8). None is confirmed.

| id | request | effect if absent |
|---|---|---|
| X1 | Read the resolved session configuration before the first request. | read-back is unavailable; verification relies on the wire |
| X2 | Configuration fields for the lead agent's context window and for the data directory. | the runner uses the same side routes as the CLI |
| X3 | Disable, or report, the merge of stored provider profiles and global settings. | the runner relies on an empty data directory |
| X4 | Mention expansion as an SDK function. | the runner re-implements it and may drift |
| X5 | A hook for the error-retry decision. | listed as a known difference |
| X6 | Confirmation of the extension-parity inventory. | the inventory rests on a source read |

## 5. Not adopted

| item | source | why |
|---|---|---|
| Completion-form tasks in the multi-language suite | omnimergekit R11 | D22 |
| automaticum starting the model server in mode C | omnimergekit R3 | D20 |
| Agentless Docker over SSH | draft 3 | D8 |
| The request-count pause | owner's parity scope | S2: removed from the extension |
