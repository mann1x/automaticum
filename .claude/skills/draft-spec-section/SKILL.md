---
name: draft-spec-section
description: Adds or revises a section of the spec set in `docs/specs/` (`NNN-name.md`) following its pattern: numbered sections, the vocabulary of `000-overview.md`, `**Open**` markers for undecided points, dated owner decisions, and matching rows in `decisions.md`. Bumps the file's Status line. Use when the user says 'draft section', 'update the spec', 'add to the spec', 'write up X in the spec', names a spec such as '050' or '050 §3', or edits files in `docs/specs/`. Do NOT use for implementing code described by a spec, or for editing `docs/specs/archive/`.
paths:
  - docs/specs/*.md
---
# Draft Spec Section

## Critical

- Follow `.claude/rules/spec-conventions.md`.
- Only the owner decides. New undecided points are `**Open**` with a recommendation and a row in `docs/specs/decisions.md` section 3.
- Use only the terms in `docs/specs/000-overview.md` section 3. Add a row there first for a new term.
- Never edit `docs/specs/archive/`.
- The repo is public: neutral host names, no absolute paths.

## Steps

1. Find the place: `grep -n '^#' docs/specs/NNN-*.md`. Pick the file from the map in `000-overview.md` section 4.
2. Find what refers to it: `grep -rn 'NNN §' docs/specs/*.md`.
3. Write the section as requirements, each with the reason it exists when one is known. Keep section numbers stable; append rather than renumber.
4. Add or update rows in `decisions.md` (O for open, D for decided by the owner, X for an external dependency).
5. If the section is new, add it to the traceability table in `000-overview.md` section 5 when it comes from an input document.
6. Bump the `**Status:**` line (draft number, date) of every file you changed.
7. Update `.wolf/anatomy.md` and append a row to `.wolf/memory.md`.

## Verify

```bash
grep -n -F '**Open' docs/specs/*.md          # every hit has a row in decisions.md
grep -rn '](' docs/specs/*.md | grep -o '([0-9a-z./-]*\.md)' | sort -u   # link targets exist
```
