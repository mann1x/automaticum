import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { buildBrowserDrivers, buildLsp, noteTrace, sdkHasChrome, sdkHasLsp } from "../host.mjs";

// An SDK build as 4.100.277 exports it: every factory records what it was given.
function fakeSdk277() {
	const calls = {};
	const record = (name, value) => (options) => {
		calls[name] = options;
		return value(options);
	};
	const host = {
		provider: { name: "provider" },
		readDiagnostics: async () => [],
		touch: async () => ({ languageId: "typescript" }),
		serves: () => true,
		prewarm: () => [],
		status: () => [],
	};
	const sdk = {
		createLspHost: record("createLspHost", () => host),
		createDiagnosticsCheckFileTool: record("createDiagnosticsCheckFileTool", () => ({ name: "check_file" })),
		createEditorDiagnosticsHooks: record("createEditorDiagnosticsHooks", () => ({ beforeTool() {}, afterTool() {} })),
		createAskLspTool: record("createAskLspTool", () => ({ name: "ask_lsp" })),
		createChromeBrowserDriver: record("createChromeBrowserDriver", () => ({ kind: "browser" })),
		createChromeRenderDriver: record("createChromeRenderDriver", () => ({ kind: "render" })),
		createTool: (t) => t,
		describeDelimiterBalance: () => "",
		RENDER_ROOT_ID: "root",
	};
	return { sdk, calls, host };
}

test("a build without the exports is not taken for one with them", () => {
	assert.equal(sdkHasLsp({ createAskLspTool() {}, createTool() {} }), false);
	assert.equal(sdkHasChrome({ createBrowserTool() {} }), false);
	const { sdk } = fakeSdk277();
	assert.equal(sdkHasLsp(sdk), true);
	assert.equal(sdkHasChrome(sdk), true);
	assert.equal(sdkHasLsp({ ...sdk, createEditorDiagnosticsHooks: undefined }), false);
});

test("4.100.277: the SDK's host on the runner's servers, its check_file and hooks on the host", () => {
	const { sdk, calls, host } = fakeSdk277();
	const root = mkdtempSync(join(tmpdir(), "host-lsp-"));
	mkdirSync(join(root, "bin"), { recursive: true });
	writeFileSync(join(root, "bin", "gopls"), "");
	const built = buildLsp({ sdk, cwd: "/workspace", root, node: "/opt/automaticum/bin/node" });
	assert.equal(built.source, "sdk");
	assert.equal(built.host, host);
	assert.deepEqual(
		built.tools.map((t) => t.name),
		["ask_lsp", "check_file"],
	);
	const opts = calls.createLspHost;
	assert.equal(opts.cwd, "/workspace");
	const gopls = opts.servers.find((s) => s.name === "gopls");
	const pyright = opts.servers.find((s) => s.name === "pyright");
	assert.equal(gopls.command, join(root, "bin", "gopls"));
	assert.equal(opts.isInstalled(gopls), true, "a probe under the prefix that exists");
	assert.equal(opts.isInstalled(pyright), false, "a probe that does not exist");
	assert.equal(opts.isInstalled({ command: "sh", args: [], languages: [] }), false, "no probe: never looked up on a PATH");
	assert.equal(calls.createAskLspTool.provider, host.provider);
	const check = calls.createDiagnosticsCheckFileTool;
	assert.equal(check.readDiagnostics, host.readDiagnostics);
	assert.equal(check.touch, host.touch);
	assert.equal(check.serves, host.serves);
	const hooks = calls.createEditorDiagnosticsHooks;
	assert.equal(hooks.touch, host.touch);
	assert.equal(typeof hooks.trace, "function");
	assert.ok(built.hooks);
});

test("4.100.277 with diagnostics_after_edits = false: no hooks", () => {
	const { sdk, calls } = fakeSdk277();
	const built = buildLsp({ sdk, cwd: "/workspace", diagnosticsAfterEdits: false });
	assert.equal(built.hooks, undefined);
	assert.equal(calls.createEditorDiagnosticsHooks, undefined);
});

