// deno-lint-ignore-file no-explicit-any
type Mod = Record<string, any>;

async function load(rel: string): Promise<Mod> {
	try {
		return await import(`../src/${rel}`);
	} catch (e) {
		const msg = String((e as Error)?.message ?? e).split("\n")[0]
			.replace(/ imported from \S+/g, "")
			.replace(/(file:\/\/)?\/\S*\/src\//g, "src/");
		throw new Error(`cannot load src/${rel}: ${msg}`);
	}
}

function need(mod: Mod, name: string, rel: string): any {
	if (mod[name] === undefined) throw new Error(`src/${rel} does not export ${name}`);
	return mod[name];
}

function fail(msg: string): never {
	throw new Error(msg);
}

function eq(got: unknown, want: unknown, what: string): void {
	const g = JSON.stringify(got);
	const w = JSON.stringify(want);
	if (g !== w) fail(`${what}: expected ${w}, got ${g}`);
}

function throwsRange(fn: () => unknown, what: string): void {
	try {
		fn();
	} catch (e) {
		if (e instanceof RangeError) return;
		fail(`${what}: expected RangeError, got ${(e as Error)?.name}: ${(e as Error)?.message}`);
	}
	fail(`${what}: expected RangeError, nothing was thrown`);
}

const ok = (body: string, headers: Record<string, string> = {}) => ({ status: 200, headers, body });
const GET = (url: string) => ({ method: "GET", url });
const API = "https://api.example.com";

async function cacheClass(): Promise<any> {
	return need(await load("index.ts"), "ResponseCache", "index.ts");
}

function clock() {
	const c = { t: 1_000_000, now: () => c.t };
	return c;
}

const tests: [string, () => Promise<void>][] = [
	["caches_get", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0 });
		eq(c.get(GET(`${API}/v1/items`)), undefined, "get before put");
		eq(c.put(GET(`${API}/v1/items`), ok("[1]")), true, "put GET 200");
		eq(c.get(GET(`${API}/v1/items`))?.body, "[1]", "get after put");
		eq(c.get(GET(`${API}/v1/other`)), undefined, "get of another URL");
		const s = c.stats();
		eq([s.hits, s.misses, s.size], [1, 2, 1], "stats() [hits, misses, size]");
	}],
	["method_rules", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0 });
		eq(c.put({ method: "POST", url: `${API}/a` }, ok("p")), false, "put POST");
		eq(c.put({ method: "get", url: `${API}/a` }, ok("g")), true, "put lower-case get");
		eq(c.get({ method: "HEAD", url: `${API}/a` }), undefined, "HEAD after only GET was stored");
		eq(c.put({ method: "HEAD", url: `${API}/a` }, ok("")), true, "put HEAD");
		eq(c.get(GET(`${API}/a`))?.body, "g", "GET body after HEAD stored");
		eq(c.get({ method: "DELETE", url: `${API}/a` }), undefined, "get DELETE");
		const s = c.stats();
		eq([s.hits, s.misses, s.size], [1, 1, 2], "stats() [hits, misses, size]");
	}],
	["status_rules", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0 });
		eq(c.put(GET(`${API}/x`), { status: 404, headers: {}, body: "nope" }), false, "put 404");
		eq(c.put(GET(`${API}/x`), { status: 204, headers: {}, body: "" }), false, "put 204");
		eq(c.stats().size, 0, "size");
	}],
	["cache_control_rules", async () => {
		const RC = await cacheClass();
		const k = clock();
		const c = new RC({ now: k.now, defaultTtlMs: 5_000 });
		eq(c.put(GET(`${API}/ns`), ok("x", { "Cache-Control": "No-Store" })), false, "Cache-Control: No-Store");
		eq(c.put(GET(`${API}/pv`), ok("x", { "cache-control": "max-age=60, private" })), false, "cache-control: max-age=60, private");
		eq(c.put(GET(`${API}/z`), ok("x", { "Cache-Control": "max-age=0" })), false, "max-age=0");
		eq(c.put(GET(`${API}/ma`), ok("ma", { "CACHE-CONTROL": "public, Max-Age = 120" })), true, "Max-Age = 120");
		eq(c.put(GET(`${API}/sm`), ok("sm", { "Cache-Control": "max-age=600, s-maxage=30" })), true, "max-age=600, s-maxage=30");
		eq(c.put(GET(`${API}/bad`), ok("bad", { "Cache-Control": "max-age=soon" })), true, "max-age=soon");
		k.t += 29_999;
		eq(c.get(GET(`${API}/sm`))?.body, "sm", "s-maxage=30 entry at 29.999 s");
		eq(c.get(GET(`${API}/bad`)), undefined, "invalid max-age entry (default TTL 5 s) at 29.999 s");
		k.t += 1;
		eq(c.get(GET(`${API}/sm`)), undefined, "s-maxage=30 entry at 30 s");
		k.t += 89_999;
		eq(c.get(GET(`${API}/ma`))?.body, "ma", "Max-Age=120 entry at 119.999 s");
	}],
	["expiry", async () => {
		const RC = await cacheClass();
		const k = clock();
		const c = new RC({ now: k.now });
		c.put(GET(`${API}/d`), ok("d"));
		k.t += 59_999;
		eq(c.get(GET(`${API}/d`))?.body, "d", "default TTL entry at 59.999 s");
		k.t += 1;
		eq(c.get(GET(`${API}/d`)), undefined, "default TTL entry at 60 s");
		const s = c.stats();
		eq([s.hits, s.misses, s.size], [1, 1, 0], "stats() after expiry [hits, misses, size]");
	}],
	["key_normalization", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0 });
		c.put(GET("https://API.Example.com/v1/Items?b=2&a=1#top"), ok("items"));
		eq(c.get(GET("https://api.example.com/v1/Items?a=1&b=2"))?.body, "items", "same URL, other query order / host case / no fragment");
		eq(c.get(GET("https://api.example.com/v1/items?a=1&b=2")), undefined, "path with different case");
		eq(c.get(GET("https://api.example.com/v1/Items?a=1&b=3")), undefined, "different query value");
	}],
	["invalidate_prefix", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0 });
		c.put(GET(`${API}/v1/users/1`), ok("u1"));
		c.put({ method: "HEAD", url: `${API}/v1/users/2` }, ok(""));
		c.put(GET(`${API}/v1/orders/9`), ok("o9"));
		eq(c.invalidate(`${API}/v1/users`), 2, "invalidate(.../v1/users)");
		eq(c.get(GET(`${API}/v1/orders/9`))?.body, "o9", "unrelated entry after invalidate");
		eq(c.stats().size, 1, "size after invalidate");
	}],
	["returns_copy", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0 });
		const res = ok("orig", { "content-type": "text/plain" });
		c.put(GET(`${API}/c`), res);
		res.headers["content-type"] = "changed";
		const a = c.get(GET(`${API}/c`));
		a.body = "mutated";
		a.headers["x-extra"] = "1";
		const b = c.get(GET(`${API}/c`));
		eq([b.body, b.headers], ["orig", { "content-type": "text/plain" }], "second get after mutating input and first result");
	}],
	["module_cache_control", async () => {
		const rel = "cacheControl.ts";
		const parse = need(await load(rel), "parseCacheControl", rel);
		eq(parse(undefined), { noStore: false, private: false }, "parseCacheControl(undefined)");
		const a = parse("Public, MAX-AGE=300, s-maxage=20");
		eq([a.noStore, a.private, a.maxAge, a.sMaxAge], [false, false, 300, 20], "parseCacheControl('Public, MAX-AGE=300, s-maxage=20') [noStore, private, maxAge, sMaxAge]");
		const b = parse(" no-store , private, max-age=-1");
		eq([b.noStore, b.private, b.maxAge, b.sMaxAge], [true, true, undefined, undefined], "parseCacheControl(' no-store , private, max-age=-1') [noStore, private, maxAge, sMaxAge]");
	}],
	["module_key", async () => {
		const rel = "key.ts";
		const m = await load(rel);
		const cacheKey = need(m, "cacheKey", rel);
		const normalizeUrl = need(m, "normalizeUrl", rel);
		eq(normalizeUrl("HTTPS://Api.Example.com/v1/X?z=1&a=2&a=1#f"), "https://api.example.com/v1/X?a=2&a=1&z=1", "normalizeUrl");
		eq(cacheKey({ method: "head", url: "https://api.example.com/p?b=1&a=1" }), "HEAD https://api.example.com/p?a=1&b=1", "cacheKey");
	}],
	["module_lru", async () => {
		const rel = "lru.ts";
		const Lru = need(await load(rel), "LruStore", rel);
		const s = new Lru(3);
		eq([s.set("a", 1), s.set("b", 2), s.set("c", 3)], [undefined, undefined, undefined], "set a,b,c into LruStore(3)");
		eq(s.get("a"), 1, "get(a)");
		eq(s.has("b"), true, "has(b)");
		eq(s.keys(), ["b", "c", "a"], "keys() after get(a) and has(b)");
		eq(s.set("d", 4), "b", "set(d) evicts");
		eq(s.set("c", 30), undefined, "replacing c evicts");
		eq([s.keys(), s.size], [["a", "d", "c"], 3], "[keys(), size]");
		eq([s.delete("a"), s.delete("zz"), s.size], [true, false, 2], "[delete(a), delete(zz), size]");
		eq(s.get("zz"), undefined, "get(zz)");
		const big = new Lru();
		for (let i = 0; i < 1000; i++) big.set(`k${i}`, i);
		eq(big.size, 1000, "default LruStore size after 1000 sets");
		throwsRange(() => new Lru(0), "new LruStore(0)");
	}],
	["module_layout", async () => {
		const index = await load("index.ts");
		for (const name of ["ResponseCache", "LruStore", "parseCacheControl", "cacheKey", "normalizeUrl"]) need(index, name, "index.ts");
		const rc = need(await load("responseCache.ts"), "ResponseCache", "responseCache.ts");
		if (index.ResponseCache !== rc) fail("index.ts must re-export the ResponseCache of src/responseCache.ts");
		const legacy = need(await load("cache.ts"), "ResponseCache", "cache.ts");
		if (legacy !== rc) fail("src/cache.ts must re-export the ResponseCache of src/responseCache.ts, not define its own");
		if (index.LruStore !== need(await load("lru.ts"), "LruStore", "lru.ts")) fail("index.ts must re-export LruStore from src/lru.ts");
	}],
	["lru_eviction", async () => {
		const RC = await cacheClass();
		const c = new RC({ now: () => 0, maxEntries: 2 });
		c.put(GET(`${API}/a`), ok("a"));
		c.put(GET(`${API}/b`), ok("b"));
		eq(c.get(GET(`${API}/a`))?.body, "a", "get a");
		c.put(GET(`${API}/c`), ok("c"));
		eq(c.get(GET(`${API}/b`)), undefined, "b after a was read and c was added (maxEntries 2)");
		eq(c.get(GET(`${API}/a`))?.body, "a", "a after c was added");
		eq(c.get(GET(`${API}/c`))?.body, "c", "c after it was added");
		const s = c.stats();
		eq([s.size, s.evictions], [2, 1], "stats() [size, evictions]");
	}],
	["lru_replace_and_expiry", async () => {
		const RC = await cacheClass();
		const k = clock();
		const c = new RC({ now: k.now, maxEntries: 2 });
		c.put(GET(`${API}/a`), ok("a"));
		c.put(GET(`${API}/b`), ok("b", { "cache-control": "max-age=1" }));
		c.put(GET(`${API}/a`), ok("a2"));
		eq(c.stats().evictions, 0, "evictions after replacing an existing key");
		c.put(GET(`${API}/c`), ok("c"));
		eq(c.get(GET(`${API}/a`))?.body, "a2", "a (re-put most recently) after c was added");
		eq(c.get(GET(`${API}/b`)), undefined, "b after c was added");
		k.t += 120_000;
		eq(c.get(GET(`${API}/a`)), undefined, "a after expiry");
		c.invalidate(API);
		const s = c.stats();
		eq([s.size, s.evictions], [0, 1], "stats() after expiry + invalidate [size, evictions]");
	}],
	["max_entries_validation", async () => {
		const RC = await cacheClass();
		for (const bad of [0, -1, 1.5, Number.NaN]) throwsRange(() => new RC({ maxEntries: bad }), `maxEntries ${bad}`);
		const c = new RC({ now: () => 0, maxEntries: Number.POSITIVE_INFINITY });
		for (let i = 0; i < 300; i++) c.put(GET(`${API}/n/${i}`), ok(String(i)));
		const d = new RC({ now: () => 0 });
		for (let i = 0; i < 300; i++) d.put(GET(`${API}/n/${i}`), ok(String(i)));
		eq([c.stats().size, c.stats().evictions, d.stats().size], [300, 0, 300], "[size, evictions] with Infinity, size with default");
	}],
];

for (const [name, fn] of tests) {
	try {
		await fn();
		console.log(`TEST ${name} PASS`);
	} catch (e) {
		console.log(`TEST ${name} FAIL ${String((e as Error)?.message ?? e).split("\n")[0]}`);
	}
}
