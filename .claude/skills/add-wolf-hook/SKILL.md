---
name: add-wolf-hook
description: Creates or modifies an OpenWolf hook script in `.wolf/hooks/` as a Node ESM module, reusing helpers from `.wolf/hooks/shared.js`, and documents the matching registration entry in `.claude/settings.json` (event, matcher, `node "$CLAUDE_PROJECT_DIR/..."` command, 5 second timeout). Verifies with `node --check` and a piped-stdin smoke run. Use when the user says 'add a hook', 'new hook', 'change the pre-read hook', 'hook on write', or edits files in `.wolf/hooks/`. Do NOT use for the Caliber shell hooks in `.claude/hooks/` or for git pre-commit hooks (use setup-caliber).
paths:
  - .wolf/hooks/**
---
# Add Wolf Hook

Create or modify an OpenWolf hook in `.wolf/hooks/` so it matches the seven existing modules (`session-start.js`, `pre-read.js`, `post-read.js`, `pre-write.js`, `post-write.js`, `stop.js`, `shared.js`).

## Critical

- **A hook never blocks Claude.** Every existing hook exits `0` on every path, including bad input and thrown errors. Never call `process.exit(1)` or `process.exit(2)`.
- **Messages go to stderr only.** Use `process.stderr.write("... \n")`. No existing hook writes to stdout or uses `console.log`.
- **Node builtins only.** `.wolf/hooks/package.json` is exactly `{ "type": "module" }`. Do not add dependencies, a lockfile or `node_modules`. Import builtins with the `node:` prefix.
- **ESM only.** Use `import`, never `require`. A relative import must carry the extension: `"./shared.js"`.
- **Stay inside the timeout.** `readStdin()` in `shared.js` gives up after 4000 ms and the registered timeout is 5 seconds, so the hook body must be synchronous file work only: no network calls, no child processes.
- **Do not touch the Caliber entries** in `.claude/settings.json` (`caliber learn ...`, `.claude/hooks/caliber-*.sh`). They are out of scope for this skill.
- **Show the `.claude/settings.json` entry to the user before applying it.** Add only the new entry and leave every existing entry byte-for-byte unchanged.
- The stock hooks end with `//# sourceMappingURL=<name>.js.map`, so they are compiled output shipped by OpenWolf. Prefer a new file over editing a stock one, and tell the user that an edit to a stock hook can be overwritten by an OpenWolf upgrade.

## Instructions

### Step 1: Pick the event, matcher and file name

Read `.claude/settings.json` and choose from the patterns already registered:

| Event | Matcher | Existing file | Timeout |
|---|---|---|---|
| `SessionStart` | `""` | `session-start.js` | 5 |
| `PreToolUse` | `"Read"` | `pre-read.js` | 5 |
| `PreToolUse` | `"Write\|Edit\|MultiEdit"` | `pre-write.js` | 5 |
| `PostToolUse` | `"Read"` | `post-read.js` | 5 |
| `PostToolUse` | `"Write\|Edit\|MultiEdit"` | `post-write.js` | 10 |
| `Stop` | `""` | `stop.js` | 10 |

(The matcher in the JSON file is `Write|Edit|MultiEdit` with plain pipes.)

Naming: lower-case kebab-case, `<phase>-<subject>.js`, for example `pre-bash.js`, `post-bash.js`. Session-level hooks are named after the event: `session-start.js`, `stop.js`.

If the request changes the behavior of an existing event and matcher pair, modify that file instead of registering a second hook for the same pair.

**Verify** before Step 2: `ls .wolf/hooks/` does not already contain the chosen name (new hook), or does contain it (modification).

### Step 2: Check what `shared.js` already provides

List the exports instead of reading the 613-line file:

```bash
grep -n '^export function' .wolf/hooks/shared.js
```

Reuse these and do not reimplement them:

- `getWolfDir()` returns `<CLAUDE_PROJECT_DIR or cwd>/.wolf`
- `ensureWolfDir()` exits `0` silently when `.wolf/` is missing
- `readJSON(filePath, fallback)` never throws, returns `fallback` on error
- `writeJSON(filePath, data)` writes atomically through a `.tmp` file and rename
- `readMarkdown(filePath)` returns `""` on error
- `appendMarkdown(filePath, line)` appends, creating the directory if needed
- `parseAnatomy(content)` returns sections, iterated as `[sectionKey, entries]` with `entry.file`, `entry.description`, `entry.tokens`
- `serializeAnatomy(sections, metadata)`, `extractDescription(filePath)`
- `estimateTokens(text, type)` with `type` one of `"code"`, `"prose"`, `"mixed"`
- `timestamp()` ISO string, `timeShort()` `HH:MM`
- `readStdin()` resolves the hook payload as a string
- `normalizePath(p)` converts backslashes to forward slashes

Add a new helper to `shared.js` only when two or more hooks need it. Write it as `export function name(...) {` at column 0, like the others.

**Verify** before Step 3: every helper you plan to import appears in the grep output.

### Step 3: Write the hook

Use this skeleton. It is the shape shared by `pre-read.js`, `post-read.js` and `pre-write.js`. Style: 4-space indent, double quotes, semicolons, `catch` on its own line after the closing brace, optional chaining with `??` defaults.

```js
import * as path from "node:path";
import { getWolfDir, ensureWolfDir, readJSON, writeJSON, readStdin, normalizePath } from "./shared.js";
async function main() {
    ensureWolfDir();
    const wolfDir = getWolfDir();
    const hooksDir = path.join(wolfDir, "hooks");
    const sessionFile = path.join(hooksDir, "_session.json");
    const raw = await readStdin();
    let input;
    try {
        input = JSON.parse(raw);
    }
    catch {
        process.exit(0);
        return;
    }
    const filePath = input.tool_input?.file_path ?? input.tool_input?.path ?? "";
    if (!filePath) {
        process.exit(0);
        return;
    }
    const normalizedFile = normalizePath(filePath);
    const session = readJSON(sessionFile, { files_read: {} });
    // ... hook logic ...
    process.stderr.write(`OpenWolf: ${path.basename(normalizedFile)} ...\n`);
    writeJSON(sessionFile, session);
    process.exit(0);
}
main().catch(() => process.exit(0));
```

Rules taken from the existing hooks:

1. `ensureWolfDir();` is the first statement of `main()`.
2. Every early return is the pair `process.exit(0);` then `return;`.
3. The last line of the file is `main().catch(() => process.exit(0));`. Do not add a `//# sourceMappingURL` trailer to a hand-written file.
4. Session state lives in `.wolf/hooks/_session.json`. Read it with `readJSON(sessionFile, <fallback with every field you touch>)` and write it back with `writeJSON`. `session-start.js` creates the file with `session_id`, `started`, `files_read`, `files_written`, `edit_counts` and the counters `anatomy_hits`, `anatomy_misses`, `repeated_reads_warned`, `cerebrum_warnings`, `stop_count`. A new counter must also be added to the fallback object of every hook that reads it.
5. Payload fields by event:
   - `PreToolUse`: `input.tool_input?.file_path`, `input.tool_input?.content`, `input.tool_input?.old_string`, `input.tool_input?.new_string`
   - `PostToolUse`: the same, plus `input.tool_output?.content ?? ""`
   - `SessionStart` and `Stop`: do not call `readStdin()` (`session-start.js` and `stop.js` do not), and drop it from the import list.
6. To ignore OpenWolf's own files, copy the guard from `pre-read.js`: compute `projectDir = normalizePath(process.env.CLAUDE_PROJECT_DIR || process.cwd())`, strip it from `normalizedFile`, and exit `0` when the remainder starts with `.wolf/`.
7. Put helper functions after `main()` as plain `function name(...) {` declarations, as `pre-write.js` does with `checkCerebrum` and `checkBugLog`.
8. Import only what you use. Add `import * as fs from "node:fs";` only when calling `fs` directly.

**Verify** before Step 4:

```bash
node --check .wolf/hooks/<name>.js
node --check .wolf/hooks/shared.js
```

Both must print nothing and exit `0`.

### Step 4: Smoke-test with a piped payload

Uses the file from Step 3. Run from the repo root:

```bash
echo '{"tool_input":{"file_path":"docs/specs/000-overview.md"}}' \
  | CLAUDE_PROJECT_DIR="$PWD" node .wolf/hooks/<name>.js; echo "exit=$?"
```

Then the bad-input path:

```bash
echo 'not json' | CLAUDE_PROJECT_DIR="$PWD" node .wolf/hooks/<name>.js; echo "exit=$?"
```

For a `SessionStart` or `Stop` hook, run it with no pipe: `CLAUDE_PROJECT_DIR="$PWD" node .wolf/hooks/<name>.js; echo "exit=$?"`.

**Verify** before Step 5: both runs print `exit=0`, return in under 1 second, and write nothing to stdout. A run that hangs for about 4 seconds means stdin was not piped and `readStdin()` hit its fallback.

### Step 5: Document the registration entry

Skip this step when you modified an existing hook: it is already registered.

For a new hook, present this entry to the user, to be appended to the array of the event chosen in Step 1 inside `"hooks"` in `.claude/settings.json`:

```json
{
  "matcher": "<Tool or Tool1|Tool2, or empty string>",
  "hooks": [
    {
      "type": "command",
      "command": "node \"$CLAUDE_PROJECT_DIR/.wolf/hooks/<name>.js\"",
      "timeout": 5
    }
  ]
}
```

- The command is always `node \"$CLAUDE_PROJECT_DIR/.wolf/hooks/<name>.js\"` with the escaped quotes. Never use a relative path or a bare `.wolf/hooks/...`.
- `"timeout": 5` is the default. Use `10` only for a hook that rewrites `.wolf/anatomy.md` or the token ledger, as `post-write.js` and `stop.js` do.
- OpenWolf entries have no `"description"` key. Only the Caliber entries carry one.
- Place the new object before the Caliber entry of the same event, matching the existing order.

Apply the entry only after the user confirms.

**Verify** after it is applied:

```bash
node -e 'JSON.parse(require("fs").readFileSync(".claude/settings.json","utf8"))' && echo ok
grep -c 'caliber' .claude/settings.json
```

The first prints `ok`. The second prints the same count as before the edit (6).

### Step 6: Update OpenWolf state

1. Append one row to `.wolf/memory.md`: `| HH:MM | add <name>.js hook (<Event>/<matcher>) | .wolf/hooks/<name>.js, .claude/settings.json | ok | ~tokens |`
2. If the change fixed a broken hook, read `.wolf/buglog.json` first and append an entry with `error_message`, `root_cause`, `fix` and `tags` afterwards.
3. Record any non-obvious discovery, for example a payload field name, in `.wolf/cerebrum.md` under `## Key Learnings`.

## Examples

**User says:** "Add a hook that warns when a Bash command touches `/tmp`."

**Actions taken:**

1. Step 1: event `PreToolUse`, matcher `Bash`, file `.wolf/hooks/pre-bash.js`. `ls .wolf/hooks/` confirms the name is free.
2. Step 2: `grep -n '^export function' .wolf/hooks/shared.js` confirms `ensureWolfDir` and `readStdin` exist. No session state is needed, so nothing else is imported.
3. Step 3: write the file:

```js
import { ensureWolfDir, readStdin } from "./shared.js";
async function main() {
    ensureWolfDir();
    const raw = await readStdin();
    let input;
    try {
        input = JSON.parse(raw);
    }
    catch {
        process.exit(0);
        return;
    }
    const command = input.tool_input?.command ?? "";
    if (!command) {
        process.exit(0);
        return;
    }
    if (/(^|\s|=)\/tmp(\/|\s|$)/.test(command)) {
        process.stderr.write("OpenWolf: this command touches /tmp (tmpfs). Use a persistent path for anything over 1MB.\n");
    }
    process.exit(0);
}
main().catch(() => process.exit(0));
```

4. `node --check .wolf/hooks/pre-bash.js` prints nothing.
5. Step 4: `echo '{"tool_input":{"command":"cp a.bin /tmp/a.bin"}}' | CLAUDE_PROJECT_DIR="$PWD" node .wolf/hooks/pre-bash.js; echo "exit=$?"` prints the warning on stderr and `exit=0`.
6. Step 5: show the user the entry for the `PreToolUse` array with `"matcher": "Bash"`, the command `node \"$CLAUDE_PROJECT_DIR/.wolf/hooks/pre-bash.js\"` and `"timeout": 5`. Apply it after confirmation.
7. Step 6: append the row to `.wolf/memory.md`.

**Result:** a new `pre-bash.js` that warns and never blocks, one new `PreToolUse` entry, and all Caliber entries unchanged.

## Common Issues

**`SyntaxError: Cannot use import statement outside a module`**
1. Confirm `.wolf/hooks/package.json` still contains `{ "type": "module" }`.
2. Confirm the hook file is directly inside `.wolf/hooks/`, not in a subdirectory without its own `package.json`.

**`ReferenceError: require is not defined in ES module scope`**
Replace `const fs = require("fs")` with `import * as fs from "node:fs";`.

**`Error [ERR_MODULE_NOT_FOUND]: Cannot find module '.../.wolf/hooks/shared'`**
The import is missing its extension. Write `from "./shared.js"`.

**`SyntaxError: The requested module './shared.js' does not provide an export named 'X'`**
Run `grep -n '^export function' .wolf/hooks/shared.js` and use a name from that list, or add the helper to `shared.js` as `export function X(...) {`.

**The smoke test hangs for about 4 seconds, then exits 0 with no output**
Nothing was piped to stdin, so `readStdin()` resolved `"{}"` after its 4000 ms fallback. Pipe a payload with `echo '{...}' |`. For `SessionStart` and `Stop` hooks, remove the `readStdin()` call.

**The hook runs by hand but does nothing in a session**
1. `grep -n '<name>.js' .claude/settings.json` must return one line. No line means Step 5 was not applied.
2. Check the matcher is the exact tool name (`Read`, `Write|Edit|MultiEdit`, `Bash`) and that the entry sits under the intended event.
3. Hook configuration is loaded at session start, so start a new session after changing `.claude/settings.json`.

**The hook is cut off or its stderr message never appears**
It ran past the registered timeout. Remove any network call or child process. If the work is file rewriting on the scale of `post-write.js`, set `"timeout": 10`.

**A tool call is blocked or shows a hook error after adding the hook**
A path exits non-zero or throws outside `main()`. Check with `grep -n 'process.exit(' .wolf/hooks/<name>.js` that every call is `process.exit(0)`, and that the file ends with `main().catch(() => process.exit(0));`.

**`.wolf/hooks/_session.json` loses fields such as `files_written`**
A hook wrote back an object built from a narrow fallback while the file was missing. Use `readJSON(sessionFile, {...})` with every field the hook touches, mutate that object, and pass the same object to `writeJSON`. Never write a freshly built object.

**`Unexpected token` when Claude Code loads `.claude/settings.json`**
The inner quotes of the command were not escaped. The value must read `"node \"$CLAUDE_PROJECT_DIR/.wolf/hooks/<name>.js\""`. Re-run the `JSON.parse` check from Step 5.