# sh_backup_rotate — notes (not shown to the agent)

- Family feature, tier long, bash + GNU date/find. Spec = README.md (restic-style GFS retention per set,
  sorted report, dry run, sidecars, exit codes); seed/examples/backups + the README dry-run output are runnable.
- Seed: rotate.sh only parses -n/--today (no counts, no validation, no `--`), then exits 3 "not implemented".
  lib/backups.sh helpers are usable but `week_of` uses `%Y-W%V` (calendar year) instead of `%G-W%V` (ISO year):
  a planted defect that only shows across a new year (2025-12-29 and 2026-01-01 are both 2026-W01).
- Reference (fixtures/good): records `set-key<TAB>stamp<TAB>date<TAB>rel` sorted by set then stamp desc,
  one pass per set with per-rule counters/last-period, then an assoc-array plan printed in byte order; deletes
  with sidecar, failures to stderr + exit 1; `./`-prefixes a BACKUP_DIR that starts with `-` for find.
- Near-miss `calendar-year-weeks`: the reference rotate.sh with the seed's `%Y` week helper -> fails `iso_week_year`.
- Checker: check.py builds trees under `__checker__/sandbox/run/backups`, runs `bash rotate.sh`, compares stdout
  with a Python oracle of the same rules (plus literal expectations for daily/ISO-week cases) and the files left
  on disk. `delete_failure` chmods a subdir 0555; if the checker runs as root (permissions not enforced) that
  check only asserts exit 0. Verified as uid 1000 (enforced).
