# 040 — The runner

**Status:** draft 1.19, 2026-11-12. Facts about the SDK in this file were read
from the Cerebriline source at release 4.100.247 and were not verified by
running it. The parity table in §6 was checked row by row by the Cerebriline
session on 2026-10-13 against its main branch (release 4.100.249 plus requests
1 to 3); §6.1 was checked by it the same day (§6.1.1); §6.2 was not checked. Items under "Requests to Cerebriline" are dependencies on another
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

**Decided by the owner (2026-10-25, D113):** the runner and what it runs on
live under its own prefix, `/opt/automaticum`, which is on no PATH: the
harness starts `/opt/automaticum/bin/node` (protocol 9). A lane layer on a
task's own image (066) adds Node, the runner and a static ripgrep there and
nothing else; the runner routes the SDK's own calls of `rg` to it before the
SDK is imported, and the agent's commands, which go through the shell, find
only the image's tools.

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
  **Decided by the owner (2026-10-13):** every run records the **SDK release
  version**. A run whose release version cannot be established does not
  start. A build from a commit therefore still names the release it carries.
  Milestone 2 runs on release 4.100.250, which contains requests 1 to 3
  (§8.2).
  The five first real runs used release 4.100.250 (§8.2, 110 §3.1).
  **Decided by the owner (2026-11-12):** the arm template's default release
  is 4.100.277 (D152; 4.100.276 from 2026-11-08, D143; 4.100.274 from
  2026-11-05, D142); an arm that names its own release keeps it.
- **How the release is established:** see §3.1. A run's record holds a
  release only once it is established, never the one its arm merely names.
  The SDK's own build constant is not used: it carries another number than
  the release.
- **Refuse to run** when the installed package does not match what the arm
  names. This is a gate (050 §3).

### 3.1 Several builds at once, each reproducible (as built)

**Decided by the owner (2026-10-13):** the harness supports several SDK
builds at the same time. Lane images hold no SDK; the build an arm names is
provisioned at run time; and a build must be reproducible on demand (D66).
How, also decided that day:

- **A prebuilt bundle, mounted** (D67). Each build is installed once, in a
  container, and its bundle is mounted read-only into the run's container
  at the place the runner's imports are looked up. Nothing is downloaded
  during a run.
- **The lockfile and the packages are kept** (D67). Every build has a
  definition directory in the repository, `runner/sdk/<build>/`:
  `package.json`, `package-lock.json`, and `sdk.toml` with the package
  version the build must install as, where it comes from, and
  `bundle_sha256`, the hash of the bundle. Everything the install downloaded
  is archived in the store's archive tier, so the build can be installed
  again with no network.
- **Releases and local builds** (D68). `source = "registry"` is a published
  release, named by its number. `source = "local"` is packed from a commit
  of the SDK's repository and never released; it is named
  `<release number>+<commit>`, states the commit, and its packed packages
  are given to the install and archived with the rest.

The bundle's hash is over every file's path and content and every link's
target; times and owners do not enter it. The first install of a definition
writes the hash into `sdk.toml`, to be committed; every later install, on
any machine, must give that hash or is refused.

| command | what it does |
|---|---|
| `automaticum sdk add <build> [--packs D]` | installs the bundle (install scripts of dependencies are never run), checks the installed package's version, archives what was downloaded, pins or compares the hash |
| `automaticum sdk ls [--deep]` | the defined builds and whether each can be used |
| `automaticum sdk verify <build>` | installs the build again in a container **with no network** from the archived packages and compares the hash: the proof that it can be reproduced |

What stands between an arm and the wrong SDK, in order: gate
`harness_build` (the build is defined, installed and pinned); the run itself
hashes every file of the bundle before it starts and is `SKIPPED` on a
difference; the runner reads the installed package's own version and hashes
the package; and a run whose runner reports another version than the
build's is `INVALID`. The run record holds the build's name, the package
version, the source and commit, the bundle's hash, the lockfile's hash and
the package's hash as the runner found it.