test("the SDK's errors are noted as the runner's: lsp_error capped at 20, the hooks' by phase", () => {
	const { sdk, calls } = fakeSdk277();
	const notes = [];
	buildLsp({ sdk, cwd: "/workspace", note: (kind, body) => notes.push([kind, body]) });
	for (let i = 0; i < 25; i++) calls.createLspHost.onError("[lsp] failed", new Error(`e${i}`));
	assert.equal(notes.filter(([k]) => k === "lsp_error").length, 20);
	calls.createEditorDiagnosticsHooks.onError("[EditorDiagnostics] failed to snapshot diagnostics:", new Error("x"));
	calls.createEditorDiagnosticsHooks.onError("[EditorDiagnostics] failed to report diagnostics:", new Error("y"));
	const failed = notes.filter(([k]) => k === "editor_diagnostics_failed").map(([, b]) => b);
	assert.deepEqual(failed, [
		{ phase: "before", error: "x" },
		{ phase: "after", error: "y" },
	]);
});

test("the SDK hooks' trace: only the after phase, as the runner's editor_diagnostics note", () => {
	const notes = [];
	const note = (kind, body) => notes.push([kind, body]);
	noteTrace(note, "/workspace", { phase: "before", tool: "editor", targets: ["/workspace/a.ts"], matched: 1 });
	noteTrace(note, "/workspace", {
		phase: "after",
		tool: "editor",
		targets: ["/workspace/src/a.ts"],
		before: 1,
		after: 3,
		blockChars: 120,
		balanceChars: 0,
		regressed: true,
	});
	assert.deepEqual(notes, [
		[
			"editor_diagnostics",
			{ tool: "editor", targets: ["src/a.ts"], before: 1, after: 3, block_chars: 120, balance_chars: 0, regressed: true },
		],
	]);
});

test("an older build: the runner's ports", () => {
	const { sdk } = fakeSdk277();
	const older = { ...sdk, createLspHost: undefined, createChromeBrowserDriver: undefined };
	const root = mkdtempSync(join(tmpdir(), "host-old-"));
	const built = buildLsp({ sdk: older, cwd: root, root });
	assert.equal(built.source, "runner");
	assert.deepEqual(
		built.tools.map((t) => t.name),
		["ask_lsp", "check_file"],
	);
	assert.equal(buildBrowserDrivers({ sdk: older, root }).source, "runner");
});

test("4.100.277: the SDK's Chrome drivers on the lane's Chrome, Mermaid and puppeteer-core", async () => {
	const { sdk, calls } = fakeSdk277();
	const root = mkdtempSync(join(tmpdir(), "host-chrome-"));
	mkdirSync(join(root, "chrome-linux64"), { recursive: true });
	writeFileSync(join(root, "chrome-linux64", "chrome"), "");
	mkdirSync(join(root, "node_modules", "mermaid", "dist"), { recursive: true });
	writeFileSync(join(root, "node_modules", "mermaid", "dist", "mermaid.min.js"), "");
	mkdirSync(join(root, "node_modules", "puppeteer-core"), { recursive: true });
	writeFileSync(join(root, "node_modules", "puppeteer-core", "index.js"), "module.exports = { launch() { return 'launched'; } };");
	writeFileSync(join(root, "node_modules", "puppeteer-core", "package.json"), '{"name":"puppeteer-core","main":"index.js"}');
	writeFileSync(join(root, "package.json"), "{}");
	const drivers = buildBrowserDrivers({ sdk, root });
	assert.equal(drivers.source, "sdk");
	assert.deepEqual(drivers.createDriver(), { kind: "browser" });
	assert.deepEqual(drivers.createRenderDriver(), { kind: "render" });
	const b = calls.createChromeBrowserDriver;
	assert.equal(b.chromePath, join(root, "chrome-linux64", "chrome"));
	assert.equal(b.args, undefined, "the SDK's default arguments");
	assert.equal((await b.loadPuppeteer()).launch(), "launched");
	const r = calls.createChromeRenderDriver;
	assert.equal(r.executablePath, join(root, "chrome-linux64", "chrome"));
	assert.equal(r.mermaidScriptPath, join(root, "node_modules", "mermaid", "dist", "mermaid.min.js"));
});
