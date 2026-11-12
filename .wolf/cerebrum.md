# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-11-08

## User Preferences
- 2026-11-12 (owner: "measuring is not going to change anything. what do you measure? a 0.5b model? or a 120b model?"): do not offer to derive a default from stored data when the quantity varies with model size or hardware; a default must be safe across all of them, and edge cases are set by hand through the override. model_reloaded.min_load_seconds stays 1 s.

- 2026-11-09, the owner: keep .wolf/memory.md to useful actions only. Do not append a row for a status tick that changed nothing ("no mail, nothing to do"); log a tick only when it acted (a deploy, a mail answered, a failure found).
- 2026-11-09, the owner: a daemon must take every configuration change live, handle signals fully, and never stop unless it truly cannot ("do I have to specify even the most basic things?"). Build the operational basics a long-running service needs without being told: live reload (SIGHUP, an API verb, a directory watch), validation before swap, graceful drain, fault isolation, clear status of what is pending a restart.
- Only the owner turns an **Open** item into a decision. Carry a recommendation and ask in batches. Never quietly bend a decision to fit an import: record it and open an O item.
- Do the owner's setup steps in the order listed. GitHub repos go under `mann1x`.
- Public repo: neutral host names only, with no nicknames, private paths or addresses. The mapping is in docs/local/hosts.local.md (untracked).
- Data never goes on the dev NVMe or the old harness's spool NVMe. Use the disk named in docs/local.
- C# is a main language, so dotnet leads the language layers.
- Write durations as "8m 13s" or "1h 02m 05s", never bare seconds.
- Every run and report starts with model (family, variant), GPU, host, inference point and sampler.
- Fix forward and label each number's method.
- Arm, sampler, guard and threshold values come from the owner. They are arm config: never hard-coded and never baked into an image. Show them before the first run. "128k" means 131072.
- (2026-10-29) This session only develops automaticum. Client sessions write their own arms, models, endpoints and lanes in their configuration directory (D118) and start their own batches.
- Every spec requirement is either built, marked "not built" with its milestone, or dropped by a decision. Re-audit with docs/audit/ after each milestone. A noticed gap is a design flaw: name it and fix it.
- Ship ready-made waiters (`automaticum wait ...`, `--each`, `--next --seen N`). Never make a client write its own polling loop.

- 2026-11-09, the owner ("add -arm-name too, of course, I mean, it's not obvious it's needed?"): when a selector has a known blind spot, close it in the same change, without asking. If an identity-by-content filter (arm id) can match another arm, a by-name filter is part of the job. Don't end a report by offering obvious completions; do them.

## Key Learnings

- 2026-11-12: the SDK's CLI and extension drivers differ (browser: headless true vs "shell", 15 s vs 7 s, extra request-failure lines); for plugin parity port the extension's (BrowserSession, renderWithChrome), never the CLI's. Code the SDK inlines by Function.toString() breaks under minification (transform_image's planTransform, bug-096): test SDK page builders by running their built output.

- 2026-11-12 (D149): VS Code parity of ask_lsp/check_file needs the extension's halves, not core's: core's createCheckFileTool is a syntax check with another description; the extension's (apps/vscode/src/sdk/check-file-tool.ts) reads LSP diagnostics. Core replaces its check_file/list_files by name when a host passes its own in extraTools. Core reads `regressed` on a tool result (loop tracker), so diagnostics after edits change guard behaviour.
- 2026-11-12: language servers need every server request answered with an object (null crashes the CSS validator and the HTML page validation with it); the HTML server (vscode-langservers-extracted 4.10.0) needs js/ts.implicitProjectConfig and TypeScript libs at node_modules/node_modules/typescript; csharp-ls is pull-only; rust-analyzer's type errors come from cargo check on didSave and need a uid-1000-writable workspace.
- Detector thresholds (D148): the package constant stays the registry default (single source); a function reads locals from `o.T`/`v Values` at its top (`x := o.T.Int(ThX)`) so its body is unchanged; a constant used as an array size becomes `make([]int, n+1)`; a count used as a divisor gets `Min: 1`. New detector code must read thresholds this way, never the constant directly.
- A threshold ported from a client script is checked against that script's own code: score the cases with the script verbatim (Python) into testdata and compare (internal/detect/testdata/block_repeats.json).