Proven on 2026-10-13: releases 4.100.250 and 4.100.251 installed side by
side; each reproduced with no network from its archive; the same task run on
both from one lane image; and the package of 4.100.250, as the runner hashes
it, is the one the five first real runs used.

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

**Measured on 2026-10-13 (release 4.100.250, provider `xollama`):** layer 2
is not proof of layer 3. The SDK reported a reasoning effort, the thinking
budget it derives from that effort, and a sampler value of zero as resolved;
none of the three was in the request. The request carried `think: true` and
the server applied the model tag's own thinking budget (110 §3.1). An arm
field that the provider does not send must therefore show in the run's
record as "stated, not sent", and that comes from the wire alone. The
comparison of the first request with the arm is not automatic yet; it is
detector work (070 §4).

A value the SDK derives from what the arm states (a thinking budget from an
effort, an output cap from the context window) is not a deviation. The runner
records it beside the resolved configuration.

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
| Browser tool | present only where a Chrome is found | `[agent.browser]`: the lane's Chrome and the extension's driver (§6.1.3, D150); without, absent |
| `render_image`, `transform_image`, `generate_icon` | extension only, through the same browser driver; the CLI registers none | `[agent.browser]`: `render_image` and `transform_image` on the extension's render driver (§6.1.3); `generate_icon` is a media tool, absent as by default |
| `check_file` tool | the extension registers its own, which reads the editor's diagnostics (`apps/vscode/src/sdk/check-file-tool.ts`); the SDK adds a syntax-only one of its own only to a host that names none | with `[agent.lsp]`: the extension's tool, ported, reading the lane's language servers (§6.1.2, D149). Without: the SDK's syntax check, a different description and answer under the same name, a recorded difference |
| `ask_lsp` tool | extra tool on the extension's code-intelligence provider (VS Code's language servers); the CLI has one on its own LSP client | with `[agent.lsp]`: the SDK's own tool on the runner's language servers (§6.1.2, D149); without, absent |
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

#### 6.1.1 What the emulation must copy

Reported by the Cerebriline session on 2026-10-13 from its main branch; not
verified here, and nothing was run. All four rows hold. An emulation is the
same behaviour only if it copies these details, and each one it does not copy
is a recorded difference.

- **Diagnostics after edits.** Only after the tools `editor` and `apply_patch`,
  and only when the edit succeeded. Only what the edit **introduced**: the
  diagnostics are read before the tool and the difference is reported. Only
  errors and warnings. Files with opaque extensions are skipped. Settling:
  poll every 250 ms, stop when the file's diagnostics stop changing, give up at
  2,000 ms; a language server in a container that needs longer reports nothing
  under that rule, and a larger budget is a recorded difference. When there is
  a diagnostics block, a second block follows with the delimiter-balance
  verdict for the touched files (the SDK exports the function). The result is
  marked as regressed when the count of errors and warnings went up. The text
  is appended to the tool result, not sent as a message.
- **Terminal.** The extension's default is the visible VS Code terminal; the
  background executor is the user's other choice and the forced mode of the
  standalone build. The runner therefore emulates the **non-default** mode,
  and the record says so. Auto-proceed is at 300 s: the pending call returns
  the partial output with a sentence saying so, the command keeps running and
  its further output goes to a log file capped at 10 MiB. Not verified by
  either side: how far output capture and exit-code reporting differ between
  the two modes.
- **`@problems`** appends the **whole** workspace's problems to the user
  message. Same source as the first row, another filter.
- **Mentions** are expanded by the extension before the prompt is sent, each
  into its own wrapper appended to the message. **File mentions**, the most
  used kind, were missing from the table above and belong to this row.
- **The IDE name** is used twice: in the system prompt's IDE line and in a
  token that tool descriptions may carry. One arm field sets both.

**The extension's extra tools, tool by tool** (`createVscodeExtraTools`,
`apps/vscode/src/sdk/vscode-runtime-builder.ts` at 4.100.276, audited
2026-11-12 for D149). "Default" is what a fresh install of the extension
offers; a tool the owner's own installation turns on is a difference from
that installation, not from the default.

