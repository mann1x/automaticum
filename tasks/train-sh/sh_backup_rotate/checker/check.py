import datetime as dt
import os
import re
import shutil
import subprocess

ROOT = os.getcwd()
SANDBOX = os.path.join(ROOT, "__checker__", "sandbox")
SCRIPT = os.path.join(ROOT, "rotate.sh")
NAME = re.compile(r"^(.+)-(\d{4}-\d{2}-\d{2})(_(\d{4}))?\.([A-Za-z0-9][A-Za-z0-9.]*)$")


def case(name, fn):
    try:
        fn()
        print(f"TEST {name} PASS")
    except AssertionError as e:
        print(f"TEST {name} FAIL {e}")
    except subprocess.TimeoutExpired:
        print(f"TEST {name} FAIL rotate.sh did not finish within 60s")
    except Exception as e:
        print(f"TEST {name} FAIL {type(e).__name__}: {e}")


def unlock(path):
    for d, _, _ in os.walk(path):
        try:
            os.chmod(d, 0o755)
        except OSError:
            pass


class Tree:
    def __init__(self, files, links=None):
        self.top = os.path.join(SANDBOX, "run")
        unlock(self.top)
        shutil.rmtree(self.top, ignore_errors=True)
        self.dir = os.path.join(self.top, "backups")
        os.makedirs(self.dir)
        for rel in files:
            p = os.path.join(self.dir, rel)
            os.makedirs(os.path.dirname(p), exist_ok=True)
            with open(p, "w") as fh:
                fh.write(f"data of {rel}\n")
        for rel, target in (links or {}).items():
            os.symlink(target, os.path.join(self.dir, rel))

    def run(self, *args, dir_arg: str | None = "backups"):
        argv = ["bash", SCRIPT, *args] + ([dir_arg] if dir_arg else [])
        r = subprocess.run(argv, cwd=self.top, capture_output=True, text=True, timeout=60)
        return r.returncode, r.stdout, r.stderr

    def files(self):
        out = set()
        for d, _, names in os.walk(self.dir):
            for n in names:
                out.add(os.path.relpath(os.path.join(d, n), self.dir))
        return out


def oracle(rels, today, daily, weekly, monthly):
    sets = {}
    for rel in rels:
        base = rel.rsplit("/", 1)[-1]
        sub = rel[: len(rel) - len(base)]
        if base.endswith(".sha256"):
            continue
        m = NAME.match(base)
        if not m:
            continue
        try:
            day = dt.date.fromisoformat(m[2])
        except ValueError:
            continue
        sets.setdefault((sub, m[1], m[5]), []).append((m[2] + (m[4] or "0000"), day, rel))
    plan = {}
    for items in sets.values():
        items.sort(reverse=True)
        limits, counts, last = [daily, weekly, monthly], [0, 0, 0], [None, None, None]
        for _, day, rel in items:
            if day > today:
                plan[rel] = "keep (future)"
                continue
            periods = [day, day.isocalendar()[:2], (day.year, day.month)]
            reasons = []
            for i, rule in enumerate(["daily", "weekly", "monthly"]):
                if periods[i] != last[i] and counts[i] < limits[i]:
                    reasons.append(rule)
                    counts[i] += 1
                    last[i] = periods[i]
            plan[rel] = f"keep ({','.join(reasons)})" if reasons else "delete"
    lines = []
    for rel in sorted(plan, key=lambda s: s.encode()):
        v = plan[rel]
        lines.append(f"delete {rel}" if v == "delete" else f"keep {rel} {v[5:]}")
    return lines, {r for r, v in plan.items() if v == "delete"}


def same(got, want_lines, what):
    g = got.rstrip("\n").split("\n") if got.strip() else []
    for i in range(max(len(g), len(want_lines))):
        gl = g[i] if i < len(g) else "<missing>"
        wl = want_lines[i] if i < len(want_lines) else "<nothing more>"
        assert gl == wl, f"{what}: line {i + 1}: expected {wl!r}, got {gl!r}"


