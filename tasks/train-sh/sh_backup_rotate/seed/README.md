# backup rotation

The nightly jobs drop their backups into one tree (`/srv/backups` on the backup host). Nothing ever
deletes them, and the disk is filling up. `rotate.sh` is meant to apply a daily/weekly/monthly
("grandfather-father-son") retention policy to that tree. Only the option parsing skeleton exists so
far; `lib/backups.sh` has helpers for parsing names and dates.

```
bash rotate.sh [-n|--dry-run] [--today YYYY-MM-DD] [--daily N] [--weekly N] [--monthly N] BACKUP_DIR
```

| option | meaning | default |
|---|---|---|
| `-n`, `--dry-run` | print the plan, delete nothing | off |
| `--today DATE` | the date to treat as today | `date +%F` |
| `--daily N` | keep the newest backup of each of the N most recent days that have backups | 7 |
| `--weekly N` | ... of each of the N most recent ISO weeks that have backups | 4 |
| `--monthly N` | ... of each of the N most recent calendar months that have backups | 6 |

Options may come in any order, before or after `BACKUP_DIR`; `--` ends the options. `N` is a whole
number (`0` turns that rule off).

Environment: bash 5, GNU coreutils (GNU `date -d` works), find, grep, sed, and `awk` is **mawk**.
`jq` and `sqlite3` are not installed.

## Which files are backups

Every regular file anywhere under `BACKUP_DIR` (subdirectories included; symbolic links are never
touched) whose name is

```
<set>-<YYYY-MM-DD>[_<HHMM>].<ext>
```

- `<set>` is any text, including spaces and dashes (`shop front`, `my-app`).
- `<YYYY-MM-DD>` must be a real calendar date; a name with an impossible date (`2026-02-30`) is left
  alone and reported on stderr as `rotate: skipping <path>: invalid date <date>`.
- `_<HHMM>` is optional (no time counts as `0000`); it orders backups taken on the same day.
- `<ext>` is everything after the first `.` that follows the date/time (`tar.gz`, `sql.gz`, `dump`).

Files with other names are ignored: never listed, never deleted. Names never contain tabs or
newlines.

A **backup set** is all the backups in the same directory with the same `<set>` and the same
`<ext>`. Each set is rotated on its own: `db/orders-*.sql.gz`, `db/orders-*.tar.gz` and
`old/orders-*.sql.gz` are three different sets.

**Checksum sidecars:** a file `<backup>.sha256` (the backup's full name plus `.sha256`) belongs to that
backup. It is deleted together with it, and it is not listed in the report. A sidecar whose backup
does not exist is left alone.

## Retention

For each set, take its backups newest first (by date, then time). A backup dated **after** `--today`
is kept with the reason `future` and is ignored by the rules below. Then, walking from newest to
oldest, each rule keeps a backup when the backup is the first one seen in a new period (day, ISO
week, month) and the rule has kept fewer than N backups so far:

- `daily`: period = the date;
- `weekly`: period = the ISO 8601 week (weeks start on Monday; the week of 2026-01-01 is `2026-W01`,
  which starts on Monday 2025-12-29);
- `monthly`: period = `YYYY-MM`.

A backup that at least one rule keeps is kept; every other backup is deleted (with its sidecar).
The periods of a rule only advance on backups that rule kept, so the result is "the newest backup of
each of the N most recent periods that have backups".

If all three counts are 0 the script refuses to run (exit 2) instead of deleting everything.

## Report (stdout)

One line per backup, all sets together, sorted by path relative to `BACKUP_DIR` in byte order
(`LC_ALL=C`):

```
keep <path> (<reasons>)
delete <path>
```

`<reasons>` is the comma-separated list of the rules that keep it, in the order
`daily,weekly,monthly`, or `future`. Then one summary line:

```
kept K, deleted D                # normal run
kept K, would delete D           # --dry-run (and nothing is deleted)
kept K, deleted D, failed F      # some deletions failed
```

## Errors and exit status

- 0 — success (also when there is nothing to do).
- 1 — at least one deletion failed (e.g. a read-only directory). Each failure is reported on stderr as
  `rotate: cannot delete <path>`, its plan line is still printed, and the remaining deletions still
  happen. A failed deletion is not counted in `deleted`.
- 2 — bad usage: unknown option, a count that is not a whole number, a `--today` that is not a real
  `YYYY-MM-DD` date, a missing or non-existent `BACKUP_DIR`, more than one directory, or all counts 0.
  Nothing is deleted.

## Example

`examples/backups` holds a small tree. From `examples/`,
`bash ../rotate.sh -n --today 2026-10-22 --daily 3 --weekly 2 --monthly 3 backups` prints:

```
keep db/orders-2026-08-30.sql.gz (monthly)
delete db/orders-2026-09-14.sql.gz
keep db/orders-2026-09-27.sql.gz (monthly)
delete db/orders-2026-10-04.sql.gz
delete db/orders-2026-10-12.sql.gz
keep db/orders-2026-10-18.sql.gz (daily,weekly)
keep db/orders-2026-10-19.sql.gz (daily)
delete db/orders-2026-10-21.sql.gz
delete db/orders-2026-10-21_0300.sql.gz
keep db/orders-2026-10-21_2300.sql.gz (daily,weekly,monthly)
keep web/shop front-2026-09-30.tar.gz (monthly)
keep web/shop front-2026-10-07.tar.gz (daily,weekly)
keep web/shop front-2026-10-20.tar.gz (daily)
keep web/shop front-2026-10-21.tar.gz (daily,weekly,monthly)
kept 9, would delete 5
```

Without `-n` the same plan is carried out (the summary becomes `kept 9, deleted 5`), the
`.sha256` sidecars of the deleted backups go with them, and `web/RESTORE.txt` is untouched. Use a copy of
`examples/` when you try a real run.