| tool | in the extension | in the runner |
|---|---|---|
| MCP tools of the extension's MCP hub | one tool per server the user configured | absent: no arm starts an MCP server (row above) |
| VS Code's own MCP servers (`createVscodeLmMcpTools`) | the servers VS Code runs, adopted; the owner's sessions carry `lsp__*` tools from one | absent: VS Code only. `ask_lsp`'s description tells a model reaching for an LSP MCP server that it is the same thing |
| `check_file` | always | `[agent.lsp]`: emulated (§6.1.2); otherwise the SDK's syntax check |
| `ask_lsp` | always | `[agent.lsp]`: emulated (§6.1.2); otherwise absent |
| `list_files` | always, on VS Code's file search, which honours the user's excludes | the SDK's own, on the local file system, which the SDK adds to a host that names none: no editor excludes, a recorded difference |
| `browser` | on unless the user turns it off (`cline.browserTool`); Chrome is started on the first call | `[agent.browser]`: emulated (§6.1.3, D150) |
| `render_image`, `transform_image` | always, on the same Chrome | `[agent.browser]`: emulated (§6.1.3) |
| media tools (`generate_image`, `generate_icon`, audio, video) | off until an endpoint and a model are configured | absent, as by default |
| `sdd` | only when a spec-driven skill is on | absent, as by default (no arm turns such a skill on) |
| Library tools (`library_*`) | only when the Library panel has them on; search by meaning with an embedding endpoint | absent, as by default. The owner's sessions use them (about 400 calls), but they are not wanted now (**decided by the owner, 2026-11-12, D151**); an emulation would need the Library's index and an embedding endpoint, as an arm field |
| `web_scrape` | only when scraping is set up and allowed | absent, as by default |
| memory tools | only when the Memory panel has them on | absent, as by default |
| `jev` | only with the user's key (a paid service) | absent, as by default |
| `run_commands` | the extension's own, on the VS Code terminal | the SDK's background executor (§6.1, terminal row) |

