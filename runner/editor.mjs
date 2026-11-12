// The extension's two uses of the editor's diagnostics (040 §6.1, D149),
// read from the session's language servers (lsp.mjs) instead of VS Code:
//
//   - its own `check_file` tool, which replaces the SDK's syntax check by
//     name, as the extension's does (the SDK adds its own only when the host
//     names none);
//   - the diagnostics after an edit: after `editor` and `apply_patch`, what
//     the edit introduced is appended to the tool result, with the delimiter
//     scan, and the result is marked regressed when the errors and warnings
//     went up (the SDK's loop tracker reads that mark).
//
// Ported from the extension at SDK release 4.100.276:
// apps/vscode/src/sdk/check-file-tool.ts, sdk/editor-diagnostics.ts and
// integrations/diagnostics/index.ts. The texts the model reads are the
// extension's, word for word; the budgets are its own. Where the editor's
// copy of a file could lag the disk the runner has no second copy: a file is
// handed to its server as it is on disk before every read.
//
// Nothing here imports the SDK: the functions the extension imports from it
// (createTool, describeDelimiterBalance) are passed in.

import { readFile } from "node:fs/promises";
import { extname, relative, resolve } from "node:path";

/** LSP severities. */
const ERROR = 1;
const WARNING = 2;
const REPORTED_SEVERITIES = [ERROR, WARNING];

const FILE_WRITING_TOOLS = new Set(["editor", "apply_patch"]);

const OPAQUE_EXTENSIONS = new Set([
	".7z", ".aac", ".avi", ".bin", ".bmp", ".class", ".dll", ".dylib", ".eot", ".exe", ".flac", ".gif", ".gz",
	".ico", ".jar", ".jpeg", ".jpg", ".mov", ".mp3", ".mp4", ".ogg", ".otf", ".pdf", ".png", ".so", ".tar",
	".ttf", ".wasm", ".wav", ".webm", ".webp", ".woff", ".woff2", ".zip",
]);

export const MAX_DIAGNOSTICS_PER_FILE = 20;
export const SETTLE_POLL_MS = 250;
export const SETTLE_TIMEOUT_MS = 2_000;

export const CHECK_FILE_TOOL_NAME = "check_file";

export const CHECK_FILE_TOOL_DESCRIPTION = `Check files for errors and warnings, using the editor's own language servers (LSP). **This is the linter.** It is also the type checker, the syntax check, and the source of the problems that would show in a Problems panel — whichever of those words the question uses, this is the tool that answers it. The results are live and follow your edits: one is current as of the moment you ask, so if it still reports a problem after an edit, the problem is still there. Restarting a language server is neither possible nor necessary from here.

Ask this before running a checker yourself. For a file whose language a language server covers, it answers the same question as \`tsc\`, \`eslint\`, \`biome\`, \`ruff\`, \`mypy\`, \`go build\` or \`cargo check\` would — for the files you name, in milliseconds, without building the project.

When to call it:
- Whenever the question is about the linter, lint errors, diagnostics, problems, warnings, type errors, syntax errors or compile errors — "how many errors is the linter reporting?", "is it clean now?", "what is still broken?". You have no other way to know, and the report you were shown after an earlier edit does not answer it: that was true then, and you have edited since.
- After editing a file, to confirm the edit is valid before moving on.
- Before reporting a task finished, on every file you changed.
- On a file you are about to change, when you want to know what was already wrong with it.

Pass every file you want checked in one call.

Read a clean result carefully. "No problems reported by the editor" is conclusive only where a language server covers that file, and it does not for every language on every machine. If this reports nothing and you have reason to expect a problem, or the project has a checker the language servers do not run, run that checker with \`run_commands\`. Tests and builds are always \`run_commands\`; this tool does not run them.

Output: plain text, one section per file you named, each problem on its own line as \`file:line:column\` with its severity and message. A file with nothing wrong says so in one line. Problems being listed is this tool working, not failing.

A parse error is reported where the parser gave up. That is where the code stopped making sense to it, which is often but not always where the mistake is: a bracket left open earlier makes it give up later. When a file's brackets do not match, a \`Delimiter scan\` section adds the lines where they stop pairing up and how many each line is out by, one line per place the trouble starts — read both before you edit, and work through every line listed before re-checking. A count says where brackets stop pairing, not which bracket is wrong, so read the code at those lines rather than editing them blind. The scan reads the whole file from line 1, and names each bracket by its line, its column and the text just before it: find it by that text rather than by counting columns. Do not count brackets yourself — the scan skips strings, comments and regex literals, which counting characters does not. It runs whether or not the editor reported anything, so it can appear beneath a file the editor called clean — no language server checks the script inside an \`.html\` file, and there this is the only report you will get.`;

