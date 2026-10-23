const SPEC = {
 "env": {
  "NODE_NO_WARNINGS": "1"
 },
 "files": {
  "__checker__/check.ts": "// deno-lint-ignore-file no-explicit-any\ntype Mod = Record<string, any>;\n\nasync function load(rel: string): Promise<Mod> {\n\ttry {\n\t\treturn await import(`../src/${rel}`);\n\t} catch (e) {\n\t\tconst msg = String((e as Error)?.message ?? e).split(\"\\n\")[0]\n\t\t\t.replace(/ imported from \\S+/g, \"\")\n\t\t\t.replace(/(file:\\/\\/)?\\/\\S*\\/src\\//g, \"src/\");\n\t\tthrow new Error(`cannot load src/${rel}: ${msg}`);\n\t}\n}\n\nfunction need(mod: Mod, name: string, rel: string): any {\n\tif (mod[name] === undefined) throw new Error(`src/${rel} does not export ${name}`);\n\treturn mod[name];\n}\n\nfunction fail(msg: string): never {\n\tthrow new Error(msg);\n}\n\nfunction eq(got: unknown, want: unknown, what: string): void {\n\tconst g = JSON.stringify(got);\n\tconst w = JSON.stringify(want);\n\tif (g !== w) fail(`${what}: expected ${w}, got ${g}`);\n}\n\nfunction throwsRange(fn: () => unknown, what: string): void {\n\ttry {\n\t\tfn();\n\t} catch (e) {\n\t\tif (e instanceof RangeError) return;\n\t\tfail(`${what}: expected RangeError, got ${(e as Error)?.name}: ${(e as Error)?.message}`);\n\t}\n\tfail(`${what}: expected RangeError, nothing was thrown`);\n}\n\nconst ok = (body: string, headers: Record<string, string> = {}) => ({ status: 200, headers, body });\nconst GET = (url: string) => ({ method: \"GET\", url });\nconst API = \"https://api.example.com\";\n\nasync function cacheClass(): Promise<any> {\n\treturn need(await load(\"index.ts\"), \"ResponseCache\", \"index.ts\");\n}\n\nfunction clock() {\n\tconst c = { t: 1_000_000, now: () => c.t };\n\treturn c;\n}\n\nconst tests: [string, () => Promise<void>][] = [\n\t[\"caches_get\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0 });\n\t\teq(c.get(GET(`${API}/v1/items`)), undefined, \"get before put\");\n\t\teq(c.put(GET(`${API}/v1/items`), ok(\"[1]\")), true, \"put GET 200\");\n\t\teq(c.get(GET(`${API}/v1/items`))?.body, \"[1]\", \"get after put\");\n\t\teq(c.get(GET(`${API}/v1/other`)), undefined, \"get of another URL\");\n\t\tconst s = c.stats();\n\t\teq([s.hits, s.misses, s.size], [1, 2, 1], \"stats() [hits, misses, size]\");\n\t}],\n\t[\"method_rules\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0 });\n\t\teq(c.put({ method: \"POST\", url: `${API}/a` }, ok(\"p\")), false, \"put POST\");\n\t\teq(c.put({ method: \"get\", url: `${API}/a` }, ok(\"g\")), true, \"put lower-case get\");\n\t\teq(c.get({ method: \"HEAD\", url: `${API}/a` }), undefined, \"HEAD after only GET was stored\");\n\t\teq(c.put({ method: \"HEAD\", url: `${API}/a` }, ok(\"\")), true, \"put HEAD\");\n\t\teq(c.get(GET(`${API}/a`))?.body, \"g\", \"GET body after HEAD stored\");\n\t\teq(c.get({ method: \"DELETE\", url: `${API}/a` }), undefined, \"get DELETE\");\n\t\tconst s = c.stats();\n\t\teq([s.hits, s.misses, s.size], [1, 1, 2], \"stats() [hits, misses, size]\");\n\t}],\n\t[\"status_rules\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0 });\n\t\teq(c.put(GET(`${API}/x`), { status: 404, headers: {}, body: \"nope\" }), false, \"put 404\");\n\t\teq(c.put(GET(`${API}/x`), { status: 204, headers: {}, body: \"\" }), false, \"put 204\");\n\t\teq(c.stats().size, 0, \"size\");\n\t}],\n\t[\"cache_control_rules\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst k = clock();\n\t\tconst c = new RC({ now: k.now, defaultTtlMs: 5_000 });\n\t\teq(c.put(GET(`${API}/ns`), ok(\"x\", { \"Cache-Control\": \"No-Store\" })), false, \"Cache-Control: No-Store\");\n\t\teq(c.put(GET(`${API}/pv`), ok(\"x\", { \"cache-control\": \"max-age=60, private\" })), false, \"cache-control: max-age=60, private\");\n\t\teq(c.put(GET(`${API}/z`), ok(\"x\", { \"Cache-Control\": \"max-age=0\" })), false, \"max-age=0\");\n\t\teq(c.put(GET(`${API}/ma`), ok(\"ma\", { \"CACHE-CONTROL\": \"public, Max-Age = 120\" })), true, \"Max-Age = 120\");\n\t\teq(c.put(GET(`${API}/sm`), ok(\"sm\", { \"Cache-Control\": \"max-age=600, s-maxage=30\" })), true, \"max-age=600, s-maxage=30\");\n\t\teq(c.put(GET(`${API}/bad`), ok(\"bad\", { \"Cache-Control\": \"max-age=soon\" })), true, \"max-age=soon\");\n\t\tk.t += 29_999;\n\t\teq(c.get(GET(`${API}/sm`))?.body, \"sm\", \"s-maxage=30 entry at 29.999 s\");\n\t\teq(c.get(GET(`${API}/bad`)), undefined, \"invalid max-age entry (default TTL 5 s) at 29.999 s\");\n\t\tk.t += 1;\n\t\teq(c.get(GET(`${API}/sm`)), undefined, \"s-maxage=30 entry at 30 s\");\n\t\tk.t += 89_999;\n\t\teq(c.get(GET(`${API}/ma`))?.body, \"ma\", \"Max-Age=120 entry at 119.999 s\");\n\t}],\n\t[\"expiry\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst k = clock();\n\t\tconst c = new RC({ now: k.now });\n\t\tc.put(GET(`${API}/d`), ok(\"d\"));\n\t\tk.t += 59_999;\n\t\teq(c.get(GET(`${API}/d`))?.body, \"d\", \"default TTL entry at 59.999 s\");\n\t\tk.t += 1;\n\t\teq(c.get(GET(`${API}/d`)), undefined, \"default TTL entry at 60 s\");\n\t\tconst s = c.stats();\n\t\teq([s.hits, s.misses, s.size], [1, 1, 0], \"stats() after expiry [hits, misses, size]\");\n\t}],\n\t[\"key_normalization\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0 });\n\t\tc.put(GET(\"https://API.Example.com/v1/Items?b=2&a=1#top\"), ok(\"items\"));\n\t\teq(c.get(GET(\"https://api.example.com/v1/Items?a=1&b=2\"))?.body, \"items\", \"same URL, other query order / host case / no fragment\");\n\t\teq(c.get(GET(\"https://api.example.com/v1/items?a=1&b=2\")), undefined, \"path with different case\");\n\t\teq(c.get(GET(\"https://api.example.com/v1/Items?a=1&b=3\")), undefined, \"different query value\");\n\t}],\n\t[\"invalidate_prefix\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0 });\n\t\tc.put(GET(`${API}/v1/users/1`), ok(\"u1\"));\n\t\tc.put({ method: \"HEAD\", url: `${API}/v1/users/2` }, ok(\"\"));\n\t\tc.put(GET(`${API}/v1/orders/9`), ok(\"o9\"));\n\t\teq(c.invalidate(`${API}/v1/users`), 2, \"invalidate(.../v1/users)\");\n\t\teq(c.get(GET(`${API}/v1/orders/9`))?.body, \"o9\", \"unrelated entry after invalidate\");\n\t\teq(c.stats().size, 1, \"size after invalidate\");\n\t}],\n\t[\"returns_copy\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0 });\n\t\tconst res = ok(\"orig\", { \"content-type\": \"text/plain\" });\n\t\tc.put(GET(`${API}/c`), res);\n\t\tres.headers[\"content-type\"] = \"changed\";\n\t\tconst a = c.get(GET(`${API}/c`));\n\t\ta.body = \"mutated\";\n\t\ta.headers[\"x-extra\"] = \"1\";\n\t\tconst b = c.get(GET(`${API}/c`));\n\t\teq([b.body, b.headers], [\"orig\", { \"content-type\": \"text/plain\" }], \"second get after mutating input and first result\");\n\t}],\n\t[\"module_cache_control\", async () => {\n\t\tconst rel = \"cacheControl.ts\";\n\t\tconst parse = need(await load(rel), \"parseCacheControl\", rel);\n\t\teq(parse(undefined), { noStore: false, private: false }, \"parseCacheControl(undefined)\");\n\t\tconst a = parse(\"Public, MAX-AGE=300, s-maxage=20\");\n\t\teq([a.noStore, a.private, a.maxAge, a.sMaxAge], [false, false, 300, 20], \"parseCacheControl('Public, MAX-AGE=300, s-maxage=20') [noStore, private, maxAge, sMaxAge]\");\n\t\tconst b = parse(\" no-store , private, max-age=-1\");\n\t\teq([b.noStore, b.private, b.maxAge, b.sMaxAge], [true, true, undefined, undefined], \"parseCacheControl(' no-store , private, max-age=-1') [noStore, private, maxAge, sMaxAge]\");\n\t}],\n\t[\"module_key\", async () => {\n\t\tconst rel = \"key.ts\";\n\t\tconst m = await load(rel);\n\t\tconst cacheKey = need(m, \"cacheKey\", rel);\n\t\tconst normalizeUrl = need(m, \"normalizeUrl\", rel);\n\t\teq(normalizeUrl(\"HTTPS://Api.Example.com/v1/X?z=1&a=2&a=1#f\"), \"https://api.example.com/v1/X?a=2&a=1&z=1\", \"normalizeUrl\");\n\t\teq(cacheKey({ method: \"head\", url: \"https://api.example.com/p?b=1&a=1\" }), \"HEAD https://api.example.com/p?a=1&b=1\", \"cacheKey\");\n\t}],\n\t[\"module_lru\", async () => {\n\t\tconst rel = \"lru.ts\";\n\t\tconst Lru = need(await load(rel), \"LruStore\", rel);\n\t\tconst s = new Lru(3);\n\t\teq([s.set(\"a\", 1), s.set(\"b\", 2), s.set(\"c\", 3)], [undefined, undefined, undefined], \"set a,b,c into LruStore(3)\");\n\t\teq(s.get(\"a\"), 1, \"get(a)\");\n\t\teq(s.has(\"b\"), true, \"has(b)\");\n\t\teq(s.keys(), [\"b\", \"c\", \"a\"], \"keys() after get(a) and has(b)\");\n\t\teq(s.set(\"d\", 4), \"b\", \"set(d) evicts\");\n\t\teq(s.set(\"c\", 30), undefined, \"replacing c evicts\");\n\t\teq([s.keys(), s.size], [[\"a\", \"d\", \"c\"], 3], \"[keys(), size]\");\n\t\teq([s.delete(\"a\"), s.delete(\"zz\"), s.size], [true, false, 2], \"[delete(a), delete(zz), size]\");\n\t\teq(s.get(\"zz\"), undefined, \"get(zz)\");\n\t\tconst big = new Lru();\n\t\tfor (let i = 0; i < 1000; i++) big.set(`k${i}`, i);\n\t\teq(big.size, 1000, \"default LruStore size after 1000 sets\");\n\t\tthrowsRange(() => new Lru(0), \"new LruStore(0)\");\n\t}],\n\t[\"module_layout\", async () => {\n\t\tconst index = await load(\"index.ts\");\n\t\tfor (const name of [\"ResponseCache\", \"LruStore\", \"parseCacheControl\", \"cacheKey\", \"normalizeUrl\"]) need(index, name, \"index.ts\");\n\t\tconst rc = need(await load(\"responseCache.ts\"), \"ResponseCache\", \"responseCache.ts\");\n\t\tif (index.ResponseCache !== rc) fail(\"index.ts must re-export the ResponseCache of src/responseCache.ts\");\n\t\tconst legacy = need(await load(\"cache.ts\"), \"ResponseCache\", \"cache.ts\");\n\t\tif (legacy !== rc) fail(\"src/cache.ts must re-export the ResponseCache of src/responseCache.ts, not define its own\");\n\t\tif (index.LruStore !== need(await load(\"lru.ts\"), \"LruStore\", \"lru.ts\")) fail(\"index.ts must re-export LruStore from src/lru.ts\");\n\t}],\n\t[\"lru_eviction\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst c = new RC({ now: () => 0, maxEntries: 2 });\n\t\tc.put(GET(`${API}/a`), ok(\"a\"));\n\t\tc.put(GET(`${API}/b`), ok(\"b\"));\n\t\teq(c.get(GET(`${API}/a`))?.body, \"a\", \"get a\");\n\t\tc.put(GET(`${API}/c`), ok(\"c\"));\n\t\teq(c.get(GET(`${API}/b`)), undefined, \"b after a was read and c was added (maxEntries 2)\");\n\t\teq(c.get(GET(`${API}/a`))?.body, \"a\", \"a after c was added\");\n\t\teq(c.get(GET(`${API}/c`))?.body, \"c\", \"c after it was added\");\n\t\tconst s = c.stats();\n\t\teq([s.size, s.evictions], [2, 1], \"stats() [size, evictions]\");\n\t}],\n\t[\"lru_replace_and_expiry\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tconst k = clock();\n\t\tconst c = new RC({ now: k.now, maxEntries: 2 });\n\t\tc.put(GET(`${API}/a`), ok(\"a\"));\n\t\tc.put(GET(`${API}/b`), ok(\"b\", { \"cache-control\": \"max-age=1\" }));\n\t\tc.put(GET(`${API}/a`), ok(\"a2\"));\n\t\teq(c.stats().evictions, 0, \"evictions after replacing an existing key\");\n\t\tc.put(GET(`${API}/c`), ok(\"c\"));\n\t\teq(c.get(GET(`${API}/a`))?.body, \"a2\", \"a (re-put most recently) after c was added\");\n\t\teq(c.get(GET(`${API}/b`)), undefined, \"b after c was added\");\n\t\tk.t += 120_000;\n\t\teq(c.get(GET(`${API}/a`)), undefined, \"a after expiry\");\n\t\tc.invalidate(API);\n\t\tconst s = c.stats();\n\t\teq([s.size, s.evictions], [0, 1], \"stats() after expiry + invalidate [size, evictions]\");\n\t}],\n\t[\"max_entries_validation\", async () => {\n\t\tconst RC = await cacheClass();\n\t\tfor (const bad of [0, -1, 1.5, Number.NaN]) throwsRange(() => new RC({ maxEntries: bad }), `maxEntries ${bad}`);\n\t\tconst c = new RC({ now: () => 0, maxEntries: Number.POSITIVE_INFINITY });\n\t\tfor (let i = 0; i < 300; i++) c.put(GET(`${API}/n/${i}`), ok(String(i)));\n\t\tconst d = new RC({ now: () => 0 });\n\t\tfor (let i = 0; i < 300; i++) d.put(GET(`${API}/n/${i}`), ok(String(i)));\n\t\teq([c.stats().size, c.stats().evictions, d.stats().size], [300, 0, 300], \"[size, evictions] with Infinity, size with default\");\n\t}],\n];\n\nfor (const [name, fn] of tests) {\n\ttry {\n\t\tawait fn();\n\t\tconsole.log(`TEST ${name} PASS`);\n\t} catch (e) {\n\t\tconsole.log(`TEST ${name} FAIL ${String((e as Error)?.message ?? e).split(\"\\n\")[0]}`);\n\t}\n}\n"
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
  "caches_get",
  "method_rules",
  "status_rules",
  "cache_control_rules",
  "expiry",
  "key_normalization",
  "invalidate_prefix",
  "returns_copy",
  "module_cache_control",
  "module_key",
  "module_lru",
  "module_layout",
  "lru_eviction",
  "lru_replace_and_expiry",
  "max_entries_validation"
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
