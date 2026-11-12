// A language server for the tests of lsp.mjs: it speaks LSP over stdio and
// answers what the runner asks. It asks the client for its configuration
// once initialized and publishes diagnostics only after the answer, as the
// HTML and Python servers do; a line holding BAD is an error, WARN a warning.
// Every document event it saw is echoed in a hover answer, so a test can
// tell a didChange from a second didOpen.
// FAKE_PULL=1 announces pull diagnostics and answers a pull only from a
// client that advertised them, refusing it as vscode-languageserver does
// otherwise; FAKE_PUSH_DELAY_MS delays every push (the HTML server's script
// validation comes about a second after the open).
const PULL = process.env.FAKE_PULL === "1";
const PUSH_DELAY_MS = Number(process.env.FAKE_PUSH_DELAY_MS ?? 0);
let clientPulls = false;
let buffer = Buffer.alloc(0);
let nextId = 1000;
let configured = null;
const docs = new Map();
const events = [];
const send = (m) => {
	const body = JSON.stringify({ jsonrpc: "2.0", ...m });
	process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
};
const diagnosticsOf = (uri) => {
	const text = docs.get(uri) ?? "";
	const diagnostics = [];
	text.split("\n").forEach((line, i) => {
		const bad = line.indexOf("BAD");
		if (bad >= 0) diagnostics.push({ range: { start: { line: i, character: bad }, end: { line: i, character: bad + 3 } }, severity: 1, message: "bad thing", source: "fake" });
		const warn = line.indexOf("WARN");
		if (warn >= 0) diagnostics.push({ range: { start: { line: i, character: warn }, end: { line: i, character: warn + 4 } }, severity: 2, message: "warned", source: "fake" });
	});
	return diagnostics;
};
const publish = (uri) => {
	if (configured === null) return;
	const push = () => send({ method: "textDocument/publishDiagnostics", params: { uri, diagnostics: diagnosticsOf(uri) } });
	if (PUSH_DELAY_MS > 0) setTimeout(push, PUSH_DELAY_MS);
	else push();
};
const handle = (m) => {
	if (m.id !== undefined && m.method === undefined) {
		if (m.id === 999) {
			configured = m.result;
			for (const uri of docs.keys()) publish(uri);
		}
		return;
	}
	switch (m.method) {
		case "initialize":
			clientPulls = m.params?.capabilities?.textDocument?.diagnostic !== undefined;
			return send({
				id: m.id,
				result: { capabilities: { hoverProvider: true, definitionProvider: true, textDocumentSync: 1, ...(PULL ? { diagnosticProvider: { interFileDependencies: false, workspaceDiagnostics: false } } : {}) } },
			});
		case "textDocument/diagnostic":
			if (!PULL || !clientPulls) return send({ id: m.id, error: { code: -32601, message: "Unhandled method textDocument/diagnostic" } });
			events.push("pull");
			return send({ id: m.id, result: { kind: "full", items: diagnosticsOf(m.params.textDocument.uri) } });
		case "initialized":
			return send({ id: 999, method: "workspace/configuration", params: { items: [{ section: "fake.answer" }] } });
		case "textDocument/didOpen":
			events.push("open");
			docs.set(m.params.textDocument.uri, m.params.textDocument.text);
			return publish(m.params.textDocument.uri);
		case "textDocument/didChange":
			events.push(`change:${m.params.textDocument.version}`);
			docs.set(m.params.textDocument.uri, m.params.contentChanges[0].text);
			return publish(m.params.textDocument.uri);
		case "textDocument/didSave":
			events.push("save");
			return;
		case "textDocument/hover":
			return send({ id: m.id, result: { contents: { kind: "plaintext", value: `at ${m.params.position.line}:${m.params.position.character} config=${JSON.stringify(configured)} events=${events.join(",")}` } } });
		case "textDocument/definition":
			return send({ id: m.id, result: [{ uri: m.params.textDocument.uri, range: { start: { line: 0, character: 4 }, end: { line: 0, character: 7 } } }] });
		case "shutdown":
			return send({ id: m.id, result: null });
		case "exit":
			return process.exit(0);
		default:
			if (m.id !== undefined) send({ id: m.id, result: null });
	}
};
process.stdin.on("data", (chunk) => {
	buffer = Buffer.concat([buffer, chunk]);
	for (;;) {
		const h = buffer.indexOf("\r\n\r\n");
		if (h < 0) return;
		const len = Number(/content-length:\s*(\d+)/i.exec(buffer.subarray(0, h).toString())[1]);
		if (buffer.length < h + 4 + len) return;
		const body = buffer.subarray(h + 4, h + 4 + len).toString();
		buffer = buffer.subarray(h + 4 + len);
		handle(JSON.parse(body));
	}
});