export const CHECK_FILE_TOOL_INPUT_SCHEMA = {
	type: "object",
	properties: {
		paths: {
			type: "array",
			items: { type: "string" },
			description: "Files to check. Absolute, or relative to the working directory. Pass all of them in one call.",
		},
	},
	required: ["paths"],
};

const MAX_FILES_PER_CALL = 20;
const CHECK_SETTLE_BUDGET = { pollMs: 150, timeoutMs: 1_500 };

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const toPosix = (p) => p.replace(/\\/g, "/");

export function isLintableFile(filePath) {
	return !OPAQUE_EXTENSIONS.has(extname(filePath).toLowerCase());
}

const isReportable = (d) => REPORTED_SEVERITIES.includes(d.severity);

function filterToPaths(files, absolutePaths) {
	const wanted = new Set([...absolutePaths].map((p) => resolve(p)));
	return files.filter((f) => wanted.has(resolve(f.filePath)));
}

/** The files a file-writing call is about: `editor` names one, `apply_patch` its headers. */
export function readTargetPaths(toolName, input) {
	if (toolName === "editor") {
		const r = input && typeof input === "object" && !Array.isArray(input) ? input : {};
		const p = typeof r.file_path === "string" && r.file_path ? r.file_path : typeof r.path === "string" && r.path ? r.path : undefined;
		return p !== undefined && p.trim() !== "" ? [p] : [];
	}
	if (toolName !== "apply_patch") return [];
	const body = typeof input === "string" ? input : input?.input;
	if (typeof body !== "string") return [];
	const paths = [];
	for (const line of body.split("\n")) {
		const match = line.match(/^\*\*\*\s+(?:Add|Update|Delete)\s+File:\s*(.+?)\s*$/);
		if (match?.[1]) paths.push(match[1]);
	}
	return paths;
}

/**
 * The files' diagnostics once the servers have stopped changing their mind,
 * or once the budget runs out. A file changed after its server last reported
 * on it is `pending`, which is part of what must stop changing.
 */
export async function readSettledDiagnostics(absolutePaths, read, delay, budget = {}) {
	const pollMs = budget.pollMs ?? SETTLE_POLL_MS;
	let previous = "";
	let latest = [];
	const deadline = Date.now() + (budget.timeoutMs ?? SETTLE_TIMEOUT_MS);
	do {
		await delay(pollMs);
		latest = filterToPaths(await read(), absolutePaths);
		const fingerprint = JSON.stringify(latest);
		if (fingerprint === previous && !latest.some((f) => f.pending)) return latest;
		previous = fingerprint;
	} while (Date.now() < deadline);
	return latest;
}

// From integrations/diagnostics/index.ts.

function sameDiagnostic(a, b) {
	return JSON.stringify(a) === JSON.stringify(b);
}

export function getNewDiagnostics(oldDiagnostics, newDiagnostics) {
	const oldMap = new Map();
	for (const f of oldDiagnostics) oldMap.set(f.filePath, f.diagnostics);
	const out = [];
	for (const f of newDiagnostics) {
		const old = oldMap.get(f.filePath) || [];
		const fresh = f.diagnostics.filter((d) => !old.some((o) => sameDiagnostic(o, d)));
		if (fresh.length > 0) out.push({ filePath: f.filePath, diagnostics: fresh });
	}
	return out;
}

