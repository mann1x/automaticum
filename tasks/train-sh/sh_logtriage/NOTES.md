# sh_logtriage — notes (not shown to the agent)

- Family bug-fix, tier short, bash + mawk. The report spec is README.md; seed/samples/access.log + the README example let the agent diff.
- Planted defects: (1) `read_logs` is `cat $@` — no `.gz` decompression and word-splits names with spaces;
  (2) top-paths tie order uses `sort -rn` (ties come out in reverse path order) instead of `-k1,1nr -k2,2`;
  (3) slow filter uses `>` not `>=`; (4) parse.awk formats the month with `%d` (no zero pad: wrong keys, wrong
  chronological order across Sep/Oct); (5) parse.awk only checks `nw == 0` for the request field, so `"-"`,
  `GET`, lowercase methods, targets without `/` count as parsed; plus blank-line rule `/^$/` misses whitespace-only lines.
- Reference (fixtures/good): `case *.gz) gzip -dc` per quoted file, proper sort keys, `>=`, `%02d`, full request validation.
- Near-miss `request-field-unchecked`: everything fixed except the request-field validation -> fails `bad_request_field`.
- Checker: check.py writes hidden logs into `__checker__/sandbox/run/` at test time and runs `bash bin/logsum.sh` there
  (no exec from /tmp needed). Uses no tools beyond bash/coreutils/mawk/gzip/python3.