def check_run(files, today, counts, what, links=None, extra=(), dry=False):
    t = Tree(files, links)
    args = ["--today", today, "--daily", str(counts[0]), "--weekly", str(counts[1]), "--monthly", str(counts[2])]
    args += list(extra) + (["--dry-run"] if dry else [])
    rc, out, err = t.run(*args)
    assert rc == 0, f"{what}: exit status {rc}, expected 0 (stderr: {err.strip()[:200]!r})"
    lines, deleted = oracle(files, dt.date.fromisoformat(today), *counts)
    kept = len(lines) - len(deleted)
    summary = f"kept {kept}, would delete {len(deleted)}" if dry else f"kept {kept}, deleted {len(deleted)}"
    same(out, lines + [summary], what)
    gone_side = {f"{r}.sha256" for r in deleted} if not dry else set()
    want_left = set(files) - (set() if dry else deleted) - gone_side
    left = t.files() - set(links or {})
    missing = sorted(want_left - left)
    extra_left = sorted(left - want_left)
    assert not missing, f"{what}: {missing[0]} was deleted but should have been kept"
    assert not extra_left, f"{what}: {extra_left[0]} should have been deleted"
    return t, out, err


def daily_series(prefix, start, days, step=1, ext="tar.gz"):
    d = dt.date.fromisoformat(start)
    return [f"{prefix}-{(d + dt.timedelta(days=i)).isoformat()}.{ext}" for i in range(0, days, step)]


def t_usage_errors():
    files = ["app-2020-01-01.tar.gz", "app-2020-01-02.tar.gz"]
    bad = [(["--bogus"], "an unknown option"), (["--daily", "x"], "--daily x"), (["--weekly", "-1"], "--weekly -1"),
           (["--today", "2026-13-01"], "--today 2026-13-01"), (["--today", "yesterday"], "--today yesterday"),
           (["--daily", "0", "--weekly", "0", "--monthly", "0"], "all counts 0")]
    for args, what in bad:
        t = Tree(files)
        rc, _, _ = t.run(*args)
        assert rc == 2, f"{what}: expected exit 2, got {rc}"
        assert t.files() == set(files), f"{what}: files were deleted although the usage was bad"
    t = Tree(files)
    rc, _, _ = t.run("--today", "2020-02-01", dir_arg="missing-dir")
    assert rc == 2, f"a BACKUP_DIR that does not exist: expected exit 2, got {rc}"
    rc, _, _ = t.run("--today", "2020-02-01", dir_arg=None)
    assert rc == 2, f"no BACKUP_DIR: expected exit 2, got {rc}"
    rc, _, _ = t.run("--today", "2020-02-01", "backups", dir_arg="backups")
    assert rc == 2, f"two directories: expected exit 2, got {rc}"
    assert t.files() == set(files), "files were deleted although the usage was bad"


def t_daily_rule():
    files = ["db-2026-03-01.dump", "db-2026-03-03_0100.dump", "db-2026-03-03_2330.dump", "db-2026-03-03.dump",
             "db-2026-03-06_1200.dump", "db-2026-03-07.dump", "db-2026-03-07_0600.dump"]
    t, out, _ = check_run(files, "2026-03-08", (3, 0, 0), "--daily 3 only")
    same(out, ["delete db-2026-03-01.dump", "delete db-2026-03-03.dump", "delete db-2026-03-03_0100.dump",
               "keep db-2026-03-03_2330.dump (daily)", "keep db-2026-03-06_1200.dump (daily)",
               "delete db-2026-03-07.dump", "keep db-2026-03-07_0600.dump (daily)", "kept 3, deleted 4"],
         "--daily 3 only")


def t_weekly_rule():
    files = daily_series("site", "2026-05-01", 40, step=3) + ["site-2026-06-07_2200.tar.gz", "site-2026-06-08.tar.gz"]
    check_run(files, "2026-06-10", (0, 3, 0), "--weekly 3 only")
    check_run(files, "2026-06-10", (2, 5, 0), "--daily 2 --weekly 5")


def t_iso_week_year():
    files = ["mail-2025-12-21.tar.gz", "mail-2025-12-23.tar.gz", "mail-2025-12-29.tar.gz", "mail-2026-01-01.tar.gz",
             "mail-2026-01-02.tar.gz"]
    t, out, _ = check_run(files, "2026-01-03", (0, 2, 0), "weeks across new year")
    same(out, ["delete mail-2025-12-21.tar.gz", "keep mail-2025-12-23.tar.gz (weekly)", "delete mail-2025-12-29.tar.gz",
               "delete mail-2026-01-01.tar.gz", "keep mail-2026-01-02.tar.gz (weekly)", "kept 2, deleted 3"],
         "weeks across new year")
    files = daily_series("mail", "2020-12-20", 30) + daily_series("mail", "2026-12-20", 20)
    check_run(files, "2027-01-20", (0, 6, 0), "weeks across 2020/2021 and 2026/2027")