const UNBALANCED_DELIMITER_PATTERNS = [
	/^'[)\]}>]' expected/i,
	/expected '[)\]}]'/i,
	/unclosed delimiter/i,
	/unmatched '[([{]'/i,
	/unexpected (?:eof|end of file|end of input)/i,
	/unterminated (?:string|template|comment|block)/i,
];

const isUnbalancedDelimiter = (message) => UNBALANCED_DELIMITER_PATTERNS.some((p) => p.test(message.trim()));

function rootCauseHint(diagnostics, shape) {
	if (diagnostics.length < 3) return null;
	const structural = diagnostics.filter((d) => isUnbalancedDelimiter(d.message));
	if (structural.length === 0 || structural.length === diagnostics.length) return null;
	const where = structural
		.slice(0, 2)
		.map((d) => {
			const start = d.range?.start;
			const at = start ? describePosition(start, shape) : "an unknown position";
			return `${d.message.trim().replace(/\.$/, "")} at ${at}`;
		})
		.join("; ");
	const rest = diagnostics.length - structural.length;
	return (
		`- Start here: ${where}. An unclosed delimiter is reported where the parser gave up, so the delimiter itself ` +
		`belongs somewhere above that point. The other ${rest} error${rest === 1 ? "" : "s"} below are usually cascade ` +
		`from it and clear on their own once it is fixed — fix this one first and re-check before editing the rest.`
	);
}

async function readFileShape(filePath) {
	let text;
	try {
		text = await readFile(filePath, "utf8");
	} catch {
		return null;
	}
	const lines = text === "" ? [] : text.split(/\r\n|\r|\n/);
	if (lines[lines.length - 1] === "") lines.pop();
	const scriptEnds = [];
	if (/\.html?$/i.test(filePath)) {
		for (const match of text.matchAll(/<\/script\s*>/gi)) scriptEnds.push(text.slice(0, match.index).split(/\r\n|\r|\n/).length);
	}
	return { lines: lines.length, scriptEnds };
}

function describePosition(start, shape) {
	const line = start.line + 1;
	const column = start.character !== undefined ? `, column ${start.character + 1}` : "";
	if (!shape || line <= shape.lines) return `line ${line}${column}`;
	const end =
		`the end of the file (after line ${shape.lines}, its last; the editor numbers the empty line after ` +
		`the final line break ${line})`;
	const script = shape.scriptEnds[shape.scriptEnds.length - 1];
	return script ? `${end}, which for the script is its \`</script>\` on line ${script}` : end;
}

function severityToString(severity) {
	switch (severity) {
		case 1:
			return "Error";
		case 2:
			return "Warning";
		case 3:
			return "Information";
		case 4:
			return "Hint";
		default:
			return "Diagnostic";
	}
}

export async function singleFileDiagnosticsToProblemsString(cwd, filePath, diagnostics) {
	if (!diagnostics.length) return "";
	let result = toPosix(relative(cwd, filePath));
	const shape = await readFileShape(filePath);
	const hint = rootCauseHint(diagnostics, shape);
	if (hint) result += `\n${hint}`;
	for (const d of diagnostics) {
		const start = d.range?.start;
		const at = start ? describePosition(start, shape) : "line ";
		const source = d.source ? `${d.source} ` : "";
		result += `\n- [${source}${severityToString(d.severity)}] ${at.charAt(0).toUpperCase()}${at.slice(1)}: ${d.message}`;
	}
	return result;
}

// From sdk/editor-diagnostics.ts.

export async function renderFileDiagnostics(cwd, filePath, diagnostics) {
	const reportable = diagnostics.filter(isReportable);
	if (reportable.length === 0) return "";
	const shown = reportable.slice(0, MAX_DIAGNOSTICS_PER_FILE);
	const section = await singleFileDiagnosticsToProblemsString(cwd, filePath, shown);
	if (!section) return "";
	const omitted = reportable.length - shown.length;
	return omitted > 0 ? `${section}\n- ...and ${omitted} more` : section;
}

