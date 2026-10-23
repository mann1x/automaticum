const SPEC = {
 "env": {
  "PYTHONDONTWRITEBYTECODE": "1"
 },
 "files": {
  "__checker__/check.py": "import sys\nimport traceback\nfrom decimal import Decimal as D\n\nsys.path.insert(0, \".\")\n\n\ndef case(name, fn):\n    try:\n        fn()\n        print(f\"TEST {name} PASS\")\n    except AssertionError as e:\n        print(f\"TEST {name} FAIL {e}\")\n    except Exception as e:\n        frame = traceback.extract_tb(e.__traceback__)[-1]\n        where = \"\" if \"__checker__\" in frame.filename else f\" at {frame.filename.split('/')[-1]}:{frame.lineno}\"\n        print(f\"TEST {name} FAIL {type(e).__name__}: {e}{where}\")\n\n\ndef eq(got, want, what):\n    assert got == want and type(got) is D, f\"{what}: expected Decimal {want}, got {type(got).__name__} {got}\"\n\n\ndef t_round_half_up():\n    from ledger import cents\n    eq(cents(D(\"0.005\")), D(\"0.01\"), \"cents(0.005)\")\n    eq(cents(D(\"2.675\")), D(\"2.68\"), \"cents(2.675)\")\n    eq(cents(D(\"1.125\")), D(\"1.13\"), \"cents(1.125)\")\n\n\ndef t_no_float_error():\n    from ledger import cents\n    eq(cents(D(\"1234567.895\")), D(\"1234567.90\"), \"cents(1234567.895)\")\n\n\ndef t_line_rounding():\n    from ledger import Invoice\n    inv = Invoice()\n    inv.add_line(\"bolts\", 3, \"0.335\")\n    eq(inv.subtotal(), D(\"1.01\"), \"subtotal of 3 x 0.335\")\n\n\ndef t_qty_validation():\n    from ledger import Invoice\n    for bad in (0, -2, 1.5, \"3\", True):\n        try:\n            Invoice().add_line(\"x\", bad, \"1.00\")\n        except ValueError:\n            continue\n        raise AssertionError(f\"add_line with quantity {bad!r} did not raise ValueError\")\n\n\ndef t_discount_before_tax():\n    from ledger import Invoice\n    inv = Invoice(tax_rate=\"20\", percent_off=\"10\")\n    inv.add_line(\"desk\", 1, \"100.00\")\n    s = inv.summary()\n    eq(s[\"taxable\"], D(\"90.00\"), \"taxable\")\n    eq(s[\"tax\"], D(\"18.00\"), \"tax\")\n    eq(s[\"total\"], D(\"108.00\"), \"total\")\n    eq(s[\"discount\"], D(\"10.00\"), \"discount\")\n\n\ndef t_fractional_percent():\n    from ledger import Invoice\n    inv = Invoice(percent_off=\"12.5\")\n    inv.add_line(\"chair\", 2, \"40.00\")\n    eq(inv.summary()[\"discount\"], D(\"10.00\"), \"12.5% of 80.00\")\n\n\ndef t_fixed_after_percent():\n    from ledger import Invoice\n    inv = Invoice(tax_rate=\"10\", percent_off=\"50\", fixed_off=\"5\")\n    inv.add_line(\"lamp\", 1, \"30.00\")\n    s = inv.summary()\n    eq(s[\"taxable\"], D(\"10.00\"), \"taxable (30 - 50% - 5)\")\n    eq(s[\"total\"], D(\"11.00\"), \"total\")\n\n\ndef t_never_negative():\n    from ledger import Invoice\n    inv = Invoice(tax_rate=\"10\", fixed_off=\"50\")\n    inv.add_line(\"pen\", 2, \"1.50\")\n    s = inv.summary()\n    eq(s[\"taxable\"], D(\"0.00\"), \"taxable\")\n    eq(s[\"total\"], D(\"0.00\"), \"total\")\n    eq(s[\"discount\"], D(\"3.00\"), \"discount\")\n\n\ndef t_mixed_invoice():\n    from ledger import Invoice\n    inv = Invoice(tax_rate=\"7.25\", percent_off=\"3\", fixed_off=\"1.10\")\n    inv.add_line(\"a\", 7, \"1.99\")\n    inv.add_line(\"b\", 1, \"0.015\")\n    inv.add_line(\"c\", 12, \"0.125\")\n    s = inv.summary()\n    eq(s[\"subtotal\"], D(\"15.45\"), \"subtotal\")\n    eq(s[\"taxable\"], D(\"13.89\"), \"taxable\")\n    eq(s[\"tax\"], D(\"1.01\"), \"tax\")\n    eq(s[\"total\"], D(\"14.90\"), \"total\")\n\n\nfor name, fn in [(\"round_half_up\", t_round_half_up), (\"no_float_error\", t_no_float_error),\n                 (\"line_rounding\", t_line_rounding), (\"qty_validation\", t_qty_validation),\n                 (\"discount_before_tax\", t_discount_before_tax), (\"fractional_percent\", t_fractional_percent),\n                 (\"fixed_after_percent\", t_fixed_after_percent), (\"never_negative\", t_never_negative),\n                 (\"mixed_invoice\", t_mixed_invoice)]:\n    case(name, fn)\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "python3",
    "-m",
    "compileall",
    "-q",
    "ledger"
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
   "timeout_s": 120
  }
 ],
 "tests": [
  "round_half_up",
  "no_float_error",
  "line_rounding",
  "qty_validation",
  "discount_before_tax",
  "fractional_percent",
  "fixed_after_percent",
  "never_negative",
  "mixed_invoice"
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
