---
name: draft-spec-section
description: Adds or revises a section of `docs/specs/automaticum-draft-spec.md` following the spec's existing pattern: numbered headings, the Vocabulary table terms, `**Open**` markers for undecided points, dated owner decisions, and Appendix A/C cross-updates. Bumps the Status line draft number and date. Use when the user says 'draft section', 'update the spec', 'add to the spec', 'write up X in the spec', gives a bare spec number such as '6.2', or edits files in `docs/specs/`. Do NOT use for implementing code described by the spec or for logging decisions outside the spec.
paths:
  - docs/specs/**
---
# Draft Spec Section

Adds or revises a section of `docs/specs/automaticum-draft-spec.md`, the project's single source of truth, so it reads like the rest of the draft.

## Critical

- **Never turn a question into a decision.** If the owner has not decided a point, mark it `**Open**` in the section and list it in `## Appendix C — open questions for the owner`. Only the owner removes an `**Open**`.
- **Never contradict the owner decisions in section 3** (`**Decided by the owner (2026-10-12):**`): Go for the application and `automaticumd`; a host runs only the Go application (no Node, no Python, no helper scripts); the Node runner lives in the Docker image; lanes run in Docker; Gitea for git remotes; LanceDB as the store, with the owner's dated preference in 10.1 for DuckDB plus Lance.
- **Use the Vocabulary terms from section 2 exactly:** `task`, `suite`, `arm`, `run`, `batch`, `campaign`, `lane`, `endpoint`, `oracle`, `gate`, `finding`. Do not write "job", "experiment", "worker", "slot", "check" or "test case" for these.
- **Do not invent Go packages, `go.mod`, file layouts or build commands** in the spec. Name commands only in the `automaticum <noun> <verb>` form the spec already uses (for example `automaticum run show <id>`).
- **Grep before reading.** The file is about 775 lines (~10k tokens). Read only the line range of the section you touch.
- **Bump the Status line on line 3** in every change set (Step 7).

## Instructions

### Step 1. Locate the section

```bash
grep -n '^#' docs/specs/automaticum-draft-spec.md
```

- For a bare number such as `6.2`, find it with `grep -n '^### 6.2 ' docs/specs/automaticum-draft-spec.md`.
- Read from that heading's line to the line before the next heading, using Read with `offset` and `limit`.
- Check `.wolf/cerebrum.md` (`## Do-Not-Repeat`, `## Decision Log`) before writing.

Verify you have the start and end line of the target section before proceeding.

### Step 2. Choose the heading

Match the existing forms exactly:

| level | form | example from the spec |
|---|---|---|
| top-level section | `## N. Title` (period after the number) | `## 6. Orchestration` |
| subsection | `### N.M Title` (no period) | `### 6.2 Gates (before every run)` |
| unnumbered aside | `### Title` | `### Non-goals` |
| appendix | `## Appendix X — title` (em dash, lower-case title) | `## Appendix C — open questions for the owner` |

- Titles are sentence case. A parenthetical or a command in backticks is allowed: `### 8.1 Per-run report (`automaticum run show <id>`)`.
- **New subsection:** append it as the next free `N.M` at the end of its parent section. Do not insert in the middle.
- **New top-level section:** only when the user asks for one. It goes before `## 11. Acceptance: when it replaces the old harness` only if the user accepts renumbering; otherwise propose a subsection instead.
- Put a `---` line, with a blank line on each side, before every `## N.` section and before `## Appendix A`. There is no `---` between the appendices.

If you renumber anything, find every reference to the old number first:

```bash
grep -n 'see 6\.2\|Section 6\.2\|(see 6\.2)' docs/specs/automaticum-draft-spec.md
```

Verify the new number does not already exist in the Step 1 heading list before proceeding.

### Step 3. Write the body

Uses the line range from Step 1 and the heading from Step 2.

- Wrap prose at 80 columns. Tables and the ASCII diagram in section 3 are exempt.
- Open with one or two sentences that state the rule or the reason. The spec grounds each rule in something that went wrong in the old harness ("Each item is here because its absence invalidated or blinded a real campaign."). Use a real incident from the spec or from the user; do not make one up.
- Bold lead-ins carry the structure: `**The rule: no custom script.**`, `**What this suggests**`, `- **Pause** of a run in flight: ...`.
- Use `-` bullets for parallel items, ending with `;` in a run-on list or `.` for full sentences, as section 6.2 does. Use numbered lists only for ordered criteria (section 11) and Appendix C.
- Use a table for term/meaning or before/after mappings, with a lower-case header row and `|---|---|` separator.
- Vocabulary terms are lower-case plain text in prose (bold only in the section 2 table).
- Verdicts are upper-case in backticks: `PASS`, `FAIL`, `INVALID`, `SKIPPED`. Take any other verdict name from the table in section 4.3; do not coin one.
- Commands, file names, states and config values go in backticks: `automaticum agent`, `chain-*.sh`, `idle`, `gpu=a6000`.
- Cross-references use the bare number: `(see 6.2)`, `see 5.3`, `Section 10.1`, `Appendix B`. No markdown links.
- Write flat declarative sentences: "A lane has a state", "A gate failure produces `SKIPPED` or `INVALID` with the reason". Avoid "should consider" and "might want to".

Verify every Vocabulary term in your text matches a row in the section 2 table before proceeding.

### Step 4. Mark what is undecided

Two forms exist; use whichever fits the sentence:

```markdown
**Open:** whether the daemon may also drive a bare Docker engine over SSH/TLS
with no agent.
```

```markdown
Declarative files (YAML or TOML, **Open**), versioned, validated on load, and
resolved into the run record.
```

- State the question and, when known, the starting point and what would settle it ("LanceDB is the owner's starting point, but its Go support was not checked when it was chosen. Section 10.1 lists what was found").
- Anything you inferred rather than were told is `**Open**`.

Verify with `grep -n 'Open' docs/specs/automaticum-draft-spec.md` that each new marker appears, then proceed.

### Step 5. Record owner decisions with a date

Only when the user (the owner) states a decision in this session. Use today's date in `YYYY-MM-DD` form.

```markdown
**Decided by the owner (2026-10-12):**

- The main application and daemon are written in **Go**.
```

For a preference that keeps fallbacks alive, use the softer form from 10.1:

```markdown
**Owner's preference (2026-10-12): shape 3 below**, DuckDB for state,
configuration, metrics and statistics, with Lance for text chunks and vectors.
```

For facts checked against outside sources, date the check: `Checked on 2026-10-12 against each project's repository and documentation.`

- Remove or rewrite the inline `**Open**` the decision settles.
- Also add the decision to `.wolf/cerebrum.md` under `## Decision Log`, with the same date.

Verify the decision does not conflict with the bullets under `**Decided by the owner` in section 3. If it does, stop and ask the owner which one stands.

### Step 6. Cross-update the appendices

Uses the markers from Steps 4 and 5.

- **Appendix C** (numbered list): add one item for each new `**Open**`, as the next number, phrased as a question to the owner. When a decision settles an item, delete it and renumber, or narrow it the way item 3 does ("the owner prefers DuckDB with Lance for vectors. Still open inside that: ...").
- **Appendix A** (table `| today | what it does | automaticum |`): when the section replaces a script or practice of the old harness, add a row or extend the third column of the existing row. Script names go in backticks in the first column.
- **Appendix B** (bullet list of questions): add a bullet only when the section makes a new question answerable in one command.
- **Section 2 table:** add a row only when the user introduces a new term; format `| **term** | meaning in one or two sentences. |`.

Verify the count of Appendix C items still matches the open points: every `**Open**` in the body maps to an item, and no item refers to a settled point.

### Step 7. Bump the Status line

Line 3 currently starts:

```markdown
**Status:** draft 0.4, 2026-10-12. Written by the Cerebriline session (the first
```

- Raise the draft number by 0.1 (0.4 becomes 0.5) once per change set, not once per edit.
- Set the date to today.
- Leave the rest of the paragraph unchanged.
- Update the draft number in `CLAUDE.md` (`draft 0.4` under `## Status`) to match.

Verify with `sed -n '3p' docs/specs/automaticum-draft-spec.md`.

### Step 8. Check and log

```bash
grep -n '^#' docs/specs/automaticum-draft-spec.md
grep -n 'Open' docs/specs/automaticum-draft-spec.md
grep -c '^---$' docs/specs/automaticum-draft-spec.md
awk 'length > 80 && substr($0,1,1) != "|" {print FNR": "length}' docs/specs/automaticum-draft-spec.md
```

- Headings are in numeric order with no gaps or duplicates.
- The `---` count is 12 unless you added a top-level section (then 13).
- The awk output lists only lines of the section 3 diagram and lines that were already long before your edit.
- Append one row to `.wolf/memory.md`: `| HH:MM | description | file(s) | outcome | ~tokens |`.
- Update the token estimate for `automaticum-draft-spec.md` in `.wolf/anatomy.md` if the file grew noticeably.

## Examples

**User says:** "Add to the spec: a lane that fails three gates in a row gets quarantined. I haven't decided whether it comes back by itself."

**Actions taken:**

1. `grep -n '^#' docs/specs/automaticum-draft-spec.md` shows `### 6.2 Gates (before every run)` and `### 5.1 Lanes`, which already names the `quarantined` state. Read 6.2 only.
2. The rule belongs to gates, so extend 6.2 rather than create a section. Append after the built-in list:

```markdown
A lane whose gates fail on three consecutive runs moves to `quarantined` (see
5.1) and takes no further run. The runs it would have taken stay queued for
another lane. **Open:** whether a quarantined lane returns to `idle` by itself
after its gates pass again, or only when the operator releases it.
```

3. The count of three came from the owner in this session, so it is stated as fact. The return path was not decided, so it is `**Open**`.
4. Add to Appendix C as item 10: `Quarantined lanes: automatic return to `idle` once gates pass, or operator release only?`
5. No old-harness script is replaced, so Appendix A is unchanged.
6. Line 3 becomes `**Status:** draft 0.5, <today>. Written by ...`; `CLAUDE.md` now says `draft 0.5`.
7. Run the Step 8 checks; append a row to `.wolf/memory.md`.

**Result:** 6.2 gains one paragraph in the spec's voice, Appendix C has a tenth question, the draft is 0.5, and no decision was invented.

## Common Issues

- **`grep -n '^### 6.2 '` returns nothing:** the number does not exist. Run `grep -n '^#' docs/specs/automaticum-draft-spec.md` and show the user the nearest headings; do not create the section under a guessed number.
- **Two headings share a number after your edit:** you inserted in the middle. Move the new subsection to the end of its parent as the next free `N.M`, or renumber and fix every `see N.M` / `Section N.M` reference found by the grep in Step 2.
- **A cross-reference points to the wrong section after renumbering:** references are bare numbers, not links, so nothing fails visibly. Run `grep -n 'see [0-9]\|Section [0-9]' docs/specs/automaticum-draft-spec.md` and check each hit against the heading list.
- **`grep -n 'Open'` shows a marker with no Appendix C item:** add the item (Step 6). The reverse, an Appendix C item whose inline `**Open**` is gone, means a decision was recorded without updating the appendix; delete or narrow the item.
- **The text says "job", "worker", "experiment" or "test":** replace with the section 2 term (`run`, `lane`, `batch` or `campaign`, `task`). Check with `grep -n -i 'job\|worker\|experiment' docs/specs/automaticum-draft-spec.md`.
- **The section describes Node, Python or a script running on a host:** this breaks the section 3 decision "A host runs only the Go application". Move the work into the daemon, the `automaticum agent` mode, or a container, or mark the conflict `**Open**` and ask the owner.
- **Status line still says the old draft number:** the bump was skipped. Edit line 3 and the `draft 0.x` mention in `CLAUDE.md` together; `grep -rn 'draft 0\.' CLAUDE.md docs/specs/automaticum-draft-spec.md` must show the same number in both.
- **The awk check prints your new lines:** re-wrap the paragraph at 80 columns. Table rows are exempt and stay on one line.
- **`---` count is 11:** a rule before a `## N.` section was deleted with the text above it. Restore it with a blank line on each side.