export async function formatIntroducedDiagnostics(cwd, before, after) {
	const sections = [];
	for (const file of getNewDiagnostics(before, after)) {
		const section = await renderFileDiagnostics(cwd, file.filePath, file.diagnostics);
		if (section !== "") sections.push(section);
	}
	if (sections.length === 0) return "";
	return [
		"<editor_diagnostics>",
		"The editor reported these problems on the file(s) this edit touched. They were not present before it.",
		sections.join("\n\n"),
		"</editor_diagnostics>",
	].join("\n");
}

export function appendToOutput(output, block) {
	if (output && typeof output === "object" && "result" in output) {
		const existing = typeof output.result === "string" ? output.result : "";
		return { ...output, result: existing === "" ? block : `${existing}\n\n${block}` };
	}
	if (typeof output === "string") return output === "" ? block : `${output}\n\n${block}`;
	return output;
}

function countReportable(files) {
	let total = 0;
	for (const f of files) total += f.diagnostics.filter(isReportable).length;
	return total;
}

export function markRegressed(output) {
	if (output && typeof output === "object" && !Array.isArray(output)) return { ...output, regressed: true };
	return output;
}

async function describeBalanceForPaths(describeDelimiterBalance, paths) {
	const sections = [];
	for (const filePath of paths) {
		try {
			const verdict = describeDelimiterBalance(filePath, await readFile(filePath, "utf8"));
			if (verdict) sections.push(verdict);
		} catch {
			// no verdict for a file that cannot be read
		}
	}
	return sections.join("\n\n");
}

/** Diagnostics without the host's `pending` mark, as the editor reports them. */
const plain = (files) => files.map(({ filePath, diagnostics }) => ({ filePath, diagnostics }));

/**
 * The hooks that report an edit's new diagnostics back to the model.
 *
 * Before the call the target files are handed to their servers as they are
 * now and, the first time, given the edit's settling budget: in VS Code the
 * file an agent edits is open in the editor, so what was wrong with it
 * before is known and is not reported as the edit's (D149). `note` records
 * one line per edit, the extension's trace.
 */
export function createEditorDiagnosticsHooks({ cwd, lsp, describeDelimiterBalance, note = () => {}, delay = sleep }) {
	const pending = new Map();
	const opened = new Set();
	const read = lsp.readDiagnostics;
	const targets = (toolName, input) =>
		readTargetPaths(toolName, input)
			.filter(isLintableFile)
			.map((p) => resolve(cwd, p))
			.filter((p) => lsp.serves(p));

	return {
		async beforeTool(ctx) {
			try {
				if (!FILE_WRITING_TOOLS.has(ctx.toolCall.toolName)) return undefined;
				const paths = targets(ctx.toolCall.toolName, ctx.input);
				if (paths.length === 0) return undefined;
				const fresh = [];
				for (const p of paths) {
					const held = await lsp.touch(p);
					if (held && !opened.has(p)) fresh.push(p);
					opened.add(p);
				}
				const before = fresh.length > 0 ? await readSettledDiagnostics(new Set(paths), read, delay) : filterToPaths(await read(), new Set(paths));
				pending.set(ctx.toolCall.toolCallId, { paths, before: plain(before) });
			} catch (error) {
				note("editor_diagnostics_failed", { phase: "before", error: String(error?.message ?? error) });
			}
			return undefined;
		},

		async afterTool(ctx) {
			const snapshot = pending.get(ctx.toolCall.toolCallId);
			pending.delete(ctx.toolCall.toolCallId);
			try {
				if (!snapshot || ctx.result?.isError) return undefined;
				for (const p of snapshot.paths) await lsp.touch(p, { save: true });
				const after = plain(await readSettledDiagnostics(new Set(snapshot.paths), read, delay));
				const block = await formatIntroducedDiagnostics(cwd, snapshot.before, after);
				const balance = block ? await describeBalanceForPaths(describeDelimiterBalance, snapshot.paths) : "";
				const wasCount = countReportable(snapshot.before);
				const nowCount = countReportable(after);
				const regressed = nowCount > wasCount;
				note("editor_diagnostics", {
					tool: ctx.toolCall.toolName,
					targets: snapshot.paths.map((p) => toPosix(relative(cwd, p))),
					before: wasCount,
					after: nowCount,
					block_chars: block.length,
					balance_chars: balance.length,
					regressed,
				});
				if (!block) return undefined;
				const appended = [block, balance].filter((part) => part !== "").join("\n\n");
				const output = appendToOutput(ctx.result.output, appended);
				return { result: { ...ctx.result, output: regressed ? markRegressed(output) : output } };
			} catch (error) {
				note("editor_diagnostics_failed", { phase: "after", error: String(error?.message ?? error) });
				return undefined;
			}
		},
	};
}

