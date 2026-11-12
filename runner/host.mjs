// The host halves of the editor's tools: the language servers, check_file,
// the diagnostics after edits, and the browser's drivers (040 §6.1.2,
// §6.1.3; D149, D150).
//
// From SDK release 4.100.277 core exports them (X15): createLspHost,
// createDiagnosticsCheckFileTool, createEditorDiagnosticsHooks,
// createChromeBrowserDriver, createChromeRenderDriver, the same functions
// the extension's own check_file, post-edit report and browser now run. A
// build that has them gets the SDK's, on the lane's servers and Chrome; an
// older build gets the runner's ports of the 4.100.276 extension (lsp.mjs,
// editor.mjs, browser.mjs). Either way the runner notes the same
// `editor_diagnostics`, `editor_diagnostics_failed` and `lsp_error`, and
// says in the prepared note whose halves ran (`source`).

import { existsSync } from "node:fs";
import { relative } from "node:path";
import { BROWSER_ROOT, chromePath, createBrowserDriver, createRenderDriver, loadPuppeteer, mermaidPath } from "./browser.mjs";
import { createCheckFileTool, createEditorDiagnosticsHooks } from "./editor.mjs";
import { createLspHost, LSP_ROOT, serverSpecs } from "./lsp.mjs";

const SDK_LSP = ["createLspHost", "createDiagnosticsCheckFileTool", "createEditorDiagnosticsHooks"];
const SDK_CHROME = ["createChromeBrowserDriver", "createChromeRenderDriver"];

/** Whether the SDK build exports the language-server halves (4.100.277 on). */
export const sdkHasLsp = (sdk) => SDK_LSP.every((name) => typeof sdk?.[name] === "function");

/** Whether the SDK build exports the Chrome drivers (4.100.277 on). */
export const sdkHasChrome = (sdk) => SDK_CHROME.every((name) => typeof sdk?.[name] === "function");

const toPosix = (p) => p.replace(/\\/g, "/");
const message = (error) => String(error?.message ?? error);

/**
 * The session's language servers, check_file and (unless the arm says
 * `diagnostics_after_edits = false`) the diagnostics after edits.
 * `ask_lsp` is the SDK's in both cases, built on `host.provider`.
 */
export function buildLsp({ sdk, cwd, note = () => {}, root = LSP_ROOT, diagnosticsAfterEdits = true, node = process.execPath }) {
	let errors = 0;
	const onHostError = (text, error) => {
		if (++errors <= 20) note("lsp_error", { message: text, error: message(error) });
	};
	const onToolError = (text, error) => note("lsp_error", { message: text, error: message(error) });
	if (sdkHasLsp(sdk)) {
		const host = sdk.createLspHost({
			cwd,
			servers: serverSpecs(root, node),
			onError: onHostError,
			// Under the runner's prefix, never on a PATH (D113).
			isInstalled: (spec) => typeof spec.probe === "string" && existsSync(spec.probe),
		});
		const tools = [
			sdk.createAskLspTool({ cwd, provider: host.provider, onError: onToolError }),
			sdk.createDiagnosticsCheckFileTool({
				cwd,
				readDiagnostics: host.readDiagnostics,
				touch: host.touch,
				serves: host.serves,
				onError: onToolError,
			}),
		];
		const hooks = diagnosticsAfterEdits
			? sdk.createEditorDiagnosticsHooks({
					cwd,
					readDiagnostics: host.readDiagnostics,
					touch: host.touch,
					serves: host.serves,
					trace: (entry) => noteTrace(note, cwd, entry),
					onError: (text, error) =>
						note("editor_diagnostics_failed", { phase: /snapshot/i.test(String(text)) ? "before" : "after", error: message(error) }),
				})
			: undefined;
		return { host, tools, hooks, source: "sdk" };
	}
	const host = createLspHost({ cwd, root, onError: onHostError });
	const tools = [
		sdk.createAskLspTool({ cwd, provider: host.provider, onError: onToolError }),
		createCheckFileTool({ createTool: sdk.createTool, describeDelimiterBalance: sdk.describeDelimiterBalance, cwd, lsp: host }),
	];
	const hooks = diagnosticsAfterEdits
		? createEditorDiagnosticsHooks({ cwd, lsp: host, describeDelimiterBalance: sdk.describeDelimiterBalance, note })
		: undefined;
	return { host, tools, hooks, source: "runner" };
}

/**
 * The SDK hooks' trace, as the runner's own hooks note it: one
 * `editor_diagnostics` per edit that got as far as reading the files again.
 */
export function noteTrace(note, cwd, entry) {
	if (entry?.phase !== "after") return;
	note("editor_diagnostics", {
		tool: entry.tool,
		targets: (entry.targets ?? []).map((p) => toPosix(relative(cwd, p))),
		before: entry.before,
		after: entry.after,
		block_chars: entry.blockChars,
		balance_chars: entry.balanceChars,
		regressed: entry.regressed === true,
	});
}

/**
 * The browser's driver and the render driver of render_image and
 * transform_image, on the lane's Chrome and puppeteer-core.
 */
export function buildBrowserDrivers({ sdk, root = BROWSER_ROOT }) {
	if (sdkHasChrome(sdk)) {
		const puppeteer = async () => loadPuppeteer(root);
		return {
			// The SDK's default arguments carry the two a container needs
			// (--no-sandbox, --disable-dev-shm-usage) beside the extension's.
			createDriver: () => sdk.createChromeBrowserDriver({ chromePath: chromePath(root), loadPuppeteer: puppeteer }),
			createRenderDriver: () =>
				sdk.createChromeRenderDriver({ executablePath: chromePath(root), mermaidScriptPath: mermaidPath(root), loadPuppeteer: puppeteer }),
			source: "sdk",
		};
	}
	return {
		createDriver: () => createBrowserDriver({ root }),
		createRenderDriver: () => createRenderDriver({ root, renderRootId: sdk.RENDER_ROOT_ID }),
		source: "runner",
	};
}
