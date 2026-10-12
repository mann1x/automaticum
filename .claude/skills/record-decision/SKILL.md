---
name: record-decision
description: Records an owner decision consistently: converts the matching `**Open**` item in the spec set (`docs/specs/NNN-name.md`) into a dated 'Decided by the owner' entry, moves its row in `docs/specs/decisions.md` from Open to Decided, and appends it to `## Decision Log` in `.wolf/cerebrum.md`. Use when the user says 'we decided', 'go with X', 'that's settled', 'decision:', or answers an open question from the specs. Do NOT use for tentative ideas, personal preferences (use save-learning), or questions that are still open.
paths:
  - docs/specs/*.md
  - .wolf/cerebrum.md
---
# Record Decision

## Critical

- Only the owner decides. Record a decision only when the user states it outright. If the wording is tentative ('maybe', 'I lean towards'), ask once: 'Record this as decided, or leave it Open?'
- A preference is not a decision (D6 in `decisions.md` is recorded as a preference pending a spike).
- If an answer is ambiguous or conflicts with an earlier one, record the reading you chose as a note under the decision table, as was done for D15.
- Resolve only what was decided. Keep the unresolved part of a bundled question Open.

## Steps

1. Find the item: `grep -n -F '**Open' docs/specs/*.md` and its row in `docs/specs/decisions.md` section 3.
2. In the spec, replace the `**Open:**` text with `**Decided by the owner (YYYY-MM-DD):**` and the decision, and adjust the surrounding requirements to match.
3. In `decisions.md`: remove the O row, add a D row with the next id and the place (`NNN §n`). If an alternative from an input document was rejected, add it to section 5.
4. Follow consequences: `grep -rn` for the affected terms in the other specs and in `README.md`.
5. Append to `.wolf/cerebrum.md` under `## Decision Log`, dated, with the rationale in the owner's words or `rationale not stated`.
6. Bump the `**Status:**` line of each spec changed; append a row to `.wolf/memory.md`.

## Verify

```bash
grep -n -F '**Open' docs/specs/*.md      # the resolved item is gone
grep -n 'D[0-9]* ' docs/specs/decisions.md | tail -3
```
