import os
import subprocess
import sys
import traceback

HERE = os.path.dirname(os.path.abspath(__file__))
HEADER = "date,region,product,channel,units,revenue\n"

DATA_A = HEADER + """\
2024-01-05,north,widget,web,3,30.00
2024-01-20,south,widget,store,1,10.00
2024-02-02,north,gadget,web,2,50.00
2024-02-28,south,gadget,web,4,100.00
2024-02-29,north,widget,store,5,50.00
2024-03-01,east,widget,web,1,10.00
"""

# shares 6.25 / 18.75 / 75.00 -> half-up
DATA_SHARE = HEADER + """\
2024-05-01,north,widget,web,1,10.00
2024-05-02,south,widget,web,3,30.00
2024-05-03,west,widget,web,12,120.00
"""

DATA_THIRDS = HEADER + """\
2024-06-01,north,widget,web,1,5.00
2024-06-01,north,gadget,web,1,5.00
2024-06-02,south,widget,web,2,10.00
2024-06-03,west,widget,store,1,10.00
"""

DATA_TIES = HEADER + """\
2024-07-01,west,widget,web,1,25.00
2024-07-02,north,gizmo,store,1,25.00
2024-07-03,east,widget,web,1,25.00
2024-07-04,north,gadget,web,1,25.00
2024-07-05,east,gizmo,store,2,40.00
"""


def write(name, text):
    path = os.path.join(HERE, name)
    with open(path, "w", newline="") as fh:
        fh.write(text)
    return path


def run(data, *args):
    path = write("data.csv", data)
    r = subprocess.run([sys.executable, "-B", "-m", "salesreport", path, *args],
                       capture_output=True, text=True, timeout=30)
    return r


def ok_output(data, *args):
    r = run(data, *args)
    cmd = " ".join(["salesreport", "FILE", *args])
    if r.returncode != 0:
        err = (r.stderr.strip().splitlines() or ["(no stderr)"])[-1]
        raise AssertionError(f"`{cmd}` exited {r.returncode}: {err}")
    return cmd, r.stdout


def same(cmd, got, want):
    g = got.rstrip("\n").split("\n")
    w = want.rstrip("\n").split("\n")
    for i, (a, b) in enumerate(zip(g, w), start=1):
        assert a == b, f"`{cmd}` line {i}: expected {b!r}, got {a!r}"
    assert len(g) == len(w), f"`{cmd}`: expected {len(w)} lines, got {len(g)}"


def case(name, fn):
    try:
        fn()
        print(f"TEST {name} PASS")
    except AssertionError as e:
        print(f"TEST {name} FAIL {e}")
    except Exception as e:
        frame = traceback.extract_tb(e.__traceback__)[-1]
        where = "" if "__checker__" in frame.filename else f" at {frame.filename.split('/')[-1]}:{frame.lineno}"
        print(f"TEST {name} FAIL {type(e).__name__}: {e}{where}")


def t_single_key_text():
    cmd, out = ok_output(DATA_A)
    same(cmd, out, """\
region  units  revenue   share
------  -----  -------  ------
north      10   130.00   52.0%
south       5   110.00   44.0%
east        1    10.00    4.0%
------  -----  -------  ------
TOTAL      16   250.00  100.0%
""")


def t_multi_key_text():
    cmd, out = ok_output(DATA_A, "--group-by", "region,product")
    same(cmd, out, """\
region  product  units  revenue   share
------  -------  -----  -------  ------
south   gadget       4   100.00   40.0%
north   widget       8    80.00   32.0%
north   gadget       2    50.00   20.0%
east    widget       1    10.00    4.0%
south   widget       1    10.00    4.0%
------  -------  -----  -------  ------
TOTAL               16   250.00  100.0%
""")


def t_month_bucket():
    cmd, out = ok_output(DATA_A, "--group-by", "month")
    same(cmd, out, """\
month    units  revenue   share
-------  -----  -------  ------
2024-02     11   200.00   80.0%
2024-01      4    40.00   16.0%
2024-03      1    10.00    4.0%
-------  -----  -------  ------
TOTAL       16   250.00  100.0%
""")


def t_month_with_channel():
    cmd, out = ok_output(DATA_A, "--group-by", "month,channel")
    same(cmd, out, """\
month    channel  units  revenue   share
-------  -------  -----  -------  ------
2024-02  web          6   150.00   60.0%
2024-02  store        5    50.00   20.0%
2024-01  web          3    30.00   12.0%
2024-01  store        1    10.00    4.0%
2024-03  web          1    10.00    4.0%
-------  -------  -----  -------  ------
TOTAL                16   250.00  100.0%
""")