Semantic search (`search_codebase`'s `mode: "semantic"`) is not an extra
tool: the extension hands the SDK a `semanticSearch` executor
(`createCodeSearch`) only for a folder the user opted into the Library's
index, with an embedding model set. The runner hands none, so the mode is
absent, as for a folder that is not opted in. **Decided by the owner
(2026-11-12, D151):** not wanted now, like the Library tools; an emulation
would need the same embedding endpoint.

Two behaviours were missing from the table and were added as known
differences until emulated: `check_file` in the extension reads the editor's
diagnostics for the file, so it needs the same language servers (emulated
since D149, §6.1.2); and semantic search is supplied by the extension from
its code index, so without an emulation that mode of the search tool is
absent (still so, see the audit in §6).

#### 6.1.2 The language servers (as built, D149)

**Decided by the owner (2026-11-12, D149):** `ask_lsp` and the editor's
diagnostics are emulated, for every language the lane layers carry, as the
arm field `[agent.lsp]`, so a run can be compared with the plugin, and with
and without them.

- **Arm field.** `[agent.lsp] enabled = true` gives the run `ask_lsp`, the
  extension's `check_file` and the diagnostics after edits;
  `diagnostics_after_edits = false` keeps the two tools and drops the third.
  Left out or off, the run is what every run before it was and the arm keeps
  its identity (030 §4). One tool alone is taken away with
  `agent.disabled_tools` (`["ask_lsp"]`), the A/B the owner asked for.
- **Servers.** Under the runner's prefix, `/opt/automaticum/lsp`, on no PATH
  of the agent (D113). The base (`images/lsp`, a lockfile) carries those
  that run on Node: typescript-language-server 6.0.2 on TypeScript 5.9.3 for
  TypeScript and JavaScript (VS Code also uses its own TypeScript, not the
  workspace's); the HTML, CSS and JSON servers of vscode-langservers-extracted
  4.10.0, which are VS Code's own servers, the HTML one checking a page's
  script and style as VS Code does; pyright 1.1.414 (Pylance is built on it);
  bash-language-server 5.8.1 with shellcheck 0.10.0. The layers add the
  servers of their toolchains: gopls 0.18.1 (`go`, built with the layer's
  Go), rust-analyzer and the standard library's source (`rust`, the
  toolchain's components), clangd (`cpp`, Debian's), csharp-ls 0.16.0
  (`dotnet`, its last release on .NET 8). A Terminal-Bench layer copies the
  Node servers. A language with no server answers nothing, as in an editor
  without the language's extension; the runner's `prepared` note lists the
  servers, which were installed and which started.
- **Host.** `runner/lsp.mjs`, ported from the SDK CLI's LSP client and
  code-intelligence provider (4.100.276), with the extension's way of
  finding a symbol (its document symbols first). It differs from the CLI
  where the CLI stops short: a changed file goes to its server as a change,
  not as a second open (which a server ignores, then answers from the old
  text); the server's own requests are answered with settings objects, as
  an editor answers (left null, the CSS validator fails and the HTML server
  then reports nothing for the page); the HTML server gets VS Code's defaults
  for the settings it reads. The servers of the workspace's languages start
  with the session, as an editor's do when it opens the folder. It says it
  pulls diagnostics, as VS Code does: a server built on vscode-languageserver
  refuses a pull from a client that does not, and a refused pull leaves the
  file waiting for the server's push rather than reported clean (bug-097).
- **The SDK's halves, from 4.100.277** (X15, D152): a build that exports
  `createLspHost`, `createDiagnosticsCheckFileTool` and
  `createEditorDiagnosticsHooks` gets them (`runner/host.mjs`), the functions
  the extension's own `check_file`, post-edit report and Problems renderer
  run from that release; the runner hands them the lane's servers
  (`serverSpecs`, each found by its probe under the prefix, never on a PATH)
  and notes the hooks' trace as the same `editor_diagnostics`. An older
  build gets the runner's ports below. The `prepared` note says which:
  `lsp.source` is `sdk` or `runner`. On the stub, both give the model the
  same text for the same workspace.
- **Diagnostics after edits** (`runner/editor.mjs`, ported from
  `apps/vscode/src/sdk/editor-diagnostics.ts`): every rule of §6.1.1 is
  copied, budgets included (250 ms polls, 2,000 ms). One addition: before
  the edit the file is handed to its server and, the first time, given the
  same budget to report, since in VS Code the file an agent edits is open
  and what was wrong with it before is known. Each edit is a runner note,
  `editor_diagnostics` (the extension's trace file), counted as the
  `lsp.*` metrics (070 §2).
- **`check_file`** is the extension's tool, its description word for word,
  its budget (150 ms polls, 1,500 ms) and its delimiter scan. The
  extension's lint command is a user setting, unset by default, and not
  offered.
- **Known differences.** No jquery typings for a page's script (the server
  package does not ship them); a file changed by the agent's shell is seen
  when it is next asked about or edited, not when it changes; no `list_files`
  excludes and no semantic search (§6 audit).

#### 6.1.3 The browser (as built, D150)

**Decided by the owner (2026-11-12, D150):** "add the browser too, with
chrome in the lane".

- **Arm field.** `[agent.browser] enabled = true` gives the run the SDK's
  `browser`, `render_image` and `transform_image`; left out or off, the arm
  keeps its identity. One of them alone is taken away with
  `agent.disabled_tools`.
- **Chrome.** Chrome for Testing 131.0.6778.204, the revision puppeteer-core
  23.11.1 expects (the version the SDK's hosts use), pinned by checksum, with
  puppeteer-core and Mermaid 11.16.1 (the extension's) from a lockfile
  (`images/browser`), under `/opt/automaticum/browser`, on no PATH of the
  agent (D113). Its libraries are those of its own `deb.deps`, less `wget`,
  `xdg-utils` and GTK. In the base, so in every language layer; the `cpp`
  layer installs the same libraries under Debian 13's names. A Terminal-Bench
  layer has none (its image is the task's own, and libraries would change
  it): the `prepared` note says `chrome: null` and the tool says so.
- **Drivers** (`runner/browser.mjs`): ported from the extension, not the
  CLI, whose driver gives the model other text: `BrowserSession`'s local mode
  (headless `shell`, its user agent, the default 900x600 viewport, a page
  loaded to `domcontentloaded` and `networkidle2` within 7 s and then until
  its HTML stops changing, console messages and page errors only, a quiet
  console waited for, a PNG screenshot) and `renderWithChrome` (a Chrome per
  render, Mermaid drawn in the page). Whether the model sees the screenshot
  is the SDK tool's decision, from the model's image support. From 4.100.277
  the SDK's own `createChromeBrowserDriver` and `createChromeRenderDriver`
  (the same ports, moved into core, X15) replace these on a build that
  exports them, on the lane's Chrome, Mermaid and puppeteer-core;
  `browser.source` in the `prepared` note says whose ran (D152).
- **Container differences.** `--no-sandbox` (Chrome's sandbox needs user
  namespaces Docker does not give) and `--disable-dev-shm-usage` (a 64 MB
  `/dev/shm`). The extension's custom arguments and remote browser are user
  settings, unset by default, and not offered.
- **A defect of the SDK, kept on the builds that have it.** Up to 4.100.276 `transform_image` fails on
  every call in a minified build: the page calls `planTransform` by name
  while the bundle inlines it under its minified name. The extension's
  production build minifies too, so the plugin is taken to fail the same
  way; the runner does not patch the SDK's output, and the fix is asked of
  the SDK (§8). Fixed in 4.100.277: the page binds `planTransform` by its own
  name, and the tool writes its image on the stub.

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
- **Decided by the owner (2026-10-14), D86:** a script may restore a
  checkpoint's messages. The SDK then starts a new session fork within the
  same run, and its guards, mistake counter and compaction state start from
  nothing. The run records each fork with its time, and every counter and
  guard metric says it restarted there, so reports can filter such runs.

**As built, stage 1 (2026-10-14, runner protocol 2):** the script is the
task's `[user]` table in `task.toml`, so it is part of the task's version:

```toml
[user.default]            # mandatory once [user] is there
approval = "approve"      # or "deny"; an approval no rule matches
feedback = "…"            # optional, with a default denial
answer = "…"              # a question no rule answers

[[user.rule]]             # in order; the first that matches acts
on = "approval"           # approval | question | turn | elapsed | completion | plan
tool = "run_commands"     # approval: one tool, by name (one that asks)
category = "commands"     # approval: or one of the extension's categories
match = "rm -rf"          # approval: on the tool's input as JSON; question: on its text
turn = 3                  # on = "turn": the lead agent's turn, counted over the whole run
after_s = 600             # on = "elapsed": time of the run, time held left out
times = 1                 # how often it may act; 1 when not stated, 0 every time
do = "deny"               # see below
text = "…"                # feedback, answer or message
option = 2                # do = "option": the question's suggested option, from 1
mode = "act"              # do = "mode": act | plan
restore = "workspace"     # do = "restore": workspace | messages | both
checkpoint = 1            # do = "restore": the SDK's checkpoint run count
```

| on | do |
|---|---|
| `approval` | `approve`, `deny` (with `text` as feedback) |
| `question` | `answer` (`text`), `option` (an option not offered is the default answer, and the line says so) |
| `turn`, `elapsed` | `steer` (now, into the running turn), `queue` (when the turn ends), `cancel` (abort the running work, then resume with `text`), `restore` (`workspace`; `messages` or `both` fork the session, D86, and need `text`), `mode` (abort, switch, then send `text`) |
| `completion` | `queue`: one more message after the agent ends the task; `mode` |
| `plan` | the agent ended a run in plan mode: `queue`, or `mode` (to act, `text` optional) |

- Unknown fields, a rule whose fields do not go with its event or
  response, and patterns that would mean something else in JavaScript
  (inline flags) are refused when the task is loaded. (Stage 1 refused
  the mode switch, the restore of the messages, approval by category and
  the plan event by name; stage 2 below builds them.)
- With a script the tools of the extension's approval categories ask for
  approval, and the script decides; the others run without asking, as in
  the extension (stage 2). Without one, nothing changes from protocol 1.
- A denial's reason is the SDK's sentence, "This tool call was rejected by
  the user and not executed.", followed with feedback by " The user provided
  the following feedback:" and the feedback on its own line inside
  `<feedback>` tags. The SDK appends its own guidance to every denial.
- Turn and elapsed rules act before the next model request: the last
  turn's tools have run. A turn rule acts once on its turn.
- A script with a cancel or a completion rule runs the session as
  interactive (the run says so in a `runner` line `interactive`), so the
  session stays open after a run of it ends; the runner stops it at the
  end. Otherwise the session is non-interactive, as before.
- Observed on the stub, SDK 4.100.250: a steer aborts the request in
  flight, and the next request carries it as a user message; a cancel
  then a send resumes the same session; the SDK numbers turns from 1 again
  in each run of the session, so the script counts them itself.
- Each action is a `user` line (PROTOCOL.md); ingest counts
  `user.actions`, `user.denials` and `user.failed` (an action the SDK
  refused; the run carries on, and did not play its script as meant).
- A task whose script restores a checkpoint needs an arm with checkpoints
  on, or the run is `SKIPPED` before it starts. The restore itself fails on
  the stub ("No checkpoint found at or before run 1"); what the SDK's
  checkpoint run count counts is asked of the Cerebriline session.
- Demo tasks `demo/scripted` (a denial with feedback, a steer, a message
  after completion) and `demo/cancel` (a cancel and resume, a restore) pass
  on the stub.

**As built, stage 2 (2026-10-14, runner protocol 3)**, from the Cerebriline
session's answers of 2026-10-14 (SDK and extension 4.100.251 source):

- **Categories.** Approval goes by the extension's categories
  (`sdk-tool-policies.ts`): `read` (read_files, read_file, list_files,
  list_code_definition_names, search_codebase, search_files, grep, awk),
  `edit` (editor, replace_in_file, write_to_file, apply_patch, delete_file,
  sed), `commands` (run_commands, execute_command), `web`
  (fetch_web_content, web_fetch, web_search), `browser` (browser,
  browser_action, render_image). A tool in none of them never asks, so a
  rule naming it is refused; `mcp` is refused (no arm starts an MCP
  server); there is no safe-command test. The table is in
  `internal/task/user.go` and goes to the runner with the script.
- **Denials** use the extension's sentences: "The user denied this edit.
  The file was NOT modified and still contains its original content." for
  an `edit` tool, the stage-1 sentence for the others; feedback is trimmed.
- **Mode switch** as the extension does it: abort, read the live messages
  and the compaction state, stop, start the same session id in the other
  mode with both, send the text. `send` with a mode does not switch.
  A `runner` line `mode_switched`.
- **Plan.** A run of the session in plan mode that the agent ended by
  itself on text is a plan, and the script's `plan` rule answers it.
- **Fork (D86).** `restore = "messages"` or `"both"` restores the
  checkpoint into a new session `<session>-fork<n>`; the old session is
  stopped. The `fork` line lists what started again from nothing:
  iterations, consecutive mistakes, loop guards, compaction state. Ingest
  records each fork in the finding `session_fork` and counts `user.forks`
  and `user.mode_switches` (070 §4.2).
- **Checkpoints need a git repository.** The SDK's checkpoints are commits;
  it keeps no shadow repository. Until 2026-10-14 no lane workspace was a
  repository, so an arm with checkpoints on never had one.
  **Decided by the owner (2026-10-16), D87:** every workspace is one by
  default, the seed one commit; a task may say `[workspace] git = false`
  (060 §2), and a script that restores cannot then load. The workspace
  diff leaves `.git` out; the record says `workspace.git`, and the basis's
  task fact says "workspace a git repository".
- Observed on the stub, SDK 4.100.250, arm `stub-plan`: plan mode offers
  no `editor` (it keeps `run_commands` and `sed`): the SDK's own plan
  filter. The arm's no-tool-call nudge answers the first text-only reply
  of each run, so the run ends on the second.
- **`switch_to_act_mode`, ported from the extension (2026-10-16).** It is
  in no SDK release (Cerebriline session, 2026-10-15): the extension builds
  it (`sdk-session-config-builder.ts`, the same at 4.100.250 and
  4.100.251), and the runner now does too, in `runner/plan.mjs`, its
  description and texts word for word. It is an extra tool in plan mode
  only, with no input, `completesRun`, no retry. Called in plan mode it
  queues the switch and answers "You successfully switched to act mode,
  proceed with the plan. …"; the run ends. Then, as the extension applies
  a queued switch when the session goes idle (`sdk-mode-coordinator.ts`),
  and before the script is asked, the session is started again in act mode
  with its messages and continued with "The user approved switching to act
  mode. Continue with the approved plan now." No user approval stands
  between: the description tells the model to call it only after the user
  approved the plan in a message, so a script that wants this path answers
  the plan with a `queue` message. A `beforeModel` hook stops any request
  while a switch is queued, as the extension's does. A session whose arm
  starts in plan mode is interactive. The `mode_switched` line says
  `source`, `agent` or `user`; ingest counts the agent's in
  `mode_switches.by_agent`, hidden where zero like `user.*` (D88). Demo
  `demo/plan-switch` passes on the stub.
- **Read receipts.** The extension keeps one record of the files read for
  its controller's lifetime and hands it to every session it builds, so
  the read-before-edit guard does not forget across a switch (Cerebriline
  session, 2026-10-15). Core makes a fresh one per session unless given
  one. The runner first did not, and an edit after a switch was refused
  (observed on the stub, 2026-10-14); since 2026-10-16 it passes one
  `capabilities.readReceipts` for the whole run, so a switch and a fork
  keep it. Not copied: the extension's record also outlives the task, so a
  task in the same window inherits the receipts of the one before.
- Demo tasks `demo/plan` (plan, then a switch to act) and `demo/fork` (a
  fork at turn 3 to checkpoint 1) pass on the stub; `demo/cancel` now
  restores the workspace for real.

## 7. What the runner returns

- The SDK event stream unchanged, including teammate and sub-agent events.
- The wire capture (§4), the session data directory, the runtime log.
- Its own stdout, stderr, exit code and signal.
- A final record: how the agent ended, whether it claimed success, usage as the
  SDK reports it.

The runner never runs the oracle and never sees it (060 §3).

### 7.1 The contract with the harness (protocol 1, as built)

- **In:** one job file, mounted read-only: the run id, a session id made
  beforehand, the resolved arm, the endpoint as the container reaches it, the
  prompt, the default answer, the wall limit and the SDK release that must be
  installed.
- **Out, on stdout:** one JSON object per line with a kind: `event` (an SDK
  event, unchanged, under `event`), `runner` (the runner's own notes), and one
  last `end` line. The harness stores the stream as it arrives and measures
  idleness on it. **Decided by the owner (2026-11-04, D138, protocol 10):**
  bytes arriving on a model response are activity. While they arrive the
  runner notes `receiving` (the request's `seq`, the bytes read so far) at
  most every 10 s, the cadence the harness reads `/slots` at, so a request
  that is alive but slow (keepalive comments, a proxy holding the answer
  while it retries) does not run out the idle limit; the transcript
  (`run tail`, `logs --render`) leaves the note out.
- **Out, in the output directory:** the wire capture and the session's data
  directory. **Decided by the owner (2026-11-04, D139, protocol 10):** each
  request of the wire capture also carries when its response's first body
  byte came (`t_first_byte`, `ms_first_byte`) and, when the endpoint said
  anything of it, `endpoint_meta`: its response header `x-endpoint-meta`,
  then every SSE comment `: x-endpoint-meta <json>` in the body, in order,
  each parsed as JSON or kept as text. The harness stores it and shows it
  (070 §1.1, §2); it judges nothing by it.
- **The `end` line** carries an outcome (`completed`, `refused`, `invalid`,
  `wall_limit`, `error`) with its reason, how the agent ended, whether it
  claimed success, usage, the SDK release and package hash found installed,
  and whether the read-back was made. `refused` is a run that never started
  (`SKIPPED`); `invalid` lists the settings that did not survive (`INVALID`).
- A runner that ends without its `end` line crashed: `ABORTED`.
- The wall limit is enforced twice: by the runner, which stops the agent and
  still writes its `end` line, and by the harness, which kills the container a
  grace period later. The record says which of the two fired.
- **Guard trips (protocols 4 and 5, 2026-10-23):** the runner notes every
  guard action the SDK announces as a notice (`guard_trip`: guard, phase,
  turn, and the cause or tool when there is one). The first trip the arm's
  `guards.stop_on` names (D100) stops the run: the runner aborts the session
  and ends with the outcome `guard_stop`, which the harness makes `FAIL` after
  running the oracle. From SDK 4.100.259 every guard announces its actions;
  4.100.258 announced only the reasoning-loop and tool-intent guards and the
  nudges (runner/PROTOCOL.md).
- **The reasoning-loop limit (protocol 8, 2026-10-25, D108):** the arm's
  `guards.reasoning_loop.max_consecutive_trips` goes to the SDK as
  `maxConsecutiveTrips`, and the runner answers the SDK's
  `onReasoningLoopLimitReached` from `stop_on` (`stop` only when it names
  `reasoning_loop.aborted`), noting each answer (`guard_limit`). A
  `reasoning_loop` notice of phase `aborted` where the arm goes on, or after
  another number of cut turns than the arm states, is a
  `guard_limit_breach` note and the outcome `invalid`, so the run is
  `INVALID`: the SDK release did not take the answer. Up to 4.100.262 the
  SDK's core does not pass a host's answer on (X9, §8); 4.100.266 does, and
  its notices say the limit that applied (`maxConsecutiveTrips`), which the
  runner also holds against the arm's.

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
| X9 | Pass the host's **`onReasoningLoopLimitReached`** from the session's configuration on to the agent, as core does `onConsecutiveMistakeLimitReached` (D108). | **released in 4.100.266** (2026-10-25); the notice also carries `maxConsecutiveTrips` |
| X11 | Let the session's **abort** reach the capped-thinking note, the retrospective and the compaction requests, and stop a runaway note (bug-045). | **released in 4.100.268** (2026-10-29) |
| X12 | Let the session's **abort** reach the Ollama provider's first-use **probes**, and bound the model and account reads (bug-047). | **released in 4.100.269** (2026-10-29) |
| X13 | Let `sed`, `awk`, `grep` and `search_codebase` refuse a **binary** or a file over 100 MB before reading it whole, instead of taking the runner out of heap (exit 134, `ABORTED`). | **released in 4.100.274** (2026-11-05) |
| X14 | Let every tool result claim only what the tool established: the 2026-11-08 tool audit's findings, the delimiter scan's edits proved by a real parser (bug-082). | **released as 4.100.275** (2026-11-08), first pinned as the local build `4.100.275+da478ecc8a`; on the registry as **4.100.276** (2026-11-08), the default (D143) |
| X15 | Fix the CLI's LSP client (server requests answered and told apart from responses, didChange, settings objects, the HTML server's settings and TypeScript path, the missing servers, the extension's symbol lookup) and `transform_image`'s minified `planTransform`; make the two browser drivers agree; move the host halves of `ask_lsp`, the diagnostics, `check_file` and the browser and render drivers into core, so the runner imports them instead of its ports (D149, D150). | asked 2026-11-12 (owner); **released as 4.100.277** (2026-11-12): taken by the runner on that build, the ports serve older ones (D152) |
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
