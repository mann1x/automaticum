const SPEC = {
 "env": {
  "PYTHONDONTWRITEBYTECODE": "1"
 },
 "files": {
  "__checker__/check.py": "import datetime as dt\nimport os\nimport re\nimport shutil\nimport subprocess\n\nROOT = os.getcwd()\nSANDBOX = os.path.join(ROOT, \"__checker__\", \"sandbox\")\nSCRIPT = os.path.join(ROOT, \"rotate.sh\")\nNAME = re.compile(r\"^(.+)-(\\d{4}-\\d{2}-\\d{2})(_(\\d{4}))?\\.([A-Za-z0-9][A-Za-z0-9.]*)$\")\n\n\ndef case(name, fn):\n    try:\n        fn()\n        print(f\"TEST {name} PASS\")\n    except AssertionError as e:\n        print(f\"TEST {name} FAIL {e}\")\n    except subprocess.TimeoutExpired:\n        print(f\"TEST {name} FAIL rotate.sh did not finish within 60s\")\n    except Exception as e:\n        print(f\"TEST {name} FAIL {type(e).__name__}: {e}\")\n\n\ndef unlock(path):\n    for d, _, _ in os.walk(path):\n        try:\n            os.chmod(d, 0o755)\n        except OSError:\n            pass\n\n\nclass Tree:\n    def __init__(self, files, links=None):\n        self.top = os.path.join(SANDBOX, \"run\")\n        unlock(self.top)\n        shutil.rmtree(self.top, ignore_errors=True)\n        self.dir = os.path.join(self.top, \"backups\")\n        os.makedirs(self.dir)\n        for rel in files:\n            p = os.path.join(self.dir, rel)\n            os.makedirs(os.path.dirname(p), exist_ok=True)\n            with open(p, \"w\") as fh:\n                fh.write(f\"data of {rel}\\n\")\n        for rel, target in (links or {}).items():\n            os.symlink(target, os.path.join(self.dir, rel))\n\n    def run(self, *args, dir_arg: str | None = \"backups\"):\n        argv = [\"bash\", SCRIPT, *args] + ([dir_arg] if dir_arg else [])\n        r = subprocess.run(argv, cwd=self.top, capture_output=True, text=True, timeout=60)\n        return r.returncode, r.stdout, r.stderr\n\n    def files(self):\n        out = set()\n        for d, _, names in os.walk(self.dir):\n            for n in names:\n                out.add(os.path.relpath(os.path.join(d, n), self.dir))\n        return out\n\n\ndef oracle(rels, today, daily, weekly, monthly):\n    sets = {}\n    for rel in rels:\n        base = rel.rsplit(\"/\", 1)[-1]\n        sub = rel[: len(rel) - len(base)]\n        if base.endswith(\".sha256\"):\n            continue\n        m = NAME.match(base)\n        if not m:\n            continue\n        try:\n            day = dt.date.fromisoformat(m[2])\n        except ValueError:\n            continue\n        sets.setdefault((sub, m[1], m[5]), []).append((m[2] + (m[4] or \"0000\"), day, rel))\n    plan = {}\n    for items in sets.values():\n        items.sort(reverse=True)\n        limits, counts, last = [daily, weekly, monthly], [0, 0, 0], [None, None, None]\n        for _, day, rel in items:\n            if day > today:\n                plan[rel] = \"keep (future)\"\n                continue\n            periods = [day, day.isocalendar()[:2], (day.year, day.month)]\n            reasons = []\n            for i, rule in enumerate([\"daily\", \"weekly\", \"monthly\"]):\n                if periods[i] != last[i] and counts[i] < limits[i]:\n                    reasons.append(rule)\n                    counts[i] += 1\n                    last[i] = periods[i]\n            plan[rel] = f\"keep ({','.join(reasons)})\" if reasons else \"delete\"\n    lines = []\n    for rel in sorted(plan, key=lambda s: s.encode()):\n        v = plan[rel]\n        lines.append(f\"delete {rel}\" if v == \"delete\" else f\"keep {rel} {v[5:]}\")\n    return lines, {r for r, v in plan.items() if v == \"delete\"}\n\n\ndef same(got, want_lines, what):\n    g = got.rstrip(\"\\n\").split(\"\\n\") if got.strip() else []\n    for i in range(max(len(g), len(want_lines))):\n        gl = g[i] if i < len(g) else \"<missing>\"\n        wl = want_lines[i] if i < len(want_lines) else \"<nothing more>\"\n        assert gl == wl, f\"{what}: line {i + 1}: expected {wl!r}, got {gl!r}\"\n\n\ndef check_run(files, today, counts, what, links=None, extra=(), dry=False):\n    t = Tree(files, links)\n    args = [\"--today\", today, \"--daily\", str(counts[0]), \"--weekly\", str(counts[1]), \"--monthly\", str(counts[2])]\n    args += list(extra) + ([\"--dry-run\"] if dry else [])\n    rc, out, err = t.run(*args)\n    assert rc == 0, f\"{what}: exit status {rc}, expected 0 (stderr: {err.strip()[:200]!r})\"\n    lines, deleted = oracle(files, dt.date.fromisoformat(today), *counts)\n    kept = len(lines) - len(deleted)\n    summary = f\"kept {kept}, would delete {len(deleted)}\" if dry else f\"kept {kept}, deleted {len(deleted)}\"\n    same(out, lines + [summary], what)\n    gone_side = {f\"{r}.sha256\" for r in deleted} if not dry else set()\n    want_left = set(files) - (set() if dry else deleted) - gone_side\n    left = t.files() - set(links or {})\n    missing = sorted(want_left - left)\n    extra_left = sorted(left - want_left)\n    assert not missing, f\"{what}: {missing[0]} was deleted but should have been kept\"\n    assert not extra_left, f\"{what}: {extra_left[0]} should have been deleted\"\n    return t, out, err\n\n\ndef daily_series(prefix, start, days, step=1, ext=\"tar.gz\"):\n    d = dt.date.fromisoformat(start)\n    return [f\"{prefix}-{(d + dt.timedelta(days=i)).isoformat()}.{ext}\" for i in range(0, days, step)]\n\n\ndef t_usage_errors():\n    files = [\"app-2020-01-01.tar.gz\", \"app-2020-01-02.tar.gz\"]\n    bad = [([\"--bogus\"], \"an unknown option\"), ([\"--daily\", \"x\"], \"--daily x\"), ([\"--weekly\", \"-1\"], \"--weekly -1\"),\n           ([\"--today\", \"2026-13-01\"], \"--today 2026-13-01\"), ([\"--today\", \"yesterday\"], \"--today yesterday\"),\n           ([\"--daily\", \"0\", \"--weekly\", \"0\", \"--monthly\", \"0\"], \"all counts 0\")]\n    for args, what in bad:\n        t = Tree(files)\n        rc, _, _ = t.run(*args)\n        assert rc == 2, f\"{what}: expected exit 2, got {rc}\"\n        assert t.files() == set(files), f\"{what}: files were deleted although the usage was bad\"\n    t = Tree(files)\n    rc, _, _ = t.run(\"--today\", \"2020-02-01\", dir_arg=\"missing-dir\")\n    assert rc == 2, f\"a BACKUP_DIR that does not exist: expected exit 2, got {rc}\"\n    rc, _, _ = t.run(\"--today\", \"2020-02-01\", dir_arg=None)\n    assert rc == 2, f\"no BACKUP_DIR: expected exit 2, got {rc}\"\n    rc, _, _ = t.run(\"--today\", \"2020-02-01\", \"backups\", dir_arg=\"backups\")\n    assert rc == 2, f\"two directories: expected exit 2, got {rc}\"\n    assert t.files() == set(files), \"files were deleted although the usage was bad\"\n\n\ndef t_daily_rule():\n    files = [\"db-2026-03-01.dump\", \"db-2026-03-03_0100.dump\", \"db-2026-03-03_2330.dump\", \"db-2026-03-03.dump\",\n             \"db-2026-03-06_1200.dump\", \"db-2026-03-07.dump\", \"db-2026-03-07_0600.dump\"]\n    t, out, _ = check_run(files, \"2026-03-08\", (3, 0, 0), \"--daily 3 only\")\n    same(out, [\"delete db-2026-03-01.dump\", \"delete db-2026-03-03.dump\", \"delete db-2026-03-03_0100.dump\",\n               \"keep db-2026-03-03_2330.dump (daily)\", \"keep db-2026-03-06_1200.dump (daily)\",\n               \"delete db-2026-03-07.dump\", \"keep db-2026-03-07_0600.dump (daily)\", \"kept 3, deleted 4\"],\n         \"--daily 3 only\")\n\n\ndef t_weekly_rule():\n    files = daily_series(\"site\", \"2026-05-01\", 40, step=3) + [\"site-2026-06-07_2200.tar.gz\", \"site-2026-06-08.tar.gz\"]\n    check_run(files, \"2026-06-10\", (0, 3, 0), \"--weekly 3 only\")\n    check_run(files, \"2026-06-10\", (2, 5, 0), \"--daily 2 --weekly 5\")\n\n\ndef t_iso_week_year():\n    files = [\"mail-2025-12-21.tar.gz\", \"mail-2025-12-23.tar.gz\", \"mail-2025-12-29.tar.gz\", \"mail-2026-01-01.tar.gz\",\n             \"mail-2026-01-02.tar.gz\"]\n    t, out, _ = check_run(files, \"2026-01-03\", (0, 2, 0), \"weeks across new year\")\n    same(out, [\"delete mail-2025-12-21.tar.gz\", \"keep mail-2025-12-23.tar.gz (weekly)\", \"delete mail-2025-12-29.tar.gz\",\n               \"delete mail-2026-01-01.tar.gz\", \"keep mail-2026-01-02.tar.gz (weekly)\", \"kept 2, deleted 3\"],\n         \"weeks across new year\")\n    files = daily_series(\"mail\", \"2020-12-20\", 30) + daily_series(\"mail\", \"2026-12-20\", 20)\n    check_run(files, \"2027-01-20\", (0, 6, 0), \"weeks across 2020/2021 and 2026/2027\")\n\n\ndef t_monthly_rule():\n    files = daily_series(\"erp\", \"2025-01-15\", 420, step=9, ext=\"sql.gz\")\n    check_run(files, \"2026-04-01\", (0, 0, 5), \"--monthly 5 only\")\n    check_run(files, \"2026-04-01\", (1, 1, 12), \"--daily 1 --weekly 1 --monthly 12\")\n\n\ndef t_default_policy():\n    files = daily_series(\"orders\", \"2025-09-01\", 420, step=1, ext=\"sql.gz\")\n    files += [f\"orders-2026-10-{d:02d}_{h:02d}00.sql.gz\" for d in (20, 21, 22) for h in (6, 18)]\n    t = Tree(files)\n    rc, out, err = t.run(\"--today\", \"2026-10-23\")\n    assert rc == 0, f\"default policy: exit status {rc}, expected 0 (stderr: {err.strip()[:200]!r})\"\n    lines, deleted = oracle(files, dt.date(2026, 10, 23), 7, 4, 6)\n    same(out, lines + [f\"kept {len(lines) - len(deleted)}, deleted {len(deleted)}\"], \"default policy (7/4/6)\")\n\n\ndef t_dry_run():\n    files = daily_series(\"app\", \"2026-01-01\", 60) + [\"app-2026-01-05.tar.gz.sha256\"]\n    t, _, _ = check_run(files, \"2026-03-15\", (3, 2, 2), \"--dry-run\", dry=True)\n    t = Tree(files)\n    rc, out, _ = t.run(\"-n\", \"--today\", \"2026-03-15\", \"--daily\", \"3\", \"--weekly\", \"2\", \"--monthly\", \"2\")\n    assert rc == 0 and t.files() == set(files), \"-n: files were deleted or the exit status was not 0\"\n    lines, deleted = oracle(files, dt.date(2026, 3, 15), 3, 2, 2)\n    same(out, lines + [f\"kept {len(lines) - len(deleted)}, would delete {len(deleted)}\"], \"-n\")\n\n\ndef t_separate_sets():\n    files = (daily_series(\"orders\", \"2026-02-01\", 10, ext=\"sql.gz\") +\n             daily_series(\"orders\", \"2026-02-03\", 6, ext=\"tar.gz\") +\n             [f\"eu/{n}\" for n in daily_series(\"orders\", \"2026-02-02\", 5, ext=\"sql.gz\")] +\n             [f\"eu/{n}\" for n in daily_series(\"shop front\", \"2026-02-04\", 4)] +\n             [f\"eu/{n}\" for n in daily_series(\"my-app-v2\", \"2026-02-01\", 7, ext=\"tgz\")] +\n             [f\"deep/a b/{n}\" for n in daily_series(\"x\", \"2026-01-30\", 5)])\n    check_run(files, \"2026-02-20\", (2, 0, 0), \"several sets\", extra=[\"--\"])\n    t = Tree(files)\n    rc, out, _ = t.run(\"--today\", \"2026-02-20\", \"--daily\", \"2\", \"--weekly\", \"0\", \"--monthly\", \"0\", dir_arg=\"backups/\")\n    lines, deleted = oracle(files, dt.date(2026, 2, 20), 2, 0, 0)\n    assert rc == 0, f\"BACKUP_DIR with a trailing slash: exit status {rc}\"\n    same(out, lines + [f\"kept {len(lines) - len(deleted)}, deleted {len(deleted)}\"], \"BACKUP_DIR given as backups/\")\n\n\ndef t_sidecars():\n    files = daily_series(\"vault\", \"2026-04-01\", 6)\n    files += [f\"{f}.sha256\" for f in files] + [\"vault-2026-03-01.tar.gz.sha256\", \"vault.sha256\"]\n    t, out, _ = check_run(files, \"2026-04-30\", (2, 0, 0), \"checksum sidecars\")\n    assert \".sha256\" not in out, \"a .sha256 sidecar appears in the report\"\n\n\ndef t_ignored_files():\n    files = (daily_series(\"pg\", \"2026-06-01\", 5, ext=\"dump\") +\n             [\"pg-2026-02-30.dump\", \"pg-2026-06-01.dump.partial~\", \"README\", \"pg-latest.dump\", \"pg_2026-06-01.dump\",\n              \"pg-26-06-01.dump\", \"notes/2026-06-01.txt\", \"pg-2026-06-01_12.dump\"])\n    links = {\"pg-2026-05-01.dump\": \"pg-2026-06-01.dump\", \"pg-2026-05-02.dump\": \"nowhere\"}\n    t, out, err = check_run(files, \"2026-06-30\", (1, 0, 0), \"files that are not backups\", links=links)\n    assert \"pg-2026-02-30.dump: invalid date 2026-02-30\" in err, \\\n        \"no 'rotate: skipping pg-2026-02-30.dump: invalid date 2026-02-30' on stderr\"\n    for rel in links:\n        assert os.path.islink(os.path.join(t.dir, rel)), f\"the symbolic link {rel} was removed\"\n\n\ndef t_future_backups():\n    files = daily_series(\"crm\", \"2026-07-01\", 12)\n    t, out, _ = check_run(files, \"2026-07-08\", (2, 1, 0), \"backups dated after --today\")\n    assert \"keep crm-2026-07-12.tar.gz (future)\" in out.split(\"\\n\"), \"a future backup is not kept as (future)\"\n\n\ndef t_option_order():\n    files = daily_series(\"ci\", \"2001-01-01\", 20)\n    t = Tree(files)\n    rc, out, err = t.run(\"backups\", \"--daily\", \"2\", \"--monthly\", \"0\", \"--weekly\", \"1\", dir_arg=None)\n    lines, deleted = oracle(files, dt.date.today(), 2, 1, 0)\n    assert rc == 0, f\"options after BACKUP_DIR (and today's real date): exit status {rc} (stderr: {err.strip()[:200]!r})\"\n    same(out, lines + [f\"kept {len(lines) - len(deleted)}, deleted {len(deleted)}\"], \"options after BACKUP_DIR\")\n    t = Tree([])\n    os.rename(t.dir, os.path.join(t.top, \"-odd\"))\n    t.dir = os.path.join(t.top, \"-odd\")\n    for f in files:\n        with open(os.path.join(t.dir, f), \"w\") as fh:\n            fh.write(\"x\\n\")\n    rc, out, err = t.run(\"--today\", \"2001-02-01\", \"--daily\", \"1\", \"--weekly\", \"0\", \"--monthly\", \"0\", \"--\", \"-odd\",\n                         dir_arg=None)\n    assert rc == 0, f\"'-- -odd': exit status {rc} (stderr: {err.strip()[:200]!r})\"\n    last = out.rstrip(\"\\n\").split(\"\\n\")[-1]\n    assert last == \"kept 1, deleted 19\", f\"'-- -odd': summary line: expected 'kept 1, deleted 19', got {last!r}\"\n\n\ndef t_empty_tree():\n    t = Tree([\"README.txt\"])\n    rc, out, _ = t.run(\"--today\", \"2026-01-01\")\n    assert rc == 0, f\"a tree without backups: exit status {rc}, expected 0\"\n    same(out, [\"kept 0, deleted 0\"], \"a tree without backups\")\n\n\ndef t_delete_failure():\n    files = daily_series(\"lab\", \"2026-08-01\", 5) + [f\"locked/{n}\" for n in daily_series(\"lab\", \"2026-08-01\", 5)]\n    t = Tree(files)\n    locked = os.path.join(t.dir, \"locked\")\n    os.chmod(locked, 0o555)\n    try:\n        probe = os.path.join(locked, \"probe\")\n        try:\n            open(probe, \"w\").close()\n            os.remove(probe)\n            enforced = False  # running as root: permissions are not enforced, only the normal path is checked\n        except OSError:\n            enforced = True\n        rc, out, err = t.run(\"--today\", \"2026-08-31\", \"--daily\", \"1\", \"--weekly\", \"0\", \"--monthly\", \"0\")\n    finally:\n        os.chmod(locked, 0o755)\n    lines, deleted = oracle(files, dt.date(2026, 8, 31), 1, 0, 0)\n    if not enforced:\n        assert rc == 0, f\"exit status {rc}\"\n        return\n    assert rc == 1, f\"deletions failed in a read-only directory: expected exit 1, got {rc}\"\n    same(out, lines + [\"kept 2, deleted 4, failed 4\"], \"report with failed deletions\")\n    for rel in sorted(deleted):\n        if rel.startswith(\"locked/\"):\n            assert f\"rotate: cannot delete {rel}\" in err, f\"no 'rotate: cannot delete {rel}' on stderr\"\n    left = t.files()\n    assert not any(r in left for r in deleted if not r.startswith(\"locked/\")), \\\n        \"the deletions outside the read-only directory did not happen\"\n\n\nTESTS = [(\"usage_errors\", t_usage_errors), (\"daily_rule\", t_daily_rule), (\"weekly_rule\", t_weekly_rule),\n         (\"iso_week_year\", t_iso_week_year), (\"monthly_rule\", t_monthly_rule), (\"default_policy\", t_default_policy),\n         (\"dry_run\", t_dry_run), (\"separate_sets\", t_separate_sets), (\"sidecars\", t_sidecars),\n         (\"ignored_files\", t_ignored_files), (\"future_backups\", t_future_backups), (\"option_order\", t_option_order),\n         (\"empty_tree\", t_empty_tree), (\"delete_failure\", t_delete_failure)]\n\nfor name, fn in TESTS:\n    case(name, fn)\nunlock(SANDBOX)\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "bash",
    "-n",
    "rotate.sh"
   ],
   "label": "syntax",
   "timeout_s": 30
  },
  {
   "cmd": [
    "python3",
    "-B",
    "__checker__/check.py"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 240
  }
 ],
 "tests": [
  "usage_errors",
  "daily_rule",
  "weekly_rule",
  "iso_week_year",
  "monthly_rule",
  "default_policy",
  "dry_run",
  "separate_sets",
  "sidecars",
  "ignored_files",
  "future_backups",
  "option_order",
  "empty_tree",
  "delete_failure"
 ]
};
// The checker every training task ships as `run_tests`: the agent may run it, and cannot read it.
//
// make_task.py prepends `const SPEC = {...};` (the task's hidden test files, its build and test
// steps, the test names it must see) and build.mjs (manic_miner's) turns the result into a Node
// single executable whose source is masked.
//
// It copies the working directory to a private temporary directory, writes the hidden tests into
// it, runs the steps there with the language's own tools, and prints ONE JSON line:
//
//   {"ok":true|false,"stage":"build"|"test"|"done","passed":N,"total":N,"failures":[...],"output":"..."}
//
// Test programs report one line per test on stdout:  TEST <name> PASS   or   TEST <name> FAIL <message>
// "ok" needs every step to exit 0 and every name in SPEC.tests to report PASS: a test that never
// reports (the program died, or exited early) is a failure. Output lines that point into the hidden
// test directory are dropped, so a build error never prints the tests' source.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const cp = require("node:child_process");

