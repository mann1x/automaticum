import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
	CHECK_FILE_TOOL_NAME,
	createCheckFileTool,
	createEditorDiagnosticsHooks,
	formatIntroducedDiagnostics,
	readSettledDiagnostics,
	readTargetPaths,
	singleFileDiagnosticsToProblemsString,
} from "../editor.mjs";

const diag = (line, character, severity, message, source = "ts") => ({
	range: { start: { line, character }, end: { line, character: character + 1 } },
	severity,
	message,
	source,
});
const instant = async () => {};

/** A language-server host whose diagnostics the test sets. */
function fakeLsp(dir) {
	const state = new Map();
	const touched = [];
	return {
		state,
		touched,
		serves: (p) => p.endsWith(".js") || p.endsWith(".html"),
		touch: async (p, o = {}) => {
			touched.push([p, o.save === true]);
			return {};
		},
		readDiagnostics: async () => [...state.entries()].map(([filePath, diagnostics]) => ({ filePath, diagnostics, pending: false })),
	};
}

test("readTargetPaths: editor names its file, apply_patch its headers", () => {
	assert.deepEqual(readTargetPaths("editor", { path: "a.js" }), ["a.js"]);
	assert.deepEqual(readTargetPaths("editor", { file_path: "b.js", path: "a.js" }), ["b.js"]);
	assert.deepEqual(readTargetPaths("apply_patch", { input: "*** Begin Patch\n*** Update File: x.ts\n*** Add File: y.ts\n" }), ["x.ts", "y.ts"]);
	assert.deepEqual(readTargetPaths("run_commands", { commands: ["ls"] }), []);
});

test("problems are rendered as the extension renders them, with the column and the root cause first", async () => {
	const dir = mkdtempSync(join(tmpdir(), "ed-"));
	const file = join(dir, "page.html");
	writeFileSync(file, "<script>\nlet a = {\n</script>\n");
	const out = await singleFileDiagnosticsToProblemsString(dir, file, [
		diag(1, 8, 1, "',' expected."),
		diag(1, 9, 1, "':' expected."),
		diag(3, 0, 1, "'}' expected."),
	]);
	assert.equal(
		out,
		[
			"page.html",
			"- Start here: '}' expected at the end of the file (after line 3, its last; the editor numbers the empty line after the final line break 4), which for the script is its `</script>` on line 3. An unclosed delimiter is reported where the parser gave up, so the delimiter itself belongs somewhere above that point. The other 2 errors below are usually cascade from it and clear on their own once it is fixed — fix this one first and re-check before editing the rest.",
			"- [ts Error] Line 2, column 9: ',' expected.",
			"- [ts Error] Line 2, column 10: ':' expected.",
			"- [ts Error] The end of the file (after line 3, its last; the editor numbers the empty line after the final line break 4), which for the script is its `</script>` on line 3: '}' expected.",
		].join("\n"),
	);
});

test("only what the edit introduced is reported, errors and warnings only", async () => {
	const dir = mkdtempSync(join(tmpdir(), "ed-"));
	const file = join(dir, "a.js");
	writeFileSync(file, "x\ny\n");
	const before = [{ filePath: file, diagnostics: [diag(0, 0, 1, "old")] }];
	const after = [{ filePath: file, diagnostics: [diag(0, 0, 1, "old"), diag(1, 0, 2, "new"), diag(1, 0, 4, "hint")] }];
	const block = await formatIntroducedDiagnostics(dir, before, after);
	assert.equal(
		block,
		"<editor_diagnostics>\nThe editor reported these problems on the file(s) this edit touched. They were not present before it.\na.js\n- [ts Warning] Line 2, column 1: new\n</editor_diagnostics>",
	);
	assert.equal(await formatIntroducedDiagnostics(dir, after, after), "");
});

test("settling waits while a file is pending and returns once nothing changes", async () => {
	let reads = 0;
	const read = async () => {
		reads++;
		return [{ filePath: "/w/a.js", diagnostics: [], pending: reads < 3 }];
	};
	const got = await readSettledDiagnostics(new Set(["/w/a.js"]), read, instant, { timeoutMs: 1000 });
	assert.equal(got[0].pending, false);
	assert.equal(reads, 4, "two pending reads, then two equal ones");
});

