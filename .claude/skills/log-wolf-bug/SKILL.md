---
name: log-wolf-bug
description: Appends a correctly structured entry to `.wolf/buglog.json` (`id`, `timestamp`, `error_message`, `file`, `root_cause`, `fix`, `tags`, `related_bugs`, `occurrences`, `last_seen`) after checking for an existing matching entry to increment instead of duplicating. Also adds a dated `## Do-Not-Repeat` line to `.wolf/cerebrum.md` when the bug is a recurring gotcha, and a row to `.wolf/memory.md`. Use when the user says 'log this bug', 'that was broken', 'doesn't work', 'shows wrong X', after any failed command, failed hook, failed `node --check`, or fix, or after editing one file more than twice. Do NOT use for feature requests, for `**Open**` questions in `docs/specs/automaticum-draft-spec.md`, for owner decisions (those go in the cerebrum `## Decision Log`), or for prose-only edits where nothing was broken.
paths:
  - .wolf/buglog.json
  - .wolf/cerebrum.md
  - .wolf/memory.md
---
# log-wolf-bug

Record a bug in `.wolf/buglog.json` so the next session finds the fix instead of rediscovering it.

## Critical

- **The file is an object, not an array.** Its shape is `{ "version": 1, "bugs": [ ... ] }`. Entries go inside `bugs`. Never replace the file with a bare array and never drop `"version": 1`.
- **Search before you append.** If an entry for the same defect already exists, increment `occurrences` and update `last_seen` on that entry. Do not create a second entry.
- **Mint the `id` from the file as it is on disk right now**, immediately before writing (Step 3). The hook `.wolf/hooks/post-write.js` also writes to this file, so an id computed from an earlier read can collide.
- **Write with the Edit tool, not with shell redirection or a Node script.** `.wolf/hooks/post-write.js` runs `repairDuplicateBugIds` only on Edit/Write tool calls to `.wolf/buglog.json`. A Bash write bypasses that check.
- **Never renumber or delete an existing entry.** Ids are cited from `related_bugs` and from `.wolf/cerebrum.md`.
- **Log only what was observed.** `error_message` is the exact error text or the user's exact words. `root_cause` is what you confirmed. If the cause is unconfirmed, say so in `root_cause`.
- Entries whose `error_message` starts with `auto-note (unverified): ` are written by the hook from the shape of a diff. They are not confirmed bugs. Do not treat one as a match for a real bug unless it is the same file and the same defect (see Step 2).

## Instructions

### Step 1: Collect the facts

Before touching any file, have these five values ready:

| Field | What to put in it |
|---|---|
| `error_message` | Exact stderr line, or the user's complaint quoted verbatim |
| `file` | Repo-relative path with forward slashes of the file that was fixed, e.g. `.wolf/hooks/shared.js` or `.claude/hooks/caliber-check-sync.sh` |
| `root_cause` | One or two sentences: why it broke |
| `fix` | One or two sentences: what you changed |
| `tags` | 2 to 5 lowercase keywords; include the file extension without the dot (`js`, `sh`, `json`, `md`) and the area (`hook`, `caliber`, `openwolf`, `spec`) |

If no fix exists yet (the bug is only reported), set `fix` to `"Not fixed yet: <what is known>"` and update the same entry when the fix lands.

Pick tags that will actually appear in a future edit to that file. `.wolf/hooks/pre-write.js` surfaces a past bug when the edited file has the same basename and either a tag appears in the edit text or 3+ meaningful words overlap with `error_message` + `root_cause`.

**Verify:** you can state all five values without guessing. If `root_cause` is a guess, write it as one.

### Step 2: Look for an existing entry

List what is already logged:

```bash
node -e 'for(const b of JSON.parse(require("fs").readFileSync(".wolf/buglog.json","utf8")).bugs)console.log(b.id,"|",b.file,"|",b.occurrences,"|",String(b.error_message).slice(0,80))'
```

For a large log, narrow it with a keyword from the error or the file basename:

```bash
grep -n -i 'shared.js' .wolf/buglog.json
```

An entry is a match when **both** are true:

1. Same `file` (compare basenames), and
2. Same defect: the same error text, or the same `root_cause`.

