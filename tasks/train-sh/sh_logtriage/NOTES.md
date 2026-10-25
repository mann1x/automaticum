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

- **v2 (draft 2026-10-24, bm-889):** README now states the request-field rule outright (upper-case METHOD, target
  starting with `/`, protocol starting with `HTTP/`). v1's README only implied the upper-case rule; all 3 v1 runs
  (25, 25 and 50 min caps) ended at the identical 12/13 oracle (only bad_request_field, `parsed: 4` vs 1): the agent
  checks only the word count, cannot see which hidden lines are wrong, and loops until the SDK guard ends the run.
