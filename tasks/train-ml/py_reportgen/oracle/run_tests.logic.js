const SPEC = {
 "env": {
  "PYTHONDONTWRITEBYTECODE": "1"
 },
 "files": {
  "__checker__/check.py": "import os\nimport subprocess\nimport sys\nimport traceback\n\nHERE = os.path.dirname(os.path.abspath(__file__))\nHEADER = \"date,region,product,channel,units,revenue\\n\"\n\nDATA_A = HEADER + \"\"\"\\\n2024-01-05,north,widget,web,3,30.00\n2024-01-20,south,widget,store,1,10.00\n2024-02-02,north,gadget,web,2,50.00\n2024-02-28,south,gadget,web,4,100.00\n2024-02-29,north,widget,store,5,50.00\n2024-03-01,east,widget,web,1,10.00\n\"\"\"\n\n# shares 6.25 / 18.75 / 75.00 -> half-up\nDATA_SHARE = HEADER + \"\"\"\\\n2024-05-01,north,widget,web,1,10.00\n2024-05-02,south,widget,web,3,30.00\n2024-05-03,west,widget,web,12,120.00\n\"\"\"\n\nDATA_THIRDS = HEADER + \"\"\"\\\n2024-06-01,north,widget,web,1,5.00\n2024-06-01,north,gadget,web,1,5.00\n2024-06-02,south,widget,web,2,10.00\n2024-06-03,west,widget,store,1,10.00\n\"\"\"\n\nDATA_TIES = HEADER + \"\"\"\\\n2024-07-01,west,widget,web,1,25.00\n2024-07-02,north,gizmo,store,1,25.00\n2024-07-03,east,widget,web,1,25.00\n2024-07-04,north,gadget,web,1,25.00\n2024-07-05,east,gizmo,store,2,40.00\n\"\"\"\n\n\ndef write(name, text):\n    path = os.path.join(HERE, name)\n    with open(path, \"w\", newline=\"\") as fh:\n        fh.write(text)\n    return path\n\n\ndef run(data, *args):\n    path = write(\"data.csv\", data)\n    r = subprocess.run([sys.executable, \"-B\", \"-m\", \"salesreport\", path, *args],\n                       capture_output=True, text=True, timeout=30)\n    return r\n\n\ndef ok_output(data, *args):\n    r = run(data, *args)\n    cmd = \" \".join([\"salesreport\", \"FILE\", *args])\n    if r.returncode != 0:\n        err = (r.stderr.strip().splitlines() or [\"(no stderr)\"])[-1]\n        raise AssertionError(f\"`{cmd}` exited {r.returncode}: {err}\")\n    return cmd, r.stdout\n\n\ndef same(cmd, got, want):\n    g = got.rstrip(\"\\n\").split(\"\\n\")\n    w = want.rstrip(\"\\n\").split(\"\\n\")\n    for i, (a, b) in enumerate(zip(g, w), start=1):\n        assert a == b, f\"`{cmd}` line {i}: expected {b!r}, got {a!r}\"\n    assert len(g) == len(w), f\"`{cmd}`: expected {len(w)} lines, got {len(g)}\"\n\n\ndef case(name, fn):\n    try:\n        fn()\n        print(f\"TEST {name} PASS\")\n    except AssertionError as e:\n        print(f\"TEST {name} FAIL {e}\")\n    except Exception as e:\n        frame = traceback.extract_tb(e.__traceback__)[-1]\n        where = \"\" if \"__checker__\" in frame.filename else f\" at {frame.filename.split('/')[-1]}:{frame.lineno}\"\n        print(f\"TEST {name} FAIL {type(e).__name__}: {e}{where}\")\n\n\ndef t_single_key_text():\n    cmd, out = ok_output(DATA_A)\n    same(cmd, out, \"\"\"\\\nregion  units  revenue   share\n------  -----  -------  ------\nnorth      10   130.00   52.0%\nsouth       5   110.00   44.0%\neast        1    10.00    4.0%\n------  -----  -------  ------\nTOTAL      16   250.00  100.0%\n\"\"\")\n\n\ndef t_multi_key_text():\n    cmd, out = ok_output(DATA_A, \"--group-by\", \"region,product\")\n    same(cmd, out, \"\"\"\\\nregion  product  units  revenue   share\n------  -------  -----  -------  ------\nsouth   gadget       4   100.00   40.0%\nnorth   widget       8    80.00   32.0%\nnorth   gadget       2    50.00   20.0%\neast    widget       1    10.00    4.0%\nsouth   widget       1    10.00    4.0%\n------  -------  -----  -------  ------\nTOTAL               16   250.00  100.0%\n\"\"\")\n\n\ndef t_month_bucket():\n    cmd, out = ok_output(DATA_A, \"--group-by\", \"month\")\n    same(cmd, out, \"\"\"\\\nmonth    units  revenue   share\n-------  -----  -------  ------\n2024-02     11   200.00   80.0%\n2024-01      4    40.00   16.0%\n2024-03      1    10.00    4.0%\n-------  -----  -------  ------\nTOTAL       16   250.00  100.0%\n\"\"\")\n\n\ndef t_month_with_channel():\n    cmd, out = ok_output(DATA_A, \"--group-by\", \"month,channel\")\n    same(cmd, out, \"\"\"\\\nmonth    channel  units  revenue   share\n-------  -------  -----  -------  ------\n2024-02  web          6   150.00   60.0%\n2024-02  store        5    50.00   20.0%\n2024-01  web          3    30.00   12.0%\n2024-01  store        1    10.00    4.0%\n2024-03  web          1    10.00    4.0%\n-------  -------  -----  -------  ------\nTOTAL                16   250.00  100.0%\n\"\"\")\n\n\ndef t_share_half_up():\n    cmd, out = ok_output(DATA_SHARE)\n    same(cmd, out, \"\"\"\\\nregion  units  revenue   share\n------  -----  -------  ------\nwest       12   120.00   75.0%\nsouth       3    30.00   18.8%\nnorth       1    10.00    6.3%\n------  -----  -------  ------\nTOTAL      16   160.00  100.0%\n\"\"\")\n\n\ndef t_share_thirds():\n    cmd, out = ok_output(DATA_THIRDS, \"--group-by\", \"region\", \"--format\", \"markdown\")\n    same(cmd, out, \"\"\"\\\n| region | units | revenue | share |\n| --- | ---: | ---: | ---: |\n| north | 2 | 10.00 | 33.3% |\n| south | 2 | 10.00 | 33.3% |\n| west | 1 | 10.00 | 33.3% |\n| TOTAL | 5 | 30.00 | 100.0% |\n\"\"\")\n\n\ndef t_tie_order():\n    cmd, out = ok_output(DATA_TIES, \"--group-by\", \"region,product\")\n    same(cmd, out, \"\"\"\\\nregion  product  units  revenue   share\n------  -------  -----  -------  ------\neast    gizmo        2    40.00   28.6%\neast    widget       1    25.00   17.9%\nnorth   gadget       1    25.00   17.9%\nnorth   gizmo        1    25.00   17.9%\nwest    widget       1    25.00   17.9%\n------  -------  -----  -------  ------\nTOTAL                6   140.00  100.0%\n\"\"\")\n\n\ndef t_date_range_inclusive():\n    cmd, out = ok_output(DATA_A, \"--since\", \"2024-02-01\", \"--until\", \"2024-02-29\")\n    same(cmd, out, \"\"\"\\\nregion  units  revenue   share\n------  -----  -------  ------\nnorth       7   100.00   50.0%\nsouth       4   100.00   50.0%\n------  -----  -------  ------\nTOTAL      11   200.00  100.0%\n\"\"\")\n\n\ndef t_markdown_single():\n    cmd, out = ok_output(DATA_A, \"--format\", \"markdown\")\n    same(cmd, out, \"\"\"\\\n| region | units | revenue | share |\n| --- | ---: | ---: | ---: |\n| north | 10 | 130.00 | 52.0% |\n| south | 5 | 110.00 | 44.0% |\n| east | 1 | 10.00 | 4.0% |\n| TOTAL | 16 | 250.00 | 100.0% |\n\"\"\")\n\n\ndef t_markdown_multi():\n    cmd, out = ok_output(DATA_A, \"--group-by\", \"product,region\", \"--format\", \"markdown\",\n                         \"--since\", \"2024-01-20\")\n    same(cmd, out, \"\"\"\\\n| product | region | units | revenue | share |\n| --- | --- | ---: | ---: | ---: |\n| gadget | south | 4 | 100.00 | 45.5% |\n| gadget | north | 2 | 50.00 | 22.7% |\n| widget | north | 5 | 50.00 | 22.7% |\n| widget | east | 1 | 10.00 | 4.5% |\n| widget | south | 1 | 10.00 | 4.5% |\n| TOTAL |  | 13 | 220.00 | 100.0% |\n\"\"\")\n\n\ndef t_unknown_key():\n    r = run(DATA_A, \"--group-by\", \"region,colour\")\n    assert r.returncode == 2, f\"--group-by region,colour: expected exit code 2, got {r.returncode}\"\n    assert \"unknown group key: colour\" in r.stderr, \\\n        f\"--group-by region,colour: stderr should contain 'unknown group key: colour', got {r.stderr.strip()[-200:]!r}\"\n    assert \"Traceback\" not in r.stderr, \"--group-by region,colour: printed a traceback\"\n\n\ndef t_no_records():\n    r = run(DATA_A, \"--since\", \"2030-01-01\", \"--group-by\", \"month\")\n    assert r.returncode == 0, f\"--since 2030-01-01: expected exit code 0, got {r.returncode}: {(r.stderr.strip().splitlines() or [''])[-1][:200]}\"\n    assert r.stdout.strip() == \"no matching records\", \\\n        f\"--since 2030-01-01: expected 'no matching records', got {r.stdout.strip()[:200]!r}\"\n\n\nfor name, fn in [(\"single_key_text\", t_single_key_text), (\"multi_key_text\", t_multi_key_text),\n                 (\"month_bucket\", t_month_bucket), (\"month_with_channel\", t_month_with_channel),\n                 (\"share_half_up\", t_share_half_up), (\"share_thirds\", t_share_thirds),\n                 (\"tie_order\", t_tie_order), (\"date_range_inclusive\", t_date_range_inclusive),\n                 (\"markdown_single\", t_markdown_single), (\"markdown_multi\", t_markdown_multi),\n                 (\"unknown_key\", t_unknown_key), (\"no_records\", t_no_records)]:\n    case(name, fn)\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "python3",
    "-m",
    "compileall",
    "-q",
    "salesreport"
   ],
   "label": "compile",
   "timeout_s": 60
  },
  {
   "cmd": [
    "python3",
    "-B",
    "__checker__/check.py"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 180
  }
 ],
 "tests": [
  "single_key_text",
  "multi_key_text",
  "month_bucket",
  "month_with_channel",
  "share_half_up",
  "share_thirds",
  "tie_order",
  "date_range_inclusive",
  "markdown_single",
  "markdown_multi",
  "unknown_key",
  "no_records"
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
