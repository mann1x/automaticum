# config migration

The services are moving from per-service INI files to the flat `key=value` layout the new
deployment tooling reads. `migrate.sh` converts a directory of old configs and prints a validation
report; ops runs it before every rollout and refuses to deploy when it exits non-zero.

```
bash migrate.sh MAPPING SRC_DIR DEST_DIR
```

- `MAPPING` — tab-separated `old_section.old_key<TAB>new.key<TAB>type`, type is `str`, `int` or
  `bool`. Blank lines and lines starting with `#` are ignored. See `mapping.tsv`.
- `SRC_DIR` — every regular file `*.ini` directly in it (not in subdirectories) is migrated, in byte
  order of the file name.
- `DEST_DIR` — created if missing. `NAME.ini` becomes `DEST_DIR/NAME.conf`.

The parsing is in `lib/ini2flat.awk`, the driver in `migrate.sh`.

Environment: bash 5, GNU coreutils, grep, sed, find, and `awk` is **mawk** (no gawk extensions, no
`{n}` regex intervals). `jq` and `sqlite3` are not installed.

## Reading the old INI files

1. Blank lines are ignored. A line whose first non-blank character is `;` or `#` is a comment.
2. `[name]` starts a section; the name is trimmed and lower-cased. Keys before the first section
   belong to the section `global`.
3. `key = value` (spaces around `=` optional): the key is the text before the first `=`, trimmed and
   lower-cased; the value is the text after it, trimmed. An empty value is allowed.
4. **Quoted values:** if the value starts with `"`, the value is everything up to the next `"`,
   verbatim (it may contain spaces, `;`, `#` and `=`); a missing closing quote is the error
   `unterminated quote`. Single quotes have no special meaning.
5. **Inline comments** (unquoted values only): a `;` or `#` that follows a space or tab starts a
   comment, so `30 # seconds` is `30`, but `a#b` stays `a#b`. A value that is only a comment is empty.
6. Any other line (no `=`, or nothing before it) is the error `cannot parse line`.
7. A key that appears twice in the same section keeps the **last** value and is reported (at the
   later line) as the warning `duplicate key SECTION.KEY, earlier value replaced`.

## Writing the new file

- First line `# migrated from NAME.ini`, then one `new.key=value` line per key, sorted by key in
  byte order (`LC_ALL=C`).
- A key listed in the mapping is renamed. Its type is checked:
  - `int`: an optional `-` and digits, else the error `SECTION.KEY: expected an integer, got 'VALUE'`;
  - `bool`: `true`/`yes`/`on`/`1` become `true`, `false`/`no`/`off`/`0` become `false`, in any
    letter case; anything else is the error `SECTION.KEY: expected a boolean, got 'VALUE'`;
  - `str`: copied as it is.
- A key that is not in the mapping is kept as `legacy.SECTION.KEY=value` and reported as the warning
  `unmapped key SECTION.KEY, kept as legacy.SECTION.KEY`.
- A file with **any error** is not migrated: no `.conf` is written for it, and an existing
  `DEST_DIR/NAME.conf` from an earlier run is removed, so a broken config can never be deployed.

## Report (stdout)

One line per finding, files in order, findings of a file ordered by line number (for one line, the
parse finding first, then the mapping/type finding):

```
NAME.ini:LINE: error: MESSAGE
NAME.ini:LINE: warning: MESSAGE
```

`LINE` is the 1-based line in the old file (for mapping/type findings, the line that set the value).
Then one summary line:

```
migrated M of N files, E errors, W warnings
```

Exit status: 0 when there are no errors (warnings are fine), 1 when any file had an error, 2 on bad
usage (wrong argument count, unreadable mapping, missing SRC_DIR).

## Example

`bash migrate.sh mapping.tsv examples/old out` prints

```
payments.ini:14: warning: duplicate key database.pool, earlier value replaced
payments.ini:24: warning: unmapped key metrics.path, kept as legacy.metrics.path
search.ini:4: error: server.workers: expected an integer, got 'four'
migrated 1 of 2 files, 1 errors, 2 warnings
```

exits 1, writes `out/payments.conf` identical to `examples/new/payments.conf`, and writes no
`out/search.conf`.