- Go harness for coding agents on @cerebriline/sdk. The truth is docs/specs plus decisions.md. archive/ is input only and the old harness is READ-ONLY. During a batch: no lane image or bin/ rebuild and no daemon restart.
- Host GCC 10 can't link DuckDB, so packages that import `internal/store/duck` use `make build`. A store command goes in `store_cgo.go` with a stub in `store_nocgo.go`. The old corpus has private strings (never quote it) and streams of up to 5 GB, so corpus walks are daemon jobs.
- Config: `template = true` arms are extend-only. Use `parse()`, because Go `flag` stops at the first positional. Drop a field from Resolved() if a false value must not change the arm's identity. Decode JSON into a fresh value, because encoding/json merges keys into an existing map.
- Runs: a run row needs a verdict. In-flight runs live in `lane_state`. Verdict rules live only in `run.Decide`. A new `store.State` method must also go into the fakes in the daemon and run tests. `run.Fault` (ported from llms `Kr`): the SDK already retries infrastructure errors, so a run that ends on one is ABORTED unless the oracle passed.
- Daemon: the regression view is `compareView(split="build")`. `pool` excludes runs on an older task version unless pooled. Mutating calls carry `Automaticum-Client`.
- Ingest: add new detectors to `owned`, and bump `AnalysisVersion` when `Versions` changes. `ingest.Load`+`Derive` is the analysis without writes, so reuse it for read-only reruns. Error classes already exist (`ErrorClass`): never write a second classifier.
- Event stream: a turn cut by the output or wall limit has no `content_end` (`Block.Unfinished`; it ends at its last delta). Its retry exists only in the wire (`WireAnswers`, bug-076/077). A cut tool call has input "{}", and its arguments exist only in the wire's `Request.Calls`. A run in flight has no wire capture yet.
- To set a threshold from data, use the learned-detector-measurement skill: a throwaway `go test -c` binary run read-only on the lane host and deleted afterwards.
- Docker:
  - `/changes` lists mount points, so filter them (`docker.notMounts`).
  - A tmpfs takes its target's owner and mode, so oracle `writable` paths use `exec,mode=1777`.
  - CapDrop ALL blocks a root chroot, so `run_as="root"` adds the default caps.
  - Local images have no repo digest, so an imported suite's oracle uses the source image's digest.
  - Lane layers `COPY --from` the oracle image's digest: check its Debian release first (bug-032).
  - Use MinAPIVersion and never pin one API version (bug-027).
- SDK: it is the owner's fork, the cline monorepo beside this one. Fix it on `main` with a Vitest and release it per `docs/protocols/BUILD-RELEASE-DEPLOY.md`. Releasing is outward-facing, so ask the owner first. Then pin it with learned-new-sdk-release and check that the ported guard code is unchanged (`git diff --stat v<old> v<new> -- sdk/packages`).
- SDK behaviour:
  - A run ends on the second text-only reply.
  - The SDK spawns `rg` and `git`.
  - The reasoning guard ends a run after `maxConsecutiveTrips` unless the host answers `continue`.
  - The struggle detector is attached only when escalation tools or spawn_agent exist.
  - Grep the bundle for `kind:"..."` before claiming the SDK sends no event for something.
  - Use notices and never match guard sentences.
- SDK builds live in `runner/sdk/<build>/`, with the lockfile from the Node 22 container. Bundles are mounted read-only. `Run.Harness` holds only the release confirmed at run time.
- Wire: a settings read-back proves nothing; check the first `/api/chat` request carrying `tools`. On OpenAI-compatible endpoints the arm owns thinking. opencoti and xollama servers get that SDK provider. Check `Request.OpenAI` before expecting `options.*` (bug-019).
- Export spans count code points, while guard `verdict.chars` counts UTF-16 units.
- Runner: a new file goes on the COPY line of images/base/Dockerfile. Regenerate `runner/test/arm-stub.json` after changing stub.toml or base.toml. Run tests with `--network none`.
- Gates: run `oracle verify` per task hash on each installation (bug-038). `internal/gate` imports `internal/run`, never the reverse.
- Human-read output goes through `internal/human`; `--json` stays raw. Use `fail()`, because `do()` drops error bodies.
- llama-server: `/props` is at the root and `n_ctx` is per slot. On a new server, check chat_format and reasoning_format.
- Verify an audit gap against the code before building it. A batch id is not its runs' prefix: read `batch` from `run show -json`.

