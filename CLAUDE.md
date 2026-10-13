# automaticum

@.wolf/OPENWOLF.md

Follow `.wolf/OPENWOLF.md` every session. Check `.wolf/anatomy.md` before reading files and `.wolf/cerebrum.md` before generating code.

## Status

- Milestone 1 (store spike and legacy importer) is approved and in progress; the application code so far is the store in `internal/store/` and the legacy importer in `internal/legacyimport/`. The source of truth is the spec set in `docs/specs/` (`000-overview.md` to `110-acceptance-and-roadmap.md`, plus `decisions.md`). The two input documents are in `docs/specs/archive/` and are not edited.
- automaticum is a test harness that runs coding agents against tasks with a verdict, on lanes it orchestrates itself.
- Executable code today: tooling in `.wolf/hooks/` (Node ESM) and `.claude/hooks/` (Shell), the build setup (`go.mod`, `go.sum`, `Makefile`, `build/`), the store spike in `spike/001-store/`, the store in `internal/store/` and the legacy importer in `internal/legacyimport/`.
- Do not invent Go packages or build commands outside a milestone plan the owner has approved: check `docs/specs/110-acceptance-and-roadmap.md` section 3 and `docs/specs/decisions.md` first.

## Owner decisions

- All owner decisions are in `docs/specs/decisions.md` (D1 onwards). Read it before proposing anything.
- Go daemon `automaticumd`; a host runs only the Go application; Node only inside containers.
- Lanes run in Docker; remote hosts use agent mode only; configuration is TOML; store is DuckDB + Lance + blobs on disk.
- Anything marked `**Open**` in a spec is a question, not a decision. Only the owner decides: ask, never close one by default.

## Vocabulary

- Use the terms of `docs/specs/000-overview.md` section 3 exactly: `task`, `suite`, `arm`, `run`, `batch`, `cell`, `cohort`, `campaign`, `basis`, `lane`, `endpoint`, `oracle`, `gate`, `finding`, `runner`.
- The `oracle` is run by the harness after the agent stops, never by the agent.
- A `gate` must hold before a `run` starts; a `lane` runs one `run` at a time.

## Commands

Spec navigation (grep before reading whole files):

```bash
grep -n '^#' docs/specs/0*.md docs/specs/1*.md
grep -n -F '**Open' docs/specs/*.md
```

OpenWolf hooks (wired in `.claude/settings.json`):

```bash
node .wolf/hooks/session-start.js
node --check .wolf/hooks/shared.js
openwolf designqc
```

Caliber config sync (uses the hooks in `.claude/hooks/`):

```bash
caliber --version
caliber skills --query "go docker"
bash .claude/hooks/caliber-check-sync.sh
```

Native (cgo) builds run only in the build containers (`Makefile`; `SPIKE_ROOT` is a local directory for caches and sources, never stored in a tracked file). `make test` runs the pure-Go packages (`internal/store/blob`, `internal/legacyimport`) on the host; `make test-store` runs `internal/store/...` in the link image and needs the archive from `make lance-c` first:

```bash
make images
make lance-c SPIKE_ROOT=<dir>
make test
make test-store SPIKE_ROOT=<dir>
make shell SPIKE_ROOT=<dir>
```

## Layout