def t_monthly_rule():
    files = daily_series("erp", "2025-01-15", 420, step=9, ext="sql.gz")
    check_run(files, "2026-04-01", (0, 0, 5), "--monthly 5 only")
    check_run(files, "2026-04-01", (1, 1, 12), "--daily 1 --weekly 1 --monthly 12")


def t_default_policy():
    files = daily_series("orders", "2025-09-01", 420, step=1, ext="sql.gz")
    files += [f"orders-2026-10-{d:02d}_{h:02d}00.sql.gz" for d in (20, 21, 22) for h in (6, 18)]
    t = Tree(files)
    rc, out, err = t.run("--today", "2026-10-23")
    assert rc == 0, f"default policy: exit status {rc}, expected 0 (stderr: {err.strip()[:200]!r})"
    lines, deleted = oracle(files, dt.date(2026, 10, 23), 7, 4, 6)
    same(out, lines + [f"kept {len(lines) - len(deleted)}, deleted {len(deleted)}"], "default policy (7/4/6)")


def t_dry_run():
    files = daily_series("app", "2026-01-01", 60) + ["app-2026-01-05.tar.gz.sha256"]
    t, _, _ = check_run(files, "2026-03-15", (3, 2, 2), "--dry-run", dry=True)
    t = Tree(files)
    rc, out, _ = t.run("-n", "--today", "2026-03-15", "--daily", "3", "--weekly", "2", "--monthly", "2")
    assert rc == 0 and t.files() == set(files), "-n: files were deleted or the exit status was not 0"
    lines, deleted = oracle(files, dt.date(2026, 3, 15), 3, 2, 2)
    same(out, lines + [f"kept {len(lines) - len(deleted)}, would delete {len(deleted)}"], "-n")


def t_separate_sets():
    files = (daily_series("orders", "2026-02-01", 10, ext="sql.gz") +
             daily_series("orders", "2026-02-03", 6, ext="tar.gz") +
             [f"eu/{n}" for n in daily_series("orders", "2026-02-02", 5, ext="sql.gz")] +
             [f"eu/{n}" for n in daily_series("shop front", "2026-02-04", 4)] +
             [f"eu/{n}" for n in daily_series("my-app-v2", "2026-02-01", 7, ext="tgz")] +
             [f"deep/a b/{n}" for n in daily_series("x", "2026-01-30", 5)])
    check_run(files, "2026-02-20", (2, 0, 0), "several sets", extra=["--"])
    t = Tree(files)
    rc, out, _ = t.run("--today", "2026-02-20", "--daily", "2", "--weekly", "0", "--monthly", "0", dir_arg="backups/")
    lines, deleted = oracle(files, dt.date(2026, 2, 20), 2, 0, 0)
    assert rc == 0, f"BACKUP_DIR with a trailing slash: exit status {rc}"
    same(out, lines + [f"kept {len(lines) - len(deleted)}, deleted {len(deleted)}"], "BACKUP_DIR given as backups/")


def t_sidecars():
    files = daily_series("vault", "2026-04-01", 6)
    files += [f"{f}.sha256" for f in files] + ["vault-2026-03-01.tar.gz.sha256", "vault.sha256"]
    t, out, _ = check_run(files, "2026-04-30", (2, 0, 0), "checksum sidecars")
    assert ".sha256" not in out, "a .sha256 sidecar appears in the report"


def t_ignored_files():
    files = (daily_series("pg", "2026-06-01", 5, ext="dump") +
             ["pg-2026-02-30.dump", "pg-2026-06-01.dump.partial~", "README", "pg-latest.dump", "pg_2026-06-01.dump",
              "pg-26-06-01.dump", "notes/2026-06-01.txt", "pg-2026-06-01_12.dump"])
    links = {"pg-2026-05-01.dump": "pg-2026-06-01.dump", "pg-2026-05-02.dump": "nowhere"}
    t, out, err = check_run(files, "2026-06-30", (1, 0, 0), "files that are not backups", links=links)
    assert "pg-2026-02-30.dump: invalid date 2026-02-30" in err, \
        "no 'rotate: skipping pg-2026-02-30.dump: invalid date 2026-02-30' on stderr"
    for rel in links:
        assert os.path.islink(os.path.join(t.dir, rel)), f"the symbolic link {rel} was removed"