## Do-Not-Repeat
- 2026-11-12: a JSON field that may be "not asked" must never share its encoding with "empty": no omitempty on an opt-in block, and the header says what was included (bug-094: clients read a missing metrics block as zeros).

- 2026-11-12 (owner: "why v4 and v5 should not be comparable? don't be silly"): a task version bump is not automatically a comparability boundary. Ask what the measurement is: if the verdict is unchanged, or a stored workspace can be scored by the new oracle (rescore), the versions are comparable. Never copy a client's "boundary" wording without checking it against the verdict pattern.

- 2026-11-12: OpenWolf's session consolidation can replace real .wolf/memory.md rows with `> Consolidated session (0 actions)` lines. Before committing, diff memory.md against HEAD; rebuild from HEAD plus the new rows if rows vanished.

- 2026-11-12: an oracle the agent runs must never print a top-level success beside a failing part: the agent reads the first `ok`. Fold every check into the headline field and keep any older meaning under its own name (bug-090).

- 2026-11-09: a filter or list must cover runs in every state, queued and running included, or say loudly what it leaves out. A run not finished still has an arm (the one it started with, or would start with now). Every noun the harness runs needs a way to see what is active (`arm ls --active`, `run ls --state`): ask "can a person and an assistant see what is running right now, by this noun?" before calling a command done (bug-088).

- 2026-11-09: never capture a config object's maps for direct reads in a long-running process. Read through locked accessors and swap whole after validating (config.Local.Reload/Apply); a run in flight keeps the value it took. Take signal handlers at the first moment of start-up (a default SIGHUP kills the process), ignore SIGPIPE, and let the CLI reach the daemon from automaticum.toml alone (config.LoadClient), never a full LoadLocal.

- 2026-11-08: anything that compares SDK releases must read a local build's version (`4.100.275+<commit>` in an arm, `4.100.275-g<commit>` as the package and run record version): use detect's releaseAtLeast, never split on '.' and Atoi. When an SDK release changes a guard automaticum replays (loop-detection.ts, reasoning guards), add a release-gated rule set (`CallGuardFor`, `RulesFor`) and bump detect.Version; diff the guard file between release tags before adopting a release.

- 2026-11-08: a tool hint that names an edit or a verdict must be proved by the authority it names (the parser, the oracle), never by a heuristic of its own. Its tests must use real code that passes that authority after the fix. The SDK's delimiter scan called a bracket-count edit 'checked' and deleted a correct } on the manic_miner seed (bug-082); its fixtures never parsed. vitest runs under Node and the CLI under Bun: test runtime-dependent code on both.
- 2026-11-07: in a stub smoke test, `run start` prints the batch id first. Take the run id with an anchored `grep -oE '^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}'`, check the stub's `/api/version` first, and stop the stub only after `wait run` returns 0.
- 2026-11-05: never chain `git push` after the `git show HEAD` gate. Run the gate, read it, then push in a separate call.
- 2026-11-04: before telling a client a config key, read `examples/config/automaticum.toml`. Its keys are top-level; there is no `[daemon]` table (bug-080).
- 2026-11-02: when deploying to a lane host, copy binaries to `bin/<name>.new` and `mv` them after the idle check. Never scp into `bin/`.
- 2026-10-29: the private-string gate fails closed:
  - Use NUL-separated names and accept only grep exits 0, 1 or 123.
  - Never chain the commit with `;` and never write patterns to tracked files.
  - Read false positives.
  - Caliber rewrites files after the staged gate, so rerun the gate over `git show HEAD` before pushing.
- 2026-10-25: "must declare" means a config declaration, not a heuristic. Grow the tool; never drop tasks.
- 2026-10-25: in hand-written TOML, put top-level keys before the first table. Don't rewrite catalogue family or size from a model description: use `notes` and ask.
- 2026-10-25: rebuild the base on a host before a stub run on a new layer (bug-041). Never pkill or pgrep a pattern that appears in the same command.
- 2026-10-13: never invent detector, guard or limit numbers. xollama's GPU host is `windows-2`. Job waiters need an exit-code file, a failure exit and a time bound.

## Decision Log