- Match found → go to **Step 4a**.
- Same root cause but a different file → no match. Go to Step 3 and put the other entry's id in `related_bugs`.
- Only an `auto-note (unverified): ` entry matches → if it describes the defect you just confirmed, treat it as a match and go to Step 4a (you will promote it). Otherwise go to Step 3.
- No output (the log is empty) → go to Step 3.

**Verify:** you have decided "increment `bug-NNN`" or "new entry" before proceeding.

### Step 3: Mint the next id (new entries only)

Run this immediately before the edit in Step 4b. It mirrors `mintBugId` in `.wolf/hooks/post-write.js`: highest existing number plus one, zero-padded to 3 digits, skipping any id already in use.

```bash
node -e 'const b=JSON.parse(require("fs").readFileSync(".wolf/buglog.json","utf8")).bugs;const u=new Set(b.map(x=>String(x.id)));let n=b.reduce((m,x)=>Math.max(m,parseInt(String(x.id).replace(/[^0-9]/g,""),10)||0),0),id;do{n++;id="bug-"+String(n).padStart(3,"0")}while(u.has(id));console.log(id)'
```

Get the timestamp in the same format the hooks use (`new Date().toISOString()`):

```bash
node -e 'console.log(new Date().toISOString())'
```

**Verify:** the id printed is `bug-` followed by at least 3 digits (`bug-001` on an empty log) and does not appear in the Step 2 listing.

### Step 4a: Increment an existing entry

Uses the id chosen in Step 2. With the Edit tool, on that entry only:

1. Change `"occurrences": N` to `"occurrences": N+1`.
2. Set `"last_seen"` to the timestamp from Step 3's second command.
3. Leave `id` and `timestamp` unchanged. `timestamp` is the first sighting.
4. If this occurrence taught something new, append it to `fix` in the form the hook uses: `<existing fix> | Also: <new detail>`.
5. If you are promoting an `auto-note (unverified): ` entry: replace `error_message` with the real error text, rewrite `root_cause` with the confirmed cause, set `"verified": true`, and remove the `"unverified"` tag. Keep `auto_detected` and `auto-detected` as they are.

`occurrences` and `last_seen` values can repeat across entries, so include the entry's `"id": "bug-NNN"` line or its unique `error_message` in the Edit `old_string` to keep the match unique.

Then go to Step 5.

### Step 4b: Append a new entry

Uses the id and timestamp from Step 3. Field order and 2-space indentation match what `writeJSON` in `.wolf/hooks/shared.js` produces (`JSON.stringify(data, null, 2)`).

**Empty log.** The file is exactly:

```json
{
  "version": 1,
  "bugs": []
}
```

Edit `old_string`: `"bugs": []` → `new_string`:

```json
"bugs": [
    {
      "id": "bug-001",
      "timestamp": "2026-10-12T20:31:07.412Z",
      "error_message": "exact error or user complaint",
      "file": "path/relative/to/repo/root",
      "root_cause": "why it broke",
      "fix": "what you changed to fix it",
      "tags": [
        "relevant",
        "keywords"
      ],
      "related_bugs": [],
      "occurrences": 1,
      "last_seen": "2026-10-12T20:31:07.412Z"
    }
  ]
```

**Non-empty log.** Anchor on the end of the last entry. Edit `old_string` is the last entry's `"last_seen"` line through the closing bracket:

```
      "last_seen": "<last entry's value>"
    }
  ]
```

`new_string` is the same text with `    },` after the last entry, then the new object at the same 4-space indentation, then `  ]`.

Rules for the new object:

- `timestamp` and `last_seen` are the same value on a new entry.
- `occurrences` is the number `1`, not the string `"1"`.
- `related_bugs` is an array of id strings, e.g. `["bug-004"]`, or `[]`.
- `tags` is an array of strings. Never `null`, never omitted: `.wolf/hooks/pre-write.js` calls `bug.tags.some(...)` and `.wolf/hooks/post-write.js` calls `b.tags.includes(...)` on every entry.
- `file`, `error_message`, `root_cause` and `fix` are strings. Never `null`: the hooks call `path.basename(b.file)` and `.slice()` on them.
- Escape `"` as `\"` and backslashes as `\\` inside string values. Replace literal newlines in a pasted error with a space or `\n`.
- Do not add `auto_detected` or `verified` to an entry you write by hand. Those mark hook-written notes.

### Step 5: Validate the file

