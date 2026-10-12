---
paths:
  - .wolf/hooks/**
  - .claude/hooks/**
---

# Hook conventions

- `.wolf/hooks/package.json` sets `"type": "module"`: use `import`/`export`, never `require`.
- Put helpers used by more than one hook in `.wolf/hooks/shared.js`.
- Each hook is registered in `.claude/settings.json` as `node "$CLAUDE_PROJECT_DIR/.wolf/hooks/<name>.js"` with `"timeout": 5`; keep hooks well under 5 seconds.
- Hook names follow the event: `pre-read.js`, `post-read.js`, `pre-write.js`, `post-write.js`, `session-start.js`, `stop.js`.
- Write hooks match `Write|Edit|MultiEdit`; read hooks match `Read`.
- These files are installed by OpenWolf and may be overwritten on upgrade: note local changes in `.wolf/cerebrum.md`.
- `.claude/hooks/caliber-check-sync.sh` and `.claude/hooks/caliber-session-freshness.sh` are owned by Caliber; do not edit by hand.
- Syntax-check after editing: `node --check .wolf/hooks/shared.js`.
- Hooks must not add dependencies: there is no `node_modules` under `.wolf/hooks/`.
