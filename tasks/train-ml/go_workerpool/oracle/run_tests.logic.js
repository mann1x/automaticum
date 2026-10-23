const SPEC = {
 "env": {
  "CGO_ENABLED": "0",
  "GOCACHE": "$WORK/.gocache",
  "GOFLAGS": "-mod=mod",
  "GOPATH": "$WORK/.gopath",
  "GOPROXY": "off",
  "GOSUMDB": "off",
  "GOTMPDIR": "$WORK",
  "GOTOOLCHAIN": "local",
  "HOME": "$WORK",
  "TMPDIR": "/tmp"
 },
 "files": {
  "__checker__/main.go": "package main\n\nimport (\n\t\"context\"\n\t\"errors\"\n\t\"fmt\"\n\t\"runtime\"\n\t\"strings\"\n\t\"sync\"\n\t\"sync/atomic\"\n\t\"time\"\n\n\t\"github.com/northwind/batchrun/pool\"\n)\n\nfunc run(name string, f func() error) {\n\tdone := make(chan error, 1)\n\tgo func() {\n\t\tdefer func() {\n\t\t\tif r := recover(); r != nil {\n\t\t\t\tdone <- fmt.Errorf(\"panic: %v\", r)\n\t\t\t}\n\t\t}()\n\t\tdone <- f()\n\t}()\n\tselect {\n\tcase err := <-done:\n\t\tif err != nil {\n\t\t\tfmt.Printf(\"TEST %s FAIL %s\\n\", name, strings.ReplaceAll(err.Error(), \"\\n\", \" \"))\n\t\t\treturn\n\t\t}\n\t\tfmt.Printf(\"TEST %s PASS\\n\", name)\n\tcase <-time.After(5 * time.Second):\n\t\tfmt.Printf(\"TEST %s FAIL timed out after 5s (Run never returned: deadlock?)\\n\", name)\n\t}\n}\n\nfunc value(v string) func(context.Context) (string, error) {\n\treturn func(context.Context) (string, error) { return v, nil }\n}\n\nfunc ids(rs []pool.Result) string {\n\ts := make([]string, len(rs))\n\tfor i, r := range rs {\n\t\ts[i] = r.ID\n\t}\n\treturn \"[\" + strings.Join(s, \" \") + \"]\"\n}\n\nfunc main() {\n\tbg := context.Background()\n\n\trun(\"results_in_job_order\", func() error {\n\t\tcDone := make(chan struct{})\n\t\tbDone := make(chan struct{})\n\t\tjobs := []pool.Job{\n\t\t\t{ID: \"a\", Run: func(context.Context) (string, error) { <-bDone; return \"A\", nil }},\n\t\t\t{ID: \"b\", Run: func(context.Context) (string, error) { <-cDone; defer close(bDone); return \"B\", nil }},\n\t\t\t{ID: \"c\", Run: func(context.Context) (string, error) { defer close(cDone); return \"C\", nil }},\n\t\t}\n\t\trs, err := pool.Run(bg, pool.Options{Workers: 3}, jobs)\n\t\tif err != nil {\n\t\t\treturn fmt.Errorf(\"Run returned error %v\", err)\n\t\t}\n\t\tif ids(rs) != \"[a b c]\" {\n\t\t\treturn fmt.Errorf(\"jobs a,b,c finishing in order c,b,a: expected results [a b c], got %s\", ids(rs))\n\t\t}\n\t\tif rs[0].Value != \"A\" || rs[2].Value != \"C\" {\n\t\t\treturn fmt.Errorf(\"values: expected A..C, got %q..%q\", rs[0].Value, rs[2].Value)\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"each_job_runs_once\", func() error {\n\t\tconst n = 50\n\t\tvar calls [n]int32\n\t\tjobs := make([]pool.Job, n)\n\t\tfor i := range jobs {\n\t\t\ti := i\n\t\t\tjobs[i] = pool.Job{ID: fmt.Sprintf(\"j%02d\", i), Run: func(context.Context) (string, error) {\n\t\t\t\tatomic.AddInt32(&calls[i], 1)\n\t\t\t\treturn fmt.Sprint(i * i), nil\n\t\t\t}}\n\t\t}\n\t\trs, err := pool.Run(bg, pool.Options{Workers: 4, MaxAttempts: 3}, jobs)\n\t\tif err != nil {\n\t\t\treturn fmt.Errorf(\"Run returned error %v\", err)\n\t\t}\n\t\tif len(rs) != n {\n\t\t\treturn fmt.Errorf(\"expected %d results, got %d\", n, len(rs))\n\t\t}\n\t\tfor i := range jobs {\n\t\t\tif c := atomic.LoadInt32(&calls[i]); c != 1 {\n\t\t\t\treturn fmt.Errorf(\"job j%02d ran %d times, expected once\", i, c)\n\t\t\t}\n\t\t\tif rs[i].ID != jobs[i].ID || rs[i].Value != fmt.Sprint(i*i) || rs[i].Attempts != 1 || rs[i].Err != nil {\n\t\t\t\treturn fmt.Errorf(\"results[%d]: expected {%s %d 1 attempt}, got {%s %s %d attempts err=%v}\", i, jobs[i].ID, i*i, rs[i].ID, rs[i].Value, rs[i].Attempts, rs[i].Err)\n\t\t\t}\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"respects_worker_limit\", func() error {\n\t\tvar running, peak int32\n\t\treached := make(chan struct{})\n\t\tvar once sync.Once\n\t\tjobs := make([]pool.Job, 12)\n\t\tfor i := range jobs {\n\t\t\tjobs[i] = pool.Job{ID: fmt.Sprint(i), Run: func(context.Context) (string, error) {\n\t\t\t\tnow := atomic.AddInt32(&running, 1)\n\t\t\t\tfor {\n\t\t\t\t\tp := atomic.LoadInt32(&peak)\n\t\t\t\t\tif now <= p || atomic.CompareAndSwapInt32(&peak, p, now) {\n\t\t\t\t\t\tbreak\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t\tif now >= 3 {\n\t\t\t\t\tonce.Do(func() { close(reached) })\n\t\t\t\t}\n\t\t\t\tselect {\n\t\t\t\tcase <-reached:\n\t\t\t\tcase <-time.After(time.Second):\n\t\t\t\t}\n\t\t\t\ttime.Sleep(2 * time.Millisecond)\n\t\t\t\tatomic.AddInt32(&running, -1)\n\t\t\t\treturn \"ok\", nil\n\t\t\t}}\n\t\t}\n\t\tif _, err := pool.Run(bg, pool.Options{Workers: 3}, jobs); err != nil {\n\t\t\treturn fmt.Errorf(\"Run returned error %v\", err)\n\t\t}\n\t\tif p := atomic.LoadInt32(&peak); p != 3 {\n\t\t\treturn fmt.Errorf(\"with Workers=3 expected exactly 3 jobs running at the peak, saw %d\", p)\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"invalid_workers\", func() error {\n\t\tfor _, w := range []int{0, -2} {\n\t\t\trs, err := pool.Run(bg, pool.Options{Workers: w}, []pool.Job{{ID: \"x\", Run: value(\"x\")}})\n\t\t\tif !errors.Is(err, pool.ErrInvalidWorkers) || rs != nil {\n\t\t\t\treturn fmt.Errorf(\"Workers=%d: expected nil, ErrInvalidWorkers; got %d results, %v\", w, len(rs), err)\n\t\t\t}\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"empty_batch\", func() error {\n\t\trs, err := pool.Run(bg, pool.Options{Workers: 2}, nil)\n\t\tif err != nil || rs == nil || len(rs) != 0 {\n\t\t\treturn fmt.Errorf(\"empty batch: expected an empty non-nil slice and nil error, got %v (nil=%t), %v\", rs, rs == nil, err)\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"retry_attempt_count\", func() error {\n\t\tfor _, tc := range []struct{ max, want int }{{3, 3}, {1, 1}, {0, 1}} {\n\t\t\tvar calls int32\n\t\t\tjob := pool.Job{ID: \"flaky\", Run: func(context.Context) (string, error) {\n\t\t\t\tatomic.AddInt32(&calls, 1)\n\t\t\t\treturn \"\", errors.New(\"connection reset\")\n\t\t\t}}\n\t\t\trs, _ := pool.Run(bg, pool.Options{Workers: 1, MaxAttempts: tc.max}, []pool.Job{job})\n\t\t\tif len(rs) != 1 {\n\t\t\t\treturn fmt.Errorf(\"expected 1 result, got %d\", len(rs))\n\t\t\t}\n\t\t\tif calls != int32(tc.want) || rs[0].Attempts != tc.want {\n\t\t\t\treturn fmt.Errorf(\"MaxAttempts=%d on an always-failing job: expected %d calls and Attempts=%d, got %d calls, Attempts=%d\", tc.max, tc.want, tc.want, calls, rs[0].Attempts)\n\t\t\t}\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"retry_then_success\", func() error {\n\t\tvar calls int32\n\t\tjob := pool.Job{ID: \"eventually\", Run: func(context.Context) (string, error) {\n\t\t\tif atomic.AddInt32(&calls, 1) < 3 {\n\t\t\t\treturn \"\", errors.New(\"timeout\")\n\t\t\t}\n\t\t\treturn \"done\", nil\n\t\t}}\n\t\trs, _ := pool.Run(bg, pool.Options{Workers: 2, MaxAttempts: 5}, []pool.Job{job})\n\t\tif len(rs) != 1 || rs[0].Err != nil || rs[0].Value != \"done\" || rs[0].Attempts != 3 {\n\t\t\treturn fmt.Errorf(\"job failing twice then succeeding: expected {done, 3 attempts, nil}, got %+v\", rs)\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"permanent_error_not_retried\", func() error {\n\t\tvar calls int32\n\t\tjob := pool.Job{ID: \"bad\", Run: func(context.Context) (string, error) {\n\t\t\tatomic.AddInt32(&calls, 1)\n\t\t\treturn \"\", fmt.Errorf(\"record 7 malformed: %w\", pool.ErrPermanent)\n\t\t}}\n\t\trs, _ := pool.Run(bg, pool.Options{Workers: 1, MaxAttempts: 4}, []pool.Job{job})\n\t\tif len(rs) != 1 || calls != 1 || rs[0].Attempts != 1 {\n\t\t\treturn fmt.Errorf(\"permanent error: expected 1 call and Attempts=1, got %d calls, results %+v\", calls, rs)\n\t\t}\n\t\tif !errors.Is(rs[0].Err, pool.ErrPermanent) {\n\t\t\treturn fmt.Errorf(\"Result.Err %q does not wrap ErrPermanent (errors.Is is false)\", rs[0].Err)\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"error_wrapping\", func() error {\n\t\terrQuota := errors.New(\"quota exceeded\")\n\t\tjob := pool.Job{ID: \"j7\", Run: func(context.Context) (string, error) { return \"\", errQuota }}\n\t\trs, _ := pool.Run(bg, pool.Options{Workers: 1, MaxAttempts: 2}, []pool.Job{job})\n\t\tif len(rs) != 1 || rs[0].Err == nil {\n\t\t\treturn fmt.Errorf(\"expected one failed result, got %+v\", rs)\n\t\t}\n\t\tif got := rs[0].Err.Error(); got != \"job j7: quota exceeded\" {\n\t\t\treturn fmt.Errorf(\"Err message: expected %q, got %q\", \"job j7: quota exceeded\", got)\n\t\t}\n\t\tif !errors.Is(rs[0].Err, errQuota) {\n\t\t\treturn fmt.Errorf(\"errors.Is(Result.Err, jobErr) is false: the job error is not wrapped\")\n\t\t}\n\t\treturn nil\n\t})\n\n\tcancelBatch := func(n, workers int) ([]pool.Result, error, *int32) {\n\t\tctx, cancel := context.WithCancel(bg)\n\t\tdefer cancel()\n\t\tvar calls int32\n\t\tjobs := make([]pool.Job, n)\n\t\tfor i := range jobs {\n\t\t\ti := i\n\t\t\tjobs[i] = pool.Job{ID: fmt.Sprintf(\"t%d\", i), Run: func(ctx context.Context) (string, error) {\n\t\t\t\tatomic.AddInt32(&calls, 1)\n\t\t\t\tif i == 0 {\n\t\t\t\t\tcancel()\n\t\t\t\t\treturn \"first\", nil\n\t\t\t\t}\n\t\t\t\treturn \"late\", nil\n\t\t\t}}\n\t\t}\n\t\trs, err := pool.Run(ctx, pool.Options{Workers: workers, MaxAttempts: 2}, jobs)\n\t\treturn rs, err, &calls\n\t}\n\n\trun(\"cancel_marks_unstarted_jobs\", func() error {\n\t\trs, err, calls := cancelBatch(5, 1)\n\t\tif !errors.Is(err, context.Canceled) {\n\t\t\treturn fmt.Errorf(\"Run after cancellation: expected context.Canceled, got %v\", err)\n\t\t}\n\t\tif len(rs) != 5 {\n\t\t\treturn fmt.Errorf(\"cancelled batch of 5: expected 5 results, got %d %s\", len(rs), ids(rs))\n\t\t}\n\t\tif rs[0].ID != \"t0\" || rs[0].Value != \"first\" || rs[0].Err != nil {\n\t\t\treturn fmt.Errorf(\"results[0]: expected t0 succeeded with \\\"first\\\", got %+v\", rs[0])\n\t\t}\n\t\tif c := atomic.LoadInt32(calls); c != 1 {\n\t\t\treturn fmt.Errorf(\"%d jobs ran after the context was cancelled\", c-1)\n\t\t}\n\t\tfor i := 1; i < 5; i++ {\n\t\t\tr := rs[i]\n\t\t\twant := fmt.Sprintf(\"job t%d: context canceled\", i)\n\t\t\tif r.ID != fmt.Sprintf(\"t%d\", i) || r.Attempts != 0 || r.Err == nil || r.Err.Error() != want || !errors.Is(r.Err, context.Canceled) {\n\t\t\t\treturn fmt.Errorf(\"results[%d]: expected ID t%d, 0 attempts, Err %q wrapping context.Canceled; got %+v\", i, i, want, r)\n\t\t\t}\n\t\t}\n\t\treturn nil\n\t})\n\n\trun(\"no_goroutine_leak\", func() error {\n\t\ttime.Sleep(50 * time.Millisecond)\n\t\tbefore := runtime.NumGoroutine()\n\t\tfor round := 0; round < 5; round++ {\n\t\t\tcancelBatch(20, 2)\n\t\t\tpool.Run(bg, pool.Options{Workers: 3}, []pool.Job{{ID: \"x\", Run: value(\"x\")}, {ID: \"y\", Run: value(\"y\")}})\n\t\t}\n\t\tdeadline := time.Now().Add(2 * time.Second)\n\t\tfor {\n\t\t\tafter := runtime.NumGoroutine()\n\t\t\tif after <= before {\n\t\t\t\treturn nil\n\t\t\t}\n\t\t\tif time.Now().After(deadline) {\n\t\t\t\treturn fmt.Errorf(\"%d goroutines still running 2s after 10 calls to Run returned (5 of them cancelled)\", after-before)\n\t\t\t}\n\t\t\ttime.Sleep(20 * time.Millisecond)\n\t\t}\n\t})\n}\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "sh",
    "-c",
    "cp /bin/true ./.exec_probe && ./.exec_probe || { echo 'this checker runs a compiled Go test binary, but the temp dir is mounted noexec here: run_tests needs an exec /tmp'; exit 1; }"
   ],
   "label": "exec probe",
   "timeout_s": 30
  },
  {
   "cmd": [
    "go",
    "build",
    "./..."
   ],
   "label": "go build ./...",
   "timeout_s": 240
  },
  {
   "cmd": [
    "go",
    "build",
    "-o",
    ".chk",
    "./__checker__"
   ],
   "label": "build the hidden tests",
   "timeout_s": 240
  },
  {
   "cmd": [
    "./.chk"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 120
  }
 ],
 "tests": [
  "results_in_job_order",
  "each_job_runs_once",
  "respects_worker_limit",
  "invalid_workers",
  "empty_batch",
  "retry_attempt_count",
  "retry_then_success",
  "permanent_error_not_retried",
  "error_wrapping",
  "cancel_marks_unstarted_jobs",
  "no_goroutine_leak"
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
