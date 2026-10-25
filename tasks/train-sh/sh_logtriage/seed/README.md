# logsum

`bin/logsum.sh` prints a short triage report for web-server access logs. On-call runs it on the
raw logs of a host, including logrotate's compressed files, to see what was hit, what failed and
what was slow.

```
bash bin/logsum.sh [-t SLOW_MS] [-n TOP] LOGFILE...
```

- `-t SLOW_MS` — slow-request threshold in milliseconds (default `500`).
- `-n TOP` — how many paths to list under "top paths" (default `10`).
- `LOGFILE...` — one or more files, read in the order given. A file whose name ends in `.gz` is
  gzip-compressed and is decompressed on the fly. File names may contain spaces.

It exits 0 after printing a report, 1 if a file cannot be read, 2 on bad usage. The parsing lives in
`lib/parse.awk`; `bin/logsum.sh` builds the report from its records.

Environment: bash 5, GNU coreutils, grep, sed, gzip, and `awk` is **mawk** (no gawk extensions,
no `{n}` regex intervals). `jq`, `bc` and `sqlite3` are not installed.

## Log format

One request per line, the "combined" format plus the request time in milliseconds:

```
203.0.113.7 - - [12/Mar/2026:09:15:02 +0000] "GET /api/items?id=3 HTTP/1.1" 200 512 "-" "curl/8.5.0" 87
```

client, ident, user, `[dd/Mon/yyyy:HH:MM:SS zone]`, `"METHOD TARGET PROTOCOL"`, status (3 digits),
bytes (a number or `-`), `"referer"`, `"user agent"`, request time in ms (a whole number).

- A line that does not have exactly this shape is **malformed**. That includes a request field that
  is not three words `METHOD /target HTTP/x` (nginx writes `"-"` for a connection that timed out
  before sending a request), an unknown month name, a missing time field, and truncated lines.
  A valid request field is exactly three space-separated words: a METHOD of upper-case letters only
  (`GET`, `POST`, ...; `get` is malformed), a target that starts with `/`, and a protocol that starts
  with `HTTP/`. Anything else in the request field (probe bytes, `SSH-2.0`, a fourth word) is malformed.
- **Blank lines** (empty, or only spaces/tabs) are ignored completely: they are not counted anywhere.
- The **path** of a request is its target with any query string (`?` and everything after it) removed.
- The time zone is ignored; times are reported as written.

## Report

Four sections, always all four, always in this order, each with its header line even when it is
empty:

1. `== summary ==` — `lines: N` (non-blank lines read), `parsed: N`, `malformed: N`.
2. `== top paths ==` — `COUNT PATH` for the TOP most requested paths of the parsed requests, most
   requested first; paths with the same count are listed in byte order (`LC_ALL=C`) of the path.
3. `== status by hour ==` — one line per hour that has parsed requests, in chronological order:
   `YYYY-MM-DDTHH` followed by ` STATUS=COUNT` for each status seen in that hour, statuses ascending.
   Months, days and hours are always two digits.
4. `== slow requests (>= SLOW_MS ms) ==` — every parsed request whose time is **at least** SLOW_MS,
   in input order, as `YYYY-MM-DDTHH:MM:SS MS METHOD PATH`.

Everything is printed on stdout; there is no other output on success.

## Example

`bash bin/logsum.sh samples/access.log` must print exactly:

```
== summary ==
lines: 12
parsed: 10
malformed: 2
== top paths ==
4 /api/items
2 /login
1 /account
1 /account/orders
1 /favicon.ico
1 /report/monthly
== status by hour ==
2026-03-12T09 200=4 302=1 404=1
2026-03-12T10 200=3 500=1
== slow requests (>= 500 ms) ==
2026-03-12T09:31:12 1874 GET /report/monthly
2026-03-12T10:02:13 500 GET /api/items
```

With `-n 2 -t 1000` the top-paths section holds only the first two lines above and the slow
section is headed `== slow requests (>= 1000 ms) ==` and lists only the `/report/monthly` request.
