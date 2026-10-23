# sh_user_audit — notes (not shown to the agent)

- Family feature (+2 planted bugs), tier long, bash + mawk. Spec = README.md; seed/sample + the README report are runnable.
- Seed: driver audit.sh, lib/common.sh and the code-grouping are correct. checks/uid.sh finds duplicate uids
  correctly but lists names with `grep ":$uid:" passwd` (also matches a gid field, comment lines and malformed
  lines); checks/aging.sh reads the 4th shadow field (min age) instead of the 5th (max) and does not skip locked
  (`!`/`*`) passwords. checks/homes.sh, keys.sh, sudo.sh are empty stubs to implement.
- Reference (fixtures/good): awk aggregation for DUP_UID; field 5 + locked skip; manifest into assoc arrays with
  trailing-slash normalisation, last-wins, octal validation; authorized_keys parsed in awk with quote-aware option
  skipping; sudoers + filtered sudoers.d (no `.`/`~` names, byte order) with %group expansion via members AND
  primary gid, dedup per rule, ordered by passwd order then file/line.
- Near-miss `sudoers-d-unfiltered`: everything right except sudoers.d files with `.`/`~` names are read
  -> fails `sudo_dropins` only.
- Checker writes snapshots into `__checker__/sandbox/run/snap` and runs `bash audit.sh snap`; most checks compare
  only the lines of their own code, `full_report`/`clean_snapshot` compare the whole report + exit status.
