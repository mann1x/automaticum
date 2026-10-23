const SPEC = {
 "env": {
  "CARGO_HOME": "$WORK/.cargo",
  "CARGO_INCREMENTAL": "0",
  "CARGO_NET_OFFLINE": "true",
  "CARGO_TARGET_DIR": "$WORK/target",
  "CARGO_TERM_COLOR": "never",
  "RUSTUP_HOME": "/usr/local/rustup"
 },
 "files": {
  "__checker__/calc.rs": "// Hidden checks for exprcalc. One #[test] runs every named check under catch_unwind and prints the\n// TEST protocol line for each (run with --nocapture).\nuse std::panic::{self, AssertUnwindSafe};\nuse std::sync::Mutex;\n\nuse exprcalc::lexer::{tokenize, Spanned, Token};\nuse exprcalc::{evaluate, Env, Error};\n\nstatic PANIC_MSG: Mutex<String> = Mutex::new(String::new());\n\ntype Check = Result<(), String>;\n\nfn env(pairs: &[(&str, i64)]) -> Env {\n    pairs.iter().map(|(k, v)| (k.to_string(), *v)).collect()\n}\n\nfn expect_with(e: &Env, src: &str, want: Result<i64, Error>) -> Check {\n    let got = evaluate(src, e);\n    if got == want {\n        Ok(())\n    } else {\n        Err(format!(\"evaluate({src:?}): expected {want:?}, got {got:?}\"))\n    }\n}\n\nfn expect(src: &str, want: Result<i64, Error>) -> Check {\n    expect_with(&Env::new(), src, want)\n}\n\nfn literals_and_whitespace() -> Check {\n    expect(\"42\", Ok(42))?;\n    expect(\" \\t1_000_000 +\\n2 \", Ok(1_000_002))?;\n    expect(\"1\\u{3000}+\\u{a0}2\", Ok(3))?;\n    expect(\"9223372036854775807\", Ok(i64::MAX))?;\n    expect(\"0\", Ok(0))\n}\n\nfn precedence() -> Check {\n    expect(\"2 + 3 * 4\", Ok(14))?;\n    expect(\"(2 + 3) * 4\", Ok(20))?;\n    expect(\"10 - 4 - 3\", Ok(3))?;\n    expect(\"100 / 10 / 5\", Ok(2))?;\n    expect(\"7 % 4 * 3\", Ok(9))?;\n    expect(\"2 * 3 ^ 2\", Ok(18))?;\n    expect(\"1 + 2 ^ 2 * 3\", Ok(13))\n}\n\nfn power_associativity() -> Check {\n    expect(\"2 ^ 3 ^ 2\", Ok(512))?;\n    expect(\"(2 ^ 3) ^ 2\", Ok(64))?;\n    expect(\"2 ^ 2 ^ 2 ^ 2\", Ok(65536))?;\n    expect(\"2 ^ 10\", Ok(1024))?;\n    expect(\"0 ^ 0\", Ok(1))?;\n    expect(\"(-1) ^ 3 ^ 2\", Ok(-1))\n}\n\nfn unary_minus() -> Check {\n    expect(\"-2 ^ 2\", Ok(-4))?;\n    expect(\"(-2) ^ 2\", Ok(4))?;\n    expect(\"-3 ^ 3\", Ok(-27))?;\n    expect(\"--3\", Ok(3))?;\n    expect(\"2 * -3\", Ok(-6))?;\n    expect(\"2 ^ -1\", Err(Error::NegativeExponent))?;\n    expect(\"2 ^ --2\", Ok(4))?;\n    expect(\"-(4 - 6)\", Ok(2))?;\n    expect(\"1 - -1\", Ok(2))\n}\n\nfn unicode_identifiers() -> Check {\n    let e = env(&[(\"gr\u00f6\u00dfe\", 21), (\"\u03c0\", 3), (\"\u0441\u0443\u043c\u043c\u0430\", 10), (\"_tmp1\", 7), (\"\u00e9t\u00e92\", 1)]);\n    expect_with(&e, \"gr\u00f6\u00dfe * 2\", Ok(42))?;\n    expect_with(&e, \"\u03c0^2\", Ok(9))?;\n    expect_with(&e, \"\u0441\u0443\u043c\u043c\u0430 - _tmp1 + \u00e9t\u00e92\", Ok(4))?;\n    expect_with(&e, \"max(gr\u00f6\u00dfe, \u03c0)\", Ok(21))?;\n    expect_with(&e, \"gr\u00f6\u00df\", Err(Error::UnknownVariable(\"gr\u00f6\u00df\".into())))\n}\n\nfn token_columns() -> Check {\n    let want = |src: &str, toks: Vec<(Token, usize)>| -> Check {\n        let got = tokenize(src).map_err(|e| format!(\"tokenize({src:?}): unexpected error {e:?}\"))?;\n        let want: Vec<Spanned> = toks.into_iter().map(|(token, column)| Spanned { token, column }).collect();\n        if got == want {\n            Ok(())\n        } else {\n            Err(format!(\"tokenize({src:?}): expected {want:?}, got {got:?}\"))\n        }\n    };\n    want(\"x ^ 2\", vec![(Token::Ident(\"x\".into()), 1), (Token::Caret, 3), (Token::Number(2), 5)])?;\n    want(\"\u00e9+1\", vec![(Token::Ident(\"\u00e9\".into()), 1), (Token::Plus, 2), (Token::Number(1), 3)])?;\n    want(\n        \"gr\u00f6\u00dfe*(1_0)\",\n        vec![\n            (Token::Ident(\"gr\u00f6\u00dfe\".into()), 1),\n            (Token::Star, 6),\n            (Token::LParen, 7),\n            (Token::Number(10), 8),\n            (Token::RParen, 11),\n        ],\n    )\n}\n\nfn error_columns() -> Check {\n    expect(\"1 $ 2\", Err(Error::UnexpectedChar { ch: '$', column: 3 }))?;\n    let e = env(&[(\"gr\u00f6\u00dfe\", 1), (\"\u00fc\", 2)]);\n    expect_with(&e, \"gr\u00f6\u00dfe + #\", Err(Error::UnexpectedChar { ch: '#', column: 9 }))?;\n    expect_with(&e, \"\u00fc \u00fc\", Err(Error::UnexpectedToken { column: 3 }))?;\n    expect(\"\\u{3000}\\u{3000}1 + )\", Err(Error::UnexpectedToken { column: 7 }))?;\n    expect(\"1 + \u20ac\", Err(Error::UnexpectedChar { ch: '\u20ac', column: 5 }))\n}\n\nfn overflow() -> Check {\n    expect(\"9223372036854775807 + 1\", Err(Error::Overflow))?;\n    expect(\"-9223372036854775807 - 2\", Err(Error::Overflow))?;\n    expect(\"9223372036854775807 * 2\", Err(Error::Overflow))?;\n    expect(\"3037000500 * 3037000500\", Err(Error::Overflow))?;\n    expect(\"9223372036854775808\", Err(Error::Overflow))?;\n    expect(\"-(-9223372036854775807 - 1)\", Err(Error::Overflow))?;\n    expect(\"(-9223372036854775807 - 1) / -1\", Err(Error::Overflow))?;\n    expect(\"(-9223372036854775807 - 1) % -1\", Err(Error::Overflow))?;\n    expect(\"2 ^ 63\", Err(Error::Overflow))?;\n    expect(\"2 ^ 4294967296\", Err(Error::Overflow))?;\n    expect(\"abs(-9223372036854775807 - 1)\", Err(Error::Overflow))?;\n    expect(\"-9223372036854775807 - 1\", Ok(i64::MIN))?;\n    expect(\"-2 ^ 63\", Err(Error::Overflow))?;\n    expect(\"(-2) ^ 63\", Ok(i64::MIN))\n}\n\nfn division() -> Check {\n    expect(\"7 / 2\", Ok(3))?;\n    expect(\"-7 / 2\", Ok(-3))?;\n    expect(\"-7 % 2\", Ok(-1))?;\n    expect(\"7 % -2\", Ok(1))?;\n    expect(\"1 / 0\", Err(Error::DivisionByZero))?;\n    expect(\"5 % 0\", Err(Error::DivisionByZero))?;\n    expect(\"5 % (3 - 3)\", Err(Error::DivisionByZero))\n}\n\nfn variables_and_functions() -> Check {\n    let e = env(&[(\"x\", 6), (\"y\", -4)]);\n    expect_with(&e, \"x * y\", Ok(-24))?;\n    expect_with(&e, \"x * z\", Err(Error::UnknownVariable(\"z\".into())))?;\n    expect_with(&e, \"max(1, x, 3)\", Ok(6))?;\n    expect_with(&e, \"min(x, y) + abs(y)\", Ok(0))?;\n    expect_with(&e, \"max(y)\", Ok(-4))?;\n    expect_with(&e, \"abs(1, 2)\", Err(Error::Arity { name: \"abs\".into(), got: 2 }))?;\n    expect_with(&e, \"max()\", Err(Error::Arity { name: \"max\".into(), got: 0 }))?;\n    expect_with(&e, \"foo(1)\", Err(Error::UnknownFunction(\"foo\".into())))?;\n    expect_with(&e, \"abs(-x) ^ 2\", Ok(36))\n}\n\nfn syntax_errors() -> Check {\n    expect(\"\", Err(Error::UnexpectedEnd))?;\n    expect(\"1 +\", Err(Error::UnexpectedEnd))?;\n    expect(\"(1 + 2\", Err(Error::UnexpectedEnd))?;\n    expect(\"1 2\", Err(Error::UnexpectedToken { column: 3 }))?;\n    expect(\"max(1,)\", Err(Error::UnexpectedToken { column: 7 }))?;\n    expect(\"()\", Err(Error::UnexpectedToken { column: 2 }))?;\n    expect(\"2 ^ ^ 3\", Err(Error::UnexpectedToken { column: 5 }))\n}\n\nfn run(name: &str, f: fn() -> Check) {\n    PANIC_MSG.lock().unwrap().clear();\n    let line = match panic::catch_unwind(AssertUnwindSafe(f)) {\n        Ok(Ok(())) => format!(\"TEST {name} PASS\"),\n        Ok(Err(msg)) => format!(\"TEST {name} FAIL {}\", msg.replace('\\n', \" \")),\n        Err(_) => format!(\"TEST {name} FAIL panicked: {}\", PANIC_MSG.lock().unwrap().replace('\\n', \" \")),\n    };\n    println!(\"{line}\");\n}\n\n#[test]\nfn hidden_checks() {\n    panic::set_hook(Box::new(|info| {\n        let msg = if let Some(s) = info.payload().downcast_ref::<&str>() {\n            s.to_string()\n        } else if let Some(s) = info.payload().downcast_ref::<String>() {\n            s.clone()\n        } else {\n            \"unknown panic\".to_string()\n        };\n        let at = match info.location() {\n            Some(l) if !l.file().contains(\"__checker__\") => format!(\" at {}:{}\", l.file(), l.line()),\n            _ => String::new(),\n        };\n        *PANIC_MSG.lock().unwrap() = format!(\"{msg}{at}\");\n    }));\n    run(\"literals_and_whitespace\", literals_and_whitespace);\n    run(\"precedence\", precedence);\n    run(\"power_associativity\", power_associativity);\n    run(\"unary_minus\", unary_minus);\n    run(\"unicode_identifiers\", unicode_identifiers);\n    run(\"token_columns\", token_columns);\n    run(\"error_columns\", error_columns);\n    run(\"overflow\", overflow);\n    run(\"division\", division);\n    run(\"variables_and_functions\", variables_and_functions);\n    run(\"syntax_errors\", syntax_errors);\n}\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "cargo",
    "build",
    "--offline",
    "--quiet"
   ],
   "label": "cargo build",
   "timeout_s": 180
  },
  {
   "cmd": [
    "sh",
    "-c",
    "mkdir -p tests && cp __checker__/calc.rs tests/__checker___calc.rs && out=$(cargo test --offline --no-run --test __checker___calc 2>&1) || { printf '%s\\n' \"$out\" | grep -E '^error|^ *--> src/'; echo 'the hidden checks do not compile against this crate: keep the public API described in README.md'; exit 1; }"
   ],
   "label": "compile checks",
   "timeout_s": 180
  },
  {
   "cmd": [
    "cargo",
    "test",
    "--offline",
    "--quiet",
    "--test",
    "__checker___calc",
    "--",
    "--nocapture",
    "--test-threads=1"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 180
  }
 ],
 "tests": [
  "literals_and_whitespace",
  "precedence",
  "power_associativity",
  "unary_minus",
  "unicode_identifiers",
  "token_columns",
  "error_columns",
  "overflow",
  "division",
  "variables_and_functions",
  "syntax_errors"
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
