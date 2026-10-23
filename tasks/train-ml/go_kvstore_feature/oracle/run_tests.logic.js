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
  "__checker__/main.go": "package main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"io\"\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"strings\"\n\t\"sync\"\n\t\"time\"\n\n\t\"github.com/northwind/kvd/api\"\n\t\"github.com/northwind/kvd/store\"\n)\n\ntype fakeClock struct {\n\tmu  sync.Mutex\n\tnow time.Time\n}\n\nfunc (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }\nfunc (c *fakeClock) Advance(d time.Duration) {\n\tc.mu.Lock()\n\tdefer c.mu.Unlock()\n\tc.now = c.now.Add(d)\n}\n\nvar t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)\n\ntype env struct {\n\tsrv   *httptest.Server\n\tclock *fakeClock\n}\n\ntype resp struct {\n\tstatus int\n\theader http.Header\n\traw    string\n\tbody   map[string]any\n}\n\nfunc newEnv() *env {\n\tc := &fakeClock{now: t0}\n\treturn &env{srv: httptest.NewServer(api.NewHandler(store.New(c.Now))), clock: c}\n}\n\nfunc (e *env) do(method, path, body string, hdr ...string) resp {\n\tvar rd io.Reader\n\tif body != \"\" {\n\t\trd = strings.NewReader(body)\n\t}\n\treq, err := http.NewRequest(method, e.srv.URL+path, rd)\n\tif err != nil {\n\t\tpanic(err)\n\t}\n\tif body != \"\" {\n\t\treq.Header.Set(\"Content-Type\", \"application/json\")\n\t}\n\tfor i := 0; i+1 < len(hdr); i += 2 {\n\t\treq.Header.Set(hdr[i], hdr[i+1])\n\t}\n\tr, err := http.DefaultClient.Do(req)\n\tif err != nil {\n\t\tpanic(fmt.Sprintf(\"%s %s: %v\", method, path, err))\n\t}\n\tdefer r.Body.Close()\n\traw, _ := io.ReadAll(r.Body)\n\tout := resp{status: r.StatusCode, header: r.Header, raw: string(raw)}\n\tjson.Unmarshal(raw, &out.body)\n\treturn out\n}\n\nfunc (r resp) short() string {\n\ts := strings.TrimSpace(r.raw)\n\tif len(s) > 120 {\n\t\ts = s[:120] + \"...\"\n\t}\n\treturn fmt.Sprintf(\"%d %q\", r.status, s)\n}\n\ntype failure string\n\nfunc check(cond bool, format string, args ...any) {\n\tif !cond {\n\t\tpanic(failure(fmt.Sprintf(format, args...)))\n\t}\n}\n\nfunc expectStatus(r resp, want int, what string) {\n\tcheck(r.status == want, \"%s: expected status %d, got %s\", what, want, r.short())\n}\n\nfunc expectJSON(r resp, what string) {\n\tct := r.header.Get(\"Content-Type\")\n\tcheck(strings.HasPrefix(ct, \"application/json\"), \"%s: expected Content-Type application/json, got %q\", what, ct)\n\tcheck(r.body != nil, \"%s: body is not a JSON object: %q\", what, strings.TrimSpace(r.raw))\n}\n\nfunc expectError(r resp, status int, code, what string) {\n\texpectStatus(r, status, what)\n\texpectJSON(r, what)\n\tcheck(r.body[\"error\"] == code, \"%s: expected error code %q, got %v\", what, code, r.body[\"error\"])\n\tmsg, _ := r.body[\"message\"].(string)\n\tcheck(msg != \"\", \"%s: error JSON has no message\", what)\n}\n\nfunc expectEntry(r resp, status int, key, value string, version int, what string) {\n\texpectStatus(r, status, what)\n\texpectJSON(r, what)\n\tcheck(r.body[\"key\"] == key && r.body[\"value\"] == value && r.body[\"version\"] == float64(version),\n\t\t\"%s: expected entry {key:%q value:%q version:%d}, got %s\", what, key, value, version, strings.TrimSpace(r.raw))\n\tetag := r.header.Get(\"ETag\")\n\tcheck(etag == fmt.Sprintf(\"%q\", fmt.Sprint(version)), \"%s: expected ETag %q, got %q\", what, fmt.Sprintf(\"%q\", fmt.Sprint(version)), etag)\n}\n\nfunc keysOf(r resp) []string {\n\tarr, ok := r.body[\"keys\"].([]any)\n\tcheck(ok, \"list: expected a JSON array in \\\"keys\\\", got %s\", strings.TrimSpace(r.raw))\n\tout := make([]string, len(arr))\n\tfor i, k := range arr {\n\t\tout[i], _ = k.(string)\n\t}\n\treturn out\n}\n\nfunc run(name string, f func(e *env)) {\n\te := newEnv()\n\tdefer e.srv.Close()\n\tdone := make(chan string, 1)\n\tgo func() {\n\t\tdefer func() {\n\t\t\tif r := recover(); r != nil {\n\t\t\t\tif f, ok := r.(failure); ok {\n\t\t\t\t\tdone <- string(f)\n\t\t\t\t\treturn\n\t\t\t\t}\n\t\t\t\tdone <- fmt.Sprintf(\"panic: %v\", r)\n\t\t\t}\n\t\t}()\n\t\tf(e)\n\t\tdone <- \"\"\n\t}()\n\tselect {\n\tcase msg := <-done:\n\t\tif msg != \"\" {\n\t\t\tfmt.Printf(\"TEST %s FAIL %s\\n\", name, strings.ReplaceAll(msg, \"\\n\", \" \"))\n\t\t\treturn\n\t\t}\n\t\tfmt.Printf(\"TEST %s PASS\\n\", name)\n\tcase <-time.After(10 * time.Second):\n\t\tfmt.Printf(\"TEST %s FAIL timed out after 10s\\n\", name)\n\t}\n}\n\nfunc main() {\n\thttp.DefaultClient.Timeout = 5 * time.Second\n\n\trun(\"put_get_json\", func(e *env) {\n\t\tr := e.do(\"PUT\", \"/v1/keys/user/42\", `{\"value\":\"Ada\"}`)\n\t\texpectEntry(r, 201, \"user/42\", \"Ada\", 1, \"PUT of a new key\")\n\t\t_, has := r.body[\"expires_at\"]\n\t\tcheck(!has, \"PUT without ttl_ms: entry must not have expires_at, got %s\", strings.TrimSpace(r.raw))\n\t\tr = e.do(\"GET\", \"/v1/keys/user/42\", \"\")\n\t\texpectEntry(r, 200, \"user/42\", \"Ada\", 1, \"GET\")\n\t})\n\n\trun(\"replace_bumps_version\", func(e *env) {\n\t\te.do(\"PUT\", \"/v1/keys/color\", `{\"value\":\"red\"}`)\n\t\tr := e.do(\"PUT\", \"/v1/keys/color\", `{\"value\":\"green\"}`)\n\t\texpectEntry(r, 200, \"color\", \"green\", 2, \"second PUT of a key\")\n\t\tr = e.do(\"PUT\", \"/v1/keys/color\", `{\"value\":\"\"}`)\n\t\texpectEntry(r, 200, \"color\", \"\", 3, \"third PUT (empty string value)\")\n\t\texpectEntry(e.do(\"GET\", \"/v1/keys/color\", \"\"), 200, \"color\", \"\", 3, \"GET after three PUTs\")\n\t})\n\n\trun(\"not_found_json\", func(e *env) {\n\t\texpectError(e.do(\"GET\", \"/v1/keys/missing\", \"\"), 404, \"not_found\", \"GET of a missing key\")\n\t\texpectError(e.do(\"DELETE\", \"/v1/keys/missing\", \"\"), 404, \"not_found\", \"DELETE of a missing key\")\n\t})\n\n\trun(\"delete_and_recreate\", func(e *env) {\n\t\te.do(\"PUT\", \"/v1/keys/k\", `{\"value\":\"1\"}`)\n\t\te.do(\"PUT\", \"/v1/keys/k\", `{\"value\":\"2\"}`)\n\t\tr := e.do(\"DELETE\", \"/v1/keys/k\", \"\")\n\t\texpectStatus(r, 204, \"DELETE of an existing key\")\n\t\tcheck(strings.TrimSpace(r.raw) == \"\", \"DELETE: expected no body, got %q\", r.raw)\n\t\texpectError(e.do(\"GET\", \"/v1/keys/k\", \"\"), 404, \"not_found\", \"GET after DELETE\")\n\t\texpectEntry(e.do(\"PUT\", \"/v1/keys/k\", `{\"value\":\"3\"}`), 201, \"k\", \"3\", 1, \"PUT after DELETE\")\n\t})\n\n\trun(\"bad_request\", func(e *env) {\n\t\tfor _, body := range []string{`{\"value\":`, `not json`, `{}`, `{\"value\":42}`, `{\"value\":\"x\",\"ttl_ms\":-5}`} {\n\t\t\texpectError(e.do(\"PUT\", \"/v1/keys/b\", body), 400, \"bad_request\", fmt.Sprintf(\"PUT with body %s\", body))\n\t\t}\n\t\texpectError(e.do(\"GET\", \"/v1/keys/b\", \"\"), 404, \"not_found\", \"GET after only rejected PUTs\")\n\t})\n\n\trun(\"cas_if_match\", func(e *env) {\n\t\te.do(\"PUT\", \"/v1/keys/acct\", `{\"value\":\"100\"}`)\n\t\te.do(\"PUT\", \"/v1/keys/acct\", `{\"value\":\"90\"}`)\n\t\texpectEntry(e.do(\"PUT\", \"/v1/keys/acct\", `{\"value\":\"80\"}`, \"If-Match\", `\"2\"`), 200, \"acct\", \"80\", 3, `PUT with If-Match \"2\" on version 2`)\n\t\texpectError(e.do(\"PUT\", \"/v1/keys/acct\", `{\"value\":\"70\"}`, \"If-Match\", `\"2\"`), 412, \"version_mismatch\", `PUT with If-Match \"2\" on version 3`)\n\t\texpectEntry(e.do(\"GET\", \"/v1/keys/acct\", \"\"), 200, \"acct\", \"80\", 3, \"GET after a refused If-Match PUT\")\n\t\texpectEntry(e.do(\"PUT\", \"/v1/keys/acct\", `{\"value\":\"60\"}`, \"If-Match\", \"3\"), 200, \"acct\", \"60\", 4, \"PUT with bare If-Match 3\")\n\t\texpectError(e.do(\"PUT\", \"/v1/keys/nobody\", `{\"value\":\"x\"}`, \"If-Match\", \"1\"), 412, \"version_mismatch\", \"PUT with If-Match on a missing key\")\n\t\texpectError(e.do(\"PUT\", \"/v1/keys/acct\", `{\"value\":\"x\"}`, \"If-Match\", \"abc\"), 400, \"bad_request\", \"PUT with If-Match: abc\")\n\t})\n\n\trun(\"create_only\", func(e *env) {\n\t\texpectEntry(e.do(\"PUT\", \"/v1/keys/lock\", `{\"value\":\"me\"}`, \"If-None-Match\", \"*\"), 201, \"lock\", \"me\", 1, \"PUT with If-None-Match * on a new key\")\n\t\texpectError(e.do(\"PUT\", \"/v1/keys/lock\", `{\"value\":\"you\"}`, \"If-None-Match\", \"*\"), 412, \"key_exists\", \"PUT with If-None-Match * on an existing key\")\n\t\texpectEntry(e.do(\"GET\", \"/v1/keys/lock\", \"\"), 200, \"lock\", \"me\", 1, \"GET after a refused create\")\n\t})\n\n\trun(\"ttl_expiry\", func(e *env) {\n\t\tr := e.do(\"PUT\", \"/v1/keys/session\", `{\"value\":\"abc\",\"ttl_ms\":1500}`)\n\t\texpectEntry(r, 201, \"session\", \"abc\", 1, \"PUT with ttl_ms 1500\")\n\t\texp, _ := r.body[\"expires_at\"].(string)\n\t\tts, err := time.Parse(time.RFC3339Nano, exp)\n\t\tcheck(err == nil && ts.Equal(t0.Add(1500*time.Millisecond)), \"expires_at: expected %s, got %q\", t0.Add(1500*time.Millisecond).Format(time.RFC3339Nano), exp)\n\t\te.clock.Advance(1499 * time.Millisecond)\n\t\texpectEntry(e.do(\"GET\", \"/v1/keys/session\", \"\"), 200, \"session\", \"abc\", 1, \"GET 1499ms after a 1500ms TTL\")\n\t\te.clock.Advance(time.Millisecond)\n\t\texpectError(e.do(\"GET\", \"/v1/keys/session\", \"\"), 404, \"not_found\", \"GET exactly 1500ms after a 1500ms TTL\")\n\t\texpectError(e.do(\"DELETE\", \"/v1/keys/session\", \"\"), 404, \"not_found\", \"DELETE of an expired key\")\n\t})\n\n\trun(\"ttl_replace_and_recreate\", func(e *env) {\n\t\te.do(\"PUT\", \"/v1/keys/a\", `{\"value\":\"1\",\"ttl_ms\":1000}`)\n\t\tr := e.do(\"PUT\", \"/v1/keys/a\", `{\"value\":\"2\"}`)\n\t\texpectEntry(r, 200, \"a\", \"2\", 2, \"PUT without ttl over a key with TTL\")\n\t\t_, has := r.body[\"expires_at\"]\n\t\tcheck(!has, \"a PUT without ttl_ms must clear the TTL, got %s\", strings.TrimSpace(r.raw))\n\t\te.do(\"PUT\", \"/v1/keys/b\", `{\"value\":\"old\",\"ttl_ms\":1000}`)\n\t\te.do(\"PUT\", \"/v1/keys/b\", `{\"value\":\"old2\",\"ttl_ms\":1000}`)\n\t\te.clock.Advance(5 * time.Second)\n\t\texpectEntry(e.do(\"GET\", \"/v1/keys/a\", \"\"), 200, \"a\", \"2\", 2, \"GET of the key whose TTL was cleared\")\n\t\texpectError(e.do(\"PUT\", \"/v1/keys/b\", `{\"value\":\"x\"}`, \"If-Match\", \"2\"), 412, \"version_mismatch\", \"If-Match on an expired key\")\n\t\texpectEntry(e.do(\"PUT\", \"/v1/keys/b\", `{\"value\":\"new\"}`, \"If-None-Match\", \"*\"), 201, \"b\", \"new\", 1, \"If-None-Match * over an expired key\")\n\t})\n\n\trun(\"list_prefix_sorted\", func(e *env) {\n\t\tfor _, k := range []string{\"user/b\", \"user/a\", \"admin/x\", \"user/c\", \"userx\"} {\n\t\t\te.do(\"PUT\", \"/v1/keys/\"+k, `{\"value\":\"v\"}`)\n\t\t}\n\t\te.do(\"PUT\", \"/v1/keys/user/0\", `{\"value\":\"v\",\"ttl_ms\":10}`)\n\t\te.clock.Advance(time.Second)\n\t\tr := e.do(\"GET\", \"/v1/keys?prefix=user/\", \"\")\n\t\texpectStatus(r, 200, \"list with prefix user/\")\n\t\texpectJSON(r, \"list\")\n\t\tgot := strings.Join(keysOf(r), \",\")\n\t\tcheck(got == \"user/a,user/b,user/c\", \"list prefix=user/: expected keys user/a,user/b,user/c, got %s\", got)\n\t\t_, has := r.body[\"next\"]\n\t\tcheck(!has, \"list: \\\"next\\\" must be absent when no keys follow, got %s\", strings.TrimSpace(r.raw))\n\t\tr = e.do(\"GET\", \"/v1/keys\", \"\")\n\t\tgot = strings.Join(keysOf(r), \",\")\n\t\tcheck(got == \"admin/x,user/a,user/b,user/c,userx\", \"list without prefix: expected admin/x,user/a,user/b,user/c,userx, got %s\", got)\n\t\tr = e.do(\"GET\", \"/v1/keys?prefix=zzz\", \"\")\n\t\tcheck(strings.Contains(strings.ReplaceAll(r.raw, \" \", \"\"), `\"keys\":[]`), \"list with no match: expected \\\"keys\\\":[], got %s\", strings.TrimSpace(r.raw))\n\t})\n\n\trun(\"list_pagination\", func(e *env) {\n\t\tfor _, k := range []string{\"p/1\", \"p/2\", \"p/3\", \"p/4\", \"p/5\"} {\n\t\t\te.do(\"PUT\", \"/v1/keys/\"+k, `{\"value\":\"v\"}`)\n\t\t}\n\t\tr := e.do(\"GET\", \"/v1/keys?prefix=p/&limit=2\", \"\")\n\t\tcheck(strings.Join(keysOf(r), \",\") == \"p/1,p/2\" && r.body[\"next\"] == \"p/2\", \"page 1 (limit=2): expected keys p/1,p/2 next p/2, got %s\", strings.TrimSpace(r.raw))\n\t\tr = e.do(\"GET\", \"/v1/keys?prefix=p/&limit=2&after=p/2\", \"\")\n\t\tcheck(strings.Join(keysOf(r), \",\") == \"p/3,p/4\" && r.body[\"next\"] == \"p/4\", \"page 2: expected keys p/3,p/4 next p/4, got %s\", strings.TrimSpace(r.raw))\n\t\tr = e.do(\"GET\", \"/v1/keys?prefix=p/&limit=3&after=p/2\", \"\")\n\t\t_, has := r.body[\"next\"]\n\t\tcheck(strings.Join(keysOf(r), \",\") == \"p/3,p/4,p/5\" && !has, \"limit=3 after=p/2: expected keys p/3,p/4,p/5 and no next, got %s\", strings.TrimSpace(r.raw))\n\t\tfor _, lim := range []string{\"0\", \"1001\", \"x\", \"-1\"} {\n\t\t\texpectError(e.do(\"GET\", \"/v1/keys?limit=\"+lim, \"\"), 400, \"bad_request\", \"list with limit=\"+lim)\n\t\t}\n\t})\n\n\trun(\"method_not_allowed\", func(e *env) {\n\t\tfor _, c := range [][2]string{{\"POST\", \"/v1/keys/x\"}, {\"PATCH\", \"/v1/keys/x\"}, {\"DELETE\", \"/v1/keys\"}, {\"POST\", \"/v1/keys\"}} {\n\t\t\tr := e.do(c[0], c[1], `{\"value\":\"v\"}`)\n\t\t\texpectError(r, 405, \"method_not_allowed\", c[0]+\" \"+c[1])\n\t\t\tcheck(r.header.Get(\"Allow\") != \"\", \"%s %s: 405 without an Allow header\", c[0], c[1])\n\t\t}\n\t})\n}\n"
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
  "put_get_json",
  "replace_bumps_version",
  "not_found_json",
  "delete_and_recreate",
  "bad_request",
  "cas_if_match",
  "create_only",
  "ttl_expiry",
  "ttl_replace_and_recreate",
  "list_prefix_sorted",
  "list_pagination",
  "method_not_allowed"
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
