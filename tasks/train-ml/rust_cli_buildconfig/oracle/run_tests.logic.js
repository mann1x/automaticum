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
  "__checker__/check.rs": "// Hidden checks for the csvtool workspace: the csvcore library (linked from the workspace build) and\n// the csvtool binary (run as a process). Prints one TEST line per check.\nuse std::io::Write;\nuse std::panic::{self, AssertUnwindSafe};\nuse std::path::{Path, PathBuf};\nuse std::process::{Command, Stdio};\nuse std::sync::Mutex;\n\nuse csvcore::stats::{column_sum, StatsError};\nuse csvcore::{parse_line, write_record, CsvError, LineError, Table};\n\nstatic PANIC_MSG: Mutex<String> = Mutex::new(String::new());\n\ntype Check = Result<(), String>;\n\nfn ensure(cond: bool, msg: impl FnOnce() -> String) -> Check {\n    if cond {\n        Ok(())\n    } else {\n        Err(msg())\n    }\n}\n\nfn work_dir() -> PathBuf {\n    let dir = PathBuf::from(std::env::var(\"TMPDIR\").unwrap_or_else(|_| \".\".into())).join(\"csvchk\");\n    std::fs::create_dir_all(&dir).expect(\"create the check directory\");\n    dir\n}\n\nfn write_file(name: &str, text: &str) -> PathBuf {\n    let p = work_dir().join(name);\n    std::fs::write(&p, text).expect(\"write a check file\");\n    p\n}\n\nfn binary() -> PathBuf {\n    let target = std::env::var(\"CARGO_TARGET_DIR\").unwrap_or_else(|_| \"target\".into());\n    Path::new(&target).join(\"debug\").join(\"csvtool\")\n}\n\nstruct Out {\n    code: Option<i32>,\n    stdout: String,\n    stderr: String,\n}\n\nfn csvtool(args: &[&str], stdin: Option<&str>) -> Result<Out, String> {\n    let bin = binary();\n    if !bin.exists() {\n        return Err(\"`cargo build` did not produce a binary named csvtool (target/debug/csvtool)\".into());\n    }\n    let mut child = Command::new(&bin)\n        .args(args)\n        .stdin(if stdin.is_some() { Stdio::piped() } else { Stdio::null() })\n        .stdout(Stdio::piped())\n        .stderr(Stdio::piped())\n        .spawn()\n        .map_err(|e| format!(\"could not run csvtool: {e}\"))?;\n    if let Some(text) = stdin {\n        child.stdin.take().unwrap().write_all(text.as_bytes()).map_err(|e| e.to_string())?;\n    }\n    let o = child.wait_with_output().map_err(|e| e.to_string())?;\n    Ok(Out {\n        code: o.status.code(),\n        stdout: String::from_utf8_lossy(&o.stdout).into_owned(),\n        stderr: String::from_utf8_lossy(&o.stderr).into_owned(),\n    })\n}\n\nfn show_args(args: &[&str]) -> String {\n    args.iter()\n        .map(|a| if a.contains(\"csvchk\") { Path::new(a).file_name().unwrap().to_string_lossy().into_owned() } else { a.to_string() })\n        .collect::<Vec<_>>()\n        .join(\" \")\n}\n\nfn expect_ok(args: &[&str], stdin: Option<&str>, want: &str) -> Check {\n    let o = csvtool(args, stdin)?;\n    let what = format!(\"csvtool {}\", show_args(args));\n    ensure(o.code == Some(0), || format!(\"{what}: expected exit 0, got {:?} (stderr {:?})\", o.code, o.stderr.trim_end()))?;\n    ensure(o.stdout == want, || format!(\"{what}: expected output {want:?}, got {:?}\", o.stdout))\n}\n\nfn expect_err(args: &[&str], code: i32, stderr_prefix: &str) -> Check {\n    let o = csvtool(args, None)?;\n    let what = format!(\"csvtool {}\", show_args(args));\n    ensure(o.code == Some(code), || format!(\"{what}: expected exit {code}, got {:?} (stderr {:?})\", o.code, o.stderr.trim_end()))?;\n    ensure(o.stderr.starts_with(stderr_prefix), || {\n        format!(\"{what}: expected stderr starting with {stderr_prefix:?}, got {:?}\", o.stderr.trim_end())\n    })?;\n    ensure(o.stdout.is_empty(), || format!(\"{what}: expected nothing on stdout, got {:?}\", o.stdout))\n}\n\nfn fields(v: &[&str]) -> Vec<String> {\n    v.iter().map(|s| s.to_string()).collect()\n}\n\nfn expect_line(line: &str, want: &[&str]) -> Check {\n    let got = parse_line(line);\n    ensure(got == Ok(fields(want)), || format!(\"parse_line({line:?}): expected Ok({want:?}), got {got:?}\"))\n}\n\n// ---------------------------------------------------------------- library\n\nfn lib_parse_line() -> Check {\n    expect_line(\"a,b,c\", &[\"a\", \"b\", \"c\"])?;\n    expect_line(\"a,,b\", &[\"a\", \"\", \"b\"])?;\n    expect_line(\"a,\", &[\"a\", \"\"])?;\n    expect_line(\" x , y \", &[\" x \", \" y \"])?;\n    expect_line(\"\\\"Bolt, M6\\\",3\", &[\"Bolt, M6\", \"3\"])?;\n    expect_line(\"\\\"\\\",x\", &[\"\", \"x\"])?;\n    let e = parse_line(\"\\\"abc,d\");\n    ensure(e == Err(LineError::UnterminatedQuote), || format!(\"parse_line(\\\"\\\\\\\"abc,d\\\"): expected UnterminatedQuote, got {e:?}\"))?;\n    let e = parse_line(\"\\\"a\\\"b,c\");\n    ensure(e == Err(LineError::TextAfterQuote), || format!(\"parse_line(\\\"\\\\\\\"a\\\\\\\"b,c\\\"): expected TextAfterQuote, got {e:?}\"))\n}\n\nfn lib_quote_escape() -> Check {\n    expect_line(\"\\\"say \\\"\\\"hi\\\"\\\"\\\",x\", &[\"say \\\"hi\\\"\", \"x\"])?;\n    expect_line(\"\\\"\\\"\\\"\\\"\", &[\"\\\"\"])?;\n    expect_line(\"a,\\\"\\\"\\\"quoted\\\"\\\"\\\"\", &[\"a\", \"\\\"quoted\\\"\"])?;\n    expect_line(\"\\\"x\\\"\\\"\\\",\\\"\\\"\", &[\"x\\\"\", \"\"])?;\n    let e = parse_line(\"\\\"abc\\\"\\\"\");\n    ensure(e == Err(LineError::UnterminatedQuote), || format!(\"parse_line(\\\"\\\\\\\"abc\\\\\\\"\\\\\\\"\\\"): expected UnterminatedQuote, got {e:?}\"))\n}\n\nfn lib_table() -> Check {\n    let t = Table::parse(\"id,name\\r\\n1,ann\\r\\n\\r\\n  \\n2,\\\"bo, b\\\"\\n\").map_err(|e| format!(\"Table::parse: unexpected error {e:?}\"))?;\n    ensure(t.header == fields(&[\"id\", \"name\"]), || format!(\"header: got {:?}\", t.header))?;\n    ensure(t.rows == vec![fields(&[\"1\", \"ann\"]), fields(&[\"2\", \"bo, b\"])], || format!(\"rows: got {:?}\", t.rows))?;\n    ensure(t.lines == vec![2, 5], || format!(\"line numbers: expected [2, 5], got {:?}\", t.lines))?;\n    ensure(t.column(\"name\") == Some(1) && t.column(\"Name\").is_none(), || \"Table::column lookup is wrong\".into())?;\n    let e = Table::parse(\"a,b\\n1,2\\n\\n1,2,3\\n\");\n    ensure(e == Err(CsvError::FieldCount { line: 4, expected: 2, found: 3 }), || format!(\"ragged row: got {e:?}\"))?;\n    let e = Table::parse(\"a,b\\n\\\"x,1\\n\");\n    ensure(e == Err(CsvError::Line { line: 2, error: LineError::UnterminatedQuote }), || format!(\"bad quote on line 2: got {e:?}\"))?;\n    let e = Table::parse(\"\\n \\n\");\n    ensure(e == Err(CsvError::Empty), || format!(\"blank input: expected Empty, got {e:?}\"))?;\n    let t = Table::parse(\"only,header\\n\").map_err(|e| format!(\"header only: {e:?}\"))?;\n    ensure(t.rows.is_empty(), || \"header only: expected no rows\".into())\n}\n\nfn lib_write_record() -> Check {\n    let cases: Vec<Vec<&str>> = vec![\n        vec![\"a\", \"b\"],\n        vec![\"x,y\", \"z\"],\n        vec![\"say \\\"hi\\\"\", \"\"],\n        vec![\"\\\"\", \",\", \" sp \"],\n        vec![\"\"],\n    ];\n    let w = write_record(&[\"a\", \"b,c\", \"d\\\"e\", \"\"]);\n    ensure(w == \"a,\\\"b,c\\\",\\\"d\\\"\\\"e\\\",\", || format!(\"write_record: got {w:?}\"))?;\n    for c in cases {\n        let line = write_record(&c);\n        let back = parse_line(&line);\n        ensure(back == Ok(fields(&c)), || format!(\"parse_line(write_record({c:?})) = {back:?} (line {line:?})\"))?;\n    }\n    Ok(())\n}\n\nfn lib_column_sum() -> Check {\n    let t = Table::parse(\"item,qty,price\\na,3,1.5\\nb,,2.25\\nc, 4 ,0.25\\nd,  ,-1\\n\").map_err(|e| format!(\"{e:?}\"))?;\n    let q = column_sum(&t, 1);\n    ensure(q == Ok(7.0), || format!(\"column_sum(qty) with empty cells: expected Ok(7.0), got {q:?}\"))?;\n    let p = column_sum(&t, 2);\n    ensure(p == Ok(3.0), || format!(\"column_sum(price): expected Ok(3.0), got {p:?}\"))?;\n    let t = Table::parse(\"n\\n1\\n\\nx 2\\n\").map_err(|e| format!(\"{e:?}\"))?;\n    let e = column_sum(&t, 0);\n    ensure(e == Err(StatsError::NotANumber { line: 4, value: \"x 2\".into() }), || format!(\"non-numeric cell: got {e:?}\"))?;\n    let t = Table::parse(\"n\\n\").map_err(|e| format!(\"{e:?}\"))?;\n    ensure(column_sum(&t, 0) == Ok(0.0), || \"sum of no rows: expected 0\".into())\n}\n\n// ---------------------------------------------------------------- CLI\n\nconst STOCK: &str = \"sku,name,qty,price\\nA-100,\\\"Bolt, M6\\\",120,0.15\\nA-101,Washer,,0.02\\n\\nB-200,\\\"Hex \\\"\\\"Allen\\\"\\\" key\\\",4,3.5\\n\";\n\nfn cli_binary() -> Check {\n    let o = csvtool(&[], None)?;\n    ensure(o.code == Some(2), || format!(\"csvtool with no arguments: expected exit 2, got {:?}\", o.code))?;\n    ensure(o.stderr.starts_with(\"usage:\"), || format!(\"csvtool with no arguments: expected usage on stderr, got {:?}\", o.stderr))\n}\n\nfn cli_count() -> Check {\n    let f = write_file(\"stock.csv\", STOCK);\n    expect_ok(&[\"count\", f.to_str().unwrap()], None, \"3\\n\")?;\n    let f = write_file(\"crlf.csv\", \"a,b\\r\\n1,2\\r\\n3,4\\r\\n\");\n    expect_ok(&[\"count\", f.to_str().unwrap()], None, \"2\\n\")?;\n    let f = write_file(\"header.csv\", \"a,b\\n\");\n    expect_ok(&[\"count\", f.to_str().unwrap()], None, \"0\\n\")?;\n    expect_ok(&[\"count\", \"-\"], Some(\"x\\n1\\n2\\n3\\n4\\n\"), \"4\\n\")\n}\n\nfn cli_columns() -> Check {\n    let f = write_file(\"stock.csv\", STOCK);\n    expect_ok(&[\"columns\", f.to_str().unwrap()], None, \"sku\\nname\\nqty\\nprice\\n\")?;\n    expect_ok(&[\"columns\", \"-\"], Some(\"\\\"a,b\\\",\\\"say \\\"\\\"x\\\"\\\"\\\"\\n1,2\\n\"), \"a,b\\nsay \\\"x\\\"\\n\")\n}\n\nfn cli_sum() -> Check {\n    let f = write_file(\"stock.csv\", STOCK);\n    let p = f.to_str().unwrap();\n    expect_ok(&[\"sum\", p, \"qty\"], None, \"124\\n\")?;\n    expect_ok(&[\"sum\", p, \"price\"], None, \"3.67\\n\")?;\n    expect_ok(&[\"sum\", \"-\", \"v\"], Some(\"v\\n0.1\\n0.2\\n\"), \"0.30000000000000004\\n\")?;\n    expect_ok(&[\"sum\", \"-\", \"v\"], Some(\"v\\n 2.5 \\n\\n-4\\n  \\n\"), \"-1.5\\n\")\n}\n\nfn cli_sum_errors() -> Check {\n    let f = write_file(\"bad.csv\", \"item,qty\\na,3\\nb,lots\\n\");\n    let p = f.to_str().unwrap();\n    expect_err(&[\"sum\", p, \"qty\"], 1, \"csvtool: line 3: not a number: \\\"lots\\\"\")?;\n    expect_err(&[\"sum\", p, \"nope\"], 1, \"csvtool: no column named \\\"nope\\\"\")?;\n    let f = write_file(\"stock.csv\", STOCK);\n    expect_err(&[\"sum\", f.to_str().unwrap(), \"name\"], 1, \"csvtool: line 2: not a number: \\\"Bolt, M6\\\"\")\n}\n\nfn cli_select() -> Check {\n    let f = write_file(\"stock.csv\", STOCK);\n    let p = f.to_str().unwrap();\n    expect_ok(\n        &[\"select\", p, \"name,sku\"],\n        None,\n        \"name,sku\\n\\\"Bolt, M6\\\",A-100\\nWasher,A-101\\n\\\"Hex \\\"\\\"Allen\\\"\\\" key\\\",B-200\\n\",\n    )?;\n    expect_ok(&[\"select\", p, \"qty\"], None, \"qty\\n120\\n\\n4\\n\")?;\n    expect_ok(&[\"select\", p, \"sku,sku\"], None, \"sku,sku\\nA-100,A-100\\nA-101,A-101\\nB-200,B-200\\n\")?;\n    expect_err(&[\"select\", p, \"sku,colour\"], 1, \"csvtool: no column named \\\"colour\\\"\")\n}\n\nfn cli_errors() -> Check {\n    let missing = work_dir().join(\"missing.csv\");\n    expect_err(&[\"count\", missing.to_str().unwrap()], 1, \"csvtool: cannot read \")?;\n    let f = write_file(\"ragged.csv\", \"a,b\\n1,2\\n1,2,3\\n\");\n    expect_err(&[\"count\", f.to_str().unwrap()], 1, \"csvtool: line 3: expected 2 fields, found 3\")?;\n    let f = write_file(\"quote.csv\", \"a,b\\n\\\"x,1\\n\");\n    expect_err(&[\"columns\", f.to_str().unwrap()], 1, \"csvtool: line 2: unterminated quoted field\")?;\n    let f = write_file(\"after.csv\", \"a,b\\n\\\"x\\\"y,1\\n\");\n    expect_err(&[\"count\", f.to_str().unwrap()], 1, \"csvtool: line 2: unexpected text after a closing quote\")?;\n    let f = write_file(\"empty.csv\", \"\");\n    expect_err(&[\"count\", f.to_str().unwrap()], 1, \"csvtool: empty input (no header line)\")?;\n    let f = write_file(\"stock.csv\", STOCK);\n    expect_err(&[\"avg\", f.to_str().unwrap(), \"qty\"], 2, \"usage:\")?;\n    expect_err(&[\"count\"], 2, \"usage:\")?;\n    expect_err(&[\"count\", f.to_str().unwrap(), \"extra\"], 2, \"usage:\")\n}\n\n// ---------------------------------------------------------------- harness\n\nfn run(name: &str, f: fn() -> Check) {\n    PANIC_MSG.lock().unwrap().clear();\n    let line = match panic::catch_unwind(AssertUnwindSafe(f)) {\n        Ok(Ok(())) => format!(\"TEST {name} PASS\"),\n        Ok(Err(msg)) => format!(\"TEST {name} FAIL {}\", msg.replace('\\n', \" \")),\n        Err(_) => format!(\"TEST {name} FAIL panicked: {}\", PANIC_MSG.lock().unwrap().replace('\\n', \" \")),\n    };\n    println!(\"{line}\");\n}\n\nfn main() {\n    panic::set_hook(Box::new(|info| {\n        let msg = if let Some(s) = info.payload().downcast_ref::<&str>() {\n            s.to_string()\n        } else if let Some(s) = info.payload().downcast_ref::<String>() {\n            s.clone()\n        } else {\n            \"unknown panic\".to_string()\n        };\n        let at = match info.location() {\n            Some(l) if !l.file().contains(\"__checker__\") => format!(\" at {}:{}\", l.file(), l.line()),\n            _ => String::new(),\n        };\n        *PANIC_MSG.lock().unwrap() = format!(\"{msg}{at}\");\n    }));\n    run(\"lib_parse_line\", lib_parse_line);\n    run(\"lib_quote_escape\", lib_quote_escape);\n    run(\"lib_table\", lib_table);\n    run(\"lib_write_record\", lib_write_record);\n    run(\"lib_column_sum\", lib_column_sum);\n    run(\"cli_binary\", cli_binary);\n    run(\"cli_count\", cli_count);\n    run(\"cli_columns\", cli_columns);\n    run(\"cli_sum\", cli_sum);\n    run(\"cli_sum_errors\", cli_sum_errors);\n    run(\"cli_select\", cli_select);\n    run(\"cli_errors\", cli_errors);\n}\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "cargo",
    "build",
    "--offline"
   ],
   "label": "cargo build",
   "timeout_s": 240
  },
  {
   "cmd": [
    "sh",
    "-c",
    "T=\"$CARGO_TARGET_DIR/debug\"; [ -f \"$T/libcsvcore.rlib\" ] || { echo 'the workspace build did not produce the csvcore library (libcsvcore.rlib)'; exit 1; }; out=$(rustc --edition 2021 --crate-name chk --crate-type bin --extern csvcore=\"$T/libcsvcore.rlib\" -L \"dependency=$T/deps\" -o chk __checker__/check.rs 2>&1) || { printf '%s\\n' \"$out\" | grep -E '^error'; echo 'the hidden checks do not compile against csvcore as built by the workspace: keep the library API and the stats module described in README.md'; exit 1; }"
   ],
   "label": "compile checks",
   "timeout_s": 240
  },
  {
   "cmd": [
    "./chk"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 120
  }
 ],
 "tests": [
  "lib_parse_line",
  "lib_quote_escape",
  "lib_table",
  "lib_write_record",
  "lib_column_sum",
  "cli_binary",
  "cli_count",
  "cli_columns",
  "cli_sum",
  "cli_sum_errors",
  "cli_select",
  "cli_errors"
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