const HIDDEN = SPEC.hidden_dir || "__checker__";
const MAX_OUT = 6000;

function copyTree(src, dst) {
	fs.mkdirSync(dst, { recursive: true });
	for (const e of fs.readdirSync(src, { withFileTypes: true })) {
		if (e.name === ".git" || e.name === "run_tests" || e.name === HIDDEN) continue;
		const s = path.join(src, e.name);
		const d = path.join(dst, e.name);
		if (e.isDirectory()) copyTree(s, d);
		else if (e.isFile()) fs.copyFileSync(s, d);
	}
}

// A failure message is the test's own words, but an error inside it (a stack, a module path) can name a
// hidden file: any token that contains the hidden directory's name is replaced.
function hide(text) {
	return String(text).replace(new RegExp(`\\S*${HIDDEN}\\S*`, "g"), "<checker>");
}

// A compiler names the file on one line and quotes its source on the next ones ("  12 | code", a "^~~~"
// marker line): after a line that names a hidden file, those quoted lines are dropped as well.
const QUOTED = /^\s*(\d+\s*)?\|/;
const MARKER = /^\s*\|?\s*[\^~\-+]+[\s\^~\-+]*$/;

function scrub(text) {
	const lines = [];
	let quoting = false;
	for (const l of String(text || "").split("\n")) {
		if (l.includes(HIDDEN)) {
			quoting = true;
			continue;
		}
		if (quoting && (QUOTED.test(l) || MARKER.test(l) || /^\s*$/.test(l))) continue;
		quoting = false;
		if (!/^TEST /.test(l)) lines.push(l);
	}
	const out = lines.join("\n").trim();
	return out.length > MAX_OUT ? `${out.slice(0, MAX_OUT)}\n[... output cut]` : out;
}