def t_future_backups():
    files = daily_series("crm", "2026-07-01", 12)
    t, out, _ = check_run(files, "2026-07-08", (2, 1, 0), "backups dated after --today")
    assert "keep crm-2026-07-12.tar.gz (future)" in out.split("\n"), "a future backup is not kept as (future)"


def t_option_order():
    files = daily_series("ci", "2001-01-01", 20)
    t = Tree(files)
    rc, out, err = t.run("backups", "--daily", "2", "--monthly", "0", "--weekly", "1", dir_arg=None)
    lines, deleted = oracle(files, dt.date.today(), 2, 1, 0)
    assert rc == 0, f"options after BACKUP_DIR (and today's real date): exit status {rc} (stderr: {err.strip()[:200]!r})"
    same(out, lines + [f"kept {len(lines) - len(deleted)}, deleted {len(deleted)}"], "options after BACKUP_DIR")
    t = Tree([])
    os.rename(t.dir, os.path.join(t.top, "-odd"))
    t.dir = os.path.join(t.top, "-odd")
    for f in files:
        with open(os.path.join(t.dir, f), "w") as fh:
            fh.write("x\n")
    rc, out, err = t.run("--today", "2001-02-01", "--daily", "1", "--weekly", "0", "--monthly", "0", "--", "-odd",
                         dir_arg=None)
    assert rc == 0, f"'-- -odd': exit status {rc} (stderr: {err.strip()[:200]!r})"
    last = out.rstrip("\n").split("\n")[-1]
    assert last == "kept 1, deleted 19", f"'-- -odd': summary line: expected 'kept 1, deleted 19', got {last!r}"


def t_empty_tree():
    t = Tree(["README.txt"])
    rc, out, _ = t.run("--today", "2026-01-01")
    assert rc == 0, f"a tree without backups: exit status {rc}, expected 0"
    same(out, ["kept 0, deleted 0"], "a tree without backups")


def t_delete_failure():
    files = daily_series("lab", "2026-08-01", 5) + [f"locked/{n}" for n in daily_series("lab", "2026-08-01", 5)]
    t = Tree(files)
    locked = os.path.join(t.dir, "locked")
    os.chmod(locked, 0o555)
    try:
        probe = os.path.join(locked, "probe")
        try:
            open(probe, "w").close()
            os.remove(probe)
            enforced = False  # running as root: permissions are not enforced, only the normal path is checked
        except OSError:
            enforced = True
        rc, out, err = t.run("--today", "2026-08-31", "--daily", "1", "--weekly", "0", "--monthly", "0")
    finally:
        os.chmod(locked, 0o755)
    lines, deleted = oracle(files, dt.date(2026, 8, 31), 1, 0, 0)
    if not enforced:
        assert rc == 0, f"exit status {rc}"
        return
    assert rc == 1, f"deletions failed in a read-only directory: expected exit 1, got {rc}"
    same(out, lines + ["kept 2, deleted 4, failed 4"], "report with failed deletions")
    for rel in sorted(deleted):
        if rel.startswith("locked/"):
            assert f"rotate: cannot delete {rel}" in err, f"no 'rotate: cannot delete {rel}' on stderr"
    left = t.files()
    assert not any(r in left for r in deleted if not r.startswith("locked/")), \
        "the deletions outside the read-only directory did not happen"


TESTS = [("usage_errors", t_usage_errors), ("daily_rule", t_daily_rule), ("weekly_rule", t_weekly_rule),
         ("iso_week_year", t_iso_week_year), ("monthly_rule", t_monthly_rule), ("default_policy", t_default_policy),
         ("dry_run", t_dry_run), ("separate_sets", t_separate_sets), ("sidecars", t_sidecars),
         ("ignored_files", t_ignored_files), ("future_backups", t_future_backups), ("option_order", t_option_order),
         ("empty_tree", t_empty_tree), ("delete_failure", t_delete_failure)]

for name, fn in TESTS:
    case(name, fn)
unlock(SANDBOX)