- 2026-11-12 D150: the browser with Chrome in the lane, `[agent.browser]` (browser, render_image, transform_image on the extension's drivers; Chrome for Testing 131 at puppeteer-core 23.11.1's revision, under /opt/automaticum/browser). Owner: "add the browser too, with chrome in the lane". SDK fixes asked as X15 (owner: "mail ollama to fix the sdk bugs and release a new one, then integrate it").

- 2026-11-12 D149: the editor's language servers emulated for every lane language as `[agent.lsp]` (enabled, diagnostics_after_edits); off/absent keeps arm identity; one tool removed with disabled_tools; protocol 11. Owner: "we need search_codebase and ask_lsp to work in automaticum", "complete support of lsp, not only .ts".
- 2026-11-12 (D148): model_reloaded.min_load_seconds = 1 s is the owner's ("keep 1 second ... there's no safe default value which is better than 1 second"); origin owner, no harness origin remains.

- 2026-11-12 (D148): every detector/measure threshold: default from the SDK where it has the rule, else the owner (source recorded); overridable per installation ([thresholds] in automaticum.toml) and per detect read (--threshold); every read records effective value + from (sdk/owner default, config, cli). Loop blocks: block max_repeat >= 20 (owner; SDK has no per-block repeat rule), unit rules ported from the client's loopscan and proven equal on 43 blocks. Not per arm: guards that change the run already live in the arm.
- 2026-11-12 (D147): `detect --json` carries every metric by default (owner: "mail automaticum to add the metrics"); `--metric` narrows, `--metric none` opts out; header `metrics_included`; row `metrics` null only when not asked; totals `loops` = reasoning_loop cuts per model hour. Thresholded loop measures (maxrep>=20, thinking share in loop blocks) left out until a threshold is given.
- 2026-11-12 (D146): a server already running is one automated step (owner: "it must read all the aliases, it's ridiculously complex to start a new arm ... including asking the idempotency key"). Probe takes id, aliases or /props model_alias; endpoint `forward_to` (daemon forwards a loopback-only server, no socat); run start preflights the endpoint before queueing (`--no-preflight`); `automaticum run server` writes/reuses endpoint + arm (server sampler by default, sampler.owner = "tag"), attaches an idle lane, proposes the key, reloads, starts.
- 2026-11-12 (D145): manic_miner task version 5: the oracle's top-level `ok` is runs AND game_logic.ok; the old meaning is `runs`. v4 and v5 are comparable (owner); a run rescored by a later version's oracle is compared at that version (compare/report ab/basis show).

- 2026-11-09 (D144): the running daemon takes a changed configuration without a restart (SIGHUP, `config reload`, the `watch_config` watcher); refused whole on any problem; lanes, endpoints, settings, token, listen live; data_root/archive_root/docker_socket/repo from the next start, named; removed lanes finish their run; queued runs without lane/endpoint wait; panics end one run, not the daemon.

- 2026-11-08 (D143): the arm template's default SDK release is 4.100.276 (owner: "integrate .276 when ready", read as D142's "integrate" = harness default + host). Arms that state their own release keep it.

The full list is in docs/specs/decisions.md; keep this log in step with it.

- D1-D101 (10-12 to 10-23): Go on hosts, Node in lanes; Docker and TOML; DuckDB, Lance and blobs; automaticum never launches model servers.
- D102-D117: export of conversations, request capture, the public training pool, batch limits, `stop_on`, tbench2 held out, the runner under /opt/automaticum, held-out batches, export spans.
- 2026-10-29 D118: arms and models live in `<config>/arms` and `<config>/models`; the repo ships examples only.
- 2026-11-01 D119: a drain releases a held run. D120-D125: cohorts, rescore, `requires`, `busy_command`, `quarantine_after`, priority and reserve. D126-D129: struggle signals, edit without a check, turn pathologies, the ported call guard.
- 2026-11-02 D130: the baseline is the campaign's or cohort's baseline arm, else the parent arm. D131: swallowed_argument. D132: tokens are stored hashed.
- 2026-11-03 D133: `detect` over any set of runs. D134: duplicate_definitions (more than 3, from data). D135: the repetition guard, an observe-only detector with an arm opt-in to stop. D136: on a held stream the oracle decides.
- 2026-11-04 D137: private training tasks can be exported. D138: the idle limit counts arriving bytes. D139: per-request first byte and endpoint meta. D140: `task pin`. D141: `config check`.
- 2026-11-05 D142: the default SDK release is 4.100.274.