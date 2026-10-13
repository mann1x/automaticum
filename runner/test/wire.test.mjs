import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { createWire, describe } from "../wire.mjs";

function serve(handler) {
	return new Promise((resolve) => {
		const srv = createServer(handler);
		srv.listen(0, "127.0.0.1", () => resolve({ srv, url: `http://127.0.0.1:${srv.address().port}` }));
	});
}

const lines = (file) => readFileSync(file, "utf8").trim().split("\n").map((l) => JSON.parse(l));

test("a streamed response is recorded whole and reaches the caller unchanged", async () => {
	// Pieces that cut a multi-byte character in two, sent with pauses.
	const whole = Buffer.from('{"message":{"content":"café ☃"}}\n{"done":true}\n');
	const pieces = [whole.subarray(0, 27), whole.subarray(27, 28), whole.subarray(28)];
	const { srv, url } = await serve(async (req, res) => {
		let body = "";
		for await (const c of req) body += c;
		res.writeHead(200, { "content-type": "application/x-ndjson", "x-echo": String(body.length) });
		for (const p of pieces) {
			res.write(p);
			await new Promise((r) => setTimeout(r, 15));
		}
		res.end();
	});
	const file = join(mkdtempSync(join(tmpdir(), "wire-")), "wire.jsonl");
	const seen = [];
	const wire = createWire({ file, fetch: (input, init) => (seen.push(init), fetch(input, init)) });
	const init = { method: "POST", headers: { "content-type": "application/json", Authorization: "Bearer secret" }, body: '{"model":"m","messages":[]}', dispatcher: undefined, marker: Symbol("kept") };
	const response = await wire.fetch(url + "/api/chat", init);
	assert.equal(seen[0], init, "init must be handed on as it came");
	const got = Buffer.from(await response.arrayBuffer());
	assert.deepEqual(got, whole);
	assert.equal(await wire.close(), 0);
	srv.close();

	const [rec] = lines(file);
	assert.equal(rec.seq, 1);
	assert.equal(rec.method, "POST");
	assert.ok(rec.url.endsWith("/api/chat"));
	assert.equal(rec.request_body, init.body);
	assert.equal(rec.request_headers.authorization, "[set]");
	assert.equal(rec.status, 200);
	assert.equal(rec.response_headers["x-echo"], String(init.body.length));
	assert.equal(rec.response_body, whole.toString("utf8"));
	assert.equal(rec.response_bytes, whole.length);
	assert.equal(rec.response_sha256, createHash("sha256").update(whole).digest("hex"));
	assert.ok(rec.ms_headers >= 0 && rec.ms_total >= rec.ms_headers && rec.ms_total >= 25);
});

test("a failed request is recorded and still fails; an error status is a response", async () => {
	const { srv, url } = await serve((req, res) => {
		res.writeHead(503, { "content-type": "application/json" });
		res.end('{"error":"busy"}');
	});
	const file = join(mkdtempSync(join(tmpdir(), "wire-")), "wire.jsonl");
	const wire = createWire({ file });
	const r = await wire.fetch(url + "/api/chat", { method: "POST", body: Buffer.from("{}") });
	assert.equal(r.status, 503);
	assert.equal(await r.text(), '{"error":"busy"}');
	srv.close();
	await assert.rejects(wire.fetch("http://127.0.0.1:1/nothing"));
	// A response the caller never reads is still recorded.
	assert.equal(wire.count(), 2);
	assert.equal(await wire.close(), 0);
	const recs = lines(file).sort((a, b) => a.seq - b.seq);
	assert.equal(recs[0].status, 503);
	assert.equal(recs[0].request_body, "{}");
	assert.equal(recs[0].response_body, '{"error":"busy"}');
	assert.ok(recs[1].error.message, "the failure is described");
	assert.equal(recs[1].status, undefined);
});

test("an Error is described, not serialised to {}", () => {
	const d = describe(new Error("outer", { cause: new TypeError("inner") }));
	assert.equal(d.message, "outer");
	assert.equal(d.cause.name, "TypeError");
	assert.equal(JSON.parse(JSON.stringify(d)).cause.message, "inner");
	assert.deepEqual(describe("text"), { message: "text" });
});