- **Specs** (`docs/specs/`): `000-overview.md` · `010-architecture.md` · `020-run-record.md` · `030-configuration.md` · `040-runner.md` · `050-orchestration.md` · `060-tasks-and-suites.md` · `061-suite-bfcl.md` · `062-suite-swe-bench.md` · `063-suite-multi-language.md` · `064-engine-adapters.md` · `065-cell-contract.md` · `070-diagnostics.md` · `080-store.md` · `090-interfaces.md` · `100-legacy-import.md` · `110-acceptance-and-roadmap.md` · `decisions.md`
- **Inputs** (`docs/specs/archive/`): `automaticum-draft-spec-0.4.md` · `omnimergekit-evaluation-requirements-0.1.md`
- **Build** (repo root and `build/`): `go.mod` · `go.sum` · `Makefile` · `LICENSE` · `build/Dockerfile.build` · `build/Dockerfile.cross`
- **Store spike** (`spike/001-store/`): `linkcheck/README.md`
- **Store** (`internal/store/`): `store.go` · `blob/` (`doc.go` · `blob.go` · `put.go` · `read.go` · `archive.go` · `check.go` · `format.go` · `FORMAT.md`) · `duck/duck.go` · `lance/lance.go` · `storetest/` (`doc.go` · `crash_test.go`)
- **Legacy importer** (`internal/legacyimport/`): `doc.go` · `importer.go` · `scan.go` · `parse.go` · `stream.go` · `mapping.go` · `sink.go` · `verify.go` · `types.go`
- **Local, untracked** (`docs/local/`): host mapping and private paths
- **OpenWolf state** (`.wolf/`): `OPENWOLF.md` · `anatomy.md` · `cerebrum.md` · `memory.md` · `buglog.json` · `identity.md` · `config.json` · `token-ledger.json` · `cron-manifest.json` · `cron-state.json` · `suggestions.json` · `designqc-report.json` · `reframe-frameworks.md` · `daemon.log`
- **OpenWolf hooks** (`.wolf/hooks/`): `session-start.js` · `pre-read.js` · `post-read.js` · `pre-write.js` · `post-write.js` · `stop.js` · `shared.js` · `package.json`
- **Claude config** (`.claude/`): `settings.json` · `rules/openwolf.md` · `rules/spec-conventions.md` · `rules/wolf-hooks.md` · `hooks/caliber-check-sync.sh` · `hooks/caliber-session-freshness.sh`
- **Skills** (`.claude/skills/`): `add-wolf-hook/` · `draft-spec-section/` · `find-skills/` · `log-wolf-bug/` · `record-decision/` · `save-learning/` · `setup-caliber/`

## Conventions

- Add an entry to `.wolf/anatomy.md` whenever you create, rename or delete a file.
- Append one row to `.wolf/memory.md` after each significant action.
- Read `.wolf/buglog.json` before a fix and append to it after.
- Record owner decisions in `docs/specs/decisions.md` and in `.wolf/cerebrum.md` under `## Decision Log`, with the date.
- The repo is public: tracked files use the neutral host names (`linux-gpu-1`, `linux-gpu-2`, `windows-1`, `mac-1`) and no absolute paths.
- Do not hand-edit `.claude/settings.json`; hooks there are managed by OpenWolf and Caliber.

## Tooling constraints

- Caliber must use the `claude-cli` provider; never the Anthropic API provider (no API key exists).
- `caliber init` and `caliber regenerate` are token-expensive: confirm timing with the user first.
- MCP servers available: `pgvector` (persistent memory), `context7` (library docs), `gitnexus` and `code-graph` (code analysis).
- Never write large files to `/tmp` (tmpfs); use persistent disk.
- Ask before unloading `ollama` models or freeing GPU VRAM; the user may be mid-session.

<!-- caliber:managed:pre-commit -->
## Before Committing

**IMPORTANT:** Before every git commit, you MUST ensure Caliber syncs agent configs with the latest code changes.

First, check if the pre-commit hook is already installed:
```bash
grep -q "caliber" .git/hooks/pre-commit 2>/dev/null && echo "hook-active" || echo "no-hook"
```

- If **hook-active**: the hook handles sync automatically — just commit normally. Tell the user: "Caliber will sync your agent configs automatically via the pre-commit hook."
- If **no-hook**: run Caliber manually before committing:
  1. Tell the user: "Caliber: Syncing agent configs with your latest changes..."
  2. Run: `caliber refresh && git add CALIBER_LEARNINGS.md CLAUDE.md .claude/ 2>/dev/null`
  3. After it completes, briefly tell the user what Caliber updated. Then proceed with the commit.

**Valid `caliber refresh` options:** `--quiet` (suppress output) and `--dry-run` (preview without writing). Do not pass any other flags — options like `--auto-approve`, `--debug`, or `--force` do not exist and will cause errors.

**`caliber config`** takes no flags — it runs an interactive provider setup. Do not pass `--provider`, `--api-key`, or `--endpoint`.

If `caliber` is not found, tell the user: "This project uses Caliber for agent config sync. Run /setup-caliber to get set up."
<!-- /caliber:managed:pre-commit -->

<!-- caliber:managed:learnings -->
## Session Learnings

Read `CALIBER_LEARNINGS.md` for patterns and anti-patterns learned from previous sessions.
These are auto-extracted from real tool usage — treat them as project-specific rules.
<!-- /caliber:managed:learnings -->

<!-- caliber:managed:sync -->
## Context Sync

This project uses [Caliber](https://github.com/caliber-ai-org/ai-setup) to keep AI agent configs in sync across Claude Code, Cursor, Copilot, and Codex.
Configs update automatically before each commit via `caliber refresh`.
If the pre-commit hook is not set up, run `/setup-caliber` to configure everything automatically.
<!-- /caliber:managed:sync -->
