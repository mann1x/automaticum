# Decisions and open questions

**Status:** current as of 2026-10-13 (revision 8).

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
| D44 | Lance binding: `lance-c` behind our own cgo wrapper, with our own rank fusion. `lancedb-go` main is the fallback. | 080 §7.1.1 |
| D45 | Build base for the native parts: `manylinux_2_28`. Revises the "built on Debian 11" part of D39. | 080 §7.1 |
| D46 | Store shape confirmed after the spike: DuckDB, Lance through `lance-c`, blobs on disk. The Lance binding is reviewed again when search is built into the interfaces (milestone 6). Confirms D6. | 080 §2, §7.4 |
| D47 | Wire captures are deduplicated by message part: each part stored once, a request is a list of part hashes. Sizes are confirmed on real captures in milestone 2 before the format is frozen. | 080 §4 |
| D48 | Regex search runs over the non-delta lines of a stream by default; a slow raw mode over every byte stays available. | 080 §7.4 |
| D49 | Vector search uses a refine step by default, with a factor of at least 30. | 080 §7.4 |
| D50 | Every chunk carries a locator: the blob hash and byte offset of its source line. | 080 §7.4 |
| D51 | The 30 earlier campaigns in the old harness's archive are imported only when a question needs them. | 100 §7 |
| D52 | Embeddings are updated manually: an embedding pass is started by the operator for a stated scope, never scheduled and never automatic. | 080 §5.2, 090 §1 |
| D53 | The `manic_miner` task is public and whole: seed, prompt, oracle source, reference and fixtures live in this repository. | 060 §5.1 |
| D54 | The runner uses SDK release 4.100.250. Every run records the SDK release version; a run whose release version cannot be established does not start. | 040 §3 |
| D55 | The first real runs of milestone 2: provider `xollama` on `windows-2` (a Windows host with one 24 GB GPU; earlier drafts wrongly wrote `linux-gpu-2`), model `qwen36-base-mtp_tb:27b-q4km-128k`. | 110 §3 |
| D56 | Old corpus: verdicts contradicted by how the run ended are re-mapped (operator kills to `ABORTED`, the CLI's own clock to `TIMEOUT`), original word kept; the two sibling lanes are imported. A boundary for one defective build was offered and not chosen. | 100 §7 |
| D57 | Configuration layout: the daemon reads one directory given by `--config`; lanes and endpoints live there and are not tracked; `arms/` and `tasks/` are tracked, and an arm names an endpoint, never an address. Approved with the milestone 2 plan. | 030 §1 |
| D58 | Milestone 2 plan approved: Docker through the Engine API with our own small client; the runner is plain Node ESM, one process per run; the daemon is not linked with Lance until ingest writes chunks (milestone 3); `manic_miner` is task version 4 here; its workspace oracle is built reproducibly; the lane container is unprivileged with the default bridge network; a stub model server for tests. | 110 §3 |
| D59 | The first real arm (2026-10-13): context window 128k, sent as the 131072 the model tag declares; thinking effort `high`; sampler temperature 1, top-k 20, repeat last-n 0, repeat penalty 1, presence penalty 0. The values of the old harness's last manual arm (96000, effort xhigh, temperature 0.8 and so on) are not used. | `arms/qwen36-mtp-27b-high.toml` |
| D60 | Compaction and every guard are enabled by default: the shared base states them all as on, and an arm that wants one off says so (2026-10-13). | 030 §5.1, `arms/base.toml` |
| D61 | Real runs on `windows-2` must be stoppable at any moment, gracefully or not: the owner may need the host at short notice (2026-10-13). | 050 §4 |
| D62 | Milestone 3 plan approved (2026-10-13): ingest with metrics; the comparison of every request with the arm; the detector library with controls; the gate engine with `doctor`, without the gates that need agent mode; `reanalyse`; refusing gates notify through a webhook and the daemon's log. | 110 §3 |
| D63 | An arm value that the provider does not send (first seen: thinking effort on `xollama`) does **not** stop the run: the run starts, a finding "stated, not sent" is attached, and reports show the effective value. | 050 §3.1, 070 §4 |
| D64 | When the endpoint is busy at the start of a run (another model loaded, or a request that is not ours), the run **waits** in the queue and the gate notifies once; nothing is unloaded or disturbed. | 050 §3.1 |
| D65 | The old corpus gets the text detectors, through `reanalyse`; metrics that need the wire capture exist for new runs only. | 070 §3, 100 |
| D66 | Several SDK builds are supported at the same time. Lane images are SDK-agnostic; the build an arm names is provisioned at run time; testing is reproducible on demand (2026-10-13). This replaces the image label as the way D54 is met; D54 itself stands, without its fixed release number. | 040 §3.1, 030 §3 |
| D67 | A build is a prebuilt bundle, mounted read-only into the run's container; the harness keeps the tracked lockfile and an archive of every downloaded package, so a build is made again with no registry (2026-10-13). | 040 §3.1 |
| D68 | Provisionable builds are published releases and local builds packed from a commit of the SDK's repository, named `<release>+<commit>` (2026-10-13). | 040 §3.1 |
| D69 | The command line, the terminal interface and the web interface write values in human-readable formats; durations always as hours, minutes and seconds (2026-10-13). One formatter serves all three; JSON and the API stay unformatted. | 090 §1.1 |
| D70 | Every run records the inference point, the model's family and variant, the GPU, the host and the sampling parameters. The model's stated identity lives in a model catalogue, one tracked file per tag (2026-10-13). | 020 §1.1, 030 §4.1 |
| D71 | A run whose model tag has no stated identity runs and is marked unknown, with a finding; it is not refused (2026-10-13). | 020 §1.1 |
| D72 | The GPU is declared in the endpoint's configuration now and recorded as declared; it is measured and compared when agent mode exists (2026-10-13). | 020 §1.1, 030 §4.2 |
| D73 | A run whose model was loaded again mid-run (`model_reloaded`) or that ended partly off the GPU (`gpu_residency`) is `INVALID` by default, its earlier verdict kept beside it. An arm may accept either, as the thing it tests: `[accepts] model_reload`, `partial_gpu`. Such an arm is another arm; its runs keep their verdict and the finding becomes a note (2026-10-13; closes O21). | 030 §5.3, 070 §4.1 |
| D74 | Tag `qwen36-base-mtp_tb:27b-q4km-128k` is the base Qwen 3.6 27B model (variant `base`); the GPU of `windows-2` is an NVIDIA GeForce RTX 3090, 24 GB. The ten real runs of 2026-10-13, made before the record held these facts, have them added and marked as added later (`automaticum run identify`) (2026-10-13). | 020 §1.1, 030 §4.1, 090 §1 |
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
| O5 | Which published C/C++ SWE-bench-style set, at which version; the order of languages after it. | 062 §6 | milestone 5 |
| O6 | Which external harnesses need the offline event path. | 064 §3.2 | milestone 5, per harness |
| O19 | Repeated tool call: how many identical calls, and whether they must be in a row. Built as 3 or more in a row; recommendation: keep as the default, a suite may declare another. | 070 §3.2 | the owner; nothing waits, the built default is in use |
| O20 | Severity of a runaway block: built as a note, with the default size of 10,000 characters for every run until suites declare theirs; recommendation: keep. | 070 §3.2 | the owner; nothing waits |

Closed since the first revision: O2 (D32), O4 (D30), O7 (S4, then D34), O9
(D31), O10 (D42), O11 (D33), O12 (D28), O13 (D29), O14 (D46), O8 (D47), O15
(D48), O16 (D49), O17 (D50), O18 (D51), O3 (D57).

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
| X6 | Confirmation of the extension-parity inventory. | done 2026-10-13 for 040 §6: 11 rows confirmed, 2 corrected, 6 added. §6.1 checked the same day: 4 rows hold with details the emulation must copy, 2 behaviours added (040 §6.1.1) | §6.2 rests on a source read |
| X7 | Release 4.100.250 of the SDK, which carries X1 to X3 and which every milestone 2 run must use (D54). | released and used: the lane images install it and every run records it | none |
| X8 | An SDK release whose published packages carry the patched Ollama provider library, so that a thinking effort level and the sampler options the stock library does not name reach the wire (110 §3.1). | released as 4.100.251 on 2026-10-13 and provisioned here beside 4.100.250; not yet run against a real model | the `wire_arm` findings of the first real run on it are the check (D63) |

## 5. Not adopted

| item | source | why |
|---|---|---|
| Completion-form tasks in the multi-language suite | omnimergekit R11 | D22 |
| automaticum starting the model server in mode C | omnimergekit R3 | D20 |
| Agentless Docker over SSH | draft 3 | D8 |
| The request-count pause | owner's parity scope | S2: removed from the extension |