/** Accept the shapes a model actually sends, not only the documented one. */
export function readRequestedPaths(input) {
	const raw = input?.paths;
	const list = typeof raw === "string" ? [raw] : Array.isArray(raw) ? raw : [];
	return list.filter((e) => typeof e === "string" && e.trim() !== "").map((e) => e.trim());
}

/**
 * The extension's `check_file`, its diagnostics from the language servers.
 * The extension's lint command is a user setting, unset unless set; no arm
 * sets one, so it is not offered here.
 */
export function createCheckFileTool({ createTool, describeDelimiterBalance, cwd, lsp, delay = sleep }) {
	const describeFile = async (displayPath, absolutePath, diagnostics) => {
		const file = diagnostics.find((f) => resolve(f.filePath) === absolutePath);
		let balance = null;
		let missing = false;
		try {
			balance = describeDelimiterBalance(absolutePath, await readFile(absolutePath, "utf-8"));
		} catch (error) {
			missing = error?.code === "ENOENT";
		}
		const withBalance = (text) => [text, balance].filter((part) => part).join("\n");
		const rendered = file ? await renderFileDiagnostics(cwd, file.filePath, file.diagnostics) : "";
		if (rendered !== "") return withBalance(rendered);
		if (missing) return `${displayPath}: no such file, so there is nothing to check.`;
		return withBalance(
			`${displayPath}: no problems reported by the editor. If no language server handles this file type, that is not the same as clean.`,
		);
	};

	return createTool({
		name: CHECK_FILE_TOOL_NAME,
		description: CHECK_FILE_TOOL_DESCRIPTION,
		inputSchema: CHECK_FILE_TOOL_INPUT_SCHEMA,
		execute: async (input) => {
			const requested = readRequestedPaths(input);
			if (requested.length === 0) return "No files were named. Pass the paths to check in `paths`.";
			const overflow = requested.length - MAX_FILES_PER_CALL;
			const paths = requested.slice(0, MAX_FILES_PER_CALL);
			const absolute = paths.map((p) => resolve(cwd, p));
			// A file its server has never seen has no diagnostics, which reads as clean.
			await Promise.all(absolute.map((p) => lsp.touch(p).catch(() => undefined)));
			let diagnostics;
			try {
				diagnostics = plain(await readSettledDiagnostics(new Set(absolute), lsp.readDiagnostics, delay, CHECK_SETTLE_BUDGET));
			} catch (error) {
				return `Could not read the editor's diagnostics: ${error instanceof Error ? error.message : String(error)}`;
			}
			const sections = [];
			for (let i = 0; i < absolute.length; i++) sections.push(await describeFile(paths[i], absolute[i], diagnostics));
			if (overflow > 0) sections.push(`(${overflow} more file(s) were not checked; call again with the rest.)`);
			return sections.join("\n\n");
		},
	});
}