```bash
node -e 'const l=JSON.parse(require("fs").readFileSync(".wolf/buglog.json","utf8"));const k=["id","timestamp","error_message","file","root_cause","fix","tags","related_bugs","occurrences","last_seen"];const ids=new Set();for(const b of l.bugs){for(const f of k)if(b[f]===undefined||b[f]===null)throw new Error(b.id+" missing "+f);if(ids.has(b.id))throw new Error("duplicate id "+b.id);ids.add(b.id)}console.log("ok","version",l.version,"bugs",l.bugs.length)'
```

**Verify:** the output is `ok version 1 bugs <N>`, where `<N>` is one higher than before for a new entry and unchanged for an increment. If it throws, fix the file with the Edit tool before continuing. Do not leave an invalid `.wolf/buglog.json`: every hook reads it through `readJSON`, which silently falls back to an empty log on a parse error.

### Step 6: Add a Do-Not-Repeat line when the bug is a recurring gotcha

Add a line to `.wolf/cerebrum.md` when **any** of these is true:

- The entry's `occurrences` is now 2 or more.
- The user corrected a mistake you made.
- The cause is a trap a fresh session would walk into again (a wrong assumption about the project, a tool quirk, a path or command that looks right but is not).

Skip this step for a one-off typo.

With the Edit tool, insert under the `## Do-Not-Repeat` heading, after the two HTML comment lines and after any existing entries, directly above the blank line that precedes `## Decision Log`:

```markdown
- [2026-10-12] <what went wrong> — <what to do instead>. (bug-NNN)
```

- The date is today in `YYYY-MM-DD`, in square brackets, as the comment in that section specifies.
- End with the bug id in parentheses so the line traces back to the log.
- Update the header line `> Last updated: YYYY-MM-DD` at the top of `.wolf/cerebrum.md` to today.
- Do not put owner decisions here. Those go under `## Decision Log`.

**Verify:** `grep -n 'bug-NNN' .wolf/cerebrum.md` prints the new line, and its line number falls between the `## Do-Not-Repeat` and `## Decision Log` line numbers from `grep -n '^## ' .wolf/cerebrum.md`.

### Step 7: Append the memory row

Append one row to the end of `.wolf/memory.md` (do not read the whole file first), under the current `## Session:` table:

```markdown
| 22:31 | Logged bug-NNN: <short description> | .wolf/buglog.json | logged | ~150 |
```

Columns are `| Time | Action | File(s) | Outcome | ~Tokens |`. Time is local `HH:MM`. If Step 6 ran, list both files: `.wolf/buglog.json, .wolf/cerebrum.md`.

**Verify:** `tail -3 .wolf/memory.md` shows the new row with 5 columns.

## Examples

### Example 1: new bug in a hook

**User says:** "The session-start hook is broken, it prints `SyntaxError: Unexpected token '}'`."

**Actions:**

1. Read `.wolf/buglog.json` for a known fix (none). Reproduce with `node --check .wolf/hooks/session-start.js`, fix the stray brace, re-run `node --check` until it exits 0.
2. Step 2 listing prints nothing: the log is empty. New entry.
3. Step 3 prints `bug-001` and `2026-10-12T20:31:07.412Z`.
4. Step 4b, empty-log form. Resulting file:

```json
{
  "version": 1,
  "bugs": [
    {
      "id": "bug-001",
      "timestamp": "2026-10-12T20:31:07.412Z",
      "error_message": "SyntaxError: Unexpected token '}' in .wolf/hooks/session-start.js",
      "file": ".wolf/hooks/session-start.js",
      "root_cause": "An extra closing brace was left after the buglog reminder block, ending the enclosing function early.",
      "fix": "Removed the stray brace; node --check .wolf/hooks/session-start.js exits 0.",
      "tags": [
        "hook",
        "session-start",
        "syntax-error",
        "js"
      ],
      "related_bugs": [],
      "occurrences": 1,
      "last_seen": "2026-10-12T20:31:07.412Z"
    }
  ]
}
```

5. Step 5 prints `ok version 1 bugs 1`.
6. Step 6 skipped: one-off typo, first occurrence.
7. Step 7 row: `| 22:31 | Logged bug-001: stray brace in session-start hook | .wolf/buglog.json | logged | ~150 |`

