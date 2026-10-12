# Spec conventions

- The spec set is `docs/specs/NNN-name.md`; `000-overview.md` holds the vocabulary, the map and the traceability table. Grep headings (`grep -n '^#' docs/specs/NNN-*.md`) and read only the section you need.
- `docs/specs/archive/` holds the two input documents. Do not edit them.
- Keep each file's `**Status:**` line current: bump the draft number and date on every substantive edit.
- Mark undecided points `**Open**`; mark owner choices `**Decided by the owner (YYYY-MM-DD):**`.
- Never turn an `**Open**` item into a decision without the owner saying so. Carry a recommendation and ask.
- Every Open item, decision and external dependency also has a row in `docs/specs/decisions.md` (ids `O`, `D`, `X`).
- Use only the terms in `000-overview.md` section 3; add a row there before introducing a new term.
- Cross-reference by file number and section ("050 §3.2"). Keep section numbers stable.
- When a section replaces an old script, name it in `100-legacy-import.md` section 5.
- CLI examples use the `automaticum <noun> <verb>` shape, e.g. `automaticum run show <id>`; the full list is in `090-interfaces.md` section 1.
- Respect the hard constraints of `010-architecture.md` section 1: Go on the host, Node only inside containers.
- The repo is public: neutral host names only (`linux-gpu-1`, `linux-gpu-2`, `windows-1`, `mac-1`), no absolute paths or addresses.
- Mirror every new owner decision in `.wolf/cerebrum.md` under `## Decision Log`.
