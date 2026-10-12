# OpenWolf protocol

Full protocol is in `.wolf/OPENWOLF.md`; this is the file-to-trigger map.

- `.wolf/anatomy.md`: check before reading any project file; update after creating, renaming or deleting one.
- `.wolf/cerebrum.md`: check `## Do-Not-Repeat` before generating code; update immediately after a user correction or a discovered convention.
- `.wolf/memory.md`: append one row after each significant action.
- `.wolf/buglog.json`: read before fixing any bug; append `error_message`, `root_cause`, `fix` and `tags` after fixing.
- Editing one file more than twice in a session counts as a bug: log it to `.wolf/buglog.json`.
- UI design checks: run `openwolf designqc`, then read the captured screenshots.
- UI framework changes: read the decision questions in `.wolf/reframe-frameworks.md` first, not the whole file.
