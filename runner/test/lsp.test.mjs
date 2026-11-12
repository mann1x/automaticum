import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { createLspHost, languageIdFor, serverSpecs, workspaceLanguages } from "../lsp.mjs";

const fake = fileURLToPath(new URL("./fixtures/fake-lsp.mjs", import.meta.url));
const servers = [
	{ name: "fake", command: process.execPath, args: [fake], probe: fake, languages: ["javascript"], settings: { fake: { answer: 42 } }, sources: { fake: "fk" } },
	{ name: "missing", command: "/nonexistent/server", args: [], languages: ["python"] },
];
const settle = (ms = 300) => new Promise((r) => setTimeout(r, ms));

function workspace() {
	const dir = mkdtempSync(join(tmpdir(), "lsp-test-"));
	writeFileSync(join(dir, "a.js"), "let foo = 1;\nfoo += BAD;\n");
	writeFileSync(join(dir, "b.py"), "x = 1\n");
	return dir;
}

test("languages: the lane layers' file kinds map to a server", () => {
	for (const [file, lang] of [["a.html", "html"], ["a.cs", "csharp"], ["a.cpp", "cpp"], ["a.rs", "rust"], ["a.go", "go"], ["a.sh", "shellscript"], ["a.py", "python"], ["a.json", "json"]]) {
		assert.equal(languageIdFor(file), lang, file);
	}
	const specs = serverSpecs("/opt/automaticum/lsp", "/opt/automaticum/bin/node");
	for (const lang of ["typescript", "javascript", "html", "css", "json", "python", "shellscript", "go", "rust", "c", "cpp", "csharp"]) {
		assert.ok(specs.some((s) => s.languages.includes(lang)), lang);
	}
	// Every server lives under the runner's prefix, on no PATH (D113).
	for (const s of specs) assert.ok(s.probe.startsWith("/opt/automaticum/lsp/"), s.name);
	assert.ok(specs.filter((s) => s.command === "/opt/automaticum/bin/node").length >= 6);
});

test("workspaceLanguages skips dependency trees", () => {
	const dir = workspace();
	assert.deepEqual(workspaceLanguages(dir), ["javascript", "python"]);
});

test("the host answers ask_lsp's questions, answers the server's own requests and keeps its diagnostics", async (t) => {
	const dir = workspace();
	const host = createLspHost({ cwd: dir, servers });
	t.after(() => host.dispose());
	const file = join(dir, "a.js");

	assert.deepEqual(host.prewarm(), ["fake"]);
	assert.equal(host.serves(join(dir, "b.py")), false, "a language whose server is not installed answers nothing");

	const hover = await host.provider.hover({ filePath: file, line: 1, character: 0 });
	assert.match(hover, /^at 1:0 config=.* events=open$/);
	const defs = await host.provider.definitions({ filePath: file, line: 1, character: 0 });
	assert.deepEqual(defs, [{ filePath: file, line: 0, character: 4 }]);
	assert.deepEqual(await host.provider.findSymbolPosition(file, "foo"), { filePath: file, line: 0, character: 4 });

	await settle();
	let [d] = await host.readDiagnostics();
	assert.equal(d.filePath, file);
	assert.equal(d.pending, false);
	assert.deepEqual(d.diagnostics.map((x) => [x.severity, x.message, x.range.start.line, x.source]), [[1, "bad thing", 1, "fk"]], "the producer as the editor names it");

	// A changed file goes as didChange, version 2, never as a second didOpen,
	// and is pending until the server reports on it again.
	writeFileSync(file, "let foo = 1;\nfoo += WARN;\n");
	await host.touch(file, { save: true });
	await settle();
	[d] = await host.readDiagnostics();
	assert.deepEqual(d.diagnostics.map((x) => x.severity), [2]);
	const again = await host.provider.hover({ filePath: file, line: 0, character: 0 });
	// The server's workspace/configuration request was answered from the spec's settings.
	assert.match(again, /config=\[42\] events=open,change:2,save$/);

	const status = host.status();
	assert.deepEqual(status.find((s) => s.server === "fake"), { server: "fake", languages: ["javascript"], installed: true, started: true });
	assert.equal(status.find((s) => s.server === "missing").installed, false);
});

test("a server that cannot start answers nothing and says why", async (t) => {
	const dir = workspace();
	const errors = [];
	const broken = [{ name: "broken", command: process.execPath, args: ["-e", "process.exit(3)"], probe: process.execPath, languages: ["javascript"] }];
	const host = createLspHost({ cwd: dir, servers: broken, onError: (m) => errors.push(m) });
	t.after(() => host.dispose());
	assert.equal(await host.provider.hover({ filePath: join(dir, "a.js"), line: 0, character: 0 }), undefined);
	assert.ok(host.status()[0].failed);
	assert.ok(errors.some((m) => m.includes("broken")));
});
