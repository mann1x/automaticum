// The language servers of a session (040 §6.1, D149): what the extension's
// ask_lsp, check_file and diagnostics after edits read in VS Code, which
// runs a server for every language of the workspace. A lane runs none, so
// this host starts one per language on the first question about a file of
// it (or at the start, prewarm), and keeps it for the run.
//
// Ported from the SDK's CLI host at release 4.100.276
// (apps/cli/src/runtime/lsp-client.ts and code-intel-support.ts), which
// answers ask_lsp the same way, and extended where the CLI stops short:
//   - every language of the lane layers, each server under the runner's own
//     prefix, /opt/automaticum/lsp, on no PATH of the agent (D113), the Node
//     ones started with the runner's own Node;
//   - a file that changed is sent as didChange; the CLI sends didOpen again,
//     which a server that holds the document open ignores;
//   - the server's own requests are answered (workspace/configuration,
//     client/registerCapability, ...); left unanswered, a server waits;
//   - the diagnostics a server publishes are kept, per file, as the editor
//     keeps them, for check_file and the diagnostics after edits; a server
//     that only answers when asked (pull diagnostics) is asked.
//
// Nothing here imports the SDK.

import { spawn } from "node:child_process";
import { existsSync, readdirSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { extname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

export const LSP_ROOT = "/opt/automaticum/lsp";

const HEADER_SEPARATOR = "\r\n\r\n";
// The CLI's numbers: a server still indexing answers slowly, not wrongly.
const REQUEST_TIMEOUT_MS = 20_000;
const INITIALIZE_TIMEOUT_MS = 30_000;

/** LSP language ids, by the extension that implies them. */
export const LANGUAGE_BY_EXTENSION = {
	".ts": "typescript",
	".tsx": "typescriptreact",
	".mts": "typescript",
	".cts": "typescript",
	".js": "javascript",
	".jsx": "javascriptreact",
	".mjs": "javascript",
	".cjs": "javascript",
	".html": "html",
	".htm": "html",
	".css": "css",
	".scss": "scss",
	".less": "less",
	".json": "json",
	".jsonc": "jsonc",
	".py": "python",
	".pyi": "python",
	".go": "go",
	".rs": "rust",
	".c": "c",
	".h": "c",
	".cc": "cpp",
	".cpp": "cpp",
	".cxx": "cpp",
	".hh": "cpp",
	".hpp": "cpp",
	".hxx": "cpp",
	".cs": "csharp",
	".sh": "shellscript",
	".bash": "shellscript",
};

export function languageIdFor(filePath) {
	return LANGUAGE_BY_EXTENSION[extname(filePath).toLowerCase()];
}

/**
 * The servers, by language. `probe` is the file that must exist for the
 * server to be there: a layer brings the servers of its toolchain, the base
 * those that run on Node. A language with none answers nothing, as in an
 * editor without the language's extension.
 */
export function serverSpecs(root = LSP_ROOT, node = process.execPath) {
	const mod = (...p) => join(root, "node_modules", ...p);
	const bin = (name) => join(root, "bin", name);
	const extracted = (name) => mod("vscode-langservers-extracted", "bin", name);
	return [
		{
			name: "typescript-language-server",
			command: node,
			args: [mod("typescript-language-server", "lib", "cli.mjs"), "--stdio"],
			probe: mod("typescript-language-server", "lib", "cli.mjs"),
			languages: ["typescript", "typescriptreact", "javascript", "javascriptreact"],
			// Told where TypeScript is, as the CLI does: the server exits when
			// the workspace has none, and VS Code uses its own copy too.
			initializationOptions: { tsserver: { path: mod("typescript", "lib") } },
			// VS Code's TypeScript extension labels what tsserver reports "ts".
			sources: { typescript: "ts" },
		},
		{
			name: "vscode-html-language-server",
			command: node,
			args: [extracted("vscode-html-language-server"), "--stdio"],
			probe: extracted("vscode-html-language-server"),
			languages: ["html"],
			// What VS Code's HTML extension hands its server: the script and
			// the style of a page are checked as JavaScript and CSS.
			initializationOptions: { provideFormatter: false, embeddedLanguages: { css: true, javascript: true } },
			// VS Code's defaults for what the server asks: its script mode
			// reads js/ts.implicitProjectConfig and fails without it.
			settings: {
				html: { validate: { scripts: true, styles: true } },
				"js/ts": { implicitProjectConfig: { strictNullChecks: true, experimentalDecorators: false } },
			},
		},
		{
			name: "vscode-css-language-server",
			command: node,
			args: [extracted("vscode-css-language-server"), "--stdio"],
			probe: extracted("vscode-css-language-server"),
			languages: ["css", "scss", "less"],
			initializationOptions: { provideFormatter: false },
		},
		{
			name: "vscode-json-language-server",
			command: node,
			args: [extracted("vscode-json-language-server"), "--stdio"],
			probe: extracted("vscode-json-language-server"),
			languages: ["json", "jsonc"],
			initializationOptions: { provideFormatter: false },
		},
		{
			name: "pyright",
			command: node,
			args: [mod("pyright", "langserver.index.js"), "--stdio"],
			probe: mod("pyright", "langserver.index.js"),
			languages: ["python"],
		},
		{
			name: "bash-language-server",
			command: node,
			args: [mod("bash-language-server", "out", "cli.js"), "start"],
			probe: mod("bash-language-server", "out", "cli.js"),
			languages: ["shellscript"],
			env: { SHELLCHECK_PATH: bin("shellcheck") },
			settings: { bashIde: { shellcheckPath: bin("shellcheck") } },
		},
		{ name: "gopls", command: bin("gopls"), args: ["serve"], probe: bin("gopls"), languages: ["go"] },
		{ name: "rust-analyzer", command: bin("rust-analyzer"), args: [], probe: bin("rust-analyzer"), languages: ["rust"] },
		// No background index: it is written into the workspace (.cache).
		{ name: "clangd", command: bin("clangd"), args: ["--background-index=false"], probe: bin("clangd"), languages: ["c", "cpp"] },
		{ name: "csharp-ls", command: bin("csharp-ls"), args: [], probe: bin("csharp-ls"), languages: ["csharp"] },
	];
}

export function fileUri(filePath) {
	return pathToFileURL(filePath).toString();
}

export function uriToPath(uri) {
	if (!uri.startsWith("file://")) return uri;
	try {
		return decodeURIComponent(new URL(uri).pathname);
	} catch {
		return uri;
	}
}

/** A diagnostic as the editor keeps it: position, LSP severity, message, producer. */
export function normalizeDiagnostic(d) {
	const start = d?.range?.start ?? { line: 0, character: 0 };
	const end = d?.range?.end ?? start;
	return {
		range: { start: { line: start.line, character: start.character }, end: { line: end.line, character: end.character } },
		severity: typeof d?.severity === "number" ? d.severity : 1,
		message: String(d?.message ?? ""),
		...(d?.source ? { source: String(d.source) } : {}),
	};
}

/**
 * One server process and the conversation with it, over stdio.
 */
export class LspConnection {
	constructor(spec, rootPath, { onError, onDiagnostics } = {}) {
		this.spec = spec;
		this.rootPath = rootPath;
		this.onError = onError;
		this.onDiagnostics = onDiagnostics;
		this.child = undefined;
		this.buffer = Buffer.alloc(0);
		this.nextId = 1;
		this.pending = new Map();
		/** uri -> { version, text } of the documents the server holds open. */
		this.documents = new Map();
		this.initializing = undefined;
		this.failed = undefined;
		this.capabilities = {};
	}

	/** Started once, on the first question, and shared by every one after it. */
	ready() {
		if (this.failed) return Promise.reject(this.failed);
		if (!this.initializing) {
			this.initializing = this.start().catch((error) => {
				this.failed = error instanceof Error ? error : new Error(String(error));
				throw this.failed;
			});
		}
		return this.initializing;
	}

	async start() {
		const child = spawn(this.spec.command, this.spec.args, {
			cwd: this.rootPath,
			stdio: ["pipe", "pipe", "pipe"],
			env: { ...process.env, ...(this.spec.env ?? {}) },
		});
		this.child = child;
		// Never a reason for the runner to stay alive (the CLI's lesson: the
		// pipes hold the loop open as well as the process).
		child.unref();
		for (const stream of [child.stdin, child.stdout, child.stderr]) stream?.unref?.();
		child.on("error", (error) => this.fail(error));
		child.on("exit", (code) => this.fail(new Error(`${this.spec.name} exited with code ${code}`)));
		child.stdin.on("error", (error) => this.fail(error));
		child.stdout.on("data", (chunk) => this.consume(chunk));
		child.stderr.on("data", () => {});

		const result = await this.request(
			"initialize",
			{
				processId: process.pid,
				clientInfo: { name: "automaticum-runner" },
				initializationOptions: this.spec.initializationOptions,
				rootUri: fileUri(this.rootPath),
				rootPath: this.rootPath,
				workspaceFolders: [{ uri: fileUri(this.rootPath), name: "workspace" }],
				capabilities: {
					textDocument: {
						synchronization: { didSave: true, dynamicRegistration: false },
						definition: { linkSupport: true },
						typeDefinition: { linkSupport: true },
						implementation: { linkSupport: true },
						references: {},
						hover: { contentFormat: ["plaintext", "markdown"] },
						documentSymbol: { hierarchicalDocumentSymbolSupport: true },
						callHierarchy: {},
						publishDiagnostics: { relatedInformation: false, versionSupport: true },
					},
					workspace: { symbol: {}, workspaceFolders: true, configuration: true },
					window: { workDoneProgress: false },
				},
			},
			INITIALIZE_TIMEOUT_MS,
		);
		this.capabilities = result?.capabilities ?? {};
		this.notify("initialized", {});
		if (this.spec.settings) this.notify("workspace/didChangeConfiguration", { settings: this.spec.settings });
	}

	fail(error) {
		const failure = error instanceof Error ? error : new Error(String(error));
		if (!this.failed) this.onError?.(`[lsp] ${this.spec.name} failed`, failure);
		this.failed = failure;
		for (const [id, request] of this.pending) {
			clearTimeout(request.timer);
			request.reject(failure);
			this.pending.delete(id);
		}
	}

	consume(chunk) {
		this.buffer = Buffer.concat([this.buffer, chunk]);
		for (;;) {
			const headerEnd = this.buffer.indexOf(HEADER_SEPARATOR);
			if (headerEnd === -1) return;
			const header = this.buffer.subarray(0, headerEnd).toString("ascii");
			const length = /content-length:\s*(\d+)/i.exec(header)?.[1];
			if (!length) {
				this.buffer = this.buffer.subarray(headerEnd + HEADER_SEPARATOR.length);
				continue;
			}
			const start = headerEnd + HEADER_SEPARATOR.length;
			const end = start + Number(length);
			if (this.buffer.length < end) return;
			const body = this.buffer.subarray(start, end).toString("utf8");
			this.buffer = this.buffer.subarray(end);
			this.dispatch(body);
		}
	}

	dispatch(body) {
		let message;
		try {
			message = JSON.parse(body);
		} catch (error) {
			this.onError?.("[lsp] unparseable message", error);
			return;
		}
		if (message.method !== undefined) {
			if (message.id !== undefined) this.answer(message);
			else if (message.method === "textDocument/publishDiagnostics") {
				const p = message.params ?? {};
				this.onDiagnostics?.(uriToPath(p.uri ?? ""), (p.diagnostics ?? []).map((d) => this.label(normalizeDiagnostic(d))));
			}
			return;
		}
		const pending = this.pending.get(message.id);
		if (!pending) return;
		clearTimeout(pending.timer);
		this.pending.delete(message.id);
		if (message.error) pending.reject(new Error(message.error.message ?? "LSP error"));
		else pending.resolve(message.result);
	}

	/** A request of the server's own. Unanswered, some servers wait for ever. */
	answer(message) {
		let result = null;
		switch (message.method) {
			case "workspace/configuration":
				result = (message.params?.items ?? []).map((item) => this.setting(item.section));
				break;
			case "workspace/workspaceFolders":
				result = [{ uri: fileUri(this.rootPath), name: "workspace" }];
				break;
			default:
				// client/registerCapability, window/workDoneProgress/create,
				// window/showMessageRequest, workspace/*/refresh: acknowledged.
				result = null;
		}
		this.send({ id: message.id, result });
	}

	/**
	 * A section of the settings, as an editor answers: an object, empty for
	 * a section nobody set. Never null: the CSS validator, and the HTML
	 * server's, which validates a page's style through it, fail on null
	 * and then report nothing for the whole page.
	 */
	setting(section) {
		let v = this.spec.settings ?? {};
		for (const part of (section ?? "").split(".").filter(Boolean)) {
			v = v != null && typeof v === "object" ? v[part] : undefined;
		}
		return v ?? {};
	}

	send(payload) {
		if (!this.child || this.failed) return;
		const body = JSON.stringify({ jsonrpc: "2.0", ...payload });
		this.child.stdin.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}${HEADER_SEPARATOR}${body}`);
	}

	notify(method, params) {
		this.send({ method, params });
	}

	request(method, params, timeoutMs = REQUEST_TIMEOUT_MS) {
		if (this.failed) return Promise.reject(this.failed);
		const id = this.nextId++;
		return new Promise((resolvePromise, reject) => {
			const timer = setTimeout(() => {
				this.pending.delete(id);
				reject(new Error(`${method} timed out after ${timeoutMs}ms`));
			}, timeoutMs);
			this.pending.set(id, { resolve: resolvePromise, reject, timer });
			this.send({ id, method, params });
		});
	}

	/**
	 * The server holds the file as it is now: opened the first time, sent
	 * whole as a change when it differs from what the server holds. Returns
	 * whether the server was told anything.
	 */
	sync(filePath, text, languageId) {
		const uri = fileUri(filePath);
		const held = this.documents.get(uri);
		if (!held) {
			this.documents.set(uri, { version: 1, text });
			this.notify("textDocument/didOpen", { textDocument: { uri, languageId, version: 1, text } });
			return true;
		}
		if (held.text === text) return false;
		held.version += 1;
		held.text = text;
		this.notify("textDocument/didChange", { textDocument: { uri, version: held.version }, contentChanges: [{ text }] });
		return true;
	}

	saved(filePath) {
		const uri = fileUri(filePath);
		const held = this.documents.get(uri);
		if (held) this.notify("textDocument/didSave", { textDocument: { uri }, text: held.text });
	}

	/** A diagnostic's producer as the editor names it. */
	label(d) {
		const as = d.source && this.spec.sources?.[d.source];
		return as ? { ...d, source: as } : d;
	}

	/** Diagnostics asked for, of a server that answers only when asked. */
	async pull(filePath) {
		if (!this.capabilities.diagnosticProvider || this.noPull) return undefined;
		let report;
		try {
			report = await this.request("textDocument/diagnostic", { textDocument: { uri: fileUri(filePath) } });
		} catch (error) {
			// The servers built on vscode-languageserver announce the provider
			// and still push to a client that does not ask; once refused, the
			// server is not asked again.
			if (/unhandled method|method not found/i.test(error?.message ?? "")) {
				this.noPull = true;
				return undefined;
			}
			throw error;
		}
		if (report?.kind !== "full" || !Array.isArray(report.items)) return undefined;
		return report.items.map((d) => this.label(normalizeDiagnostic(d)));
	}

	async dispose() {
		if (!this.child) return;
		try {
			await this.request("shutdown", null, 2_000);
			this.notify("exit", null);
		} catch {
			// killed below
		}
		this.child.kill();
		this.child = undefined;
	}
}

// From the CLI's code-intel-support.ts.
function toLocation(raw) {
	if (!raw || typeof raw !== "object") return undefined;
	const uri = raw.uri ?? raw.targetUri;
	const start = raw.range?.start ?? raw.targetSelectionRange?.start;
	if (!uri || !start) return undefined;
	return { filePath: uriToPath(uri), line: start.line, character: start.character };
}

function toLocations(raw) {
	if (Array.isArray(raw)) return raw.map(toLocation).filter((e) => e !== undefined);
	const single = toLocation(raw);
	return single ? [single] : [];
}

const SYMBOL_KINDS = [
	"file", "module", "namespace", "package", "class", "method", "property", "field", "constructor", "enum",
	"interface", "function", "variable", "constant", "string", "number", "boolean", "array", "object", "key",
	"null", "enum member", "struct", "event", "operator", "type parameter",
];

function kindName(kind) {
	return typeof kind === "number" && kind >= 1 && kind <= SYMBOL_KINDS.length ? SYMBOL_KINDS[kind - 1] : "symbol";
}

function toSymbols(raw, fallbackPath) {
	if (!Array.isArray(raw)) return [];
	const symbols = [];
	const walk = (entries, container) => {
		for (const node of entries) {
			if (!node || typeof node !== "object") continue;
			const start = node.selectionRange?.start ?? node.range?.start ?? node.location?.range?.start;
			const uri = node.location?.uri ?? node.uri;
			if (node.name && start) {
				symbols.push({
					name: node.name,
					kind: kindName(node.kind),
					location: { filePath: uri ? uriToPath(uri) : (fallbackPath ?? ""), line: start.line, character: start.character },
					containerName: node.containerName ?? container,
				});
			}
			if (Array.isArray(node.children)) walk(node.children, node.name);
		}
	};
	walk(raw);
	return symbols;
}

const SKIP_DIRS = new Set([".git", "node_modules", "target", "bin", "obj", "dist", "build", ".venv", "__pycache__", ".cache"]);

/** The languages of the workspace's files, looked for at most `limit` entries deep. */
export function workspaceLanguages(root, limit = 5000) {
	const found = new Set();
	const queue = [root];
	let seen = 0;
	while (queue.length > 0 && seen < limit) {
		const dir = queue.shift();
		let entries;
		try {
			entries = readdirSync(dir, { withFileTypes: true });
		} catch {
			continue;
		}
		for (const e of entries) {
			if (++seen > limit) break;
			if (e.isDirectory()) {
				if (!SKIP_DIRS.has(e.name)) queue.push(join(dir, e.name));
			} else {
				const lang = languageIdFor(e.name);
				if (lang) found.add(lang);
			}
		}
	}
	return [...found].sort();
}

/**
 * createLspHost: the session's language servers.
 *
 * `provider` is ask_lsp's CodeIntelProvider (the SDK's interface);
 * `readDiagnostics` is the editor's verdict on every file a server has
 * reported on, as `{ filePath, diagnostics, pending }`, where `pending` says
 * the file changed after the server last reported on it; `touch` hands the
 * server a file as it is on disk now (`save`: as written).
 */
export function createLspHost({ cwd, root = LSP_ROOT, servers, onError } = {}) {
	const specs = servers ?? serverSpecs(root);
	const connections = new Map(); // language -> LspConnection | null
	const byServer = new Map(); // server name -> LspConnection
	/** path -> { diagnostics, pending } */
	const diagnostics = new Map();

	const available = (spec) => existsSync(spec.probe ?? spec.command);

	function connectionFor(filePath) {
		const languageId = languageIdFor(filePath);
		return languageId ? connectionForLanguage(languageId) : undefined;
	}

	function connectionForLanguage(languageId) {
		const existing = connections.get(languageId);
		if (existing !== undefined) return existing ?? undefined;
		const spec = specs.find((s) => s.languages.includes(languageId) && available(s));
		if (!spec) {
			connections.set(languageId, null);
			return undefined;
		}
		let connection = byServer.get(spec.name);
		if (!connection) {
			connection = new LspConnection(spec, cwd, {
				onError,
				onDiagnostics: (filePath, list) => diagnostics.set(resolve(filePath), { diagnostics: list, pending: false }),
			});
			byServer.set(spec.name, connection);
		}
		connections.set(languageId, connection);
		return connection;
	}

	/**
	 * The server holds the file as it is on disk; undefined when no server
	 * answers for it or the file cannot be read.
	 */
	async function touch(filePath, { save = false } = {}) {
		const absolute = resolve(cwd, filePath);
		const connection = connectionFor(absolute);
		if (!connection) return undefined;
		let text;
		try {
			text = await readFile(absolute, "utf-8");
		} catch {
			return undefined;
		}
		try {
			await connection.ready();
		} catch {
			return undefined;
		}
		const changed = connection.sync(absolute, text, languageIdFor(absolute) ?? "plaintext");
		if (changed) {
			const held = diagnostics.get(absolute);
			diagnostics.set(absolute, { diagnostics: held?.diagnostics ?? [], pending: true });
		}
		if (save) connection.saved(absolute);
		// A server that answers only when asked is asked every time: its
		// first answer can come before it has loaded the project (csharp-ls
		// answers nothing then), and it says nothing when that changes.
		if (connection.capabilities.diagnosticProvider && !connection.noPull) {
			const held = diagnostics.get(absolute);
			diagnostics.set(absolute, { diagnostics: held?.diagnostics ?? [], pending: true });
			connection
				.pull(absolute)
				.then((list) => {
					const now = diagnostics.get(absolute);
					diagnostics.set(absolute, { diagnostics: list ?? now?.diagnostics ?? [], pending: false });
				})
				.catch((error) => {
					diagnostics.set(absolute, { diagnostics: diagnostics.get(absolute)?.diagnostics ?? [], pending: false });
					onError?.("[lsp] textDocument/diagnostic failed", error);
				});
		}
		return { connection, text };
	}

	async function ask(filePath, method, params) {
		const held = await touch(filePath);
		if (!held) return undefined;
		try {
			return await held.connection.request(method, params);
		} catch (error) {
			onError?.(`[ask_lsp] ${method} failed`, error);
			return undefined;
		}
	}

	const at = (filePath, location) => ({
		textDocument: { uri: fileUri(filePath) },
		position: { line: location.line, character: location.character },
	});

	async function positional(method, location, extra = {}) {
		return toLocations(await ask(location.filePath, method, { ...at(location.filePath, location), ...extra }));
	}

	async function lines(filePath) {
		return (await readFile(filePath, "utf-8")).split(/\r?\n/);
	}

	const provider = {
		async findSymbolPosition(filePath, symbol) {
			// As the extension: the file's own symbol table first, since a
			// declaration is what the model almost always means; then the
			// first whole-word occurrence (the CLI's only way).
			const declared = (await provider.documentSymbols(filePath).catch(() => [])).find((entry) => entry.name === symbol);
			if (declared) return declared.location;
			const source = await lines(filePath).catch(() => undefined);
			if (!source) return undefined;
			const pattern = new RegExp(`\\b${symbol.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\b`);
			for (let line = 0; line < source.length; line++) {
				const match = pattern.exec(source[line]);
				if (match) return { filePath, line, character: match.index };
			}
			return undefined;
		},
		definitions: (location) => positional("textDocument/definition", location),
		typeDefinitions: (location) => positional("textDocument/typeDefinition", location),
		implementations: (location) => positional("textDocument/implementation", location),
		references: (location) => positional("textDocument/references", location, { context: { includeDeclaration: false } }),
		async hover(location) {
			const raw = await ask(location.filePath, "textDocument/hover", at(location.filePath, location));
			const contents = raw?.contents;
			if (!contents) return undefined;
			if (typeof contents === "string") return contents;
			if (Array.isArray(contents)) {
				return contents.map((part) => (typeof part === "string" ? part : (part?.value ?? ""))).filter(Boolean).join("\n");
			}
			return contents.value;
		},
		async documentSymbols(filePath) {
			return toSymbols(await ask(filePath, "textDocument/documentSymbol", { textDocument: { uri: fileUri(filePath) } }), filePath);
		},
		async workspaceSymbols(query) {
			// Every server started so far is asked, as the CLI does.
			const results = [];
			for (const connection of byServer.values()) {
				try {
					await connection.ready();
					results.push(...toSymbols(await connection.request("workspace/symbol", { query })));
				} catch (error) {
					onError?.("[ask_lsp] workspace/symbol failed", error);
				}
			}
			return results;
		},
		async callers(location) {
			const prepared = await ask(location.filePath, "textDocument/prepareCallHierarchy", at(location.filePath, location));
			const item = Array.isArray(prepared) ? prepared[0] : undefined;
			const connection = item && connectionFor(location.filePath);
			if (!connection) return [];
			try {
				const incoming = await connection.request("callHierarchy/incomingCalls", { item });
				return Array.isArray(incoming) ? toSymbols(incoming.map((entry) => entry.from)) : [];
			} catch (error) {
				onError?.("[ask_lsp] incomingCalls failed", error);
				return [];
			}
		},
		async readLine(filePath, line) {
			return (await lines(filePath).catch(() => undefined))?.[line];
		},
		async readFile(filePath) {
			return (await lines(filePath).catch(() => undefined))?.join("\n");
		},
	};

	return {
		provider,
		touch,
		/** Whether a server answers for this file's language. */
		serves: (filePath) => connectionFor(resolve(cwd, filePath)) !== undefined,
		readDiagnostics: async () =>
			[...diagnostics.entries()].map(([filePath, held]) => ({ filePath, diagnostics: held.diagnostics, pending: held.pending })),
		/** Starts the servers of the workspace's languages, as an editor does on opening it. */
		prewarm(languages = workspaceLanguages(cwd)) {
			const started = [];
			for (const lang of languages) {
				const c = connectionForLanguage(lang);
				if (c && !started.includes(c.spec.name)) {
					started.push(c.spec.name);
					c.ready().catch(() => {});
				}
			}
			return started;
		},
		/** Which server answers for which language, and whether it is installed. */
		status: () =>
			specs.map((s) => ({
				server: s.name,
				languages: s.languages,
				installed: available(s),
				...(byServer.get(s.name)?.failed ? { failed: byServer.get(s.name).failed.message } : {}),
				...(byServer.get(s.name)?.child ? { started: true } : {}),
			})),
		async dispose() {
			await Promise.all([...byServer.values()].map((c) => c.dispose().catch(() => {})));
		},
	};
}
