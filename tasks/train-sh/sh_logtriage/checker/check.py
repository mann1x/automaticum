import gzip
import os
import shutil
import subprocess

ROOT = os.getcwd()
SANDBOX = os.path.join(ROOT, "__checker__", "sandbox")
SCRIPT = os.path.join(ROOT, "bin", "logsum.sh")


def case(name, fn):
    try:
        fn()
        print(f"TEST {name} PASS")
    except AssertionError as e:
        print(f"TEST {name} FAIL {e}")
    except subprocess.TimeoutExpired:
        print(f"TEST {name} FAIL logsum.sh did not finish within 20s")
    except Exception as e:
        print(f"TEST {name} FAIL {type(e).__name__}: {e}")


def L(ts, req, status: object = 200, ms=50, ip="10.0.0.1", size="100", user="-"):
    return f'{ip} - {user} [{ts} +0000] "{req}" {status} {size} "-" "probe/1.0" {ms}'


def run(files, args):
    """Write files (name -> list of lines) into a fresh dir, run logsum.sh there."""
    d = os.path.join(SANDBOX, "run")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(d)
    for name, lines in files.items():
        data = ("\n".join(lines) + "\n").encode()
        if name.endswith(".gz"):
            data = gzip.compress(data)
        with open(os.path.join(d, name), "wb") as fh:
            fh.write(data)
    r = subprocess.run(["bash", SCRIPT, *args], cwd=d, capture_output=True, text=True, timeout=20)
    return r.returncode, r.stdout, r.stderr


def report(files, args, what):
    rc, out, err = run(files, args)
    assert rc == 0, f"{what}: exit status {rc}, expected 0 (stderr: {err.strip()[:160]!r})"
    return out


def same(got, want, what):
    g = got.rstrip("\n").split("\n") if got.strip() else []
    w = want.strip("\n").split("\n")
    for i in range(max(len(g), len(w))):
        gl = g[i] if i < len(g) else "<missing>"
        wl = w[i] if i < len(w) else "<nothing more>"
        assert gl == wl, f"{what}: report line {i + 1}: expected {wl!r}, got {gl!r}"


def section(out, title):
    lines = out.rstrip("\n").split("\n")
    start = next((i for i, ln in enumerate(lines) if ln.startswith(f"== {title}")), None)
    assert start is not None, f"no '== {title}' section in the report"
    body = []
    for ln in lines[start + 1:]:
        if ln.startswith("== "):
            break
        body.append(ln)
    return "\n".join(body)


def t_summary_counts():
    log = [
        L("01/Feb/2026:08:00:00", "GET / HTTP/1.1"),
        "",
        "   ",
        "\t",
        L("01/Feb/2026:08:00:01", "GET /a HTTP/1.1"),
        L("01/Feb/2026:08:00:02", "GET /b HTTP/1.1").rsplit(" ", 1)[0],
        L("01/Fbr/2026:08:00:03", "GET /c HTTP/1.1"),
        L("01/Feb/2026:08:00:04", "GET /d HTTP/1.1", status="2xx"),
        '10.0.0.9 - - [01/Feb/2026:08:00:05 +0000] "GET /e HTT',
        L("01/Feb/2026:08:00:06", "HEAD /f HTTP/1.0", size="-"),
    ]
    out = report({"a.log": log}, ["a.log"], "mixed log")
    same(section(out, "summary"), "lines: 7\nparsed: 3\nmalformed: 4", "summary section")


def t_query_stripped():
    log = [
        L("01/Feb/2026:08:00:00", "GET /search?q=shoes HTTP/1.1"),
        L("01/Feb/2026:08:00:01", "GET /search?q=hats&page=2 HTTP/1.1"),
        L("01/Feb/2026:08:00:02", "GET /search HTTP/1.1"),
        L("01/Feb/2026:08:00:03", "GET /img/logo.png?v=7 HTTP/1.1"),
    ]
    out = report({"a.log": log}, ["a.log"], "query strings")
    same(section(out, "top paths"), "3 /search\n1 /img/logo.png", "top paths section")