function finish(res, work) {
	try {
		fs.rmSync(work, { recursive: true, force: true });
	} catch {}
	process.stdout.write(`${JSON.stringify(res)}\n`);
	process.exit(0);
}

const work = fs.mkdtempSync(path.join(os.tmpdir(), "chk-"));
const res = { ok: false, stage: "build", passed: 0, total: SPEC.tests.length, failures: [], output: "" };
try {
	copyTree(process.cwd(), work);
	for (const [rel, body] of Object.entries(SPEC.files)) {
		const p = path.join(work, rel);
		fs.mkdirSync(path.dirname(p), { recursive: true });
		fs.writeFileSync(p, body);
	}
} catch (e) {
	res.output = `could not prepare the check: ${e.message}`;
	finish(res, work);
}

const env = {
	PATH: process.env.PATH || "/usr/local/bin:/usr/bin:/bin",
	HOME: work,
	TMPDIR: work,
	LANG: "C.UTF-8",
	...(SPEC.env || {}),
};
for (const [k, v] of Object.entries(env)) env[k] = String(v).replaceAll("$WORK", work);

const seen = new Map();
let output = "";
for (const step of SPEC.steps) {
	const cmd = step.cmd.map((a) => String(a).replaceAll("$WORK", work));
	const r = cp.spawnSync(cmd[0], cmd.slice(1), {
		cwd: work,
		env,
		encoding: "utf8",
		timeout: (step.timeout_s || 120) * 1000,
		maxBuffer: 64 * 1024 * 1024,
	});
	const text = `${r.stdout || ""}\n${r.stderr || ""}`;
	if (step.tests) {
		res.stage = "test";
		for (const m of String(r.stdout || "").matchAll(/^TEST (\S+) (PASS|FAIL)(?: (.*))?$/gm)) {
			if (!seen.has(m[1]) || m[2] === "FAIL") seen.set(m[1], m[2] === "PASS" ? null : hide(m[3] || "failed").slice(0, 300));
		}
	}
	output += `${scrub(text)}\n`;
	if (r.error && r.error.code === "ETIMEDOUT") {
		res.output = scrub(`${output}\nstep timed out after ${step.timeout_s || 120}s: ${step.label || step.cmd[0]}`);
		break;
	}
	if (r.error) {
		res.output = scrub(`${output}\ncould not run ${cmd[0]}: ${r.error.message}`);
		break;
	}
	if (r.status !== 0 && !step.tests) {
		res.output = scrub(`${output}\n${step.label || step.cmd[0]} failed (exit ${r.status})`);
		break;
	}
	if (step.tests && r.status !== 0 && seen.size === 0) {
		res.output = scrub(`${output}\nthe tests did not run (exit ${r.status})`);
		break;
	}
}
for (const name of SPEC.tests) {
	if (!seen.has(name)) res.failures.push({ test: name, message: "did not run" });
	else if (seen.get(name) !== null) res.failures.push({ test: name, message: seen.get(name) });
	else res.passed += 1;
}
if (res.stage === "test" && !res.output) {
	res.stage = "done";
	res.output = scrub(output);
}
res.ok = res.stage === "done" && res.passed === res.total;
finish(res, work);