**Result:** one new entry, valid JSON, memory row appended.

### Example 2: the same bug comes back

**User says:** "caliber-check-sync.sh doesn't work again, same `caliber: command not found`."

**Actions:**

1. Step 2 listing shows `bug-003 | .claude/hooks/caliber-check-sync.sh | 1 | caliber: command not found`. Same file, same error: match.
2. Step 4a on `bug-003`: `"occurrences": 1` → `"occurrences": 2`, `last_seen` set to now, `fix` extended with ` | Also: <new detail>`. `id` and `timestamp` untouched.
3. Step 5 prints `ok version 1 bugs 3` (count unchanged).
4. Step 6 runs because `occurrences` is 2. Line added to `.wolf/cerebrum.md`:
   `- [2026-10-12] Ran .claude/hooks/caliber-check-sync.sh without caliber on PATH — run caliber --version first and stop if it fails. (bug-003)`
5. Step 7 row lists `.wolf/buglog.json, .wolf/cerebrum.md`.

**Result:** no duplicate entry; the gotcha is now in the Do-Not-Repeat list every session reads before generating code.

## Common Issues

**`SyntaxError: Unexpected token ... in JSON` or `Expected ',' or '}'` from the Step 5 command**
1. The usual cause is a missing comma after the previous entry's `}` or a trailing comma after the last entry.
2. Find the area: `grep -n '^    }' .wolf/buglog.json`. Every line except the last must be `    },`.
3. The second cause is an unescaped `"` or a literal newline inside `error_message`. Escape it as `\"` or `\n`.

**`Error: duplicate id bug-NNN` from the Step 5 command**
1. The id was minted from a stale read. Re-run the Step 3 command and change the id of the entry **you just added** with the Edit tool.
2. Never renumber the older entry: it may be cited from `related_bugs` or `.wolf/cerebrum.md`.

**stderr shows `OpenWolf: duplicate bug id bug-NNN -> renumbered the auto-note to bug-MMM.`**
- The hook resolved a collision by renumbering its own auto-note. Your entry kept its id. No action needed; re-run Step 5 to confirm.

**stderr shows `⚠️ OpenWolf: duplicate bug id bug-NNN — BOTH entries are substantive, so neither was renumbered`**
1. Two hand-written entries share an id. This is normally the same bug logged twice.
2. Merge them: keep the older entry, add the newer `occurrences` to it, set its `last_seen` to the later value, fold any extra detail into `fix` with ` | Also: `, then delete the newer object.
3. If they are genuinely different bugs, give the one you added in this session a fresh id from Step 3.

**`Error: bug-NNN missing tags` (or another field) from the Step 5 command**
- Add the field with the Edit tool. Use `[]` for `tags` or `related_bugs`, `1` for `occurrences`. A missing `tags` makes `.wolf/hooks/pre-write.js` throw on `bug.tags.some` during later edits to a file with the same basename.

**`TypeError: Cannot read properties of undefined (reading 'length')` from `.wolf/hooks/session-start.js` or `pre-write.js`**
- The top-level `bugs` key is gone, usually because the file was overwritten with a bare array. Restore the wrapper: `{ "version": 1, "bugs": [ ...entries... ] }`.

**stderr still shows `📋 OpenWolf: buglog.json is empty` at the next session start**
1. Check the entry landed inside `bugs`: the Step 5 command must print `bugs 1` or more.
2. Check you edited `.wolf/buglog.json` at the repo root, not a copy elsewhere.

**stderr shows `⚠️ OpenWolf: Files edited 3+ times this session (...) but buglog.json was not updated`**
- `.wolf/hooks/stop.js` checks whether `.wolf/buglog.json` was written through a tool call this session. Run this skill for the listed file. If the repeated edits were not a bug (for example iterating on spec prose), say so to the user instead of logging a fabricated entry.

**Edit tool fails with `old_string` not unique**
- `"occurrences": 1` and identical `last_seen` values repeat across entries. Extend `old_string` upward to include that entry's `"id": "bug-NNN"` line or its `error_message` line.

**The Step 3 command prints `bug-NaN` or throws `Cannot read properties of undefined (reading 'map')`**
- The file is not valid `{ "version": 1, "bugs": [...] }`. Fix the structure first (see the JSON and `TypeError` items above), then mint the id again.