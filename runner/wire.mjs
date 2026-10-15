// The wire capture: a fetch that records every request and response whole,
// with timing, and changes neither.
//
// The SDK passes options that carry more than the standard ones (a
// dispatcher with its timeouts switched off), so `init` is handed on
// untouched. The response is read through a clone, so the SDK gets the
// original object and the stream it expects.

import { createHash } from "node:crypto";
import { createWriteStream } from "node:fs";
import { once } from "node:events";

const SECRET = /^(authorization|proxy-authorization|cookie|set-cookie|x-api-key|api-key)$/i;

function headersOf(h) {
	const out = {};
	if (!h) return out;
	const each = (k, v) => {
		out[String(k).toLowerCase()] = SECRET.test(k) ? "[set]" : String(v);
	};
	if (typeof h.forEach === "function" && !Array.isArray(h)) h.forEach((v, k) => each(k, v));
	else if (Array.isArray(h)) for (const [k, v] of h) each(k, v);
	else for (const [k, v] of Object.entries(h)) each(k, v);
	return out;
}

// offersTools says whether a request body is a chat request that offers
// tools: a request of the agent's loop, as ingest counts them. The SDK's
// probes and lookups offer none.
export function offersTools(text) {
	try {
		const tools = JSON.parse(text ?? "")?.tools;
		return Array.isArray(tools) && tools.length > 0;
	} catch {
		return false;
	}
}

// bodyOf returns a request body as text when it can be read without
// consuming it, and says what it was otherwise.
function bodyOf(body) {
	if (body === undefined || body === null) return { text: null };
	if (typeof body === "string") return { text: body };
	if (body instanceof URLSearchParams) return { text: body.toString() };
	if (body instanceof ArrayBuffer) return { text: Buffer.from(body).toString("utf8") };
	if (ArrayBuffer.isView(body)) return { text: Buffer.from(body.buffer, body.byteOffset, body.byteLength).toString("utf8") };
	return { text: null, not_captured: `a ${body?.constructor?.name ?? typeof body} cannot be read without consuming it` };
}

/**
 * createWire returns a fetch that appends one JSON line per request to
 * `file`, and the means to finish.
 *
 *   seq, method, url, request_headers, request_body, request_bytes
 *   t_start, t_headers, t_end (ISO), ms_headers, ms_total
 *   status, response_headers, response_body, response_bytes, response_sha256
 *   error (when the request or the reading of the response failed)
 */
export function createWire({ file, fetch: inner = globalThis.fetch, now = () => new Date() }) {
	const out = createWriteStream(file, { flags: "a" });
	const pending = new Set();
	let seq = 0;
	// answered counts the agent's requests the server answered (2xx).
	let answered = 0;

	const write = (record) =>
		new Promise((resolve, reject) => out.write(JSON.stringify(record) + "\n", (err) => (err ? reject(err) : resolve())));

	async function wire(input, init) {
		const n = ++seq;
		const isRequest = typeof input === "object" && input !== null && "url" in input && !(input instanceof URL);
		const started = now();
		const body = bodyOf(init?.body);
		const record = {
			seq: n,
			method: String(init?.method ?? (isRequest ? input.method : "GET")).toUpperCase(),
			url: isRequest ? input.url : String(input),
			request_headers: { ...(isRequest ? headersOf(input.headers) : {}), ...headersOf(init?.headers) },
			request_body: body.text,
			request_bytes: body.text === null ? null : Buffer.byteLength(body.text),
			t_start: started.toISOString(),
		};
		if (body.not_captured) record.request_body_not_captured = body.not_captured;

		let response;
		try {
			response = await inner(input, init);
		} catch (err) {
			record.t_end = now().toISOString();
			record.ms_total = now() - started;
			record.error = describe(err);
			const done = write(record);
			pending.add(done);
			done.finally(() => pending.delete(done));
			throw err;
		}
		const atHeaders = now();
		record.t_headers = atHeaders.toISOString();
		record.ms_headers = atHeaders - started;
		record.status = response.status;
		if (response.status >= 200 && response.status < 300 && offersTools(body.text)) answered++;
		record.response_headers = headersOf(response.headers);

		// Read the copy to its end, however long the stream lasts.
		let copy;
		try {
			copy = response.clone();
		} catch (err) {
			record.error = "the response could not be copied: " + describe(err).message;
		}
		const done = (async () => {
			if (copy) {
				const hash = createHash("sha256");
				const parts = [];
				let bytes = 0;
				try {
					if (copy.body) {
						for await (const chunk of copy.body) {
							const buf = Buffer.from(chunk);
							hash.update(buf);
							parts.push(buf);
							bytes += buf.length;
						}
					}
				} catch (err) {
					record.error = describe(err);
				}
				record.response_body = Buffer.concat(parts).toString("utf8");
				record.response_bytes = bytes;
				record.response_sha256 = hash.digest("hex");
			}
			const ended = now();
			record.t_end = ended.toISOString();
			record.ms_total = ended - started;
			await write(record);
		})();
		pending.add(done);
		done.finally(() => pending.delete(done)).catch(() => {});
		return response;
	}

	return {
		fetch: wire,
		count: () => seq,
		answered: () => answered,
		// close waits for the responses still being read, up to graceMs, and
		// closes the file. It returns how many were cut short.
		async close(graceMs = 5000) {
			const all = Promise.allSettled([...pending]);
			let timer;
			const timeout = new Promise((resolve) => {
				timer = setTimeout(resolve, graceMs);
			});
			await Promise.race([all, timeout]);
			clearTimeout(timer);
			const cut = pending.size;
			out.end();
			await once(out, "finish");
			return cut;
		},
	};
}

// describe makes an error something JSON can hold: an Error serialises to {}.
export function describe(err) {
	if (!(err instanceof Error)) return { message: String(err) };
	const out = { name: err.name, message: err.message };
	if (err.code !== undefined) out.code = err.code;
	if (err.status !== undefined) out.status = err.status;
	if (err.stack) out.stack = err.stack;
	if (err.cause !== undefined) out.cause = describe(err.cause);
	return out;
}