def t_top_paths_ties():
    reqs = ["/beta", "/alpha", "/beta", "/Zulu", "/alpha", "/gamma", "/delta", "/delta", "/delta", "/a-b", "/a"]
    log = [L(f"01/Feb/2026:08:00:{i:02d}", f"GET {p} HTTP/1.1") for i, p in enumerate(reqs)]
    out = report({"a.log": log}, ["a.log"], "tied counts")
    same(section(out, "top paths"),
         "3 /delta\n2 /alpha\n2 /beta\n1 /Zulu\n1 /a\n1 /a-b\n1 /gamma", "top paths section")


def t_top_paths_limit():
    log = []
    for i in range(12):
        for k in range(12 - i):
            log.append(L(f"01/Feb/2026:08:{i:02d}:{k:02d}", f"GET /p{i:02d} HTTP/1.1"))
    out = report({"a.log": log}, ["a.log"], "12 paths, default TOP")
    want = "\n".join(f"{12 - i} /p{i:02d}" for i in range(10))
    same(section(out, "top paths"), want, "top paths with the default TOP")
    out = report({"a.log": log}, ["-n", "3", "a.log"], "-n 3")
    same(section(out, "top paths"), "12 /p00\n11 /p01\n10 /p02", "top paths with -n 3")


def t_hour_keys():
    log = [
        L("01/Oct/2025:00:10:00", "GET /x HTTP/1.1"),
        L("30/Sep/2025:23:59:59", "GET /x HTTP/1.1"),
        L("01/Jan/2026:00:00:01", "GET /x HTTP/1.1"),
        L("31/Dec/2025:23:00:00", "GET /x HTTP/1.1", status=503),
        L("05/Oct/2025:09:00:00", "GET /x HTTP/1.1"),
    ]
    out = report({"a.log": log}, ["a.log"], "hours across months")
    same(section(out, "status by hour"),
         "2025-09-30T23 200=1\n2025-10-01T00 200=1\n2025-10-05T09 200=1\n2025-12-31T23 503=1\n2026-01-01T00 200=1",
         "status by hour section")


def t_status_per_hour():
    log = [
        L("07/Jun/2026:14:00:00", "GET /a HTTP/1.1", status=404),
        L("07/Jun/2026:14:01:00", "GET /a HTTP/1.1", status=200),
        L("07/Jun/2026:14:02:00", "GET /a HTTP/1.1", status=500),
        L("07/Jun/2026:14:03:00", "GET /a HTTP/1.1", status=200),
        L("07/Jun/2026:14:59:59", "GET /a HTTP/1.1", status=304),
        L("07/Jun/2026:15:00:00", "GET /a HTTP/1.1", status=404),
        L("07/Jun/2026:15:30:00", "GET /a HTTP/1.1", status=404),
    ]
    out = report({"a.log": log}, ["a.log"], "statuses")
    same(section(out, "status by hour"), "2026-06-07T14 200=2 304=1 404=1 500=1\n2026-06-07T15 404=2",
         "status by hour section")


def t_slow_threshold():
    log = [
        L("07/Jun/2026:14:00:00", "GET /a HTTP/1.1", ms=499),
        L("07/Jun/2026:14:00:01", "POST /b?x=1 HTTP/1.1", ms=500),
        L("07/Jun/2026:14:00:02", "GET /c HTTP/1.1", ms=12000),
        L("07/Jun/2026:14:00:03", "GET /d HTTP/1.1", ms=1199),
        L("07/Jun/2026:14:00:04", "PUT /e HTTP/1.1", ms=1200),
    ]
    out = report({"a.log": log}, ["a.log"], "default threshold")
    same(out[out.index("== slow"):] if "== slow" in out else "",
         "== slow requests (>= 500 ms) ==\n2026-06-07T14:00:01 500 POST /b\n2026-06-07T14:00:02 12000 GET /c\n"
         "2026-06-07T14:00:03 1199 GET /d\n2026-06-07T14:00:04 1200 PUT /e", "slow section (default)")
    out = report({"a.log": log}, ["-t", "1200", "a.log"], "-t 1200")
    same(out[out.index("== slow"):] if "== slow" in out else "",
         "== slow requests (>= 1200 ms) ==\n2026-06-07T14:00:02 12000 GET /c\n2026-06-07T14:00:04 1200 PUT /e",
         "slow section (-t 1200)")