def t_share_half_up():
    cmd, out = ok_output(DATA_SHARE)
    same(cmd, out, """\
region  units  revenue   share
------  -----  -------  ------
west       12   120.00   75.0%
south       3    30.00   18.8%
north       1    10.00    6.3%
------  -----  -------  ------
TOTAL      16   160.00  100.0%
""")


def t_share_thirds():
    cmd, out = ok_output(DATA_THIRDS, "--group-by", "region", "--format", "markdown")
    same(cmd, out, """\
| region | units | revenue | share |
| --- | ---: | ---: | ---: |
| north | 2 | 10.00 | 33.3% |
| south | 2 | 10.00 | 33.3% |
| west | 1 | 10.00 | 33.3% |
| TOTAL | 5 | 30.00 | 100.0% |
""")


def t_tie_order():
    cmd, out = ok_output(DATA_TIES, "--group-by", "region,product")
    same(cmd, out, """\
region  product  units  revenue   share
------  -------  -----  -------  ------
east    gizmo        2    40.00   28.6%
east    widget       1    25.00   17.9%
north   gadget       1    25.00   17.9%
north   gizmo        1    25.00   17.9%
west    widget       1    25.00   17.9%
------  -------  -----  -------  ------
TOTAL                6   140.00  100.0%
""")


def t_date_range_inclusive():
    cmd, out = ok_output(DATA_A, "--since", "2024-02-01", "--until", "2024-02-29")
    same(cmd, out, """\
region  units  revenue   share
------  -----  -------  ------
north       7   100.00   50.0%
south       4   100.00   50.0%
------  -----  -------  ------
TOTAL      11   200.00  100.0%
""")


def t_markdown_single():
    cmd, out = ok_output(DATA_A, "--format", "markdown")
    same(cmd, out, """\
| region | units | revenue | share |
| --- | ---: | ---: | ---: |
| north | 10 | 130.00 | 52.0% |
| south | 5 | 110.00 | 44.0% |
| east | 1 | 10.00 | 4.0% |
| TOTAL | 16 | 250.00 | 100.0% |
""")


def t_markdown_multi():
    cmd, out = ok_output(DATA_A, "--group-by", "product,region", "--format", "markdown",
                         "--since", "2024-01-20")
    same(cmd, out, """\
| product | region | units | revenue | share |
| --- | --- | ---: | ---: | ---: |
| gadget | south | 4 | 100.00 | 45.5% |
| gadget | north | 2 | 50.00 | 22.7% |
| widget | north | 5 | 50.00 | 22.7% |
| widget | east | 1 | 10.00 | 4.5% |
| widget | south | 1 | 10.00 | 4.5% |
| TOTAL |  | 13 | 220.00 | 100.0% |
""")


def t_unknown_key():
    r = run(DATA_A, "--group-by", "region,colour")
    assert r.returncode == 2, f"--group-by region,colour: expected exit code 2, got {r.returncode}"
    assert "unknown group key: colour" in r.stderr, \
        f"--group-by region,colour: stderr should contain 'unknown group key: colour', got {r.stderr.strip()[-200:]!r}"
    assert "Traceback" not in r.stderr, "--group-by region,colour: printed a traceback"


def t_no_records():
    r = run(DATA_A, "--since", "2030-01-01", "--group-by", "month")
    assert r.returncode == 0, f"--since 2030-01-01: expected exit code 0, got {r.returncode}: {(r.stderr.strip().splitlines() or [''])[-1][:200]}"
    assert r.stdout.strip() == "no matching records", \
        f"--since 2030-01-01: expected 'no matching records', got {r.stdout.strip()[:200]!r}"


for name, fn in [("single_key_text", t_single_key_text), ("multi_key_text", t_multi_key_text),
                 ("month_bucket", t_month_bucket), ("month_with_channel", t_month_with_channel),
                 ("share_half_up", t_share_half_up), ("share_thirds", t_share_thirds),
                 ("tie_order", t_tie_order), ("date_range_inclusive", t_date_range_inclusive),
                 ("markdown_single", t_markdown_single), ("markdown_multi", t_markdown_multi),
                 ("unknown_key", t_unknown_key), ("no_records", t_no_records)]:
    case(name, fn)
