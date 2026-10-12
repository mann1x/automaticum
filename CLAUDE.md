# automaticum

@.wolf/OPENWOLF.md

Follow `.wolf/OPENWOLF.md` every session. Check `.wolf/anatomy.md` before reading files and `.wolf/cerebrum.md` before generating code.

## Status

- Spec stage: no application code yet. The source of truth is `docs/specs/automaticum-draft-spec.md` (draft 0.4).
- automaticum is a test harness that runs coding agents against tasks with a verdict, on lanes it orchestrates itself.
- The only executable code today is tooling: `.wolf/hooks/` (Node ESM) and `.claude/hooks/` (Shell).
- Do not invent Go packages, `go.mod` or build commands until the owner creates them.

## Owner decisions (spec section 3)

- Main application and daemon `automaticumd` are written in Go.
- A host runs only the Go application: no Node, no Python, no helper scripts on a host.
- The Node runner lives in the Docker image; lanes run in Docker from a slim base image.
- Gitea provides git remotes for tasks.
- Store: the owner prefers DuckDB (state, configuration, metrics, statistics) plus Lance through `lancedb-go` (text chunks, vectors, full-text), to be confirmed by the spike in spec section 10.1.
- Anything marked `**Open**` in `docs/specs/automaticum-draft-spec.md` is a question, not a decision.

## Vocabulary

- Use the spec's terms exactly: `task`, `suite`, `arm`, `run`, `batch`, `campaign`, `lane`, `endpoint`, `oracle`, `gate`, `finding`.
- The `oracle` is run by the harness after the agent stops, never by the agent.
- A `gate` must hold before a `run` starts; a `lane` runs one `run` at a time.

## Commands

Spec navigation (the file is ~10k tokens, so grep before reading):

```bash
grep -n '^#' docs/specs/automaticum-draft-spec.md
grep -n 'Open' docs/specs/automaticum-draft-spec.md
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

## Layout

- **Spec** (`docs/specs/`): `automaticum-draft-spec.md` · `omnimergekit-evaluation-requirements.md`
- **OpenWolf state** (`.wolf/`): `OPENWOLF.md` · `anatomy.md` · `cerebrum.md` · `memory.md` · `buglog.json` · `identity.md` · `config.json` · `token-ledger.json` · `cron-manifest.json` · `cron-state.json` · `suggestions.json` · `designqc-report.json` · `reframe-frameworks.md` · `daemon.log`
- **OpenWolf hooks** (`.wolf/hooks/`): `session-start.js` · `pre-read.js` · `post-read.js` · `pre-write.js` · `post-write.js` · `stop.js` · `shared.js` · `package.json`
- **Claude config** (`.claude/`): `settings.json` · `rules/openwolf.md` · `rules/spec-conventions.md` · `rules/wolf-hooks.md` · `hooks/caliber-check-sync.sh` · `hooks/caliber-session-freshness.sh`
- **Skills** (`.claude/skills/`): `add-wolf-hook/` · `draft-spec-section/` · `find-skills/` · `log-wolf-bug/` · `record-decision/` · `save-learning/` · `setup-caliber/`

## Conventions

- Add an entry to `.wolf/anatomy.md` whenever you create, rename or delete a file.
- Append one row to `.wolf/memory.md` after each significant action.
- Read `.wolf/buglog.json` before a fix and append to it after.
- Record owner decisions in `.wolf/cerebrum.md` under `## Decision Log`, with the date.
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
