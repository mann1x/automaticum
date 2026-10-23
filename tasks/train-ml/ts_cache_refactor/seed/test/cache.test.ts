import assert from "node:assert/strict";
import { test } from "node:test";
import { ResponseCache } from "../src/index.ts";

const ok = (body: string, headers: Record<string, string> = {}) => ({ status: 200, headers, body });

test("serves a stored GET", () => {
	const cache = new ResponseCache({ now: () => 0 });
	assert.equal(cache.put({ method: "GET", url: "https://api.example.com/v1/items" }, ok("[]")), true);
	assert.equal(cache.get({ method: "GET", url: "https://api.example.com/v1/items" })?.body, "[]");
	assert.deepEqual(cache.stats(), { hits: 1, misses: 0, size: 1 });
});

test("does not store POST", () => {
	const cache = new ResponseCache();
	assert.equal(cache.put({ method: "POST", url: "https://api.example.com/v1/items" }, ok("{}")), false);
});

test("expires after max-age", () => {
	let t = 0;
	const cache = new ResponseCache({ now: () => t });
	cache.put({ method: "GET", url: "https://api.example.com/a" }, ok("a", { "Cache-Control": "max-age=10" }));
	t = 9_999;
	assert.ok(cache.get({ method: "GET", url: "https://api.example.com/a" }));
	t = 10_000;
	assert.equal(cache.get({ method: "GET", url: "https://api.example.com/a" }), undefined);
});
