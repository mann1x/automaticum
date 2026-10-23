const SPEC = {
 "env": {
  "PYTHONDONTWRITEBYTECODE": "1"
 },
 "files": {
  "__checker__/check.py": "import os\nimport shutil\nimport subprocess\n\nROOT = os.getcwd()\nSANDBOX = os.path.join(ROOT, \"__checker__\", \"sandbox\")\nSCRIPT = os.path.join(ROOT, \"migrate.sh\")\n\nMAPPING = \"\\n\".join([\n    \"# section.key\\tnew.key\\ttype\",\n    \"server.host\\thttp.bind\\tstr\",\n    \"server.port\\thttp.port\\tint\",\n    \"server.debug\\thttp.debug\\tbool\",\n    \"db.dsn\\tdatabase.dsn\\tstr\",\n    \"db.retries\\tdatabase.retries\\tint\",\n    \"\",\n    \"global.name\\tservice.name\\tstr\",\n    \"global.owner\\tservice.owner\\tstr\",\n    \"app.level\\tapp.log_level\\tstr\",\n    \"app.path\\tapp.path\\tstr\",\n    \"app.flag\\tapp.flag\\tbool\",\n    \"app.flag2\\tapp.flag2\\tbool\",\n    \"app.flag3\\tapp.flag3\\tbool\",\n    \"app.flag4\\tapp.flag4\\tbool\",\n    \"app.offset\\tapp.offset\\tint\",\n]) + \"\\n\"\n\n\ndef case(name, fn):\n    try:\n        fn()\n        print(f\"TEST {name} PASS\")\n    except AssertionError as e:\n        print(f\"TEST {name} FAIL {e}\")\n    except subprocess.TimeoutExpired:\n        print(f\"TEST {name} FAIL migrate.sh did not finish within 20s\")\n    except Exception as e:\n        print(f\"TEST {name} FAIL {type(e).__name__}: {e}\")\n\n\nclass Run:\n    def __init__(self, files, args=None, pre=None):\n        self.dir = os.path.join(SANDBOX, \"run\")\n        shutil.rmtree(self.dir, ignore_errors=True)\n        os.makedirs(os.path.join(self.dir, \"old\"))\n        with open(os.path.join(self.dir, \"map.tsv\"), \"w\") as fh:\n            fh.write(MAPPING)\n        for name, text in files.items():\n            p = os.path.join(self.dir, \"old\", name)\n            os.makedirs(os.path.dirname(p), exist_ok=True)\n            with open(p, \"w\") as fh:\n                fh.write(text)\n        for name, text in (pre or {}).items():\n            p = os.path.join(self.dir, \"new\", name)\n            os.makedirs(os.path.dirname(p), exist_ok=True)\n            with open(p, \"w\") as fh:\n                fh.write(text)\n        args = [\"map.tsv\", \"old\", \"new\"] if args is None else args\n        r = subprocess.run([\"bash\", SCRIPT, *args], cwd=self.dir, capture_output=True, text=True, timeout=20)\n        self.rc, self.out, self.err = r.returncode, r.stdout, r.stderr\n\n    def conf(self, name):\n        p = os.path.join(self.dir, \"new\", name)\n        return open(p).read() if os.path.exists(p) else None\n\n    def exists(self, name):\n        return os.path.exists(os.path.join(self.dir, \"new\", name))\n\n\ndef same(got, want, what):\n    g = got.rstrip(\"\\n\").split(\"\\n\") if got and got.strip() else []\n    w = want.strip(\"\\n\").split(\"\\n\")\n    for i in range(max(len(g), len(w))):\n        gl = g[i] if i < len(g) else \"<missing>\"\n        wl = w[i] if i < len(w) else \"<nothing more>\"\n        assert gl == wl, f\"{what}: line {i + 1}: expected {wl!r}, got {gl!r}\"\n\n\ndef ok_run(files, what):\n    r = Run(files)\n    assert r.rc == 0, f\"{what}: exit status {r.rc}, expected 0 (stdout: {r.out.strip()[-200:]!r})\"\n    return r\n\n\ndef conf_of(r, name, what):\n    c = r.conf(name)\n    assert c is not None, f\"{what}: {name} was not written\"\n    return c\n\n\ndef t_basic_layout():\n    r = ok_run({\"web.ini\": \"name = web\\n[server]\\nport = 80\\nhost = 10.0.0.5\\n\"}, \"a simple config\")\n    same(conf_of(r, \"web.conf\", \"simple config\"),\n         \"# migrated from web.ini\\nhttp.bind=10.0.0.5\\nhttp.port=80\\nservice.name=web\", \"web.conf\")\n    same(r.out, \"migrated 1 of 1 files, 0 errors, 0 warnings\", \"report\")\n\n\ndef t_comments_and_blanks():\n    text = \"; header comment\\n# another\\n\\n   ; indented\\n\\t# tabbed\\n[server]\\n  ;port = 1\\nport = 81\\n   \\n\"\n    r = ok_run({\"c.ini\": text}, \"comments\")\n    same(conf_of(r, \"c.conf\", \"comments\"), \"# migrated from c.ini\\nhttp.port=81\", \"c.conf\")\n\n\ndef t_inline_comments():\n    text = (\"[server]\\nport = 8080 ; behind the LB\\nhost = db#primary\\n\"\n            \"[app]\\nlevel = info\\t# tab before the hash\\npath = ; nothing set yet\\nflag = on;off\\n\")\n    r = Run({\"i.ini\": text})\n    assert r.rc == 1, f\"inline comments: exit status {r.rc}, expected 1 ('on;off' is not a boolean)\"\n    same(r.out, \"i.ini:7: error: app.flag: expected a boolean, got 'on;off'\\n\"\n         \"migrated 0 of 1 files, 1 errors, 0 warnings\", \"report\")\n    r = ok_run({\"i.ini\": text.replace(\"flag = on;off\\n\", \"\")}, \"inline comments\")\n    same(conf_of(r, \"i.conf\", \"inline comments\"),\n         \"# migrated from i.ini\\napp.log_level=info\\napp.path=\\nhttp.bind=db#primary\\nhttp.port=8080\", \"i.conf\")\n\n\ndef t_quoted_values():\n    text = ('[db]\\ndsn = \"host=db port=5432 ; sslmode=require # primary\"\\n'\n            \"[app]\\nlevel = 'debug'\\npath = \\\"  /srv/app data  \\\" ; trailing comment\\n\"\n            'name_unused = \"#\"\\n')\n    r = Run({\"q.ini\": text})\n    assert r.rc == 0, f\"quoted values: exit status {r.rc}, expected 0 (stdout: {r.out.strip()[-200:]!r})\"\n    same(conf_of(r, \"q.conf\", \"quoted values\"),\n         \"# migrated from q.ini\\napp.log_level='debug'\\napp.path=  /srv/app data  \\n\"\n         \"database.dsn=host=db port=5432 ; sslmode=require # primary\\nlegacy.app.name_unused=#\", \"q.conf\")\n\n\ndef t_duplicate_keys():\n    text = \"[server]\\nport = 80\\nhost = a\\nPort = 8080\\n[db]\\nretries = 3\\n[server]\\nhost = b\\n\"\n    r = ok_run({\"d.ini\": text}, \"duplicate keys\")\n    same(conf_of(r, \"d.conf\", \"duplicate keys\"),\n         \"# migrated from d.ini\\ndatabase.retries=3\\nhttp.bind=b\\nhttp.port=8080\", \"d.conf\")\n    same(r.out, \"d.ini:4: warning: duplicate key server.port, earlier value replaced\\n\"\n         \"d.ini:8: warning: duplicate key server.host, earlier value replaced\\n\"\n         \"migrated 1 of 1 files, 0 errors, 2 warnings\", \"report\")\n\n\ndef t_case_and_global():\n    text = \"Name = Billing API\\nOWNER=ops@corp.example\\n[ SERVER ]\\nHost=0.0.0.0\\n\"\n    r = ok_run({\"g.ini\": text}, \"case and global keys\")\n    same(conf_of(r, \"g.conf\", \"case and global keys\"),\n         \"# migrated from g.ini\\nhttp.bind=0.0.0.0\\nservice.name=Billing API\\nservice.owner=ops@corp.example\", \"g.conf\")\n\n\ndef t_unmapped_keys():\n    text = \"[server]\\nport = 80\\nthreads = 8\\n[tls]\\ncert = /etc/tls/a.pem\\n[Server]\\nzone = eu\\n\"\n    r = ok_run({\"u.ini\": text}, \"unmapped keys\")\n    same(conf_of(r, \"u.conf\", \"unmapped keys\"),\n         \"# migrated from u.ini\\nhttp.port=80\\nlegacy.server.threads=8\\nlegacy.server.zone=eu\\n\"\n         \"legacy.tls.cert=/etc/tls/a.pem\", \"u.conf\")\n    same(r.out, \"u.ini:3: warning: unmapped key server.threads, kept as legacy.server.threads\\n\"\n         \"u.ini:5: warning: unmapped key tls.cert, kept as legacy.tls.cert\\n\"\n         \"u.ini:7: warning: unmapped key server.zone, kept as legacy.server.zone\\n\"\n         \"migrated 1 of 1 files, 0 errors, 3 warnings\", \"report\")\n\n\ndef t_bool_values():\n    text = \"[server]\\ndebug = Yes\\n[app]\\nflag = OFF\\nflag2 = 1\\nflag3 = False\\nflag4 = On\\n\"\n    r = ok_run({\"b.ini\": text}, \"booleans\")\n    same(conf_of(r, \"b.conf\", \"booleans\"),\n         \"# migrated from b.ini\\napp.flag=false\\napp.flag2=true\\napp.flag3=false\\napp.flag4=true\\nhttp.debug=true\",\n         \"b.conf\")\n    r = Run({\"b.ini\": \"[server]\\ndebug = maybe\\n\"})\n    assert r.rc == 1, f\"debug = maybe: exit status {r.rc}, expected 1\"\n    same(r.out, \"b.ini:2: error: server.debug: expected a boolean, got 'maybe'\\n\"\n         \"migrated 0 of 1 files, 1 errors, 0 warnings\", \"report\")\n\n\ndef t_int_values():\n    r = ok_run({\"n.ini\": \"[app]\\noffset = -15\\n[db]\\nretries = 007\\n\"}, \"integers\")\n    same(conf_of(r, \"n.conf\", \"integers\"), \"# migrated from n.ini\\napp.offset=-15\\ndatabase.retries=007\", \"n.conf\")\n    r = Run({\"n.ini\": \"[server]\\nport = 80a\\n[db]\\nretries = 2.5\\n\"})\n    assert r.rc == 1, f\"bad integers: exit status {r.rc}, expected 1\"\n    same(r.out, \"n.ini:2: error: server.port: expected an integer, got '80a'\\n\"\n         \"n.ini:4: error: db.retries: expected an integer, got '2.5'\\n\"\n         \"migrated 0 of 1 files, 2 errors, 0 warnings\", \"report\")\n    assert not r.exists(\"n.conf\"), \"n.conf was written although n.ini has errors\"\n\n\ndef t_parse_errors():\n    text = '[server]\\nport 80\\n= nothing\\nhost = \"db-1\\n[app]\\nlevel = warn\\nlevel = info\\nextra = 1\\n'\n    r = Run({\"p.ini\": text})\n    assert r.rc == 1, f\"parse errors: exit status {r.rc}, expected 1\"\n    same(r.out, \"p.ini:2: error: cannot parse line\\np.ini:3: error: cannot parse line\\n\"\n         \"p.ini:4: error: unterminated quote\\n\"\n         \"p.ini:7: warning: duplicate key app.level, earlier value replaced\\n\"\n         \"p.ini:8: warning: unmapped key app.extra, kept as legacy.app.extra\\n\"\n         \"migrated 0 of 1 files, 3 errors, 2 warnings\", \"report\")\n\n\ndef t_error_file_not_deployed():\n    r = Run({\"bad.ini\": \"[server]\\nport = eighty\\n\", \"good.ini\": \"[server]\\nport = 80\\n\"},\n            pre={\"bad.conf\": \"# migrated from bad.ini\\nhttp.port=79\\n\"})\n    assert r.rc == 1, f\"one broken file: exit status {r.rc}, expected 1\"\n    assert not r.exists(\"bad.conf\"), \"bad.conf from an earlier run is still there although bad.ini has an error\"\n    same(conf_of(r, \"good.conf\", \"good file next to a broken one\"), \"# migrated from good.ini\\nhttp.port=80\", \"good.conf\")\n    same(r.out.strip().split(\"\\n\")[-1], \"migrated 1 of 2 files, 1 errors, 0 warnings\", \"summary line\")\n\n\ndef t_file_selection():\n    files = {\"zeta.ini\": \"[server]\\nport = 1\\nport = 2\\n\", \"alpha svc.ini\": \"[server]\\nport = 3\\nfoo = 1\\n\",\n             \"Beta.ini\": \"name = B\\n\", \"notes.ini.bak\": \"[server]\\nport = x\\n\", \"sub/inner.ini\": \"[server]\\nport = y\\n\",\n             \"readme.txt\": \"not a config\\n\"}\n    r = ok_run(files, \"a directory of configs\")\n    same(r.out, \"alpha svc.ini:3: warning: unmapped key server.foo, kept as legacy.server.foo\\n\"\n         \"zeta.ini:3: warning: duplicate key server.port, earlier value replaced\\n\"\n         \"migrated 3 of 3 files, 0 errors, 2 warnings\", \"report\")\n    same(conf_of(r, \"alpha svc.conf\", \"file name with a space\"),\n         \"# migrated from alpha svc.ini\\nhttp.port=3\\nlegacy.server.foo=1\", \"alpha svc.conf\")\n    assert r.exists(\"Beta.conf\") and r.exists(\"zeta.conf\"), \"Beta.conf / zeta.conf not written\"\n    assert not r.exists(\"inner.conf\") and not r.exists(\"sub/inner.conf\"), \"a config in a subdirectory was migrated\"\n    assert not r.exists(\"notes.ini.conf\") and not r.exists(\"notes.conf\"), \"notes.ini.bak was migrated\"\n\n\ndef t_usage():\n    r = Run({\"a.ini\": \"name = a\\n\"}, args=[\"map.tsv\", \"old\"])\n    assert r.rc == 2, f\"two arguments: exit status {r.rc}, expected 2\"\n    r = Run({\"a.ini\": \"name = a\\n\"}, args=[\"map.tsv\", \"missing-dir\", \"new\"])\n    assert r.rc == 2, f\"missing SRC_DIR: exit status {r.rc}, expected 2\"\n    r = Run({\"a.ini\": \"name = a\\n\"}, args=[\"nomap.tsv\", \"old\", \"new\"])\n    assert r.rc == 2, f\"unreadable mapping: exit status {r.rc}, expected 2\"\n\n\nTESTS = [(\"basic_layout\", t_basic_layout), (\"comments_and_blanks\", t_comments_and_blanks),\n         (\"inline_comments\", t_inline_comments), (\"quoted_values\", t_quoted_values),\n         (\"duplicate_keys\", t_duplicate_keys), (\"case_and_global\", t_case_and_global),\n         (\"unmapped_keys\", t_unmapped_keys), (\"bool_values\", t_bool_values), (\"int_values\", t_int_values),\n         (\"parse_errors\", t_parse_errors), (\"error_file_not_deployed\", t_error_file_not_deployed),\n         (\"file_selection\", t_file_selection), (\"usage\", t_usage)]\n\nfor name, fn in TESTS:\n    case(name, fn)\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "bash",
    "-n",
    "migrate.sh"
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
  "basic_layout",
  "comments_and_blanks",
  "inline_comments",
  "quoted_values",
  "duplicate_keys",
  "case_and_global",
  "unmapped_keys",
  "bool_values",
  "int_values",
  "parse_errors",
  "error_file_not_deployed",
  "file_selection",
  "usage"
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
