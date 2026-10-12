# Decisions and open questions

**Status:** current as of 2026-10-12 (revision 6).

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
| D27 | The SDK fixes requested from Cerebriline go ahead on that side. | 040 §8 |
| D28 | The store spike runs first, before the walking skeleton. | 080 §7, 110 §3 |
| D29 | The milestone order is approved as the roadmap; milestone 1 is planned next. This supersedes the "no milestone approved" part of D7. | 110 §3 |
| D30 | One long-lived Gitea instance per daemon. | 060 §6 |
| D31 | Raw blobs are archived automatically by age, verified before the source is freed. | 080 §5 |
| D32 | Plugins are compiled into the binary only. | 010 §2 |
| D33 | The terminal dashboard has all four view groups: core, gates and doctor, A/B and reports, log viewer and search. | 090 §2 |
| D34 | For endpoint errors the runner observes the SDK's retries and does not decide them. Request X5 is withdrawn. | 040 §8.1 |
| D35 | The native agent's way of running Node is decided when milestone 7 is planned. | 010 §4 |
| D37 | Licence: Apache-2.0. | `LICENSE` |
| D38 | The Lance binding is chosen by the spike (candidates L1, L2, L3), not assumed to be `lancedb-go`. | 080 §7.1 |
| D39 | Linking posture: both engines static, glibc and libstdc++ dynamic, built on Debian 11. Fully static is reported, not required. | 080 §7.1 |
| D40 | Only the daemon holds the store. It is required on linux amd64 and arm64; Windows builds agent mode and the CLI without it. The daemon runs on Linux for now. | 080 §1, 010 §2 |
| D41 | Hybrid ranking is our own code if the binding lacks it. | 080 §7.1 |
| D42 | The spike's pass thresholds T1 to T15, with T1 to T3 as hard gates. | 080 §7.3 |
| D43 | Spike data: live tier and indexes on a dedicated NVMe that is neither the development disk nor the old harness's disk; archive tier on the large array. Real embeddings for about 100k chunks, synthetic vectors for scale. The store interface, blob store and importer are kept; the benchmark harness is throwaway. | 080 §7 |
| D36 | Archived logs stay searchable, on the model of the episodic memory in the owner's claude-hooks project: only raw blobs move, the index never does, and every reader opens archived blobs transparently. | 080 §5.1 |

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
| S3 | The old oracle's history is numbered as task versions 1, 2 and 3. Confirmed by the Cerebriline session as a numbering of scoring boundaries. | 100 §3 |
| S4 | Exit codes for `cell run` are 0, 10, 20, 30, 70. Confirmed by the omnimergekit session; this closes the former O7. | 065 §2.1 |
| S5 | Detector and cap-audit definitions follow omnimergekit's code exactly, as corrected by that session. | 070 §3, §5 |

## 3. Open

Each of these waits for a measurement or for the planning of its milestone.

| id | question | where | waits for |
|---|---|---|---|
| O1 | How the native agent on Windows and macOS meets "a host runs only the Go application", given the runner needs Node. | 010 §4 | milestone 7 (D35) |
| O3 | Configuration directory layout and file names. | 030 §1 | milestone 2; as proposed there unless the owner objects |
| O5 | Which published C/C++ SWE-bench-style set, at which version; the order of languages after it. | 062 §6 | milestone 5 |
| O6 | Which external harnesses need the offline event path. | 064 §3.2 | milestone 5, per harness |
| O14 | Confirm the build base: `manylinux_2_28` in place of Debian 11 (forced by the link gate; revises D39). | 080 §7.1 | now |
| O15 | Which Lance binding is adopted: L1 `lancedb-go` main, or L2 `lance-c` with our own wrapper. Both passed T1. | 080 §7.1.1 | now; recommendation L2 |
| O8 | Deduplication scheme for wire captures. | 080 §4 | the spike |

Closed since the first revision: O2 (D32), O4 (D30), O7 (S4, then D34), O9
(D31), O10 (D42), O11 (D33), O12 (D28), O13 (D29).

## 4. External dependencies

Requests to the Cerebriline SDK (040 §8). The owner approved the work on
2026-10-12 (D27). X1 to X3 are on the SDK's main branch and ship with the release after 4.100.249; none is in a published release yet.

| id | request | status | effect while absent |
|---|---|---|---|
| X1 | Read the resolved session configuration, with provenance, before the first request. | on Cerebriline main, unreleased | with an older SDK build: no read-back, wire only |
| X2 | Configuration fields for the data directory and the lead agent's context window. | on Cerebriline main, unreleased | the data directory stays process-wide: one run per runner process |
| X3 | A strict mode that refuses stored provider profiles and global settings. | provider profiles: on main, unreleased. Global settings: no switch | a fresh data directory, asserted through the resolved-configuration report |
| X4 | Mention expansion as an SDK function. | worked around | the scripted user sends expanded text; expansion is not exercised |
| X5 | A hook for the error-retry decision. | withdrawn (D34) | retries are observed and recorded, not decided |
| X6 | Confirmation of the extension-parity inventory. | pending | the inventory rests on a source read |

## 5. Not adopted

| item | source | why |
|---|---|---|
| Completion-form tasks in the multi-language suite | omnimergekit R11 | D22 |
| automaticum starting the model server in mode C | omnimergekit R3 | D20 |
| Agentless Docker over SSH | draft 3 | D8 |
| The request-count pause | owner's parity scope | S2: removed from the extension |