def t_bad_request_field():
    log = [
        L("07/Jun/2026:14:00:00", "GET /ok HTTP/1.1"),
        L("07/Jun/2026:14:00:01", "-", status=408, size="0", ms=30000),
        L("07/Jun/2026:14:00:02", "GET", status=400, ms=1),
        L("07/Jun/2026:14:00:03", "get /lower HTTP/1.1"),
        L("07/Jun/2026:14:00:04", "GET nopath HTTP/1.1", status=400),
        L("07/Jun/2026:14:00:05", "GET /x HTTP/1.1 extra", status=400),
        L("07/Jun/2026:14:00:06", "\\x16\\x03\\x01 /x SSH-2.0", status=400),
    ]
    out = report({"a.log": log}, ["a.log"], "bad request fields")
    same(out, """== summary ==
lines: 7
parsed: 1
malformed: 6
== top paths ==
1 /ok
== status by hour ==
2026-06-07T14 200=1
== slow requests (>= 500 ms) ==""", "full report")


def t_gzip_input():
    old = [L("06/Jun/2026:23:10:00", "GET /old HTTP/1.1", ms=900), L("06/Jun/2026:23:11:00", "GET /old HTTP/1.1")]
    new = [L("07/Jun/2026:00:01:00", "GET /new HTTP/1.1", ms=700)]
    out = report({"access.log.2.gz": old, "access.log": new}, ["access.log.2.gz", "access.log"], "a .gz file")
    same(out, """== summary ==
lines: 3
parsed: 3
malformed: 0
== top paths ==
2 /old
1 /new
== status by hour ==
2026-06-06T23 200=2
2026-06-07T00 200=1
== slow requests (>= 500 ms) ==
2026-06-06T23:10:00 900 GET /old
2026-06-07T00:01:00 700 GET /new""", "report over a .gz and a plain file")


def t_spaces_in_filename():
    log = [L("07/Jun/2026:10:00:00", "GET /spaced HTTP/1.1", ms=650)]
    out = report({"web 01 access.log": log, "web 02 access.log.gz": log}, ["web 01 access.log", "web 02 access.log.gz"],
                 "file names with spaces")
    same(section(out, "summary"), "lines: 2\nparsed: 2\nmalformed: 0", "summary section")
    same(section(out, "top paths"), "2 /spaced", "top paths section")


def t_argument_order():
    a = [L("07/Jun/2026:12:00:00", "GET /from-a HTTP/1.1", ms=800)]
    b = [L("07/Jun/2026:11:00:00", "GET /from-b HTTP/1.1", ms=800)]
    out = report({"a.log": a, "b.log": b}, ["a.log", "b.log"], "two files")
    same(section(out, "slow requests"), "2026-06-07T12:00:00 800 GET /from-a\n2026-06-07T11:00:00 800 GET /from-b",
         "slow section (input order)")
    same(section(out, "status by hour"), "2026-06-07T11 200=1\n2026-06-07T12 200=1", "status by hour section")


def t_empty_input():
    out = report({"empty.log": ["", "  "]}, ["empty.log"], "a log of blank lines")
    same(out, """== summary ==
lines: 0
parsed: 0
malformed: 0
== top paths ==
== status by hour ==
== slow requests (>= 500 ms) ==""", "full report")


def t_exit_codes():
    rc, out, err = run({"a.log": [L("07/Jun/2026:12:00:00", "GET / HTTP/1.1")]}, ["a.log", "missing.log"])
    assert rc == 1, f"a missing log file: expected exit 1, got {rc}"
    rc, out, err = run({"a.log": [L("07/Jun/2026:12:00:00", "GET / HTTP/1.1")]}, [])
    assert rc == 2, f"no log file given: expected exit 2, got {rc}"


TESTS = [("summary_counts", t_summary_counts), ("query_stripped", t_query_stripped),
         ("top_paths_ties", t_top_paths_ties), ("top_paths_limit", t_top_paths_limit),
         ("hour_keys", t_hour_keys), ("status_per_hour", t_status_per_hour), ("slow_threshold", t_slow_threshold),
         ("bad_request_field", t_bad_request_field), ("gzip_input", t_gzip_input),
         ("spaces_in_filename", t_spaces_in_filename), ("argument_order", t_argument_order),
         ("empty_input", t_empty_input), ("exit_codes", t_exit_codes)]

for name, fn in TESTS:
    case(name, fn)
