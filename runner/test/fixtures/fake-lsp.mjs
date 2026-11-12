// A language server for the tests of lsp.mjs: it speaks LSP over stdio and
// answers what the runner asks. It asks the client for its configuration
// once initialized and publishes diagnostics only after the answer, as the
// HTML and Python servers do; a line holding BAD is an error, WARN a warning.
// Every document event it saw is echoed in a hover answer, so a test can
// tell a didChange from a second didOpen.
let buffer = Buffer.alloc(0);
let nextId = 1000;
let configured = null;
const docs = new Map();
const events = [];
const send = (m) => {
	const body = JSON.stringify({ jsonrpc: "2.0", ...m });
	process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
};
const publish = (uri) => {
	if (configured === null) return;
	const text = docs.get(uri) ?? "";
	const diagnostics = [];
	text.split("\n").forEach((line, i) => {
		const bad = line.indexOf("BAD");
		if (bad >= 0) diagnostics.push({ range: { start: { line: i, character: bad }, end: { line: i, character: bad + 3 } }, severity: 1, message: "bad thing", source: "fake" });
		const warn = line.indexOf("WARN");
		if (warn >= 0) diagnostics.push({ range: { start: { line: i, character: warn }, end: { line: i, character: warn + 4 } }, severity: 2, message: "warned", source: "fake" });
	});
	send({ method: "textDocument/publishDiagnostics", params: { uri, diagnostics } });
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
			return send({ id: m.id, result: { capabilities: { hoverProvider: true, definitionProvider: true, textDocumentSync: 1 } } });
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
