const SPEC = {
 "env": {
  "NODE_NO_WARNINGS": "1",
  "TZ": "America/New_York"
 },
 "files": {
  "__checker__/check.ts": "import { BookingError, Calendar, type WeeklyRule, formatInstant } from \"../src/index.ts\";\n\nfunction fail(msg: string): never {\n\tthrow new Error(msg);\n}\n\nfunction eq(got: unknown, want: unknown, what: string): void {\n\tconst g = JSON.stringify(got);\n\tconst w = JSON.stringify(want);\n\tif (g !== w) fail(`${what}: expected ${w}, got ${g}`);\n}\n\nfunction expectCode(fn: () => unknown, code: string, what: string): void {\n\tlet caught: unknown;\n\ttry {\n\t\tfn();\n\t} catch (e) {\n\t\tcaught = e;\n\t}\n\tif (caught === undefined) fail(`${what}: expected BookingError ${code}, nothing was thrown`);\n\tif (!(caught instanceof BookingError)) {\n\t\tfail(`${what}: expected BookingError ${code}, got ${(caught as Error)?.name ?? typeof caught}: ${(caught as Error)?.message ?? caught}`);\n\t}\n\tif (caught.code !== code) fail(`${what}: expected code ${code}, got ${caught.code} (${caught.message})`);\n}\n\nfunction noThrow<T>(fn: () => T, what: string): T {\n\ttry {\n\t\treturn fn();\n\t} catch (e) {\n\t\tfail(`${what}: unexpected ${(e as Error)?.name}: ${(e as Error)?.message}`);\n\t}\n}\n\nconst starts = (list: { start: number }[]) => list.map((b) => formatInstant(b.start));\n\nfunction case_(name: string, fn: () => void): void {\n\ttry {\n\t\tfn();\n\t\tconsole.log(`TEST ${name} PASS`);\n\t} catch (e) {\n\t\tconst msg = (e as Error)?.message ?? String(e);\n\t\tconsole.log(`TEST ${name} FAIL ${msg.split(\"\\n\")[0]}`);\n\t}\n}\n\nconst tests: [string, () => void][] = [\n\t[\"series_until_inclusive\", () => {\n\t\tconst rule: WeeklyRule = { start: \"2024-04-01T09:00:00Z\", durationMin: 60, until: \"2024-04-22\" };\n\t\tconst cal = new Calendar();\n\t\tconst made = noThrow(() => cal.bookSeries(\"oak\", rule, \"standup\"), \"bookSeries until 2024-04-22\");\n\t\teq(starts(made), [\"2024-04-01T09:00:00Z\", \"2024-04-08T09:00:00Z\", \"2024-04-15T09:00:00Z\", \"2024-04-22T09:00:00Z\"],\n\t\t\t\"weekly from 2024-04-01T09:00Z until 2024-04-22\");\n\t\tconst cal2 = new Calendar();\n\t\tconst fewer = noThrow(() => cal2.bookSeries(\"oak\", { ...rule, until: \"2024-04-21\" }, \"standup\"), \"bookSeries until 2024-04-21\");\n\t\teq(starts(fewer), [\"2024-04-01T09:00:00Z\", \"2024-04-08T09:00:00Z\", \"2024-04-15T09:00:00Z\"],\n\t\t\t\"weekly from 2024-04-01T09:00Z until 2024-04-21\");\n\t}],\n\t[\"series_skip_utc\", () => {\n\t\tconst cal = new Calendar();\n\t\tconst made = noThrow(() => cal.bookSeries(\"elm\", {\n\t\t\tstart: \"2024-03-05T01:30:00Z\", durationMin: 30, until: \"2024-03-26\", skip: [\"2024-03-12\"],\n\t\t}, \"late sync\"), \"bookSeries with skip\");\n\t\teq(starts(made), [\"2024-03-05T01:30:00Z\", \"2024-03-19T01:30:00Z\", \"2024-03-26T01:30:00Z\"],\n\t\t\t\"weekly 01:30Z from 2024-03-05 until 2024-03-26 skipping 2024-03-12\");\n\t}],\n\t[\"agenda_utc_day\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-03-10T02:00:00Z\", \"2024-03-10T03:00:00Z\", \"night ops\");\n\t\tcal.book(\"oak\", \"2024-03-10T23:30:00Z\", \"2024-03-11T00:30:00Z\", \"late call\");\n\t\teq(cal.agenda(\"oak\", \"2024-03-10\").map((b) => b.who), [\"night ops\", \"late call\"], \"agenda(oak, 2024-03-10)\");\n\t\teq(cal.agenda(\"oak\", \"2024-03-09\").map((b) => b.who), [], \"agenda(oak, 2024-03-09)\");\n\t}],\n\t[\"agenda_offset_input\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-03-11T00:30:00+02:00\", \"2024-03-11T01:30:00+02:00\", \"berlin\");\n\t\teq(cal.agenda(\"oak\", \"2024-03-10\").map((b) => b.who), [\"berlin\"], \"agenda(oak, 2024-03-10) for a 00:30+02:00 booking\");\n\t\teq(cal.agenda(\"oak\", \"2024-03-11\").map((b) => b.who), [], \"agenda(oak, 2024-03-11) for a 00:30+02:00 booking\");\n\t}],\n\t[\"back_to_back\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"ana\");\n\t\tnoThrow(() => cal.book(\"oak\", \"2024-03-11T11:00:00Z\", \"2024-03-11T12:00:00Z\", \"ben\"), \"booking 11:00-12:00 after 10:00-11:00\");\n\t\tnoThrow(() => cal.book(\"oak\", \"2024-03-11T09:00:00Z\", \"2024-03-11T10:00:00Z\", \"cy\"), \"booking 09:00-10:00 before 10:00-11:00\");\n\t\teq(cal.all().length, 3, \"number of bookings\");\n\t}],\n\t[\"overlap_detected\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"ana\");\n\t\tfor (const [s, e] of [[\"10:30\", \"11:30\"], [\"09:30\", \"10:01\"], [\"10:15\", \"10:45\"], [\"09:00\", \"12:00\"], [\"10:00\", \"11:00\"], [\"10:59\", \"11:00\"]]) {\n\t\t\texpectCode(() => cal.book(\"oak\", `2024-03-11T${s}:00Z`, `2024-03-11T${e}:00Z`, \"x\"), \"CONFLICT\", `booking ${s}-${e} over 10:00-11:00`);\n\t\t}\n\t\teq(cal.all().length, 1, \"number of bookings after rejected clashes\");\n\t}],\n\t[\"other_room\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"ana\");\n\t\tnoThrow(() => cal.book(\"elm\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"ben\"), \"same slot in another room\");\n\t}],\n\t[\"series_all_or_nothing\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-04-15T09:30:00Z\", \"2024-04-15T10:30:00Z\", \"review\");\n\t\texpectCode(() => cal.bookSeries(\"oak\", { start: \"2024-04-01T09:00:00Z\", durationMin: 60, until: \"2024-04-29\" }, \"standup\"),\n\t\t\t\"CONFLICT\", \"series whose third occurrence clashes\");\n\t\teq(cal.all().length, 1, \"bookings after a rejected series\");\n\t}],\n\t[\"series_adjacent_ok\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-04-08T10:00:00Z\", \"2024-04-08T11:00:00Z\", \"review\");\n\t\tconst made = noThrow(() => cal.bookSeries(\"oak\", { start: \"2024-04-01T09:00:00Z\", durationMin: 60, until: \"2024-04-15\" }, \"standup\"),\n\t\t\t\"series ending at 10:00 next to a 10:00 booking\");\n\t\teq(made.length, 3, \"occurrences booked\");\n\t}],\n\t[\"cancel_unknown\", () => {\n\t\tconst cal = new Calendar();\n\t\tcal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"ana\");\n\t\tcal.book(\"oak\", \"2024-03-12T10:00:00Z\", \"2024-03-12T11:00:00Z\", \"ben\");\n\t\texpectCode(() => cal.cancel(\"b99\"), \"NOT_FOUND\", \"cancel(\\\"b99\\\")\");\n\t\teq(cal.all().map((b) => b.who), [\"ana\", \"ben\"], \"bookings after cancel(\\\"b99\\\")\");\n\t}],\n\t[\"cancel_then_rebook\", () => {\n\t\tconst cal = new Calendar();\n\t\tconst a = cal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"ana\");\n\t\tcal.book(\"oak\", \"2024-03-12T10:00:00Z\", \"2024-03-12T11:00:00Z\", \"ben\");\n\t\tconst removed = noThrow(() => cal.cancel(a.id), \"cancel of an existing booking\");\n\t\teq(removed?.who, \"ana\", \"cancel() return value .who\");\n\t\teq(cal.all().map((b) => b.who), [\"ben\"], \"bookings after cancel\");\n\t\tnoThrow(() => cal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T11:00:00Z\", \"cy\"), \"rebooking a cancelled slot\");\n\t\texpectCode(() => cal.cancel(a.id), \"NOT_FOUND\", \"cancelling the same id twice\");\n\t\teq(cal.all().length, 2, \"bookings after the second cancel\");\n\t}],\n\t[\"invalid_range\", () => {\n\t\tconst cal = new Calendar();\n\t\texpectCode(() => cal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T10:00:00Z\", \"x\"), \"INVALID_RANGE\", \"zero-length booking\");\n\t\texpectCode(() => cal.book(\"oak\", \"2024-03-11T10:00:00Z\", \"2024-03-11T09:00:00Z\", \"x\"), \"INVALID_RANGE\", \"booking ending before it starts\");\n\t\texpectCode(() => cal.book(\"oak\", \"2024-03-11 10:00\", \"2024-03-11T11:00:00Z\", \"x\"), \"INVALID_TIME\", \"booking without an offset\");\n\t\teq(cal.all().length, 0, \"bookings after rejected ranges\");\n\t}],\n];\n\nfor (const [name, fn] of tests) case_(name, fn);\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "node",
    "__checker__/check.ts"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 120
  }
 ],
 "tests": [
  "series_until_inclusive",
  "series_skip_utc",
  "agenda_utc_day",
  "agenda_offset_input",
  "back_to_back",
  "overlap_detected",
  "other_room",
  "series_all_or_nothing",
  "series_adjacent_ok",
  "cancel_unknown",
  "cancel_then_rebook",
  "invalid_range"
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
