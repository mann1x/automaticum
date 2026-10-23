const SPEC = {
 "env": {
  "PYTHONDONTWRITEBYTECODE": "1"
 },
 "files": {
  "__checker__/check.py": "import gzip\nimport os\nimport shutil\nimport subprocess\n\nROOT = os.getcwd()\nSANDBOX = os.path.join(ROOT, \"__checker__\", \"sandbox\")\nSCRIPT = os.path.join(ROOT, \"bin\", \"logsum.sh\")\n\n\ndef case(name, fn):\n    try:\n        fn()\n        print(f\"TEST {name} PASS\")\n    except AssertionError as e:\n        print(f\"TEST {name} FAIL {e}\")\n    except subprocess.TimeoutExpired:\n        print(f\"TEST {name} FAIL logsum.sh did not finish within 20s\")\n    except Exception as e:\n        print(f\"TEST {name} FAIL {type(e).__name__}: {e}\")\n\n\ndef L(ts, req, status: object = 200, ms=50, ip=\"10.0.0.1\", size=\"100\", user=\"-\"):\n    return f'{ip} - {user} [{ts} +0000] \"{req}\" {status} {size} \"-\" \"probe/1.0\" {ms}'\n\n\ndef run(files, args):\n    \"\"\"Write files (name -> list of lines) into a fresh dir, run logsum.sh there.\"\"\"\n    d = os.path.join(SANDBOX, \"run\")\n    shutil.rmtree(d, ignore_errors=True)\n    os.makedirs(d)\n    for name, lines in files.items():\n        data = (\"\\n\".join(lines) + \"\\n\").encode()\n        if name.endswith(\".gz\"):\n            data = gzip.compress(data)\n        with open(os.path.join(d, name), \"wb\") as fh:\n            fh.write(data)\n    r = subprocess.run([\"bash\", SCRIPT, *args], cwd=d, capture_output=True, text=True, timeout=20)\n    return r.returncode, r.stdout, r.stderr\n\n\ndef report(files, args, what):\n    rc, out, err = run(files, args)\n    assert rc == 0, f\"{what}: exit status {rc}, expected 0 (stderr: {err.strip()[:160]!r})\"\n    return out\n\n\ndef same(got, want, what):\n    g = got.rstrip(\"\\n\").split(\"\\n\") if got.strip() else []\n    w = want.strip(\"\\n\").split(\"\\n\")\n    for i in range(max(len(g), len(w))):\n        gl = g[i] if i < len(g) else \"<missing>\"\n        wl = w[i] if i < len(w) else \"<nothing more>\"\n        assert gl == wl, f\"{what}: report line {i + 1}: expected {wl!r}, got {gl!r}\"\n\n\ndef section(out, title):\n    lines = out.rstrip(\"\\n\").split(\"\\n\")\n    start = next((i for i, ln in enumerate(lines) if ln.startswith(f\"== {title}\")), None)\n    assert start is not None, f\"no '== {title}' section in the report\"\n    body = []\n    for ln in lines[start + 1:]:\n        if ln.startswith(\"== \"):\n            break\n        body.append(ln)\n    return \"\\n\".join(body)\n\n\ndef t_summary_counts():\n    log = [\n        L(\"01/Feb/2026:08:00:00\", \"GET / HTTP/1.1\"),\n        \"\",\n        \"   \",\n        \"\\t\",\n        L(\"01/Feb/2026:08:00:01\", \"GET /a HTTP/1.1\"),\n        L(\"01/Feb/2026:08:00:02\", \"GET /b HTTP/1.1\").rsplit(\" \", 1)[0],\n        L(\"01/Fbr/2026:08:00:03\", \"GET /c HTTP/1.1\"),\n        L(\"01/Feb/2026:08:00:04\", \"GET /d HTTP/1.1\", status=\"2xx\"),\n        '10.0.0.9 - - [01/Feb/2026:08:00:05 +0000] \"GET /e HTT',\n        L(\"01/Feb/2026:08:00:06\", \"HEAD /f HTTP/1.0\", size=\"-\"),\n    ]\n    out = report({\"a.log\": log}, [\"a.log\"], \"mixed log\")\n    same(section(out, \"summary\"), \"lines: 7\\nparsed: 3\\nmalformed: 4\", \"summary section\")\n\n\ndef t_query_stripped():\n    log = [\n        L(\"01/Feb/2026:08:00:00\", \"GET /search?q=shoes HTTP/1.1\"),\n        L(\"01/Feb/2026:08:00:01\", \"GET /search?q=hats&page=2 HTTP/1.1\"),\n        L(\"01/Feb/2026:08:00:02\", \"GET /search HTTP/1.1\"),\n        L(\"01/Feb/2026:08:00:03\", \"GET /img/logo.png?v=7 HTTP/1.1\"),\n    ]\n    out = report({\"a.log\": log}, [\"a.log\"], \"query strings\")\n    same(section(out, \"top paths\"), \"3 /search\\n1 /img/logo.png\", \"top paths section\")\n\n\ndef t_top_paths_ties():\n    reqs = [\"/beta\", \"/alpha\", \"/beta\", \"/Zulu\", \"/alpha\", \"/gamma\", \"/delta\", \"/delta\", \"/delta\", \"/a-b\", \"/a\"]\n    log = [L(f\"01/Feb/2026:08:00:{i:02d}\", f\"GET {p} HTTP/1.1\") for i, p in enumerate(reqs)]\n    out = report({\"a.log\": log}, [\"a.log\"], \"tied counts\")\n    same(section(out, \"top paths\"),\n         \"3 /delta\\n2 /alpha\\n2 /beta\\n1 /Zulu\\n1 /a\\n1 /a-b\\n1 /gamma\", \"top paths section\")\n\n\ndef t_top_paths_limit():\n    log = []\n    for i in range(12):\n        for k in range(12 - i):\n            log.append(L(f\"01/Feb/2026:08:{i:02d}:{k:02d}\", f\"GET /p{i:02d} HTTP/1.1\"))\n    out = report({\"a.log\": log}, [\"a.log\"], \"12 paths, default TOP\")\n    want = \"\\n\".join(f\"{12 - i} /p{i:02d}\" for i in range(10))\n    same(section(out, \"top paths\"), want, \"top paths with the default TOP\")\n    out = report({\"a.log\": log}, [\"-n\", \"3\", \"a.log\"], \"-n 3\")\n    same(section(out, \"top paths\"), \"12 /p00\\n11 /p01\\n10 /p02\", \"top paths with -n 3\")\n\n\ndef t_hour_keys():\n    log = [\n        L(\"01/Oct/2025:00:10:00\", \"GET /x HTTP/1.1\"),\n        L(\"30/Sep/2025:23:59:59\", \"GET /x HTTP/1.1\"),\n        L(\"01/Jan/2026:00:00:01\", \"GET /x HTTP/1.1\"),\n        L(\"31/Dec/2025:23:00:00\", \"GET /x HTTP/1.1\", status=503),\n        L(\"05/Oct/2025:09:00:00\", \"GET /x HTTP/1.1\"),\n    ]\n    out = report({\"a.log\": log}, [\"a.log\"], \"hours across months\")\n    same(section(out, \"status by hour\"),\n         \"2025-09-30T23 200=1\\n2025-10-01T00 200=1\\n2025-10-05T09 200=1\\n2025-12-31T23 503=1\\n2026-01-01T00 200=1\",\n         \"status by hour section\")\n\n\ndef t_status_per_hour():\n    log = [\n        L(\"07/Jun/2026:14:00:00\", \"GET /a HTTP/1.1\", status=404),\n        L(\"07/Jun/2026:14:01:00\", \"GET /a HTTP/1.1\", status=200),\n        L(\"07/Jun/2026:14:02:00\", \"GET /a HTTP/1.1\", status=500),\n        L(\"07/Jun/2026:14:03:00\", \"GET /a HTTP/1.1\", status=200),\n        L(\"07/Jun/2026:14:59:59\", \"GET /a HTTP/1.1\", status=304),\n        L(\"07/Jun/2026:15:00:00\", \"GET /a HTTP/1.1\", status=404),\n        L(\"07/Jun/2026:15:30:00\", \"GET /a HTTP/1.1\", status=404),\n    ]\n    out = report({\"a.log\": log}, [\"a.log\"], \"statuses\")\n    same(section(out, \"status by hour\"), \"2026-06-07T14 200=2 304=1 404=1 500=1\\n2026-06-07T15 404=2\",\n         \"status by hour section\")\n\n\ndef t_slow_threshold():\n    log = [\n        L(\"07/Jun/2026:14:00:00\", \"GET /a HTTP/1.1\", ms=499),\n        L(\"07/Jun/2026:14:00:01\", \"POST /b?x=1 HTTP/1.1\", ms=500),\n        L(\"07/Jun/2026:14:00:02\", \"GET /c HTTP/1.1\", ms=12000),\n        L(\"07/Jun/2026:14:00:03\", \"GET /d HTTP/1.1\", ms=1199),\n        L(\"07/Jun/2026:14:00:04\", \"PUT /e HTTP/1.1\", ms=1200),\n    ]\n    out = report({\"a.log\": log}, [\"a.log\"], \"default threshold\")\n    same(out[out.index(\"== slow\"):] if \"== slow\" in out else \"\",\n         \"== slow requests (>= 500 ms) ==\\n2026-06-07T14:00:01 500 POST /b\\n2026-06-07T14:00:02 12000 GET /c\\n\"\n         \"2026-06-07T14:00:03 1199 GET /d\\n2026-06-07T14:00:04 1200 PUT /e\", \"slow section (default)\")\n    out = report({\"a.log\": log}, [\"-t\", \"1200\", \"a.log\"], \"-t 1200\")\n    same(out[out.index(\"== slow\"):] if \"== slow\" in out else \"\",\n         \"== slow requests (>= 1200 ms) ==\\n2026-06-07T14:00:02 12000 GET /c\\n2026-06-07T14:00:04 1200 PUT /e\",\n         \"slow section (-t 1200)\")\n\n\ndef t_bad_request_field():\n    log = [\n        L(\"07/Jun/2026:14:00:00\", \"GET /ok HTTP/1.1\"),\n        L(\"07/Jun/2026:14:00:01\", \"-\", status=408, size=\"0\", ms=30000),\n        L(\"07/Jun/2026:14:00:02\", \"GET\", status=400, ms=1),\n        L(\"07/Jun/2026:14:00:03\", \"get /lower HTTP/1.1\"),\n        L(\"07/Jun/2026:14:00:04\", \"GET nopath HTTP/1.1\", status=400),\n        L(\"07/Jun/2026:14:00:05\", \"GET /x HTTP/1.1 extra\", status=400),\n        L(\"07/Jun/2026:14:00:06\", \"\\\\x16\\\\x03\\\\x01 /x SSH-2.0\", status=400),\n    ]\n    out = report({\"a.log\": log}, [\"a.log\"], \"bad request fields\")\n    same(out, \"\"\"== summary ==\nlines: 7\nparsed: 1\nmalformed: 6\n== top paths ==\n1 /ok\n== status by hour ==\n2026-06-07T14 200=1\n== slow requests (>= 500 ms) ==\"\"\", \"full report\")\n\n\ndef t_gzip_input():\n    old = [L(\"06/Jun/2026:23:10:00\", \"GET /old HTTP/1.1\", ms=900), L(\"06/Jun/2026:23:11:00\", \"GET /old HTTP/1.1\")]\n    new = [L(\"07/Jun/2026:00:01:00\", \"GET /new HTTP/1.1\", ms=700)]\n    out = report({\"access.log.2.gz\": old, \"access.log\": new}, [\"access.log.2.gz\", \"access.log\"], \"a .gz file\")\n    same(out, \"\"\"== summary ==\nlines: 3\nparsed: 3\nmalformed: 0\n== top paths ==\n2 /old\n1 /new\n== status by hour ==\n2026-06-06T23 200=2\n2026-06-07T00 200=1\n== slow requests (>= 500 ms) ==\n2026-06-06T23:10:00 900 GET /old\n2026-06-07T00:01:00 700 GET /new\"\"\", \"report over a .gz and a plain file\")\n\n\ndef t_spaces_in_filename():\n    log = [L(\"07/Jun/2026:10:00:00\", \"GET /spaced HTTP/1.1\", ms=650)]\n    out = report({\"web 01 access.log\": log, \"web 02 access.log.gz\": log}, [\"web 01 access.log\", \"web 02 access.log.gz\"],\n                 \"file names with spaces\")\n    same(section(out, \"summary\"), \"lines: 2\\nparsed: 2\\nmalformed: 0\", \"summary section\")\n    same(section(out, \"top paths\"), \"2 /spaced\", \"top paths section\")\n\n\ndef t_argument_order():\n    a = [L(\"07/Jun/2026:12:00:00\", \"GET /from-a HTTP/1.1\", ms=800)]\n    b = [L(\"07/Jun/2026:11:00:00\", \"GET /from-b HTTP/1.1\", ms=800)]\n    out = report({\"a.log\": a, \"b.log\": b}, [\"a.log\", \"b.log\"], \"two files\")\n    same(section(out, \"slow requests\"), \"2026-06-07T12:00:00 800 GET /from-a\\n2026-06-07T11:00:00 800 GET /from-b\",\n         \"slow section (input order)\")\n    same(section(out, \"status by hour\"), \"2026-06-07T11 200=1\\n2026-06-07T12 200=1\", \"status by hour section\")\n\n\ndef t_empty_input():\n    out = report({\"empty.log\": [\"\", \"  \"]}, [\"empty.log\"], \"a log of blank lines\")\n    same(out, \"\"\"== summary ==\nlines: 0\nparsed: 0\nmalformed: 0\n== top paths ==\n== status by hour ==\n== slow requests (>= 500 ms) ==\"\"\", \"full report\")\n\n\ndef t_exit_codes():\n    rc, out, err = run({\"a.log\": [L(\"07/Jun/2026:12:00:00\", \"GET / HTTP/1.1\")]}, [\"a.log\", \"missing.log\"])\n    assert rc == 1, f\"a missing log file: expected exit 1, got {rc}\"\n    rc, out, err = run({\"a.log\": [L(\"07/Jun/2026:12:00:00\", \"GET / HTTP/1.1\")]}, [])\n    assert rc == 2, f\"no log file given: expected exit 2, got {rc}\"\n\n\nTESTS = [(\"summary_counts\", t_summary_counts), (\"query_stripped\", t_query_stripped),\n         (\"top_paths_ties\", t_top_paths_ties), (\"top_paths_limit\", t_top_paths_limit),\n         (\"hour_keys\", t_hour_keys), (\"status_per_hour\", t_status_per_hour), (\"slow_threshold\", t_slow_threshold),\n         (\"bad_request_field\", t_bad_request_field), (\"gzip_input\", t_gzip_input),\n         (\"spaces_in_filename\", t_spaces_in_filename), (\"argument_order\", t_argument_order),\n         (\"empty_input\", t_empty_input), (\"exit_codes\", t_exit_codes)]\n\nfor name, fn in TESTS:\n    case(name, fn)\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "bash",
    "-n",
    "bin/logsum.sh"
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
   "timeout_s": 180
  }
 ],
 "tests": [
  "summary_counts",
  "query_stripped",
  "top_paths_ties",
  "top_paths_limit",
  "hour_keys",
  "status_per_hour",
  "slow_threshold",
  "bad_request_field",
  "gzip_input",
  "spaces_in_filename",
  "argument_order",
  "empty_input",
  "exit_codes"
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
