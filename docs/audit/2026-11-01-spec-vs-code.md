# Spec against code, 2026-11-01

Every item the spec set (`docs/specs/`), `README.md` and `decisions.md` say
the system does, checked against the code at commit 180cc97, the owner's
order of 2026-11-01. Status per row: BUILT (with the code), PARTIAL (what is
missing), NOT BUILT, SCHEDULED (a later milestone of 110 §3), OPEN (the
owner decides) or changed by a decision (its row).

**Fixed in the same change, so no longer gaps:** `wait run|batch|cell` and
the server-side wait (090 §5.1, also `campaign`, `lane`, `idle`, `--next`,
`--each`); notifications by `[notify] command` (the mailbox) and on a
batch's end, a stopped batch and a run's end (050 §2); `run tail` of a
queued run; `cell run` exiting 70 instead of 20 on a static-gate refusal
(065 §2.1); the example systemd unit killing a draining daemon after 120 s
(050 §5); binaries built without their version, so the basis's "harness
build" fact read `automaticum dev` for every build.

**Item 1 of the follow-up, the CLI basics (2026-11-01, later the same
day):** dry runs and idempotency keys on every mutating command, the exit
codes, `--json` everywhere, the lane, batch, campaign, cell, arm, endpoint
and task verbs of 090 §1, `run tail` rendered, `logs --grep|--turn`,
`status --watch`, `reanalyse --where|--detector`, `report` (regression and
census among them), `export run|cell|campaign`, and O30 decided as D119 (a
drain releases a held run). Those rows are updated in place and name the
new code. Three gaps became owner questions: O31 (how a cohort is
declared), O32 (`requeue`), O33 (`rescore`). Count after item 1: about
432 rows built, 109 partial, 144 not built, 93 scheduled, 12 open.

**Item 2, scheduling and unattended operation (2026-11-01):** endpoint
capacity in the scheduler; the heartbeat; infrastructure errors that end a
run after the SDK's own retries are `ABORTED`, never `FAIL` (the audit's
first evidence pointed at the plan-mode tool's retry setting, not at model
requests). Owner questions: O34 (labels), O35 (external busy signal), O36
(automatic quarantine), O37 (driver watchdog), O38 (priorities and
reservations).

**Item 3, secondary oracles and partial scores (2026-11-01):** secondary
oracles run and are findings; the partial score is read from the primary
oracle's output; "claimed success, oracle says no" is a finding; analysis
version 8.

**Item 4, first part (2026-11-01):** p10/p90 everywhere, the cell's
verdict-conditioned numbers (tokens over PASS runs, tokens per solved,
throughput, empty completions with the 5 % warning, loops and caps by
verdict), streaks, McNemar, time per turn and the longest gap. Owner
question: O39 (the statistics that need a significance level, against
D78). Still not built in 070: the §4 tool-call classes and output hygiene,
turn pathologies, struggle signals, guard replay, near-duplicate rounds,
edit oscillation, re-read loop, the SDK's failed-identical-call guards,
compaction detail, edits and change protocol, escalation, sub-agents,
cost, per-request rates, group-by, lane skew, bootstrap, and the per-run
report's arm highlight and timeline.

**Owner answers on O31 to O39 (2026-11-01):** D120 (cohorts), D121
(rescore), D122 (labels), D123 (busy command), D124 (automatic quarantine
after 10 infrastructure failures in a row, `quarantine_after`), D125
(priorities and reservations); `requeue` (O32), the driver watchdog (O37)
and the level-based statistics (O39) not adopted. D122 to D125 are built
and their rows updated; D121 (rescore) and D120 (cohorts) are built.

**Item 4, second part (2026-11-01 to 2026-11-02, analysis versions 9 to
11):** owner answers D126 to D129. Built: tool failures by error class,
per-request rates, thinking per block and budget hits, compaction detail,
re-reads by cause, reasoning carried, escalation, edits and edits without
a check (D127), the change protocol's transactions, thinking-only turns
and the other turn pathologies the data defines (D128), control markers,
guard trips with what came next, guard replay with the 4.100.258 rules,
the guards' review queue (`report guards`), the SDK's call guard
(failed identical call, repeated call and result, edit oscillation,
re-read loop, D129), struggle signals with recall and false alarms (D126,
`report struggle`), cost in money for a priced endpoint, loop_rate's
components, group-by and lane skew (`compare --group`, `report skew`),
the run timeline, and files written outside the workspace
(`outside_workspace`). Still open or waiting: the per-run report's arm
highlight (O40), near-duplicate rounds (D129: undefined), refusal and a
report after an early end (D128: no rule), the per-suite runaway size,
visible-character floor and loop_rate headline (`suites/*.toml`,
milestone 5), edit churn and same-region edits, parse paths and the
output hygiene the SDK keeps on its messages and not in the stream,
sandbox escapes, sub-agents (no arm runs them).

The other rows below are as found, before those fixes. Each table was written by
one reader of a group of specs; a row's evidence is a path and line at
180cc97.


## Group A

### Audit A: spec-vs-code gaps for 000, 010, 020, 030, 040