test("after an edit the new diagnostics are appended, with the delimiter scan, and a regression is marked", async () => {
	const dir = mkdtempSync(join(tmpdir(), "ed-"));
	const file = join(dir, "a.js");
	writeFileSync(file, "let a = {\n");
	const lsp = fakeLsp(dir);
	lsp.state.set(file, [diag(0, 0, 2, "was there")]);
	const notes = [];
	const hooks = createEditorDiagnosticsHooks({
		cwd: dir,
		lsp,
		describeDelimiterBalance: (p) => `Delimiter scan: ${p.endsWith("a.js") ? "1 more {" : ""}`,
		note: (what, f) => notes.push([what, f]),
		delay: instant,
	});
	const call = { toolCall: { toolName: "editor", toolCallId: "c1" }, input: { path: "a.js", command: "str_replace" } };
	await hooks.beforeTool(call);
	lsp.state.set(file, [diag(0, 0, 2, "was there"), diag(0, 8, 1, "'}' expected.")]);
	const res = await hooks.afterTool({ ...call, result: { isError: false, output: { result: "edited" } } });
	assert.deepEqual(lsp.touched, [[file, false], [file, true]], "handed over before, and as written after");
	assert.equal(res.result.output.regressed, true);
	assert.match(res.result.output.result, /^edited\n\n<editor_diagnostics>\n.*\na\.js\n- \[ts Error\] Line 1, column 9: '}' expected\.\n<\/editor_diagnostics>\n\nDelimiter scan: 1 more \{$/s);
	assert.deepEqual(notes, [["editor_diagnostics", { tool: "editor", targets: ["a.js"], before: 1, after: 2, block_chars: 194, balance_chars: 24, regressed: true }]]);

	// A failed edit, a tool that writes nothing and a file no server reads add nothing.
	await hooks.beforeTool({ toolCall: { toolName: "editor", toolCallId: "c2" }, input: { path: "a.js" } });
	assert.equal(await hooks.afterTool({ toolCall: { toolName: "editor", toolCallId: "c2" }, result: { isError: true, output: "no" } }), undefined);
	assert.equal(await hooks.beforeTool({ toolCall: { toolName: "read_files", toolCallId: "c3" }, input: { path: "a.js" } }), undefined);
	await hooks.beforeTool({ toolCall: { toolName: "editor", toolCallId: "c4" }, input: { path: "a.png" } });
	assert.equal(await hooks.afterTool({ toolCall: { toolName: "editor", toolCallId: "c4" }, result: { isError: false, output: "ok" } }), undefined);
});

test("check_file reads the servers' diagnostics and says when nothing could check a file", async () => {
	const dir = mkdtempSync(join(tmpdir(), "ed-"));
	writeFileSync(join(dir, "a.js"), "let a = 1\n");
	writeFileSync(join(dir, "b.js"), "ok\n");
	writeFileSync(join(dir, "notes.txt"), "text\n");
	const lsp = fakeLsp(dir);
	lsp.state.set(join(dir, "a.js"), [diag(0, 4, 1, "Cannot redeclare 'a'.")]);
	const tool = createCheckFileTool({ createTool: (t) => t, describeDelimiterBalance: () => null, cwd: dir, lsp, delay: instant });
	assert.equal(tool.name, CHECK_FILE_TOOL_NAME);
	assert.match(tool.description, /^Check files for errors and warnings, using the editor's own language servers \(LSP\)\. \*\*This is the linter\.\*\*/);
	const out = await tool.execute({ paths: ["a.js", "b.js", "gone.js", "notes.txt"] });
	assert.equal(
		out,
		[
			"a.js\n- [ts Error] Line 1, column 5: Cannot redeclare 'a'.",
			"b.js: no problems reported by the editor. If no language server handles this file type, that is not the same as clean.",
			"gone.js: no such file, so there is nothing to check.",
			"notes.txt: no problems reported by the editor. If no language server handles this file type, that is not the same as clean.",
		].join("\n\n"),
	);
	assert.equal(await tool.execute({}), "No files were named. Pass the paths to check in `paths`.");
	assert.equal(await tool.execute({ paths: "b.js" }), "b.js: no problems reported by the editor. If no language server handles this file type, that is not the same as clean.");
});
