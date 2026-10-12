---
name: record-decision
description: Records an owner decision consistently in two places: converts the matching `**Open**` item in `docs/specs/automaticum-draft-spec.md` into a dated 'Decided by the owner' entry and appends the rationale to `## Decision Log` in `.wolf/cerebrum.md`. Removes the resolved question from Appendix C. Use when the user says 'we decided', 'go with X', 'that's settled', 'decision:', or answers an open question from the spec. Do NOT use for tentative ideas, personal preferences (use save-learning), or questions that are still open.
paths:
  - docs/specs/automaticum-draft-spec.md
  - .wolf/cerebrum.md
---
# Record Decision

Record an owner decision in the spec and in the OpenWolf decision log so both say the same thing with the same date.

## Critical

- Only the owner decides. Record a decision only when the user states it outright ('go with TOML', 'that's settled'). If the wording is tentative ('maybe', 'I lean towards', 'probably'), ask once: 'Record this as decided, or leave it Open?' and stop until answered.
- A preference is not a decision. Appendix C item 3 ('the owner prefers DuckDB with Lance') stays open until the owner says it is settled.
- One decision touches up to three places in `docs/specs/automaticum-draft-spec.md` (the inline `**Open**` marker, the section's `**Decided by the owner (YYYY-MM-DD):**` block, Appendix C) plus `.wolf/cerebrum.md`. Never update only one of them.
- Resolve only what was decided. Appendix C items often bundle several questions (item 1 covers config format AND repo layout). Keep the unresolved part.
- Use the spec's vocabulary exactly: `task`, `suite`, `arm`, `run`, `batch`, `campaign`, `lane`, `endpoint`, `oracle`, `gate`, `finding`.
- Do not change the `**Status:** draft 0.4, ...` line or the draft number unless the owner asks.
- The spec is ~10k tokens. Grep for the lines you need; do not read the whole file.

## Instructions

### Step 1: State the decision in one sentence

Write down three things from the user's message:

- **Chosen**: what was decided (for example 'config files are TOML').
- **Rejected**: the alternative(s) the spec listed (for example 'YAML').
- **Rationale**: why, in the owner's words. If the user gave no reason, ask once: 'What is the reason, for the decision log?' If they decline, record `rationale not stated`.

Get today's date with `date +%F`. Use this one value everywhere below.

Verify you have Chosen, Rejected, Rationale and the date before proceeding.

### Step 2: Find every place the question appears

Run all three:

```bash
grep -n -F '**Open' docs/specs/automaticum-draft-spec.md
grep -n -A 22 '^## Appendix C' docs/specs/automaticum-draft-spec.md
grep -n '^#' docs/specs/automaticum-draft-spec.md
```

The inline marker has three spellings in the spec; match all of them:

- `(YAML or TOML, **Open**)` - marker inside a parenthesis
- `**Open:** whether the daemon may also drive a bare Docker engine...` - marker opening a sentence
- `**Pause** of a run in flight: **Open** — the SDK can suspend...` - marker followed by a dash

From the output, note:

- the line number(s) of the inline marker, if any (some Appendix C items, such as 7, 8 and 9, have none)
- the Appendix C item number, and whether the decision resolves the whole item or part of it
- the numbered section (`## N.` or `### N.M`) the decision belongs to

If nothing in the spec matches, the decision is new: skip the inline edit in Step 3 and the Appendix C edit in Step 5, and say so in the final report.

Verify you have a target section before proceeding. If two sections could own it, ask the user which.

### Step 3: Rewrite the inline `**Open**` text (uses Step 2 line numbers)

Read only the lines around the marker (`Read` with `offset`/`limit`). Replace the question with the decision as a plain statement, and delete the `**Open**` marker and the text that only described the question.

Before:

```markdown
Declarative files (YAML or TOML, **Open**), versioned, validated on load, and
resolved into the run record. No environment-variable knobs.
```

After:

```markdown
Declarative files (TOML), versioned, validated on load, and
resolved into the run record. No environment-variable knobs.
```

Keep the spec's wrapping at about 80 columns and its bullet indentation (continuation lines indented two spaces).

Verify: `grep -n -F '**Open' docs/specs/automaticum-draft-spec.md` no longer lists the resolved line.

### Step 4: Add the dated 'Decided by the owner' entry (uses Step 1 and Step 2)

The only existing pattern is at the top of `## 3. Architecture`:

```markdown
## 3. Architecture

**Decided by the owner (2026-10-12):**

- The main application and daemon are written in **Go**.
- **A host runs only the Go application.** No Node, no Python, no helper
  scripts on a host. Whatever else is needed runs in a container.
```

Apply it to the target section:

1. Check for a block: `grep -n 'Decided by the owner' docs/specs/automaticum-draft-spec.md`.
2. If the section has a block with **today's date**, append one bullet to it.
3. If the section has a block with an **older date**, do not edit that date. Add a new block directly below the old block's last bullet, with today's date.
4. If the section has no block, insert one directly under the section heading, before the first paragraph, with a blank line above and below.

Bullet format: one sentence stating the decision, the chosen thing in bold, optionally a second sentence for the consequence. No rationale here - that goes in the cerebrum.

```markdown
**Decided by the owner (2026-10-14):**

- Configuration files are **TOML**, not YAML.
```

If the decision overturns an earlier decided bullet, edit that bullet's block in the same way (new dated block stating the new decision) and remove the superseded bullet. Tell the user which bullet was superseded.

Verify: `grep -n -A 6 'Decided by the owner' docs/specs/automaticum-draft-spec.md` shows the new bullet under the correct section and date.

### Step 5: Remove the question from Appendix C (uses Step 2 item number)

- **Whole item resolved**: delete the item and renumber the items below it so the list stays `1.` to `N.` with no gaps.
- **Part of an item resolved**: delete only the resolved clause and keep the rest, with the number unchanged.

Partial example, after deciding TOML but not the repo layout:

```markdown
1. One repo for tasks or tasks as their own repos.
```

Then check for references to renumbered items:

```bash
grep -n -i 'appendix c' docs/specs/automaticum-draft-spec.md .wolf/cerebrum.md
```

Fix any reference that names a number you shifted.

Verify: `grep -n -A 22 '^## Appendix C' docs/specs/automaticum-draft-spec.md` shows consecutive numbering and no trace of the resolved question.

### Step 6: Append to `## Decision Log` in `.wolf/cerebrum.md` (uses Step 1)

`## Decision Log` is the last section of the file and holds one HTML comment line. Append below it; do not remove the comment and do not read the whole file first.

Entry format (date prefix matches the `[YYYY-MM-DD]` format documented under `## Do-Not-Repeat`):

```markdown
- [2026-10-14] **TOML over YAML for configuration files** (spec section 5). Why: <rationale in the owner's words>. Rejected: YAML.
```

Rules:

- The bold title is `<Chosen> over <Rejected>`; if nothing was rejected, use the chosen thing alone.
- Name the spec section number so the next session can jump to it.
- Same date as the spec's Decided block.

Then update the header line `> Last updated: YYYY-MM-DD` to today's date.

Verify: `grep -n -A 8 '^## Decision Log' .wolf/cerebrum.md` shows the new entry.

### Step 7: Update the summaries

1. If the decision belongs to spec section 3, add one bullet to `## Owner decisions (spec section 3)` in `CLAUDE.md`, matching the existing bullets (one line, ends with a full stop).
2. Append one row to `.wolf/memory.md` under the current `## Session:` table:

   ```markdown
   | 14:32 | Recorded decision: TOML over YAML | docs/specs/automaticum-draft-spec.md, .wolf/cerebrum.md | spec section 5 decided, Appendix C item 1 trimmed | ~300 |
   ```

3. No `.wolf/anatomy.md` change: no file was created, renamed or deleted.

### Step 8: Final check and report

```bash
grep -c -F '**Open' docs/specs/automaticum-draft-spec.md
git diff --stat -- docs/specs/automaticum-draft-spec.md
```

The count must be lower than in Step 2 by the number of markers you resolved (unchanged if the question had no inline marker). Tell the user: the decision sentence, the spec section and date it was recorded under, which Appendix C item was removed or trimmed, and how many `**Open**` markers remain. Do not commit unless asked.

## Examples

### Example 1: answering an open question with an inline marker

**User says:** 'Decision: config is TOML. YAML's implicit typing has bitten us before.'

**Actions taken:**

1. Chosen: TOML. Rejected: YAML. Rationale: YAML's implicit typing caused problems before. `date +%F` returns `2026-10-14`.
2. `grep -n -F '**Open'` finds line 217 `Declarative files (YAML or TOML, **Open**), ...`; Appendix C item 1 covers config format and repo layout; target section is `## 5. Configuration`.
3. Line 217 becomes `Declarative files (TOML), versioned, validated on load, and`.
4. Section 5 has no Decided block, so one is inserted under the heading:

   ```markdown
   ## 5. Configuration

   **Decided by the owner (2026-10-14):**

   - Configuration files are **TOML**, not YAML.

   Declarative files (TOML), versioned, validated on load, and
   ```

5. Appendix C item 1 is trimmed to `1. One repo for tasks or tasks as their own repos.` No renumbering.
6. `.wolf/cerebrum.md` gets `- [2026-10-14] **TOML over YAML for configuration files** (spec section 5). Why: YAML's implicit typing has caused problems before. Rejected: YAML.` and `> Last updated: 2026-10-14`.
7. One row appended to `.wolf/memory.md`. `CLAUDE.md` unchanged (not section 3).

**Result:** 'Recorded: configuration files are TOML (spec section 5, 2026-10-14). Appendix C item 1 now asks only about the task repo layout. 3 `**Open**` markers remain.'

### Example 2: question that exists only in Appendix C

**User says:** 'That's settled: verdicts are PASS/FAIL.'

**Actions taken:** No inline marker exists for item 9, so Step 3 is skipped. A Decided block is added under `### 4.3 The verdict`, item 9 is deleted from Appendix C (it is last, so no renumbering), and the cerebrum entry is `- [<date>] **PASS/FAIL over FIXED/broken for verdict names** (spec section 4.3). Why: rationale not stated. Rejected: FIXED/broken.` after the user declined to give a reason.

**Result:** Marker count unchanged; Appendix C has one item fewer.

## Common Issues

- **`grep -n -F '**Open'` does not show the question the user answered.** The question lives only in Appendix C (items 7, 8 and 9 have no inline marker). Run `grep -n -A 22 '^## Appendix C' docs/specs/automaticum-draft-spec.md`, skip Step 3, and pick the target section from `grep -n '^#'`.
- **Edit fails with 'old_string not found' on a spec line.** The text wraps across two lines at about 80 columns. Read the exact lines with `Read` (`offset` = line number minus 2, `limit` = 6) and copy the line break into `old_string`.
- **Edit fails with 'old_string is not unique' on `**Open**`.** Never use the bare marker as `old_string`. Include the surrounding words, for example `(YAML or TOML, **Open**)`.
- **The marker count did not drop after Step 3.** You rewrote the sentence but left the marker, or edited the Appendix C line instead of the inline one. Re-run `grep -n -F '**Open'` and compare against the Step 2 line numbers.
- **Appendix C numbering has a gap (1, 2, 4).** You deleted a whole item without renumbering. Renumber every item below it, then re-run `grep -n -i 'appendix c'` to fix references.
- **Two Decided blocks in one section with the same date.** Merge them: one `**Decided by the owner (YYYY-MM-DD):**` line per section per date, with all that day's bullets under it.
- **The cerebrum entry landed under `## Do-Not-Repeat` or `## Key Learnings`.** `## Decision Log` is the last heading in `.wolf/cerebrum.md`; append at end of file. Check with `grep -n '^## ' .wolf/cerebrum.md`.
- **The decision contradicts an existing decided bullet** (for example the store: section 3 says LanceDB, Appendix C item 3 says the owner prefers DuckDB with Lance). Do not pick one. Quote both lines to the user and ask which stands, then follow the supersede rule in Step 4.
- **The user's answer covers only half of a bundled question.** Record the decided half, keep the other half in Appendix C and leave its inline `**Open**` marker in place.