Repo state: main at 180cc97, 2026-11-01. Paths are relative to the repo root. Statuses: BUILT, PARTIAL, NOT BUILT, DROPPED/CHANGED (decision id), SCHEDULED (milestone), OPEN.

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 000 §1 | Harness runs Cerebriline SDK agents against tasks with a verdict, on lanes it orchestrates | BUILT | internal/run/run.go Execute; internal/daemon/daemon.go queue/worker |
| 000 §1.1 | Verdict word means one thing; timeout-exit-0 not a failure | BUILT | internal/run/verdict.go Decide; store.Verdict enum |
| 000 §1.1 | Oracle tamper / oracle from harness copy | BUILT | internal/task/exec.go RunOracle (Tampered); run.go:663 tampered -> ABORTED |
| 000 §1.1 | Thinking/sampler not applied is recorded (wire proof) | BUILT | internal/ingest/wirearm.go CompareWire -> `wire_arm` findings |
| 000 §1.1 | Stale SDK build refused | BUILT | gate `harness_build` (internal/gate/gate.go); run.go:733 SDK.Check; runner.mjs:91-94 refusal |
| 000 §1.1 | Session state kept for every run | BUILT | run.go kindOf: `data`/`home` -> `session` blob |
| 000 §1.1 | Edits to running config cannot take effect mid-campaign | BUILT | arm resolved once per run (daemon.go:552 run.Request{Arm}); campaign file hashed (config/campaign.go SHA256) |
| 000 §1.1 | Wrong port / wrong server detected | BUILT | run.Probe + endpoint_before/after Changed() (run.go:645), gates model_tag/bind_address |
| 000 §2 | Mode A autonomous | BUILT | run start / campaign / daemon queue |
| 000 §2 | Mode B engine for external harness (engine adapters) | SCHEDULED (m5) | no adapter code; 110 §3 milestone 5 |
| 000 §2 | Mode C backend of omk_eval (`cell run`, caller endpoint) | BUILT | internal/daemon/modec.go, cell.go; cmd/automaticum/cell_cmds.go |
| 000 §2 | Every mode B/C report says in its header the score is model+Cerebriline build, not comparable to published numbers | NOT BUILT | no such header/warning in internal/daemon/cell.go CellResult or compare output (grep "comparable" finds only cap notes) |
| 000 §3 | Vocabulary: task, run, batch, arm, lane, endpoint, oracle, gate, finding, campaign, basis, cell | BUILT | store.Run fields; daemon cell.go/campaign.go/basis.go |
| 000 §3 | `cohort` (named set of cells with one declared variable) | NOT BUILT | store.Run.Cohort and CellResult.Cohort/DeclaredVariable exist but nothing sets them (only duck.go read/write); no cohort entity or command |
| 000 §3 | `headline metric` declared in the suite | PARTIAL | CellResult.Headline exists (cell.go); suite definitions (suites/*.toml) not built (m5) |
| 000 §3 | `suite` with fetch/prepare/score | PARTIAL | tbench2 importer only (internal/suite/tbench2); suite interface is m5 |
| 000 §3 | `agent mode` | SCHEDULED (m7) | no `automaticum agent` command in cmd/automaticum/main.go |
| 000 §4-5 | Map and traceability tables | n/a | documentation only |
| 010 §1 | Daemon and CLI in Go; host runs only Go; Node only in containers | BUILT | cmd/automaticumd, cmd/automaticum; runner only in images/base/Dockerfile |
| 010 §1 | Lanes in Docker from slim base + one layer per language | BUILT | images/base, images/layers/{typescript,python,go,rust,cpp,dotnet}; internal/docker |
| 010 §1 | Gitea provides git for tasks that need a remote | SCHEDULED (m5) | no Gitea code (grep gitea: none); 110 §3 m5 |
| 010 §1 | Store is DuckDB + Lance + blobs | PARTIAL | daemon opens DuckDB + blobs only (cmd/automaticumd/main.go:84-91); internal/store/lance used only by tests/spike; Lance chunks/search = m6 |
| 010 §1 | Remote hosts via agent mode only | SCHEDULED (m7) | not built |
| 010 §1 | One operator, no accounts | BUILT | single token, api.go allowed() |
| 010 §2 | HTTP/JSON API + SSE stream | BUILT | api.go Handler (routes ~286-321); handleStream text/event-stream (api.go ~1076) |
| 010 §2 | WebSocket | NOT BUILT | only SSE; diagram lists "SSE/WebSocket" |
| 010 §2 | Scheduler, lane manager, gate engine, oracle runner, ingest + detectors, query + statistics | BUILT | daemon.go, gate.go, task/exec.go RunOracle, ingest/, stats/, compare.go |
| 010 §2 | Embedder client, search | SCHEDULED (m6) | none |
| 010 §2 | Gitea client | SCHEDULED (m5) | none |
| 010 §2 | Engine adapters | SCHEDULED (m5) | none |
| 010 §2 | Suite plugins | PARTIAL | tbench2 importer only; suite interface m5 |
| 010 §2 | Webhooks | BUILT | daemon.go:657 notify, config [notify] webhook |
| 010 §2 | Mailbox | NOT BUILT | none; 110 §3.2 lists "a mailbox notifier" as carried forward from m3 with no milestone |
| 010 §2 | TUI, web dashboard, MCP clients | SCHEDULED (m6) | none |
| 010 §2 | Every dashboard action exists as CLI command and API call | SCHEDULED (m6) | dashboard not built |
| 010 §2 | Plugins have a name and a version | BUILT (gates, detectors) | gate.Gate{Name,Version}; Finding.DetectorVersion; adapters/suite plugins pending m5 |
| 010 §2 | Plugins compiled in only (D32) | BUILT | no runtime plugin loading |
| 010 §2 | Python-only scorers run in their own containers | BUILT | tbench2 verifier zipapp run in oracle container (internal/suite/tbench2/verifier) |
| 010 §2 | Windows and macOS run the CLI (and agent mode) | PARTIAL | CLI builds with CGO_ENABLED=0 (store_nocgo.go) but Makefile has no windows/darwin target (targets: images, lance-c, build, bench, test, test-store, shell); agent mode m7 |
| 010 §3 | Agent mode: Docker on instruction, host facts, server log fetch, buffered event relay, mutual auth, version refusal, agent never decides | SCHEDULED (m7) | none |
| 010 §3 | Paths never assumed POSIX; Windows first-class for the binary | PARTIAL | code uses filepath, but no Windows build or test target |
| 010 §4 | Native agent for Windows/macOS | SCHEDULED (m7) | D35 |
| 010 §4 | How the native agent gets Node | OPEN | O1 |
| 010 §5 | One write token | BUILT | config/local.go Token(); api.go allowed() |
| 010 §5 | Tokens stored hashed | NOT BUILT | token read in plain text from token_file (local.go:293-313), compared directly (api.go allowed) |
| 010 §5 | Read-only mode to share a result | PARTIAL | only unauthenticated GET from loopback (api.go allowed); no read-only token/share mode |
| 010 §5 | Loopback by default; refuse non-loopback without token | BUILT | local.go:206 default 127.0.0.1:7411; token always required at start (automaticumd main.go:70) |
| 010 §5 | Every mutating call records who made it (client name) | NOT BUILT | no client/actor field in any handler or StartRequest (grep actor/client: none) |
| 010 §5 | Secrets by reference to a secret store; never in record/export/command line/log | PARTIAL | api_key_file/token_file file references; wire.mjs masks headers as `[set]`; no secret store |
| 010 §6 | Lane binds to named GPU devices; never use a device not given | PARTIAL | no device field on Lane (local.go:121-134); gate `gpu_ownership` returns Unchecked (m7) |
| 010 §6 | Wait on external busy signal; never preempt | BUILT | gate `free` Wait (gate.go:198); D64 |
| 010 §6 | Never kill by process name; only by recorded ids | BUILT | daemon.go Recover removes container by recorded id |
| 010 §6 | Readiness predicate: one-token generation returns and identity matches | PARTIAL | identity probe built (run/probe.go); no generation probe: probe.go:20 "without generating anything" |
| 010 §6 | Never probe a busy endpoint disturbingly | BUILT | probe reads /api/ps,/api/show,/props only |
| 010 §6 | Never use/start a server bound to all interfaces | PARTIAL | gate `bind_address` refuses only a 0.0.0.0 URL (gate.go:444-459); whether the server listens on all interfaces needs m7 agent; never starts servers (D97) |
| 010 §6 | Large data to data root with free-space floor; never tmpfs | BUILT | free_floor_gb (local.go:88), run.go:739 floor check; Tmpfs check in automaticumd main.go |
| 010 §6 | Never skip silently: SKIPPED with reason, cell marked incomplete | PARTIAL | SKIPPED+reason built (verdict.go); cell `complete` only counts unfinished runs, SKIPPED only gives a warning (cell.go:286-292) |
| 010 §6 | Timestamps UTC with offset | BUILT | StartedAt now.UTC() (run.go:316), RFC3339 JSON |
| 010 §6 | Configuration immutable once a run started | BUILT | arm resolved once and job file mounted read-only |
| 020 §1 | Run id sortable, batch, campaign, arm id, task id, task version, lane, host | BUILT | store.Run (store.go:55-101), run.go:314-325 |
| 020 §1 | Run record cell and cohort columns | PARTIAL | Cell only in provenance (`cell`), store.Run.Cell/Cohort never set by internal/run |
| 020 §1 | Arm fully resolved with value as sent | BUILT | Run.Arm = a.Resolved() (run.go:318) |
| 020 §1 | ...plus where each value came from | PARTIAL | Arm.Origins is `json:"-"` (config/arm.go:50); shown by `arm show` only, not stored with the run |
| 020 §1 | Harness build: SDK version, commit, sha256 of package; runner version; image digest; automaticum version | BUILT | provenance sdk/image/automaticum/runner_protocol (run.go:218-237, 726-738) |
| 020 §1 | Harness build: SDK dirty flag | NOT BUILT | no dirty field anywhere (grep dirty: none) |
| 020 §1 | Harness build: language layer digests | PARTIAL | only lane image ID + RepoDigests (run.go:726); no per-layer digests |
| 020 §1 | Endpoint identity: kind, version, digest, capabilities, template sha/size, renderer, parser, parameters, context, slots, residency | BUILT | run/probe.go Identity; props.go (n_ctx, total_slots, template hash, build_info) |
| 020 §1 | Endpoint identity: serving-library build time/hash (llama.cpp shared library), feature list, reasoning/tool-call parser flags, imatrix | PARTIAL | only llama.cpp build_info string; no library hash, no feature list, no parser flags for llama.cpp; imatrix not read (basis.go says so) |
| 020 §1 | Weights: repo/path, revision, sha256 per file | PARTIAL | only model digest and GGUF metadata via server; not_recorded "weights: needs agent mode" (run.go:336) |
| 020 §1 | Task identity: suite, sha256 of every input file, prompt, oracle hash, scorer image | BUILT | provenance task hash/files/prompt (run.go:333); OracleResult.ImageID |
| 020 §1 | Task identity: suite version | NOT BUILT | not in run provenance (only CellResult.SuiteVersion) |
| 020 §1 | Host facts: GPU, driver, power cap, free VRAM | SCHEDULED (m7) | GPU declared only; not_recorded host_facts (run.go:337) |
| 020 §1 | Limits in force: wall, idle with owner (incl. batch D107) | BUILT | provenance limits wall/idle/fired/fired_by (run.go:199-263) |
| 020 §1 | Limits in force: requests, tokens, steps | NOT BUILT | only wall and idle recorded; arm has no requests/tokens limit (max_iterations is in arm, not in limits record) |
| 020 §1.1 | `inference` object: endpoint, kind, version, engine, hosts, declared GPU, share, model as served, tag/digest, stated identity, not_stated | BUILT | run.go:50-93 Inference |
| 020 §1.1 | GPU `measured` | SCHEDULED (m7) | stays empty (run.go:70) |
| 020 §1.1 | Model queried before every run; compare shows distinct answers | BUILT | D82; compare.go Served |
| 020 §1.1 | `added_later` via `run identify` | BUILT | route POST /v1/runs/{id}/identify; AddedLater |
| 020 §1.1 | Finding `model_identity` | BUILT | ingest/sampler.go:15 |
| 020 §1.1 | Family/size `queried` (D80) | BUILT | run/probe.go:248-267 |
| 020 §1.1 | `run show` table of sampling/thinking in effect | BUILT | cmd/automaticum/client_cmds.go:864 and :618 |
| 020 §1.1 | Facts as columns to group reports by ("come with the reports of milestone 4") | NOT BUILT | store.Run has no inference columns; m4 is closed |
| 020 §2 | Agent event stream complete, compressed | BUILT | put("events") run.go:492; blob zstd (store/blob/format.go) |
| 020 §2 | Every request/response on the wire | BUILT | runner/wire.mjs -> wire.jsonl -> `wire` blob |
| 020 §2 | SDK session state | BUILT | `session` blob (data dir) |
| 020 §2 | Runtime log (output caps, discarded turns, compaction, retries) | PARTIAL | kindOf maps `runtime*` (run.go:840) but the runner writes no runtime log; only what the SDK leaves in data/ |
| 020 §2 | Server log for the run window, log level verified | SCHEDULED (m7) | not_recorded server_log; gate server_log_level Unchecked |
| 020 §2 | Workspace before and after | BUILT | putTree workspace_before/after (run.go:482,569) |
| 020 §2 | Workspace diff | NOT BUILT | not_recorded "no textual diff is made" (run.go:338); only added/removed/changed file lists |
| 020 §2 | Optional snapshot per transaction or compaction | NOT BUILT | none |
| 020 §2 | Collected paths; container user, capabilities, CPUs, memory | BUILT | putTree collected (run.go:571); provenance container (run.go:264-277) |
| 020 §2 | Oracle command, output, exit code, duration | BUILT | task.OracleResult (task/exec.go:255) |
| 020 §2 | Runner stdout, stderr, exit code | BUILT | events/runner_stderr blobs; container exit_code |
| 020 §2 | Runner signal | NOT BUILT | only exit_code/oom_killed recorded |
| 020 §2 | Material to re-run the scorer without the model | BUILT | workspace_after, tool calls, wire |
| 020 §3 | Six verdicts as one enum | BUILT | store.Verdict; verdict.go |
| 020 §3 | Oracle hash checked before output read; changed oracle ABORTED | BUILT | run.go:663-665 |
| 020 §3 | Exit-0 oracles judged by declared pattern | BUILT | task.go:140 expect_pattern |
| 020 §3 | TIMEOUT records which limit and owner | BUILT | verdict.go limitWho; LimitBy runner/harness |
| 020 §3 | ABORTED causes: crash, OOM, operator kill, tampered oracle, scorer error, D89 | BUILT | run.go:580-612 |
| 020 §3 | ABORTED on empty event stream | PARTIAL | missing end line is ABORTED (run.go:594); an end line with zero events is not checked |
| 020 §3 | INVALID: wrong build, setting not applied, not GPU-resident, endpoint changed | BUILT | run.go:599-645; ingest gpu_residency/model_reloaded |
| 020 §3 | D99 guard stop -> FAIL with oracle answer kept | BUILT | verdict.go GuardStop case |
| 020 §3 | Secondary outcome (agent end, claimed success) | BUILT | Run.AgentEnd, ClaimedSuccess (run.go:548) |
| 020 §3 | "Claimed success, oracle says no" is a finding | BUILT | ingest/oracle.go `claimed_success_oracle_no`, TestOracleFindings |
| 020 §3 | Partial score (tests passed / tests) | BUILT | `oracle.tests_passed|tests_total|partial_share` from the primary oracle output (ingest/oracle.go PartialScore) |
| 020 §3 | Paused flag and paused intervals | BUILT | Run.Paused; provenance holds/held_ms |
| 020 §3 | `run invalidate` keeps original verdict | BUILT | POST /v1/runs/{id}/invalidate; Run.PriorVerdict |
| 020 §4 | Task version recorded | BUILT | Run.TaskVersion |
| 020 §4 | A change to a task/oracle/limits/toolchain/scoring creates a new task version | PARTIAL | version is hand-declared in task.toml (task.go:30); file hash recorded, nothing forces a bump |
| 020 §4 | Scorer change voids comparability of earlier cells until re-scored | NOT BUILT | none |
| 020 §4 | Reports refuse to pool across a version boundary unless told, and list exclusions | NOT BUILT | compare groups by arm+task only (compare.go:320-322); differing versions only show as a mixed `task` basis fact |
| 020 §5 | Basis object with canonical serialisation and sha256 | BUILT | daemon/basis.go basisFields; CellResult.BasisSHA256 |
| 020 §5 | `basis diff`, `basis show`, `arm diff` | BUILT | routes GET /v1/basis; cmd basis show/diff; config_cmds arm diff |
| 020 §5 | compare prints basis diff of each pair | BUILT | compare.go Basis []BasisDiff |
| 020 §5 | Basis field weights per-file sha256/revision | PARTIAL | digest + general.finetune only |
| 020 §5 | Basis field imatrix | NOT BUILT | basis.go records "not recorded" |
| 020 §5 | Basis field GPU driver and power | SCHEDULED (m7) | needs agent mode |
| 020 §5 | Basis field suite version, task-set sha256, task ids in order | PARTIAL | in CellResult (suite_version, task_set_sha256); basis `task` fact per task only |
| 020 §5 | Cohort declared variable; **confounded** label | BUILT | D120: campaign `[cohort]`, `cell run --cohort --declared-variable`; `internal/daemon/cohort.go` (`cohortFacts`, `cohortView`); TestCohort, TestCampaign |
| 020 §5 | Refuse to put different bases in one table unless told; label differing columns | NOT BUILT | spec says not built |
| 020 §5 | Greedy anchor; sampled cells in separate sampler cohort, never mixed | NOT BUILT | none (grep greedy: none) |
| 020 §5 | Plain-text basis exported with every cell | BUILT | CellResult.BasisText (cell.go:90); the spec's "not built" line is out of date |
| 020 §5 | Mode C caller facts marked (checked against /props)/(from /props)/(caller) | BUILT | basis.go:351-449 |
| 030 §1 | TOML, validated on load, resolved into record | BUILT | config/*.go |
| 030 §1 | Unknown fields are errors | BUILT | md.Undecoded in arm.go:275, local.go:185, campaign.go:81, task.go:174, sdk.go:119, model.go decodeStrict |
| 030 §1 | No environment-variable knobs | PARTIAL | CLI and daemon read AUTOMATICUM_CONFIG/REPO/DATA/ARCHIVE/LEGACY_* as flag defaults (config_cmds.go:31-32, store_cgo.go:28-93, automaticumd main.go:35); paths only, but not recorded |
| 030 §1 | Validation commands `arm validate`, `doctor` | BUILT | main.go dispatch |
| 030 §1 | Validation command `task verify` | NOT BUILT | no `task verify` in main.go (only `oracle verify`, `task show/build/ls`) |
| 030 §1 | Validation before anything is queued | BUILT | Static gates in start (api.go; endpoint-probe rule) |
| 030 §1 | One `--config` directory with automaticum.toml, lanes, endpoints, arms, models (D118) | BUILT | config/local.go LoadLocal, ArmsDir/ModelsDir |
| 030 §1 | automaticum.toml: data root, archive root, bind address, token, [notify] webhook | BUILT | local.go:84-116 |
| 030 §1 | examples/config, tasks/, images/, runner/sdk/<build>/, campaigns/*.toml tracked | BUILT | repo tree |
| 030 §1 | suites/*.toml | SCHEDULED (m5) | no suites/ directory |
| 030 §2 | Lane: name, host, resource limits, endpoints, labels | BUILT | local.go:121-134 |
| 030 §2 | Lane: image (base plus layers) | CHANGED (not by a decision row) | no lane image field; the image comes from the task's layers (daemon.go:253 LaneImage) |
| 030 §2 | Lane: GPU devices bound | NOT BUILT | no field; gpu_ownership gate m7 |
| 030 §2 | Lane: concurrency (default 1) | PARTIAL | fixed at 1, no field |
| 030 §2 | Lane states idle, running, paused, draining, offline, quarantined | PARTIAL | idle, running, waiting, paused, drained, quarantined (by hand, or by the daemon after `quarantine_after` infrastructure failures, D124) built; offline not built |
| 030 §3 | No SDK in any image (D66) | BUILT | images/base/Dockerfile: empty node_modules; bundle mounted (internal/sdk) |
| 030 §3 | Base: runner, Node, git, shell tools | BUILT | images/base/Dockerfile |
| 030 §3 | Base: sandbox prerequisites / sandbox binary (040 §1) | NOT BUILT | no sandbox binary or setup in images/base/Dockerfile |
| 030 §3 | One layer per language, composable; task declares; daemon builds or pulls the composition | PARTIAL | layers built one per image by `image build` (image_cmds.go); a task naming two layers gets a tag ("automaticum-lane:go-python") nothing builds; the daemon neither builds nor pulls |
| 030 §3 | Language server in each layer | SCHEDULED (m5) | only typescript layer has language servers; 110 §3 puts diagnostics with language layers in m5 |
| 030 §3 | Images built by automaticum, pinned by digest in every record | BUILT | `image build`; provenance image id/digests |
| 030 §3 | First layers C#, C/C++, Python, TS, Go, Rust (D15) | BUILT | images/layers/{dotnet,cpp,python,typescript,go,rust} |
| 030 §3 | Java, PHP, Ruby layers follow | NOT BUILT | no layers; no milestone given |
| 030 §4 | Endpoint kinds ollama, xollama, openai-compatible, llamacpp | BUILT | local.go:176 |
| 030 §4 | Endpoint kinds opencoti, cloud | NOT BUILT | not in endpointKinds (opencoti only as an SDK provider, D101) |
| 030 §4 | Credentials by reference | BUILT | api_key_file |
| 030 §4 | Log source (journal, agent, API) | SCHEDULED (m7) | no field; server log needs agent mode |
| 030 §4 | How to ask it to load/unload a model | PARTIAL | unload only (unload_others, D79; pause drop D83); D97 forbids starting servers |
| 030 §4 | Capability probe per kind | BUILT | run/probe.go per kind; props.go |
| 030 §4 | Real one-token generation probe | NOT BUILT | probe.go:20 "without generating anything" |
| 030 §4 | Detect restart, auto-update, reload | BUILT | Identity.Changed before/after; model_reloaded finding |
| 030 §4 | Detect 429 Retry-After, context-size errors, evictions | PARTIAL | failed requests flagged (ingest/wire.go:97) but not classified |
| 030 §4 | Capacity; scheduler respects it across lanes | PARTIAL | Endpoint.Capacity parsed, used only for mode C (modec.go:98); scheduler does not read it (gate `free` waits on any other model instead) |
| 030 §4 | Caller-given endpoint (mode C), never loaded/unloaded | BUILT | modec.go; daemon.go:525 `!ep.Cell` |
| 030 §4 | D79 unload_others and `unloaded_before_run` | BUILT | daemon.go:525; provenance Unloaded |
| 030 §4.1 | Model catalogue fields, unknown field error, duplicate tag error | BUILT | config/model.go:17-73 |
| 030 §4.1 | D80 family/size from weights | BUILT | probe.go:248-267 |
| 030 §4.2 | `[hardware]` gpu, vram_gb, notes recorded as declared | BUILT | local.go:169-173; Inference.GPU.Declared |
| 030 §5 | Model fields: provider, model, digest pin, context window, output budget, thinking level/budget, sampler incl. name, stop | BUILT | config/arm.go:58-99; digest pin gate.go:222 |
| 030 §5 | Ownership tag/arm required for sampler and thinking | BUILT | arm.go:512-545 |
| 030 §5 | Agent: mode, tool selection, tool result cap, max iterations, mistake limit, run/idle timeouts, task progress, checkpoints, change protocol, edit verification, guards listed in arm, bundled skills, escalation flag, sub-agent flags, host IDE | BUILT | arm.go:104-245; runner/session.mjs buildConfig |
| 030 §5 | Agent: prompt template family, system prompt variant, read limit, retries, per-request timeout, compaction continuation, struggle detector, stalled-check, unreadable-call repair, sub-agent nodes/window share/swarm/teammates, sandbox mode, Jev, library, memory, diagnostics after edits | NOT BUILT | no such fields in config/arm.go |
| 030 §5 | Approval policy | PARTIAL | `agent.approval` exists but only "auto" is accepted (runner/session.mjs:29); category decisions come from the task's scripted user |
| 030 §5 | Escalation; sub-agents | PARTIAL | arm flags exist; runner refuses an arm that turns them on (session.mjs:30-31) |
| 030 §5 | Harness: SDK build | BUILT | harness.sdk_release |
| 030 §5 | Harness: runner version, image, oracle version, task version as arm fields | NOT BUILT | Harness has only SDKRelease (arm.go:245) |
| 030 §5.1 | Every guard and compaction setting stated; no defaults | BUILT | need(...) arm.go:596-667 |
| 030 §5.1 | guards.stop_on validated against guardPhases; empty list omitted from identity | BUILT | arm.go guardPhases; config_test TestStopOnIdentity |
| 030 §5.1 | guards.reasoning_loop.max_consecutive_trips (D108) | BUILT | arm.go:188,604; runner guards.mjs reasoningLimitAnswer |
| 030 §5.1 | thinking.reasoning_history / reasoning_inline (D103) | BUILT | arm.go:81-82,528-532; session.mjs providerConfig |
| 030 §5.1 | Guard trip counts `guard_trips.<guard>.<phase>` | BUILT | ingest/events.go, analyse.go |
| 030 §5.2 | extends, resolved arm with origins, arm diff, identity hash, template = true, dropInherited | BUILT | arm.go:260-352, 684-718 |
| 030 §5.3 | `[accepts] model_reload`, `partial_gpu` | BUILT | arm.go:237-239; gate.go:328; ingest/analyse.go:388-393 |
| 030 §6 | Campaign file: batches, order, interleaving, lane, stop conditions (after N pass, on invalid, at) | BUILT | config/campaign.go; daemon/campaign.go:199-206 |
| 030 §6 | Campaign: gates in addition to the built-in ones | NOT BUILT | no gates field in CampaignBatch (campaign.go:33-50) |
| 040 §1 | Runner as SDK library consumer under Node 22+, streams events, captures wire | BUILT | runner/runner.mjs, wire.mjs |
| 040 §1 | D113 /opt/automaticum prefix, private rg | BUILT | images/base/Dockerfile; runner/tools.mjs |
| 040 §1 | Sandbox binary shipped by the lane image | NOT BUILT | not in images/base/Dockerfile |
| 040 §3 | Install any published release | BUILT | `sdk add` (internal/sdk) |
| 040 §3 | Tarball built from a named commit, in a build container (Bun) | PARTIAL | `source = "local"` takes packed packages given with --packs (sdk.go:232); no build-from-commit container |
| 040 §3 | Record version, commit, sha256 | BUILT | provenance sdk |
| 040 §3 | Record dirty flag | NOT BUILT | none |
| 040 §3 | Run without established release does not start (D54); refuse on mismatch (gate) | BUILT | gate harness_build; run.go:727-735; runner.mjs:91-94 |
| 040 §3.1 | Bundle mounted read-only; lockfile + sdk.toml; archived packages; registry/local sources; bundle hash pinned | BUILT | internal/sdk/sdk.go; runner/sdk/<ver>/ |
| 040 §3.1 | `sdk add [--packs]`, `sdk ls [--deep]`, `sdk verify` (no network) | BUILT | sdk_cmds.go:41,75; sdk.go:362 network "none" |
| 040 §3.1 | Run hashes the bundle before start (SKIPPED on difference); INVALID if runner reports another version | BUILT | run.go:733 SDK.Check(deep=true); run.go:623-626 |
| 040 §4 | Typed configuration from the resolved arm | BUILT | session.mjs buildConfig |
| 040 §4 | Read-back via onConfigResolved, difference is invalid | BUILT | session.mjs compareResolved; runner.mjs:278 |
| 040 §4 | Wire: every request recorded; compared to arm; drift is a finding | BUILT | wire.mjs; ingest/wirearm.go CompareWire |
| 040 §4 | Derived values recorded beside the resolved configuration | BUILT | session.mjs derivedFromArm |
| 040 §5 | New empty data directory per run | BUILT | runner.mjs:107 |
| 040 §5 | Verify no provider profile / global settings read; else INVALID | BUILT | session.mjs:179-182 sources check |
| 040 §5 | Data directory collected | BUILT | `session` blob |
| 040 §5 | Lead context window set and verified on wire | BUILT | contextWindow (session.mjs:55); wirearm.go:145 num_ctx |
| 040 §6 | Approval by category, decided by the scripted user | BUILT | internal/task/user.go:31-35; runner/user.mjs |
| 040 §6 | Approval policy in the arm | PARTIAL | only "auto" |
| 040 §6 | Denial with feedback, extension sentences | BUILT | runner/user.mjs |
| 040 §6 | ask_question answered by scripted user | BUILT | runner.mjs:169 toolExecutors.askQuestion |
| 040 §6 | Plan/act switch; switch_to_act_mode | BUILT | runner.mjs:346-356; plan.mjs |
| 040 §6 | Steering and queued messages | BUILT | scripted user steer/queue (task/user.go:104) |
| 040 §6 | Cancel and resume used for pause/resume (050 §4); resume after restart | PARTIAL | scripted cancel+resume built; pause uses the hold file instead; no resume of a session after a daemon restart (Recover makes it ABORTED) |
| 040 §6 | Checkpoint restore | BUILT | scripted restore/fork |
| 040 §6 | compareCheckpoint; snapshots from checkpoints | NOT BUILT | no compareCheckpoint in runner |
| 040 §6 | Mistake-limit stop: arm field; record forced vs counted | PARTIAL | maxConsecutiveMistakes passed (session.mjs:67); no onConsecutiveMistakeLimitReached handler, nothing records `forced` |
| 040 §6 | Manual compaction run by the runner, scripted | NOT BUILT | no updateSessionCompactionState in runner |
| 040 §6 | Slash commands expanded by the runner | NOT BUILT | none |
| 040 §6 | Escalation and check approvals (escalation.approve, atomicProtocol.approveCheck), arm states whether supplied | NOT BUILT | escalation refused; no approveCheck handling or arm field |
| 040 §6 | MCP tools as arm field | NOT BUILT | PROTOCOL.md "does not do": MCP; category `mcp` refused |
| 040 §6 | Browser tool as arm field | NOT BUILT | no field, no browser in image |
| 040 §6 | render_image/transform_image/generate_icon driver or recorded absent | NOT BUILT | no driver, no record |
| 040 §6 | check_file registered by the runner | NOT BUILT | none |
| 040 §6 | Runner replaces no executor and records that the SDK's ran | PARTIAL | replaces none; not recorded |
| 040 §6 | Task progress arm field | BUILT | session.mjs:81 |
| 040 §6 | Sandbox arm field, same default | NOT BUILT | no field, not passed |
| 040 §6 | Bundled skills arm field | BUILT | session.mjs:24-35; runner.mjs:174-205 |
| 040 §6 | Extension values as arm fields: max changes, task-progress interval, tool result cap, compaction agentic, edit verification | BUILT | arm.go protocol/task_progress/tool_result_cap/compaction.strategy/edit_verification |
| 040 §6 | Extension value as arm field: 6 retries | NOT BUILT | no retries field |
| 040 §6 | Request-count pause not emulated | DROPPED (S2) | settled S2 |
| 040 §6.1 | Diagnostics after edits from language servers (with §6.1.1 details) | SCHEDULED (m5) | 110 §3: emulated diagnostics with language layers in m5; PROTOCOL.md lists as not done |
| 040 §6.1 | Background terminal executor emulation, auto-detach 300 s, recorded as non-default | NOT BUILT | nothing in runner; no milestone named |
| 040 §6.1 | @problems mention | SCHEDULED (m5) | depends on diagnostics |
| 040 §6.1 | @terminal/@git-changes/file/URL/folder mentions expanded by the runner | NOT BUILT | none (X4 worked around) |
| 040 §6.1 | Host IDE named in system prompt and tool token, one arm field | BUILT | agent.host_ide; runner.mjs:185-199 ideName |
| 040 §6.1 | Each behaviour not copied is a recorded difference / "recorded as emulated" | NOT BUILT | no record of emulations or differences in run provenance or runner notes |
| 040 §6.1.1 | check_file and semantic search known differences recorded | NOT BUILT | not recorded |
| 040 §6.2 | Scripted user stage 1 and 2: rules, default, categories, denials, mode switch, plan, fork (D86), interactive, read receipts, `user` lines, metrics | BUILT | internal/task/user.go; runner/user.mjs, runner.mjs:166,227,346-376; ingest analyse.go:224-227 |
| 040 §6.2 | No script: auto-approve and default answer | BUILT | PROTOCOL.md job `user` absent |
| 040 §6.2 | Restore needs checkpoints on, else SKIPPED | BUILT | run.go:707-712 |
| 040 §6.2 | D87 workspace a git repo by default | BUILT | job workspace_git; runner.mjs:398 |
| 040 §7 | Event stream unchanged incl. teammates; wire; data dir; stdout/stderr/exit; final record | BUILT | PROTOCOL.md; run.go blobs |
| 040 §7 | Runtime log returned | PARTIAL | no runtime log written by the runner |
| 040 §7 | Runner never runs or sees the oracle | BUILT | oracle in separate container (task/exec.go) |
| 040 §7.1 | Job file fields, stdout line kinds, end line, outcomes, ABORTED without end line | BUILT | runner/PROTOCOL.md; internal/run/protocol.go; run.go:594-612 |
| 040 §7.1 | Wall limit enforced twice; record says which fired | BUILT | runner.mjs:236; run.go:119-121,497 Grace; Limits.FiredBy |
| 040 §7.1 | Guard trips, guard_stop (protocols 4, 5) | BUILT | runner/guards.mjs guardTrip/stopsOn |
| 040 §7.1 | Reasoning-loop limit answer and breach -> INVALID (protocol 8) | BUILT | guards.mjs:72-89; runner.mjs:284,328 |
| 040 §8.1 | One session per runner process | BUILT | one process per run |
| 040 §8.1 | Run record says mention expansion was not exercised | NOT BUILT | no such record |
| 040 §8.1 | SDK retries observed and recorded as infrastructure | NOT BUILT | no retry counting in internal/ingest (grep retry: none) |
| 040 §8.1 | Arm retry limits passed as configuration | NOT BUILT | no retries field |
| 040 §8.2 | Bundled skills written to fresh global settings | BUILT | runner.mjs:174-176 |
| 040 §8.2 | Wire check verifies the tool list for skills | NOT BUILT | wirearm.go compares sampler/options only |
| 040 §8.2 | Harness-build gate checks the data-dir environment variables are unset | NOT BUILT | gate.go harness has no env check |
| 040 §8 | X1-X3, X9, X11, X12 requests | n/a (external) | consumed: onConfigResolved, storedProviderSettings "ignore", contextWindow, dataDir |

## Group B

### Audit B: specs 050, 060-066 against the code (HEAD 180cc97, 2026-11-01)

Audited against committed code. Note: the working tree has **uncommitted, in-progress** work by another session
(`internal/wait/wait.go`, `internal/daemon/wait.go`, route `GET /v1/wait/{kind}` added to `internal/daemon/api.go`; no CLI command yet).
It is not counted as BUILT below.

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 050 §1 | Queue of runs; one run per lane | BUILT | `internal/daemon/daemon.go:350` `startReady` (one flight per lane, `d.flights[it.Lane]`) |
| 050 §1 | Scheduler places a run on a lane that satisfies its **labels** | BUILT | D122: task and arm `requires`, lane `labels`; `laneFor` (daemon.go); TestLaneLabels |
| 050 §1 | ... and whose endpoint has **capacity** | BUILT | `endpointFull` in startReady (daemon.go); status says why a run waits; TestEndpointCapacity |
| 050 §1 | Interleaving A/B arms ABAB on the same lane | BUILT | `internal/daemon/api.go:447` (order `interleaved` default), `:620` rounds; arms forced onto one lane `api.go:545` |
| 050 §1 | `campaign start <name>` reads `campaigns/<name>.toml`, checks every batch, keeps definition | BUILT | `internal/daemon/campaign.go:272`, `cmd/automaticum/campaign_cmds.go` (`--dry-run`) |
| 050 §1 | Campaign batches started in order under key `<campaign>-<batch>`; stop conditions applied every few seconds | BUILT | `campaign.go:67` `campaignLoop`, `:104` `campaignStep`, `:92` `stopReason` (after_consecutive_pass, on_invalid, at) |
| 050 §1 | Stopped campaign batch: queued runs SKIPPED with condition, in-flight finishes | BUILT | `campaign.go` step -> `control.go:400` `stopBatch` |
| 050 §1 | Each run records its campaign | BUILT | `daemon.go:579`, `internal/run/run.go:315` |
| 050 §1 | Campaign state in `control.json`, survives restart; re-start shows state | BUILT | `campaign.go:280` (`Repeated`), `control.go:50/103` |
| 050 §1 | `campaign pause/resume/stop` | BUILT | `api.go:312-314`, `campaign.go:352` |
| 050 §1 | Campaign end (finished or stopped) notified like a refusing gate | BUILT (webhook+log only) | `campaign.go:243` `endCampaign` -> `notify`; no mailbox (see §2) |
| 050 §1 | `campaign show` with each batch compared | BUILT | `campaign.go:332` `handleCampaign` |
| 050 §1 | Gates of a campaign's own | NOT BUILT | stated "not built yet"; no milestone assigned; campaign config has no gate key (`internal/config/campaign.go`) |
| 050 §1 | Priorities and reservations ("this lane is mine until 08:00") | BUILT | D125: `run start --priority`, `lane reserve/unreserve` (lanerules.go, control.go `stopped`); TestPriorityAndReservation |
| 050 §1 | One campaign across two hosts | SCHEDULED (m7) | agent mode |
| 050 §1 | Batch of several tasks / whole suites (`--task A --task B`, `--suite`) (D107) | BUILT | `cmd/automaticum/client_cmds.go:174-175` |
| 050 §1 | Batch limits: `run start --wall-s/--idle-s`, campaign `wall_s`/`idle_s`, `batch limits` for queued runs | BUILT | `client_cmds.go` `addLimits`, `config/campaign.go:44-45`, `control.go:278` |
| 050 §1 | Campaign batch takes `suites` and `capture_requests` (D104) | BUILT | `config/campaign.go:37,48` |
| 050 §1 | Lane marked busy by an **external signal**; scheduler waits; never preempts | BUILT | D123: lane `busy_command` (`laneBusy`, lanerules.go), checked in `fly` before the unload; TestBusyCommand |
| 050 §1 | External harness concurrency is a request; adapter queues | SCHEDULED (m5) | adapters not built (064) |
| 050 §1 | Large suites sharded across lanes, per-instance env caching, resume of half-finished evaluation | SCHEDULED (m5) | 062; only mode C `cell run --lanes N` spreads a cell over lanes (`modec.go`) |
| 050 §2 | Gate failure produces SKIPPED/INVALID with reason | BUILT | `daemon.go:533` `skip("gate "+...)` |
| 050 §2 | Gates are plugins with name and version | BUILT | `internal/gate/gate.go:92-106` |
| 050 §2 | ... "so a campaign can add one" | NOT BUILT | fixed built-in list; see campaign gates above |
| 050 §2 | Refusing gate notifies via **webhook** | BUILT | `daemon.go:533,659` `notify`; `[notify] webhook` `local.go:116` |
| 050 §2/§3.1.1/§6 | Refusing gate / campaign end / stopped batch notifies via **mailbox** | NOT BUILT | spec itself: "A mailbox notifier is not built"; D62 scoped m3 to webhook+log; 110 §3.2 carries it forward with no milestone; no mailbox code anywhere |
| 050 §2 | Static-gate refusal at `run start` (422) notified | PARTIAL | `api.go:557-561` refuses the start but sends no notification (caller gets the 422; campaign batch start failure ends campaign, which is notified) |
| 050 §2 | `automaticum doctor` runs every gate on every lane | BUILT | `api.go:302` `/v1/doctor`, CLI `doctor [--lane --arm --task --all]` |
| 050 §3.1 | Gate model tag (exists, digest matches pin) | BUILT | `gate.go:94` `modelTag` |
| 050 §3.1 | Gate template (template, renderer, parser intact) | PARTIAL | checks tool capability, records hash/size; template text not parsed, weights file named not hashed (spec "not yet") |
| 050 §3.1 | Gate sampler (parses, written, read back) | PARTIAL | gate checks completeness only; "written and read back" is the post-run wire comparison (070 §4.1) |
| 050 §3.1 | Gate GPU residency | PARTIAL | loaded model fully on GPU; named tensors on CPU not checked (needs server log) |
| 050 §3.1 | Gate GPU ownership | SCHEDULED (m7) | `gate.go:104` always unchecked |
| 050 §3.1 | Gate thinking (D63 never refuses) | BUILT | `gate.go:97` |
| 050 §3.1 | Gate free: busy endpoint -> wait, one notification, nothing unloaded (D64) | PARTIAL | another model in memory -> wait + one notification BUILT (`daemon.go:519-531`); requests in flight for someone else not detected (needs agent, m7) |
| 050 §3.1 | Gate expert | SCHEDULED (m7) | `gate.go:106` unchecked |
| 050 §3.1 | Gate harness build (SDK named, empty data dir) | BUILT | `gate.go:99` + run-time hash check `run.go:727-735` |
| 050 §3.1 | Gate server log level | SCHEDULED (m7) | `gate.go:105` unchecked |
| 050 §3.1 | Gate host: disk floor, inotify, user-namespace sysctl, clock sync | PARTIAL | disk floor and inotify only; `gate.go:390` "clock sync and the user-namespace setting are not read" (no milestone) |
| 050 §3.1 | Gate oracle (verifies against fixtures, proof by task hash) | BUILT | `gate.go:101`; proof `internal/task/exec.go:575` |
| 050 §3.1 | Gate workspace (pristine, hash-checked) | BUILT | `gate.go` workspace (writable, not tmpfs `:420`), copy+hash in run |
| 050 §3.1 | Gate bind address (not bound to all interfaces, reachable) | PARTIAL | reachability only; all-interfaces check needs agent (m7) |
| 050 §3.1.1 | Four answers pass/refuse/wait/unchecked; wait re-asked every 30 s; answers recorded in run | BUILT | `gate.go:30`, `daemon.go:409` (`GateRetry` 30 s), `Gates: answers` `daemon.go:579` |
| 050 §3.1.1 | Notification: refusal every time, wait once per run; payload fields | BUILT | `daemon.go:519-533`, `Notification` struct `daemon.go:645` |
| 050 §3.1.1 | Static gates refuse `run start`/campaign batch/dry run up front (422, nothing queued) | BUILT | `api.go:557-561` |
| 050 §3.2 | Gate context per slot >= prompt + output budget of the suite's largest task | PARTIAL | only mode C: arm context window vs `/props` slot `n_ctx` (`run/props.go:95` `Contradictions`); no per-suite largest-task check, none for non-cell batches |
| 050 §3.2 | Gate parse failures degrade (server does not 500 on tool-call parse failure) | NOT BUILT | no code |
| 050 §3.2 | Gate scorer validation (gold passes on a sample) for external suites | PARTIAL | tbench2 gold validated at import (`suite/tbench2/import.go:148-172`); local tasks via `oracle` gate; no general pre-cell gate for external suites (m5 suites) |
| 050 §3.2 | Gate detector self-test before cell/batch; failing detector marks metric unavailable | PARTIAL | self-test run when the cell result is read and a warning added (`daemon/cell.go:271`); not a gate before the batch, metric not marked unavailable |
| 050 §3.2 | Gate disk for the suite (image + env size vs free) | NOT BUILT | only the generic `free_floor_gb` floor |
| 050 §3.2 | Gate credentials (token in secret store before start) | NOT BUILT | only endpoint `api_key_file` read during probe (`gate.go:120`) |
| 050 §3.3 | First-generations canary: first 5 finished requests, empty/length/control tokens, stops batch, notifies (D84) | BUILT | `internal/ingest/canary.go`, `internal/daemon/canary.go:102-118`; verdict in `batch show` |
| 050 §3.3 | Median length within the band the suite/task declares | PARTIAL | check exists but always "unchecked": no task or suite field can declare a band (`ingest/canary.go:57`; no band key in `internal/task/task.go`) |
| 050 §4 | Control matrix pause/resume/abort/drain/requeue/retry at run, batch, campaign, lane, daemon | BUILT | `requeue` not adopted (O32, decisions §5); run, batch, campaign pause/resume/stop/abort/retry; lane drain/pause/quarantine/abort/resume (`--all` for the daemon); daemon drain (signal, `POST /v1/drain`) |
| 050 §4 | `requeue` | NOT ADOPTED | O32: dropped by the owner; `run retry` and `run start` cover it |
| 050 §4 | Graceful by default | BUILT | `batch stop` graceful unless `--abort`; drain |
| 050 §4 | Abort kills process tree by recorded id; aborted run still ingested | BUILT | container killed by id (`daemon.go:593`), oracle and ingest still run |
| 050 §4 | Abort confirms on the server side that generation stopped | SCHEDULED (m7) | spec: needs agent mode |
| 050 §4 | Pause inside a run (D77/D83): hold before next request, unload on Ollama/xOllama, held time subtracted, `paused` flag | BUILT | `run.go:245-248` Holds/HeldMS, `HoldFile`; `api.go:1223` `afterHeld` unload |
| 050 §4 | Infrastructure errors (endpoint down, 429, network) wait and retry, recorded as such | BUILT | the SDK retries (llms 3 attempts, agents provider retries, turn fault recovery), each a `notices.*` metric; a run ending on a `refusal`/`transport` fault is ABORTED unless passed (`run/fault.go`, verdict.go `Infra`); the old evidence (`plan.mjs` retryable:false) was the switch tool, not requests |
| 050 §4 | Restart: runs in flight re-attached or marked ABORTED(daemon restart) | BUILT | `daemon.go:239` `Recover` (`:265` "daemon restart"); no re-attach (allowed by "or") |
| 050 §4 | Immutability: arm/task read at run start | BUILT | `daemon.go` `fly` reads arm and task at start |
| 050 §4 | Idempotency key on **every** mutating command | BUILT | `Idempotency-Key` header on every mutating route (mutate.go `idempotent`), answers kept 30 days; local commands repeatable by construction (090 §1) |
| 050 §4 | Dry run for **every** mutating command | BUILT | `?dry_run=1` on every mutating route; `--dry-run` on every mutating command incl. image build, sdk add, suite import, task build, exports |
| 050 §4 (m2) | `run stop` (queued -> SKIPPED; refuses in-flight) | BUILT | `api.go:293` |
| 050 §4 (m2) | First signal drains, second aborts (`ABORTED(daemon stop)`) | BUILT | `cmd/automaticumd/main.go:145-153` |
| 050 §4 (m2) | Idempotency key names the batch | BUILT | `api.go` handleStart (`Repeated`) |
| 050 §4 (m4) | `lane drain/resume`; status shows `drained` | BUILT | `control.go:151`, `api.go:306-307` |
| 050 §5 (D119) | A drain (lane, quarantine, daemon, signal) releases a held run; it finishes and the lane stays drained | BUILT | `releaseHeld` (control.go), called by `Drain` and `handleLane`; TestDrainReleasesHeld |
| 050 §4 (m4) | `batch pause/resume` (`--keep-model`) | BUILT | `control.go:227,335` |
| 050 §4 (m4) | `batch stop [--abort]` | BUILT | `control.go:374,400` |
| 050 §4 (m4) | `run retry` / `batch retry` rules (only SKIPPED/ABORTED/INVALID, not imported, not twice; `--dry-run`; retries/retried-by links) | BUILT | `control.go:476` `handleRetry`, `:632` `retryLinks` |
| 050 §4 (m4) | `provenance.asked` kept for never-started runs; old records refused | BUILT | `control.go:130-150`, `:597` `askedOf` |
| 050 §4 (m4) | Control state in `control.json`; unreadable file drains every lane | BUILT | `control.go:64` |
| 050 §5 | Watchdog: run whose event stream is silent N minutes | BUILT | idle limit `idle_s` ends the run as TIMEOUT (`run.go:35,122`) |
| 050 §5 | Watchdog: batch whose driver vanished | NOT ADOPTED | O37: the daemon drives its batches itself |
| 050 §5 | Watchdog: lane producing only infrastructure failures -> quarantine | BUILT | D124: 10 in a row, `quarantine_after`; `countInfra` (lanerules.go), `run.Infrastructure` (fault.go); TestAutoQuarantine, TestInfrastructure |
| 050 §5 | Watchdog: endpoint identity changed since probe -> INVALID | BUILT | `run.go:256-289` EndpointBefore/After/Changed; test `TestOperatorAbortAndEndpointChange` |
| 050 §6 | Heartbeat with counts every ten minutes, to log and on request | BUILT | heartbeat.go: log every 10 min, `status`, `GET /v1/heartbeat`; TestHeartbeat |
| 050 §6 | Server-side `wait` primitives (090 §5) | BUILT | bbdd18f: internal/wait, `GET /v1/wait/{kind}`, `automaticum wait` |
| 050 §6 | Notification (webhook) on campaign end | BUILT | `campaign.go:254` |
| 050 §6 | Notification on any refusing gate | BUILT (webhook) | per-run gates notified; static refusal at start not (see §2) |
| 050 §6 | Notification on a **stopped batch** | BUILT | bbdd18f: batch stop and a campaign batch stopped by its condition notify (control.go, daemon.go notifyEnd) |
| 050 (not specified) | Notification on run end / batch end | not in these specs | no code either; 050 promises only refusing gate, campaign end, stopped batch, canary |
| 050 §6 | Cell with any SKIPPED task marked incomplete, says so in result file | BUILT | `daemon/cell.go:209-211,292-294` |
| 060 §1 | Agent-loop tasks only (completion-form out of scope) | BUILT | by design |
| 060 §2 | task.toml: id, version, labels, layers, limits, prompt, oracles, default answer | BUILT | `internal/task/task.go:28-57` |
| 060 §2 | task.toml: **problem id** | NOT BUILT | no field (also 063 §4) |
| 060 §2 | Workspace seed restored, hash-checked; git repo with one commit; `[workspace] git = false` (D87) | BUILT | `task.go:81-88`, `task.PrepareWorkspace` |
| 060 §2 | Visible tests as a separate optional file set | PARTIAL | no distinct concept; only possible as part of the seed |
| 060 §2 | Oracle: command + exit code or pattern | BUILT | `task.go:142-143` `expect_pattern`, `expect_exit` |
| 060 §2 | Hidden tests outside the workspace | BUILT | oracle artefacts mounted only to the oracle (`exec.go:345-361`) |
| 060 §2 | Fixtures known-good/known-bad | BUILT | `task.go:153-155`, `exec.go:398` |
| 060 §2 | Scripted user | BUILT | `internal/task/user.go`, `runner/user.mjs` |
| 060 §2 | Hooks: per-task prepare and collect steps run by the harness | PARTIAL | `[workspace] collect` (D111) built; no prepare hook |
| 060 §2 | Several oracles: secondary checks recorded as findings | BUILT | run.go runs every non-primary oracle (provenance.oracles, `oracle` blobs); ingest `secondary_oracle` finding; TestSecondaryOracle |
| 060 §2 | Partial scores, recorded, never the verdict | BUILT | ingest/oracle.go PartialScore (checker JSON, pytest summary); TestPartialScore |
| 060 §2 | Limits per task | BUILT | `task.go:71-75` |
| 060 §2 | ... calibrated from the reference solution's run time on the lane | NOT BUILT | limits are hand-written in task.toml |
| 060 §3 | Oracle run after agent stops, from a copy, hash checked | BUILT | `run.go:651-674` (tampered check `:663`), network none `exec.go:361` |
| 060 §3 | `automaticum oracle verify <task>`; reference run several times | BUILT | `cmd/automaticum/config_cmds.go` oracleVerify; `exec.go:398` repeats |
| 060 §3 | Task without both fixtures cannot be used in a campaign | BUILT | `exec.go:399`; gate `oracle` requires proof |
| 060 §3 | Empty fixture = seed, may only be expected to fail | BUILT | `task.go:447` |
| 060 §3 | Oracle-only artefact mounted read-only at `/oracle/<output>`; `writable` tmpfs paths | BUILT | `exec.go:345-363` |
| 060 §3 | `run_as = "root"` for task / oracle (D110) | BUILT | `task.go:57,139` |
| 060 §3 | `[workspace] collect` copied out and given to oracle (D111) | BUILT | `task.go:93`, `exec.go` `imageCollect` |
| 060 §3 | `[limits] cpus`, `memory_mb` for agent and oracle containers (D112) | BUILT | `task.go:74-75` |
| 060 §3.1 | Periodic oracle sampling (optional, off by default; snapshot; finding "correct at some point") | NOT BUILT | decided by owner 2026-10-12; no code, no milestone |
| 060 §4 | Suite interface plugin (fetch -> prepare -> prompt -> run -> collect -> score) | SCHEDULED (m5) | "the suite interface" in 110 §3 m5; only the tbench2 importer exists |
| 060 §4 | Named subsets with stored, hashed task-id lists, part of basis | SCHEDULED (m5) | `cell run --suite S:ids` takes an ad-hoc comma list (`cell_cmds.go:192`); `task_set_sha256` computed per cell, no stored subset |
| 060 §4 | Per subset headline metric and direction | SCHEDULED (m5) | cell headline fixed `pass_rate` (`cell.go:229`) |
| 060 §4 | Per subset canary length band | NOT BUILT | see 050 §3.3 |
| 060 §4 | Suite declares scorer (tool, version, image digest) and verdict mapping | SCHEDULED (m5) | basis records oracle image (`basis.go:258`) only |
| 060 §4 | What the suite withholds the agent never gets | BUILT | oracle artefacts never in workspace (`exec.go`) |
| 060 §5.1 | `manic_miner` ported whole, version 4, `run_game` reproducible + pinned | BUILT | `tasks/custom/manic_miner/task.toml:10,34-36` |
| 060 §6 | Gitea instance per daemon, per-run repo/token/artefacts | SCHEDULED (m5) | no code |
| 060 §7 | Tasks carry canary and `private` flag | BUILT | `task.go:47-48` |
| 060 §7 | Private tasks root `private_tasks`; must be private; canary >= 16; same id in both roots refused | BUILT | `local.go:98`, `task.go:192-247` |
| 060 §7 | Export / embedder / chat box never send private task text to cloud | PARTIAL | `export conversations` refuses private tasks (`daemon/export.go:63`); embedder and chat box do not exist (m6) |
| 060 §7 | Training pool `train-*` suites, `training = true`, loader refuses mismatch (D105) | BUILT | `task.go:53`; suites `tasks/train-ml`, `train-sh`, `train-game` |
| 060 §7 | Oracle and lane `/tmp` allow exec | BUILT | `exec.go:140`, `run.go:512` |
| 061 §1-5 | BFCL suite: pinned `inspect_evals/bfcl`, categories/subsets, V3 scorer, paired comparison, scorer container, gold validation, proxy cell and agent cell, per-entry records, report header note | SCHEDULED (m5) | no BFCL code anywhere |
| 062 §1 | SWE-bench Verified/Lite/full; fixed named hashed subsets | SCHEDULED (m5) | no code |
| 062 §2 | predictions.jsonl, official eval images, ingest reports; env caching, sharding, resume, per-instance budgets | SCHEDULED (m5) | no code |
| 062 §3 | Scoring by official images; five results -> verdict mapping; `empty_patch_rate` | SCHEDULED (m5) | no code |
| 062 §4 | Gold-patch validation with recorded exclusion list; flakiness flag | SCHEDULED (m5) | no code |
| 062 §5 | Detectors per turn; `loop_rate` headline for degeneration cohort | SCHEDULED (m5) | `loop_rate` exists in cell scores (`cell.go`), headline selection does not |
| 062 §6 | Other-language SWE-style sets as external suites, C/C++ first | OPEN | O5 (which set/version/order) |
| 063 §1 | Agentic only | BUILT | by design (train-ml tasks) |
| 063 §2 | Language layers python/go/rust/cpp/dotnet/typescript, pinned by digest, offline | BUILT | `images/layers/{cpp,dotnet,go,python,rust,typescript}` |
| 063 §2 | Compiler/interpreter version in the basis | PARTIAL | basis carries lane image id (`basis.go:230`), not the toolchain version by name |
| 063 §2 | Per-language time limits calibrated from reference run time | NOT BUILT | per-task hand-set limits only |
| 063 §2 | Language server in layers | SCHEDULED (m5) | spec: added with diagnostics work (emulated diagnostics are m5 per 110 §3) |
| 063 §3 | Hidden tests run by harness; visible tests separate file set | PARTIAL | hidden: BUILT; visible as separate set: see 060 §2 |
| 063 §3 | Reference + known-bad per task; admitted only if oracle verify passes; reference run several times | BUILT | fixtures + `oracle verify` repeats + gate `oracle` |
| 063 §3 | Partial credit (tests passed / tests) recorded | BUILT | `oracle.partial_share` (see 060 §2) |
| 063 §4 | Problem id shared across languages; per-language and per-problem report views | NOT BUILT | no problem id field; `compare` groups by arm and task only (`daemon/compare.go:321`) |
| 063 §4 | Tiers and families as labels | BUILT | e.g. `tasks/train-ml/go_workerpool/task.toml` `[labels]` |
| 063 §4 | Attribution on balanced axes with per-category n | NOT BUILT | no label-axis grouping in compare (070 §6 territory) |
| 063 §4 | Versioning: any change = new task version | BUILT | task version + task hash in record/basis |
| 063 §4 | Canary + private flag | BUILT | 060 §7 |
| 063 §5 | Per language: compile errors, test failures, timeouts counted apart | NOT BUILT | no code |
| 064 §2 | Adapter rules (task id 1:1 run, harness result as oracle evidence, harness limits as arm fields, queueing, resume, real result layouts) | SCHEDULED (m5) | no adapter code |
| 064 §3.1 | Predictions-file adapter | SCHEDULED (m5) | no code |
| 064 §3.2 | Installable agent in harness's container (runner install, hash, recorded additions, events streamed) | SCHEDULED (m5) | "the in-container adapter" m5; no code |
| 064 §3.2 | Offline event path for harnesses without network | OPEN | O6 |
| 064 §3.2 | Terminal-Bench 2.0 via lane layer with runner under `/opt/automaticum` (D113) | BUILT | `internal/suite/tbench2/tbench2.go:428-445`, `images/layers/tb2-*` |
| 064 §3.3 | Python shim solver (Inspect, lm-eval) | SCHEDULED (m5) | no code |
| 064 §3.4 | OpenAI-compatible proxy adapter | SCHEDULED (m5) | no code |
| 064 §4 | Report header for adapter cells | SCHEDULED (m5) | no code |
| 065 §2 | `cell run` blocking, safe to re-run, `--json` | BUILT | `cmd/automaticum/cell_cmds.go` |
| 065 §2 | Mode C: endpoint passed in, never loaded/unloaded | BUILT | `cell-<key>` endpoint, `!ep.Cell` guard on unload (`daemon.go:497`) |
| 065 §2 | Caller-resolved arm fields recorded as caller and verified on wire | BUILT | `--caller-values`, origin `caller` |
| 065 §2 | Resume: same key does not repeat finished runs | BUILT | `cell_cmds.go:94-102` |
| 065 §2 | Progress stream started / n_done/n_total / finished | BUILT | `cell_cmds.go` |
| 065 §2.1 | Exit codes 0/10/20/30/70 and `refusal.json` on 20/30 | PARTIAL | built for per-run gate refusals (`cell.go:296-300`); but a static-gate refusal at start (422 from `api.go:561`, since 2026-10-25) makes `cell run` exit **70** with no `refusal.json` (`cell_cmds.go:91-93` returns the plain error -> `cellInternal`) |
| 065 §3 | Result file `automaticum.cell/1`, atomic, all listed fields | PARTIAL | written; `suite`/`subset` filled from the cell header; `cohort`, `declared_variable` filled (D120); `suite_version` unset |
| 065 §3 | Headline over n_expected; counts sum; errored tasks counted explicitly; warnings; complete=false implies warnings; runs.jsonl; UTC | BUILT | `cell.go:146-300` (`counted_as`) |
| 065 §3 | Suite-declared headline (e.g. resolved_rate), `empty_patch_rate` | SCHEDULED (m5) | spec 065 §3.1 says milestone 5 |
| 065 §3.1 | Mode C flags `--endpoint-kind`, `--caller-values`, `--basis-facts`, `--lanes`; `/props` contradiction -> SKIPPED `gate basis_facts`; 409 on reused key with other values; provenance `cell`; basis marks | BUILT | `daemon/modec.go:152-205`, `run/props.go:95-120`, `daemon.go:484-490` |
| 065 §4 | `automaticum reanalyse` re-derives detectors/metrics | BUILT | `api.go:303` |
| 065 §4 | `automaticum rescore` for scorers; re-scored cell gets new result file and keeps old | BUILT | D121: `internal/daemon/rescore.go`, `run.Rescore`/`run.Untar` (`internal/run/rescore.go`), `rescore_cmds.go`; TestRescore, TestRescoreVerdict, TestUntar |
| 065 §5 | `automaticum_result.json` + `runs.jsonl` per cell | BUILT | `cell_cmds.go:289` |
| 065 §5 | Basis in plain text per cell | BUILT | `basis.txt` |
| 065 §5 | Raw per-run artefacts by run id (transcript, requests as sent, patch/diff, scorer logs) | BUILT | `export run <run>... --out D [--kind K]` (report_cmds.go); no diff made, the workspaces are stored whole |
| 065 §5 | `automaticum export cohort <name> --format parquet\|jsonl` | PARTIAL | `export cohort --format jsonl\|csv` built (cohort_cmds.go, D120); Parquet not yet (DuckDB in the daemon) |
| 065 §5 | Layout option `<results>/<suite>/<served-model>/` (omnimergekit tree) | NOT BUILT | no code; 065 §3.1 lists the layout as still to agree |
| 065 §5 | Per-campaign export mirrorable to another host | BUILT | `export campaign <name> --out D` (report_cmds.go) |
| 065 §5 | Any slice as JSONL/CSV/Parquet; self-contained HTML report (090 §6) | PARTIAL | a cell as JSONL and CSV (`export cell`); slices, Parquet and the HTML report not built |
| 065 §5 | Private tasks never exported to cloud | BUILT | `daemon/export.go:63` (only export that exists) |
| 066 §1 | Source pinned at commit, image tags pinned by digest, canary kept as label | BUILT | `suite_cmds.go:41-49` (HEAD check, clean checkout), `tbench2.go:157,217,351` |
| 066 §1 | Never trained on: not a `train-*` suite, export refuses | BUILT | `daemon/export.go:63` |
| 066 §2 | Four tasks imported (regex-chess, circuit-fibsqrt, feal-linear-cryptanalysis, schemelike-metacircular-eval) | BUILT | `tasks/tbench2/` |
| 066 §2 | `path-tracing` (large wheels) | OPEN | O29 |
| 066 §2/§5 | Tasks whose image has no Python (polyglot-rust-c, write-compressor, build-pmars): pinned Python for the verifier | NOT BUILT | spec "Not built yet"; no milestone |
| 066 §3 | `suite import tbench2 -source -commit <task>...` refuses wrong HEAD / dirty checkout | BUILT | `cmd/automaticum/suite_cmds.go:41-49` |
| 066 §3 | Field mapping (prompt, limits, labels, seed with git=false, tests, wheels pinned, cpus/memory, gold fixture via solve.sh) | BUILT | `tbench2.go`, `import.go`; e.g. `tasks/tbench2/regex-chess/task.toml:67,71,86` |
| 066 §3 | Kept only if gold passes and seed fails, 3 times each; proof recorded | BUILT | `import.go:148-172` |
| 066 §3 | Lane layer: image by digest + Node/runner/rg under `/opt/automaticum`, `/app` -> `/workspace` | BUILT | `tbench2.go:428-445` |
| 066 §3 | Verifier zipapp, offline, pip in task image or official same-version Python; >1 MiB files as pinned artefact; built.txt for sdists (D114) | BUILT | `import.go:55-57,332,346-413`, `verifier/main.py`, `build.py` |
| 066 §3 | Oracle mounts `/oracle` read-only, `/app` `/tests` writable; ctrf plugin left out | BUILT | `verifier/main.py:7-47`, `tbench2.go:159,168` |
| 066 §3.1 | `import.toml` keys run_as, oracle_run_as, writable, collect, why; also the subset | BUILT | `tbench2.go:78-113`, `tasks/tbench2/import.toml` |
| 066 §4 | Differences (uid 1000, declared paths only, offline verifier, resources, storage_mb not applied, bridge network) | BUILT | by the above; descriptive |
| 066 §5 | Reference times on owner's arms | NOT BUILT | operational (needs runs of an owner-named arm), no code gap |

## Group C

### Audit C: spec vs code (070, 080, 090, 100, 110, README, decisions.md)

Repo state: main @ 180cc97 plus an untracked, unwired `internal/wait/wait.go` (created 2026-11-01 18:39; not referenced by any CLI command or daemon route).

Conventions: CLI dispatch = `cmd/automaticum/main.go:91` (one-word) and `:100-148` (noun verb). Routes = `internal/daemon/api.go:286-324`.
`--json` note: the flag is registered by `addPlace`/`addClient` (`cmd/automaticum/config_cmds.go:29-35`) on every command except `legacy import`, `store check`, `stub serve`, but it is only *honoured* by the daemon-client commands, `arm show` (`config_cmds.go:155`) and `sdk add` (`sdk_cmds.go:58`). It is accepted and ignored by `arm validate`, `arm diff`, `task ls|show|build`, `oracle verify`, `lane ls`, `endpoint ls`, `image build|ls`, `sdk ls|verify`, `suite import`, `export conversations`.

## 090 Interfaces

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 090 §1 | `<noun> <verb>` shape | BUILT | main.go:100-148 |
| 090 §1 | `--json` on every command | BUILT | every command (config, image, sdk, suite, export, store, stub, client commands) |
| 090 §1 | stable exit codes | BUILT | 0/1/2/3/6/7 (main.go exitOf, exit_test.go); wait 4/5; cell run 0/10/20/30/70 |
| 090 §1 | `status` | BUILT | client_cmds.go `status`, GET /v1/status |
| 090 §1 | `status --watch` (live output) | BUILT | `status --watch` (client_cmds.go statusOnce) |
| 090 §1 | progress bars | PARTIAL | reanalyse and cell run print progress; no general progress bars |
| 090 §1 | `sdk add` | BUILT | sdk_cmds.go:38 |
| 090 §1 | `sdk ls` | BUILT | sdk_cmds.go:72 (`--json` ignored) |
| 090 §1 | `sdk verify` | BUILT | sdk_cmds.go:114 (`--json` ignored) |
| 090 §1 | `doctor [--lane][--arm][--task][--all]` | BUILT | client_cmds.go:810-813, GET /v1/doctor |
| 090 §1 | `lane ls` | BUILT | config_cmds.go `laneLs` (`--json` ignored) |
| 090 §1 | `lane show` | BUILT | `GET /v1/lanes/{lane}` (lists.go), show_cmds.go |
| 090 §1 | `lane pause` | BUILT | handleLane("pause"), TestLanePause |
| 090 §1 | `lane resume` | BUILT | control_cmds.go laneControl(false), POST /v1/lanes/{lane}/resume |
| 090 §1 | `lane drain` | BUILT | laneControl(true), POST /v1/lanes/{lane}/drain |
| 090 §1 | `lane quarantine` | BUILT | handleLane("quarantine"), mutate_test.go |
| 090 §1 | `endpoint ls` | BUILT | config_cmds.go `endpointLs` (`--json` ignored) |
| 090 §1 | `endpoint probe` | BUILT | show_cmds.go endpointProbe (run.Probe) |
| 090 §1 | `endpoint show` | BUILT | show_cmds.go endpointShow |
| 090 §1 | `endpoint models` | BUILT | show_cmds.go endpointModels |
| 090 §1 | `arm ls` | BUILT | show_cmds.go armLs |
| 090 §1 | `arm show` | BUILT | config_cmds.go armShow |
| 090 §1 | `arm diff` | BUILT | armDiff (`--json` ignored) |
| 090 §1 | `arm validate` | BUILT | armValidate (`--json` ignored) |
| 090 §1 | `basis show` / `basis diff` | BUILT | compare_cmds.go basisShow/basisDiff, GET /v1/basis |
| 090 §1 | `task ls` / `task show` | BUILT | config_cmds.go (`--json` ignored) |
| 090 §1 | `task verify` | CHANGED (undocumented) | not built; `task build` exists instead (090 §1 "Built in milestone 2" lists `task ls|show|build`); no D row drops `verify` |
| 090 §1 | `oracle verify <task>` | BUILT | config_cmds.go oracleVerify |
| 090 §1 | `suite import tbench2 -source -commit <task>...` | BUILT | suite_cmds.go:27-29 |
| 090 §1 | `run start --arm --task [--task..] [--suite..] [-n] [--lane] [--wall-s] [--idle-s] [--capture-requests]` (+ `--order`, `--heldout`, `--dry-run`, `--idempotency-key`) | BUILT | client_cmds.go:172-181, POST /v1/runs |
| 090 §1 | `run ls` (with `--task suite/task`, `--suite`, `--model`, `--arm`, `--arm-id`) | BUILT | client_cmds.go:238-239, GET /v1/runs |
| 090 §1 | `run show` | BUILT | runShow, GET /v1/runs/{id} |
| 090 §1 | `run tail` (renders the transcript as it streams) | BUILT | ingest/render.go Renderer; `--raw`, `--max` |
| 090 §1 | `run stop|abort|pause|resume|invalidate|identify` | BUILT | main.go:105-109,129 + routes api.go:293-300 |
| 090 §1 | `run retry` | BUILT | control_cmds.go retry("run"), POST /v1/runs/{id}/retry |
| 090 §1 | `compare <batch> ...` | BUILT | compare_cmds.go:26, GET /v1/compare |
| 090 §1 | `batch start` | BUILT | alias of `run start` |
| 090 §1 | `batch ls` | BUILT | `GET /v1/batches` (lists.go), TestBatchesAndLaneShow |
| 090 §1 | `batch show|pause|resume` | BUILT | main.go:113,123-124 |
| 090 §1 | `batch abort` | CHANGED (undocumented) | `batch stop` (+`--abort` flag, control_cmds.go:57) instead; no D row |
| 090 §1 | `batch stop|retry|limits` (extras in "Built" paragraphs) | BUILT | main.go:125-127 |
| 090 §1 | `campaign start|ls|show|pause|resume` | BUILT | campaign_cmds.go, routes api.go:309-314 |
| 090 §1 | `campaign abort` | CHANGED (undocumented) | `campaign stop` instead; no D row |
| 090 §1 | `cell run` | BUILT | cell_cmds.go |
| 090 §1 | `cell show` | BUILT | show_cmds.go cellShow |
| 090 §1 | `cell ls` | BUILT | show_cmds.go cellLs (`GET /v1/batches?cell=1`) |
| 090 §1 | `cohort ls\|show` | BUILT | cohort_cmds.go, `GET /v1/cohorts[/{name}]`, D120 |
| 090 §1 | `report run\|batch\|cell\|ab\|regression\|census` | BUILT | report_cmds.go; regression `GET /v1/compare?split=build`, census `GET /v1/report/census` (report.go), TestReportRegressionAndCensus |
| 090 §1 | `find "<query>" [--semantic] [--where]` | SCHEDULED | M6 (search); no code |
| 090 §1 | `logs <run>` with `--events`, `--wire` (+ `--runner`, `--oracle`, `--kind`) | BUILT | client_cmds.go:457-463 |
| 090 §1 | `logs --server` / `--runtime` | NOT BUILT | no flags; server/runtime logs are not collected |
| 090 §1 | `logs --grep` / `--turn N` | BUILT | client_cmds.go logs (`--grep`, `--turn`, `--render`, `--max`) |
| 090 §1 | `notices (<run>... | --batch B...) [--key K]` | BUILT | client_cmds.go:501-503 (D116) |
| 090 §1 | `replay <run> --turn N [--endpoint E]` | SCHEDULED | M6 (replay); no code |
| 090 §1 | `detect <file|run> [--runaway N]`, `--self-test` (+`--first-match`) | BUILT | client_cmds.go:720-722, GET /v1/runs/{id}/detect |
| 090 §1 | `embed start|status|stop [--where]` | SCHEDULED | M6 (semantic search); no code; also D52 |
| 090 §1 | `reanalyse [--run|--batch] [--imported]` | BUILT | client_cmds.go:645-647, POST/GET /v1/reanalyse |
| 090 §1 | `reanalyse --detector D` / `--where` | BUILT | ReanalyseRequest.Where/Detector, whereFilter, TestWhereFilter |
| 090 §1 | `rescore <cell>` | BUILT | `rescore <batch> [--out D]` (rescore_cmds.go), D121 |
| 090 §1 | `export run\|cell\|cohort\|campaign` | BUILT | run, cell, campaign (report_cmds.go), cohort (cohort_cmds.go, D120) |
| 090 §1 | `export conversations --out [--batch][--run][--heldout]` | BUILT | export_cmds.go:35-38, GET /v1/runs/{id}/export |
| 090 §1, §5 | `wait run\|batch\|cell <id> [--until]` | BUILT | bbdd18f, ec5de3c: internal/wait, `GET /v1/wait/{kind}`, wait_cmds.go |
| 090 §1 | `agent` (agent mode) | SCHEDULED | M7 |
| 090 §1 | every mutating command has `--dry-run` | BUILT | every mutating command (see 050 §4 row) |
| 090 §1 | every mutating command accepts `--idempotency-key` | BUILT | every daemon command; local commands repeatable by construction (090 §1 as built) |
| 090 §1 | listed extras: `legacy import`, `store check`, `image build|ls`, `stub serve`, `task build` | BUILT | main.go:130-147 |
| 090 §1 | API: HTTP+JSON on loopback; token for changes; local reads without it | BUILT | api.go:325-348 `allowed` |
| 090 §1 | a run in flight streamed as SSE | BUILT | api.go:1079-1097 GET /v1/runs/{id}/stream |
| 090 §1.1 | one human formatter (D69) | BUILT | internal/human; `--json`/API unformatted |
| 090 §2 | TUI, four view groups (D26, D33) | SCHEDULED | M6 |
| 090 §3 | REST+JSON for everything the CLI does | PARTIAL | config-side commands (arm/task/oracle/lane/endpoint/sdk/image/suite/export conversations batch-level) have no API route; M6 hardening |
| 090 §3 | SSE/WebSocket live events | BUILT | SSE only (api.go:1097) |
| 090 §3 | token auth with read-only mode | PARTIAL | token yes; "read-only" = loopback GETs only, no share token; M6 |
| 090 §3 | every list paged | SCHEDULED | M6 hardening; only `limit` on GET /v1/runs (api.go:744), no paging |
| 090 §3 | every object has a `brief` form | SCHEDULED | M6 hardening; none |
| 090 §3 | structured errors (code, message, gate) | SCHEDULED | M6 hardening; errors are `{"error": "..."}` only (api.go:264-277) |
| 090 §4 | web dashboard (all bullets incl. read-only share) | SCHEDULED | M6 |
| 090 §5 | MCP server | SCHEDULED | M6 |
| 090 §5 | server-side "wait for" primitives | NOT BUILT | see `wait` row; not assigned in 110 §3 table except via MCP (M6) |
| 090 §5 | `brief` report format | NOT BUILT | no code; not explicitly in 110 §3 |
| 090 §5 | notifications: webhook on campaign end, refusing gate, stopped batch | BUILT | daemon.go:657-690; campaign.go:254, daemon.go:488, canary.go:118 |
| 090 §5 | notifications through the mailbox | NOT BUILT | carried forward from M3 (110 §3.2), no milestone |
| 090 §5 | dry run and idempotency keys (as CLI) | PARTIAL | as §1 |
| 090 §6 | transcript viewer, aligned server/runtime log | SCHEDULED | M6 (TUI/dashboard view group 4) |
| 090 §6 | full-text/regex search, filters, archived runs | SCHEDULED | M6 |
| 090 §6 | semantic search with coverage | SCHEDULED | M6 |
| 090 §6 | saved queries, queries as report columns | SCHEDULED | M6 (search) |
| 090 §6 | replay | SCHEDULED | M6 |
| 090 §6 | export of any slice + self-contained HTML report (065 §5) | NOT BUILT | no HTML report code; not assigned |
| 090 §7 | embedders | SCHEDULED | M6 |
| 090 §7 | chat box | SCHEDULED | M6 |
| 090 §7 | model-written run summaries / finding triage | NOT BUILT | not assigned |
| 090 §7 | private task text never to a cloud endpoint | PARTIAL | flag documented on task (task.go:43); no enforcement code found (no embedder/cloud check) |

## 070 Diagnostics

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 070 §1 | everything automatic at ingest, as CLI + API + dashboard view | PARTIAL | ingest + CLI/API yes; dashboard M6 |
| 070 §1.1 | `run show`: verdict, evidence, metrics, findings, artefact list | BUILT | runShow / GET /v1/runs/{id} |
| 070 §1.1 | arm fields differing from batch baseline highlighted | OPEN | O40: "the batch's baseline" is undefined (a batch is one arm); asked 2026-11-02 |
| 070 §1.1 | timeline | BUILT | `run timeline` (timeline_cmds.go), `Events.KeepTimeline`; TestTimeline |
| 070 §2 | tokens from server `usage` | BUILT | tokens.prompt/.cached/.fresh/.completion (ingest) |
| 070 §2 | time: wall, first token, in tools vs model | BUILT | time.wall_ms, first_token_ms, tools_ms, model_ms, agent_ms |
| 070 §2 | time per turn; idle gaps | BUILT | `time.turn_median_ms`, `time.turn_max_ms`, `time.gap_max_ms` (analysis 8); TestTurnTimes |
| 070 §2 | volume: requests, turns, tool calls by tool, failures by tool | BUILT | tool_calls.by_tool.*, tool_calls.failed.by_tool.* |
| 070 §2 | tool failures by error class | BUILT | `tool_calls.failed.by_class.<class>`, `ingest.ErrorClass` (analysis 9); TestErrorClass |
| 070 §2 | completion split reasoning/text/tool input; rates prompt/gen | BUILT | tokens.completion.reasoning/text/tool_input, rate.*_tok_s |
| 070 §2 | rates per request and with concurrency | PARTIAL | per request built (`rate.*.per_request`, analysis 9); the concurrency measured at is not recorded |
| 070 §2 | length distributions (tokens) n,sum,p10,p50,p90,max | BUILT | tokens.completion.per_request.* |
| 070 §2 | length distributions in characters, reasoning alone, text alone | PARTIAL | thinking.chars(.max), text.chars; not full p10..p90 sets for each |
| 070 §2 | finish reasons histogram; empty completions | BUILT | finish.*, completions.empty |
| 070 §2 | thinking: blocks, turns without | BUILT | thinking.blocks, thinking.turns_without |
| 070 §2 | thinking: tokens per block median/p90, budget hits, thinking-only turns | BUILT | `thinking.tokens.per_block`, `thinking.budget_hits`, `turns.thinking_only` (analysis 9); TestTurnMetrics |
| 070 §2 | context: first, last, peak, share, cached share, compactions count | BUILT | context.* |
| 070 §2 | compaction size before/after, duration, tokens, trigger; overflow errors; full re-reads with cause | BUILT | `context.compaction.*`, `context.compactions.by_cause.*`, `context.rereads.<cause>`; overflow as `notices.status.overflow_recovery_compaction.*` (analysis 9) |
| 070 §2 | reasoning carried between turns | BUILT | `context.reasoning_carried.requests/chars` from the wire's history (in characters; the server gives no tokens for it) |
| 070 §2 | edits (count, churn, reverts, edit-without-check) | PARTIAL | `edits`, `.failed`, `.files`, `.restores`, `.without_check` (D127); churn and same-region not built |
| 070 §2 | change protocol (transactions, checks, rollbacks) | BUILT | `transactions.*` from the SDK's `atomic_transaction`, `atomic_empty_attempt`, `atomic_plan` notices (analysis 10); TestStruggleSignals |
| 070 §2 | escalation | BUILT | `escalation.*` from the SDK's expert_reply/escalation_failed notices; offers and calls as notices (analysis 9) |
| 070 §2 | sub-agents | NOT BUILT | no metric |
| 070 §2 | cost in tokens and money | BUILT | tokens.*; `[price]` on the endpoint (030 §4.2), recorded per run, `cost.money` (analysis 11); TestMoney |
| 070 §2 | scripted user `user.*`, hidden where zero (D88) | BUILT | user.actions/denials/failed/forks/mode_switches; compare.go:234,266 |
| 070 §2 | guard trips and stops by guard/phase/cause (D99, D100) | BUILT | guard_trips.*, guard_stop.* (compare.go:266) |
| 070 §2 | notices by kind `notices.<type>.<kind>.<phase>` (D116); compaction counted once | BUILT | ingest (status.reasoning_loop.cut etc.) |
| 070 §2.1 | every distribution over a cell's runs | BUILT | p10/p90 added to every summary (stats.Summary) |
| 070 §2.1 | token p50/p90 over PASS runs only | BUILT | `token_stats_pass` in the result file (cell.go verdictNumbers); TestVerdictNumbers |
| 070 §2.1 | tokens per solved task | BUILT | compare.go:61,403 |
| 070 §2.1 | throughput over whole cell | BUILT | `scores.throughput_tok_s` |
| 070 §2.1 | sanity warnings: n vs expected, empty >5%, p10 visible chars under floor | PARTIAL | empty >5% warning built (`empty_completion_share`); n always equals the runs queued; the floor is declared in `suites/*.toml` (m5), as the spec now says |
| 070 §3 | Go detector library, via CLI and API (D24) | BUILT | internal/detect; `detect`, GET /v1/runs/{id}/detect |
| 070 §3 | short-unit repeat, char run, sentence repeat (§3.1) | BUILT | internal/detect/detect.go, reference_test.go |
| 070 §3 | runaway (per-suite declared size) | PARTIAL | detector and `detect --runaway N` built; the per-suite size is declared in `suites/*.toml` (m5), as the spec now says |
| 070 §3 | repeated tool call | BUILT | detectors.go:103-105 |
| 070 §3 | near-duplicate rounds | NOT BUILT | D129: held until the owner defines it |
| 070 §3 | edit oscillation | BUILT | D129: `edit_oscillation`, the call guard on writing tools (library 3) |
| 070 §3 | re-read loop | BUILT | D129: `reread_loop`, the call guard's same-result rule on other tools (library 3) |
| 070 §3 | per-block flags kept separate + looped OR | BUILT | detector.<name>.<channel>, detector.looped_blocks |
| 070 §3 | detectors evaluated per run and per cell | PARTIAL | per run yes; per cell only `loop_rate` + degeneration counts (cell.go:242) |
| 070 §3.1 | controls per domain, self-test at start, in result file | BUILT | detect controls; daemon start; cell.go:279 |
| 070 §3.1 | `loop_rate` with its components (detector, channel, turn) | BUILT | `loop_components` in the cell result (cell.go `loopComponents`); TestLoopComponents |
| 070 §3.1 | always cross-tabulated with verdict and cap hits | BUILT | `loop_by_verdict` in the result file |
| 070 §3.1 | suite subset may declare `loop_rate` as headline | SCHEDULED (m5) | the declaration is `suites/*.toml` (030 §1, m5); the spec says so; headline is pass_rate until then |
| 070 §3.1 | report says which runs had a guard fire | BUILT | guard_trips metrics in run show/compare |
| 070 §3.1 | reanalyse recomputes over corpus incl. imported (D65) | BUILT | reanalyse `--imported` |
| 070 §3.2 | library v2: reasoning_repetition, reasoning_loop, repeated_tool_call from SDK (D75) | BUILT | internal/detect/guards.go |
| 070 §3.2 | unavailable detector writes `.available = 0` | BUILT | detectors.go:65-69 |
| 070 §3.2 | every match + `matching` field + `--first-match` (D76) | BUILT | detectors.go:19,49; client_cmds.go:722 |
| 070 §3.2 | imported runs: blocks rebuilt, detector findings only | BUILT | ingest events (legacy stream mapping) |
| 070 §3.2 | criterion 8 / TestCohorts | BUILT | internal/detect/reference_test.go |
| 070 §3.2 | failed identical calls (6/4) and repeated call+result pair (3/6) | BUILT | `detect.CallGuard` (callguard.go), `failed_identical_call`, `repeated_call_result`; TestCallGuard |
| 070 §3.2 | `detect.Spans` | BUILT | internal/detect/spans.go |
| 070 §4 | guards: what fired, when, what it cut, what the model did next | BUILT | `guard_trip` finding, `guard_trips.next.<act>` (analysis 9) |
| 070 §4 | guards that should have fired (rule replayed); false-positive review queue | BUILT | `guard_replay` finding; `report guards` (`GET /v1/report/guards`); TestReportGuards |
| 070 §4 | tool-call findings (parse path, unreadable, truncated, unknown names, schema violations, arrays as strings, intent scratch, 5xx apart, no-call turns) | NOT BUILT | only failed-by-tool metrics |
| 070 §4 | output hygiene findings (empty turns, control tokens, leaks, unclosed thinking, fences...) | PARTIAL | `control_markers`, `turns.short_text`, `completions.empty`; fences, answers only in reasoning, stop inside reasoning not built |
| 070 §4 | turn pathologies | PARTIAL | D128: thinking-only turns, ended thinking-only, premature done = `claimed_success_oracle_no`; refusal and report-after-early-end held |
| 070 §4 | parameter drift | BUILT | wire_arm `drift`/`differs`/`sent_not_stated` (070 §4.1) |
| 070 §4 | server side: finish per request, reloads, GPU residency | PARTIAL | finish.*, model_reloaded, gpu_residency; no truncations/slot reuse/evictions/admission/restarts/server-log errors |
| 070 §4 | integrity: oracle tampering, workspace not pristine | PARTIAL | tampering = verdict, pristine = gate `workspace`; files outside the workspace: `outside_workspace` from the container's changes (analysis 11, TestIntegrity, TestRunChanges); sandbox escapes and in-workspace obstacle changes not built |
| 070 §4 | struggle signals with recall / false alarm | BUILT | D126: `struggle_signals` finding, `detect.Struggle` (the SDK's detector replayed), `report struggle`; TestStruggle*, TestReportStruggle |
| 070 §4 | session forks `session_fork` (D86) | BUILT | ingest/analyse.go:46 |
| 070 §4 | outcome mismatches (claimed vs oracle, fixed then broken) | PARTIAL | claimed vs oracle built (`claimed_success_oracle_no`); fixed-then-broken needs periodic oracle sampling (060 §3.1) |
| 070 §4.1 | `wire_arm` v2 kinds stated_not_sent / sent_not_stated / differs / drift (D63) | BUILT | ingest/wirearm.go |
| 070 §4.1 | `model_reloaded`, `gpu_residency` -> INVALID unless `[accepts]` (D73); reanalyse undoes own INVALID | BUILT | ingest/analyse.go:36-40; config/arm.go accepts |
| 070 §4.1 | reload after pause kept as note, endpoint.reloads_after_pause (D85) | BUILT | metric endpoint.reloads_after_pause |
| 070 §5 | cap hits per run at any turn; `cap.hits`, `cap.declared_tokens`, finding `cap_hit` | BUILT | ingest/cap.go |
| 070 §5 | per-cell capped runs/requests/passed; CLEAN/CAPPED/UNVERIFIED | BUILT | compare.go:73-145 |
| 070 §5 | cap asymmetry between arms | BUILT | compare.go:166-185 |
| 070 §5 | reasoning-budget regime recorded | BUILT | compare.go:120 (thinking.budget_tokens.served) |
| 070 §5 | derived answer allowance band; pile-up test | SCHEDULED | M5 (single-turn suites), per spec |
| 070 §6 | `compare`: Wilson, Fisher, Mann-Whitney, medians/ranges/swing, basis diff above, cap per pair, denominators | BUILT | compare.go:394-429; stats.go |
| 070 §6 | streaks; "how many more runs to tell apart" | BUILT | streaks built (CompareGroup.Streaks); "how many more runs" not adopted (O39) |
| 070 §6 | paired comparison (McNemar) | BUILT | stats.McNemar; ComparePair.Paired; TestMcNemar |
| 070 §6 | unpaired with bootstrap; statement whether distinguishable | BUILT | the spec asks "Mann-Whitney or bootstrap": Fisher and Mann-Whitney built; the "distinguishable" statement is not adopted (D78, O39) |
| 070 §6 | jitter band | NOT ADOPTED | O39 |
| 070 §6 | zero/null result carries its power | NOT ADOPTED | O39 |
| 070 §6 | distributions on request | PARTIAL | `--all` shows every metric, not distributions |
| 070 §6 | group by any arm field, task label or finding | BUILT | `compare --group arm.F|label.L|finding.D` (`?split=`), n and arm ids per group; TestCompareSplits |
| 070 §6 | census of tool failures | PARTIAL | `report census` with error classes (analysis 9); parse paths unrecorded: the SDK keeps them on its messages, not in the event stream |
| 070 §6 | regression view across SDK versions | BUILT | `report regression` (compare split by harness build) |
| 070 §6 | comparability: refusal to pool across task/oracle/scoring/basis | PARTIAL | groups never pool across tasks; basis differences are shown, not refused; no exclusion list for oracle/scoring boundaries |
| 070 §6 | lane skew | BUILT | `report skew` (`?split=lane`): one group per lane host, the same arm's paired; TestCompareSplits |
| 070 §6 | served-model facts per arm and change between runs (D82) | BUILT | compare.go:58 `Served` |

## 080 Store

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 080 §1 | only the daemon holds the store; Windows/CLI without (D40) | BUILT | store_cgo.go / store_nocgo.go |
| 080 §1 | survive a kill during a write | BUILT | blob tmp+rename; storetest/crash_test.go |
| 080 §2 | blobs + DuckDB used by the daemon (D12, D46) | BUILT | cmd/automaticumd/main.go:84 + duck |
| 080 §2 | Lance for chunks/vectors/FTS in the running system | SCHEDULED | M6 search; library exists (internal/store/lance) but daemon does not link it |
| 080 §3 | write order blobs -> DuckDB -> Lance | PARTIAL | blobs then DuckDB; no Lance writes |
| 080 §3 | metrics/findings rebuilt by reanalyse | BUILT | reanalyse |
| 080 §3 | non-derivable state written transactionally and backed up | PARTIAL | transactional in DuckDB; no backup |
| 080 §3 | consistency check: referenced blobs exist and hash | BUILT | store_cgo.go:155-233 (blob_refs + Check) |
| 080 §3 | consistency check: every run has its chunks | NOT BUILT | no chunks are written |
| 080 §4 | sha256 + zstd, content-addressed | BUILT | blob/FORMAT.md, put.go |
| 080 §4 | wire captures deduplicated by message part (D47) | NOT BUILT | no part-level storage; Put stores whole blobs only (put.go:25) |
| 080 §4 | temp name + rename | BUILT | put.go:19 |
| 080 §4 | range-readable streams | BUILT | read.go:194 ReadRange (no viewer uses it yet) |
| 080 §5 | results never deleted, nothing pruned | BUILT | no delete path |
| 080 §5 | automatic archiving by age, verified, then freed (D31) | NOT BUILT | `blob.ArchiveOlderThan` (archive.go:40) exists but nothing in the daemon calls it; no age setting in config/local.go (only archive_root) |
| 080 §5 | archived blob addressable, reads fall through | BUILT | read.go:49-80 |
| 080 §5 | reports say when a query waited on the archive | NOT BUILT | none |
| 080 §5 | archive unreachable: nothing moved, daemon notifies | PARTIAL | library refuses (archive_test.go TestArchiveRootMissing); no daemon mover, no notification |
| 080 §5 | free-space floor stops new runs | BUILT | gate.go:365-385 |
| 080 §5.1 | archived logs stay searchable (D36) | PARTIAL | readers fall through; search itself M6 |
| 080 §5.1 | regex over raw blobs incl. archive, with archive count | SCHEDULED | M6 |
| 080 §5.1 | archived unreadable blob reported per result | PARTIAL | read.go errors name hash and expected path; no search results yet |
| 080 §5.1 | consistency check covers both tiers | BUILT | check.go tiers(); store check prints live/archived |
| 080 §5.2 | ingest writes chunks with text, no vector | NOT BUILT | daemon has no Lance; D58 placed this in M3 and it was not built |
| 080 §5.2 | operator-started embedding pass, stop/resume, progress, index build (D52) | SCHEDULED | M6; no `embed` command |
| 080 §5.2 | semantic results state embedded share; no mixed embedders | SCHEDULED | M6; Chunk.Embedder field exists (store.go:230) |
| 080 §6 | one small Go store interface | BUILT | internal/store/store.go |
| 080 §6 | schema migrations | PARTIAL | schema_version table, add-only (duck.go:115-135); no real migration steps |
| 080 §6 | backup | NOT BUILT | no backup code |
| 080 §6 | import of the old corpus | BUILT | internal/legacyimport |
| 080 §6 | data root with free-space floor | BUILT | config/local.go:87, gate |
| 080 §7.1 | L2 lance-c wrapper (D44), own RRF (D41), manylinux_2_28 (D45) | BUILT | lance.go:48,593; build/Dockerfile.build:12 |
| 080 §7.4 | regex over non-delta lines by default (D48) | SCHEDULED | M6 |
| 080 §7.4 | refine factor >= 30 by default (D49) | BUILT (library) | lance.go:580-582; unused by daemon until M6 |
| 080 §7.4 | chunk locator fields (D50) | BUILT (library) | store.go:224-228 |
| 080 §7.4 | daemon rebuilds indexes on a schedule | NOT BUILT | BuildIndexes not called by daemon |
| 080 §7.2-7.4 | spike T1-T15 measured | BUILT | spike/001-store/REPORT.md |

## 100 Legacy import

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 100 §2 | run ids kept; sibling-lane prefix | BUILT | legacyimport mapping, store_cgo.go `--lane` |
| 100 §2 | verdict mapping FIXED/broken/ROLLBACK-FAILED, original word kept | BUILT | mapping.go:31-39 |
| 100 §2 | known-invalid runs from notes -> INVALID | BUILT | secondpass.go:92-98 |
| 100 §2 | CLI wrapped stream mapped to SDK shape (at read) | BUILT | stream.go |
| 100 §2 | per-run text files -> provenance and blobs | BUILT | scan.go, mapping.go |
| 100 §2 | arm reconstructed; unknown never default | BUILT | mapping.go:339 ("as configured"), verify.go:251 |
| 100 §2 | imported flag + importer version | BUILT | doc.go:14, mapping.go:200 |
| 100 §3 | task versions 1-3 + v1 finding "could not fail" | BUILT | mapping.go:389,482-490 |
| 100 §3 | change days imported as further boundaries | BUILT | importer.go:157 `Boundaries`, mapping.go:61 |
| 100 §4 | reports weaken/flag "as configured" vs "as sent" when mixed | NOT BUILT | value recorded (store.go:96) but no report says so |
| 100 §4 | runs without oracle keep analysis-script verdict, marked | PARTIAL | not confirmed in code |
| 100 §5 | scripts replaced: loops, drivers, chains, gates, run report, metrics, statistics | BUILT | scheduler, arms, campaigns, gates, run show, ingest, compare |
| 100 §5 | A/B report and auto-score | PARTIAL | `compare`; no `report ab` |
| 100 §5 | transaction watcher -> live events and watchdogs | PARTIAL | SSE stream + wall/idle limits; not separately verified |
| 100 §5 | tool-failure census | NOT BUILT | see 070 §6 |
| 100 §5 | escalation study -> struggle signals | BUILT | D126; see 070 §4 |
| 100 §5 | struggle calibration -> reanalyse | BUILT | `reanalyse` applies analysis 10's signals; `report struggle` measures them |
| 100 §5 | census scripts -> findings (leaks, missing paths) | NOT BUILT | see 070 §4 |
| 100 §5 | server-log fetcher -> endpoint log source; server run recorder -> endpoint recorder | NOT BUILT | no server-log collection (also `logs --server`) |
| 100 §5 | Windows log grabber -> native agent | SCHEDULED | M7 |
| 100 §5 | state archivers, snapshots, stripper -> store | BUILT | blobs |
| 100 §6/§7 | import reproduces verdict files exactly; 474 runs | BUILT | verify.go:107-173 |
| 100 §7 | 30 earlier campaigns imported only when needed (D51) | DROPPED/CHANGED BY DECISION | D51 |
| 100 §7 | per-loop result files; VS Code session archive | NOT BUILT | spec: "Not imported yet" |
| 100 §7 | export of imported runs passes redaction (010 §5) | NOT BUILT | no redaction code anywhere; moot today because export refuses non-training/no-capture runs |
| 100 §8 | second pass: INVALID list, ABORTED/TIMEOUT remaps with documented/reported/inferred, sibling lanes (D56) | BUILT | secondpass.go; importer 0.2.0 |

## 110 Acceptance and roadmap

| spec § | item | status | evidence / notes |
|---|---|---|---|
| 110 §1.1 | manic_miner ported, oracle verify passes | BUILT | tasks/custom/manic_miner; oracleVerify |
| 110 §1.2 | historical arm re-run within stated tolerance; corpus imported, counts exact | PARTIAL | import exact (100 §7); 110 §3.1 compares but states it is not like-for-like and no tolerance is stated |
| 110 §1.3 | every 000 §1.1 failure is gate/INVALID/finding with injected-fault test | PARTIAL | 110 §3.2: 7 of 10 with tests; session-blob deletion has no test; scaffold = M5 |
| 110 §1.4 | overnight A/B campaign across two hosts, paused/resumed, lane killed, report | SCHEDULED | needs M4 + M7 (agent mode); not done |
| 110 §1.5 | each §2 question answered by one command | PARTIAL | see §2 rows |
| 110 §1.6 | BFCL category + SWE-bench Lite shard end to end | SCHEDULED | M5; no BFCL/SWE-bench code |
| 110 §1.7 | omk_eval runs, kills, resumes a cell with the same key | PARTIAL | mechanism built (cell_cmds.go:49, cell.go resume); the omk_eval end-to-end acceptance is not recorded in the spec |
| 110 §1.8 | detector controls + reference reproduction | BUILT | 070 §3.2, reference_test.go (for the requirement-defined detectors) |
| 110 §2 | which runs of this arm looped, where, on what text | PARTIAL | per run (`detect <run>`, run show); compare gives counts per arm, no where/text across runs |
| 110 §2 | did the sampler reach the wire in every request of this batch | PARTIAL | per-run wire_arm findings; compare counts runs with wire_arm; no per-field batch answer |
| 110 §2 | what differs between two arms / builds / cells' bases | BUILT | arm diff, basis diff, compare basis |
| 110 §2 | which guard fired in failed runs and what happened after | PARTIAL | guard_trips metrics, notices; no FAIL filter, no "after" |
| 110 §2 | every unreadable tool call, by model | NOT BUILT | no unreadable-call finding |
| 110 §2 | thinking stopped after template leak this week | NOT BUILT | no leak finding |
| 110 §2 | how many requests re-read the whole prompt, and why | NOT BUILT | no metric |
| 110 §2 | is B better than A, how many more runs | PARTIAL | compare (Fisher, Wilson); no "how many more runs" |
| 110 §2 | which runs were on a model not fully on the GPU | PARTIAL | gpu_residency finding/INVALID; run ls cannot filter by finding |
| 110 §2 | turn 41 of run X as the model saw it + server log for those seconds | PARTIAL | `logs --wire` dumps all requests; no `--turn`, no server log |
| 110 §2 | find runs where the model concluded the checker was wrong | SCHEDULED | M6 semantic search |
| 110 §2 | what the last 20 FAILs had in common vs PASSes | NOT BUILT | none |
| 110 §2 | tokens per solved task; any cell capped | BUILT | compare |
| 110 §3 | M1 store spike and importer | BUILT | 080 §7.4, 100 §7 |
| 110 §3 | M2 walking skeleton | BUILT | daemon, CLI, lane, runner, manic_miner, verdicts |
| 110 §3 | M3 gates, metrics, detectors, doctor, reanalyse, run report | PARTIAL | built except carry-forwards: reanalyse --detector/--where, timeline/baseline, mailbox, 3 detectors, agent-mode gates (M7) |
| 110 §3 | M4 batches, cells, statistics | PARTIAL | scheduler, interleave, campaigns, control, recovery, basis, cell run, cap audit, paired statistics (McNemar), group-by and lane skew built |
| 110 §3 | M5 suites and adapters (suite interface, SWE-bench, BFCL, in-container adapter, multi-language reqs, Gitea) | SCHEDULED | only tbench2 importer and train-* tasks exist; no BFCL/SWE-bench/adapter/Gitea |
| 110 §3 | M6 interfaces | SCHEDULED | none built |
| 110 §3 | M7 remote and native | SCHEDULED | none built |
| 110 §3 | extension parity across milestones (scripted user, approvals, steering, resume, checkpoints; emulated diagnostics M5) | PARTIAL | scripted user, forks, plan/act built (040 §6.2); emulated diagnostics M5 |
| 110 §3.2 | criterion 3 table rows (verdicts, oracle proof, wire_arm, read-back, harness_build, run invalidate, recover, template gate, free gate, workspace) | BUILT | tests named in the table exist in internal/run, gate, ingest, daemon |
| 110 §3.2 | test that deletes a run's session blob | NOT BUILT | spec says none |
| 110 §3.2 | first-generations canary (M4) | BUILT | ingest/canary.go:10, daemon/canary.go |
| 110 §3.2 | "score mostly the scaffold" (engine as arm part) | SCHEDULED | M5 |
| 110 §3.5 | capture-requests per batch and campaign (D104) | BUILT | client_cmds.go:180, campaign.go:141 |
| 110 §3.5 | export conversations: layout, guard spans, detector spans, repeat-call spans, capped, heldout (D102/D106/D115/D117) | BUILT | internal/export/export.go, daemon/export.go |
| 110 §3.6 | suite import tbench2, 4 tasks proven | BUILT | internal/suite/tbench2; tasks/tbench2 |
| 110 §3.6 | path-tracing (O29) | OPEN | O29 |

## README.md claims

| spec § | item | status | evidence / notes |
|---|---|---|---|
| README status | "milestone 3 of 7 built ... no batches, statistics or dashboards yet" | STALE | batches, campaigns, cells, compare statistics exist (M4 largely built); claim understates |
| README core | run record proving what was sent, read-back, enum verdicts, oracle by harness, gates | BUILT | runner, run, gate |
| README core | diagnostics: loops, guard firings | BUILT | detect, guard_trips |
| README core | diagnostics: unreadable tool calls, template leaks, server-side events, integrity | NOT BUILT / PARTIAL | see 070 §4 rows |
| README core | statistics: intervals, "distinguishable at this n", refusal to pool across boundaries | PARTIAL | Wilson/Fisher/MW; no power/distinguishability (D78), pooling refusal only by task |
| README arch | API with SSE/WebSocket; scheduler; Docker lanes; ingest; query+stats | BUILT | SSE only |
| README arch | web client, agent client, embedder client, Gitea client | SCHEDULED | M6, M7, M6, M5 |
| README arch | Remote hosts in agent mode | SCHEDULED | M7 |
| README suites | Custom tasks, manic_miner | BUILT | tasks/ |
| README suites | BFCL two cells; SWE-bench; other-language SWE sets | SCHEDULED | M5 |
| README suites | multi-language suite C#, C/C++, Python, TS, Go, Rust | BUILT | tasks/train-ml (12 tasks), images/layers |
| README suites | suite is a plugin; engine adapters | SCHEDULED | M5 |
| README interfaces | CLI with `--json` and stable exit codes | PARTIAL | see 090 §1 |
| README interfaces | full terminal dashboard | SCHEDULED | M6 |
| README interfaces | HTTP API and live dashboard | PARTIAL / SCHEDULED | API built; dashboard M6 |
| README interfaces | MCP server, server-side wait-until primitives, dry-run, idempotency keys | NOT BUILT / SCHEDULED | MCP M6; wait not built; dry-run/idem partial |
| README interfaces | search + replay | SCHEDULED | M6 |
| README commands | `report ab`, `find` | NOT BUILT / SCHEDULED | report not built; find M6 |
| README layout | `build/` described as Debian 11 build container | STALE | manylinux_2_28 (D45) |

## decisions.md D rows

| spec § | item | status | evidence / notes |
|---|---|---|---|
| D1, D2 | Go daemon; only Go on host | BUILT | cmd/automaticumd |
| D3 | Node only in containers (lane image, external harness container) | BUILT (lane) / SCHEDULED (adapter, M5) | images/base, runner |
| D4 | Docker lanes, base + one layer per language | BUILT | images/ |
| D5, D30 | Gitea, one per daemon | SCHEDULED | M5; no gitea code |
| D6, D12, D46 | store shape DuckDB + Lance + blobs | PARTIAL | DuckDB + blobs live; Lance library only (M6) |
| D7 | specs only | DROPPED/CHANGED BY DECISION | superseded by D29 |
| D8, D35 | agent mode only for remotes; native agents | SCHEDULED | M7 |
| D9 | TOML config; tasks in repo; large suites fetched | BUILT | internal/config, tasks/, suite import |
| D10 | one write token + read-only share mode | PARTIAL | token built; share mode only loopback reads (M6) |
| D11 | six verdicts, original word kept | BUILT | store.Verdict; legacy OriginalVerdict |
| D13 | full parity with VS Code extension | PARTIAL | scripted user, forks, modes built; emulated diagnostics M5 |
| D14 | neutral host names | BUILT | policy |
| D15 | first languages incl. C# | BUILT | images/layers/{typescript,python,go,rust,cpp,dotnet}, train-ml |
| D16 | paused intervals excluded, run flagged | BUILT | run.Paused, wall excludes |
| D17 | periodic oracle sampling optional per task | NOT BUILT | no sampling field in internal/task/task.go |
| D18, D60 | guards/compaction stated; base states all on | BUILT | examples/config/arms/base.toml, config/arm.go |
| D19 | adapter installs runner in harness container | SCHEDULED | M5 |
| D20, D97 | omk_eval owns server in mode C; automaticum never launches a server | BUILT | daemon/modec.go; no server launch code |
| D21 | BFCL as two cells | SCHEDULED | M5 |
| D22 | no completion-form tasks | BUILT | n/a (policy) |
| D23 | other-language SWE-bench sets external, C/C++ first | SCHEDULED | M5 (O5 open) |
| D24 | Go detector library | BUILT | internal/detect |
| D25 | VS Code-only behaviour emulated as arm fields, recorded | SCHEDULED | M5 (emulated diagnostics); no emulation code |
| D26, D33 | TUI with four view groups | SCHEDULED | M6 |
| D27 | SDK fixes on Cerebriline side | BUILT | external (X rows) |
| D28, D29 | spike first; roadmap order | BUILT | M1 done |
| D31 | automatic archiving by age, verified | NOT BUILT | library function only, not wired; no age setting |
| D32 | plugins compiled in | BUILT | n/a |
| D34 | runner observes SDK retries | BUILT | runner |
| D36 | archived logs stay searchable | PARTIAL | read fall-through built; search M6 |
| D37 | Apache-2.0 | BUILT | LICENSE |
| D38, D44 | Lance binding by spike -> lance-c wrapper | BUILT | internal/store/lance |
| D39, D45 | linking posture; manylinux_2_28 | BUILT | build/Dockerfile.build |
| D40 | only daemon holds store; Windows without | BUILT | store_nocgo.go |
| D41 | own rank fusion | BUILT | lance.go:48,593 |
| D42, D43 | spike thresholds and data | BUILT | REPORT.md |
| D47 | wire capture dedup by message part | NOT BUILT | whole-blob storage only |
| D48 | regex over non-delta lines | SCHEDULED | M6 |
| D49 | refine >= 30 | BUILT (library) | lance.go:580 |
| D50 | chunk locator | BUILT (library) | store.go:224 |
| D51 | earlier campaigns only when needed | BUILT | not imported, by decision |
| D52 | manual embedding passes | SCHEDULED | M6; no `embed` command |
| D53 | manic_miner public and whole | BUILT | tasks/custom/manic_miner |
| D54, D66, D67, D68 | SDK release recorded; several builds; prebuilt bundles; `<release>+<commit>` | BUILT | internal/sdk/sdk.go:49-233, runner/sdk/*, gate harness_build |
| D55, D59, D81, D98, D101 | specific runs and arms | BUILT | done per 110 §3; D59/D98 arm files moved out of repo by D118 |
| D56 | legacy remaps, sibling lanes | BUILT | secondpass.go |
| D57 | config layout | BUILT (CHANGED by D118 for arms/models) | config/local.go |
| D58 | M2 plan incl. "daemon not linked with Lance until ingest writes chunks (M3)" | PARTIAL | everything built except ingest writing chunks, which never happened |
| D61 | real runs stoppable any time | BUILT | run stop/abort |
| D62 | M3 plan: ingest, wire compare, detectors, gates, doctor, reanalyse, webhook + log | BUILT | ingest, gate, daemon.go:657 |
| D63 | stated-not-sent is a finding, run starts | BUILT | wirearm.go |
| D64 | busy endpoint -> run waits, notify once | BUILT | gate.go:30,199 |
| D65 | old corpus gets text detectors | BUILT | reanalyse --imported |
| D69 | human formatter | BUILT | internal/human |
| D70, D80, D82 | identity, catalogue, family from weights, model queried per run | BUILT | ingest/sampler.go, general.basename metrics, probe |
| D71 | unknown identity runs, with a finding | BUILT | ingest/sampler.go:15 model_identity |
| D72 | GPU declared and recorded | BUILT | config/local.go:158 |
| D73, D85 | reload/partial GPU INVALID; pause reload a note | BUILT | ingest/analyse.go |
| D74 | run identify | BUILT | POST /v1/runs/{id}/identify |
| D75, D76 | SDK-threshold detectors; every match | BUILT | detect/guards.go |
| D77, D83 | pause via beforeModel, unload | BUILT | runPost("pause"), control_cmds.go:56 |
| D78 | reports draw no conclusion | BUILT | compare |
| D79 | unload_others | BUILT | daemon.go:498, gate.go:199 |
| D84 | canary on first 5 requests | BUILT | ingest/canary.go:10 |
| D86 | scripted user, forks recorded | BUILT | session_fork |
| D87 | workspace is a git repo by default | BUILT | task.go, run.go |
| D88 | `user.*` hidden where zero | BUILT | compare.go:234,266 |
| D89 | error before any answer -> ABORTED | BUILT | run.go:611-615 |
| D90, D91, D92 | BFCL focus; SWE-bench subset; SWE images | SCHEDULED | M5; no code |
| D93, D94, D95 | blackswan-2 install, GPUs, data disk | BUILT | operational (examples/systemd, skill) |
| D96 | disk gate 200 GB before SWE-bench evaluation | SCHEDULED | M5 (no SWE-bench, no gate) |
| D99, D100, D108 | guard stop_on; max_consecutive_trips; onReasoningLoopLimitReached | BUILT | config/arm.go:153-158, runner/runner.mjs:282 |
| D102, D104, D106, D115, D117 | export conversations | BUILT | internal/export, daemon/export.go |
| D103 | reasoning_history / reasoning_inline arm fields | BUILT | config/arm.go (not re-verified line-by-line) |
| D105 | training tasks public, `training = true` | BUILT | task loader (.claude/rules/task-loader.md) |
| D107 | batch wall/idle limits, several tasks, suites | BUILT | client_cmds.go:172-176, 974-975 |
| D109-D114 | tbench2, run_as, collect, limits, /opt/automaticum, vendoring | BUILT | internal/task/task.go, exec.go, suite/tbench2 |
| D116 | notices by kind; `notices` command | BUILT | client_cmds.go:501 |
| D118 | arms/models in config dir | BUILT | config_cmds.go `p.arms()` |
| O1, O5, O6, O29 | open questions | OPEN | decisions.md §3 |
