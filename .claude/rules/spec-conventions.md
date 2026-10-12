---
paths:
  - docs/specs/**
---

# Spec conventions

- `docs/specs/automaticum-draft-spec.md` is ~10k tokens: run `grep -n '^#' docs/specs/automaticum-draft-spec.md` and read only the section you need.
- Keep the `**Status:**` line current: bump the draft number and date on every substantive edit.
- Mark undecided points `**Open**`; mark owner choices `**Decided by the owner (YYYY-MM-DD):**`.
- Never turn an `**Open**` item into a decision without the owner saying so.
- New open questions also go in `## Appendix C`.
- Use only the terms in the `## 2. Vocabulary` table; add a row there before introducing a new term.
- Keep section numbering stable (`## 5.`, `### 5.3`); other sections cross-reference by number ("see 6.2").
- When a section replaces an old script, name the script in `## Appendix A`.
- CLI examples use the `automaticum <noun> <verb>` shape, e.g. `automaticum run show <id>`.
- Respect the hard constraints of section 3: Go on the host, Node only inside the Docker image.
- Mirror every new owner decision in `.wolf/cerebrum.md` under `## Decision Log`.
