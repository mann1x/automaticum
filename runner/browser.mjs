// The browser of a session (040 §6.1.3, D150): the drivers of the SDK's
// `browser`, `render_image` and `transform_image` tools, on a Chrome in the
// lane under the runner's own prefix, /opt/automaticum/browser, on no PATH
// of the agent (D113).
//
// Ported from the extension at SDK release 4.100.276, not from the CLI,
// whose driver differs in what the model sees: the browser driver is the
// local mode of apps/vscode/src/services/browser/BrowserSession.ts (headless
// "shell", its user agent, the 900x600 viewport of the default browser
// settings, navigation to domcontentloaded and networkidle2 in 7 s, then the
// page's HTML stable, console and page errors only, a quiet console waited
// for, a PNG screenshot as the extension's host asks for), and the render
// driver is apps/vscode/src/services/browser/render-page.ts.
//
// Differences a container makes necessary, and recorded: `--no-sandbox`
// (Chrome's sandbox needs user namespaces Docker does not give) and
// `--disable-dev-shm-usage` (the container's /dev/shm is 64 MB).
//
// Nothing here imports the SDK: what the render driver needs of it (the id
// of the element a render is captured from) is passed in.

import { existsSync } from "node:fs";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

export const BROWSER_ROOT = "/opt/automaticum/browser";

/** The extension's DEFAULT_BROWSER_SETTINGS viewport. */
export const DEFAULT_VIEWPORT = { width: 900, height: 600 };

/** The extension's BrowserSession sends this user agent from a local Chrome. */
const USER_AGENT =
	"--user-agent=Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36";

/** What the container needs and the extension's host does not. */
export const CONTAINER_ARGS = ["--no-sandbox", "--disable-dev-shm-usage"];

export function chromePath(root = BROWSER_ROOT) {
	const p = join(root, "chrome-linux64", "chrome");
	return existsSync(p) ? p : undefined;
}

export function mermaidPath(root = BROWSER_ROOT) {
	const p = join(root, "node_modules", "mermaid", "dist", "mermaid.min.js");
	return existsSync(p) ? p : undefined;
}

/** puppeteer-core from the runner's prefix, loaded on first use only. */
export function loadPuppeteer(root = BROWSER_ROOT) {
	return createRequire(join(root, "package.json"))("puppeteer-core");
}

const delay = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitFor(condition, { timeout, interval }) {
	const until = Date.now() + timeout;
	while (Date.now() < until) {
		if (condition()) return;
		await delay(interval);
	}
}

/**
 * The extension's BrowserSession, local mode: one Chrome for the session,
 * started by launchBrowser and closed by closeBrowser, which the SDK's tool
 * calls.
 */
export function createBrowserDriver({ root = BROWSER_ROOT, viewport = DEFAULT_VIEWPORT, puppeteer } = {}) {
	let browser;
	let page;
	let currentMousePosition;

	async function closeBrowser() {
		if (browser || page) {
			await browser?.close().catch(() => {});
			browser = undefined;
			page = undefined;
			currentMousePosition = undefined;
		}
		return {};
	}

	async function launchBrowser() {
		if (browser) await closeBrowser();
		const executablePath = chromePath(root);
		if (!executablePath) throw new Error(`no Chrome in this lane image (looked for ${join(root, "chrome-linux64", "chrome")})`);
		const lib = puppeteer ?? loadPuppeteer(root);
		browser = await lib.launch({
			args: [USER_AGENT, ...CONTAINER_ARGS],
			executablePath,
			defaultViewport: viewport,
			headless: "shell",
		});
		page = await browser.newPage();
	}

	async function doAction(action) {
		if (!page) {
			throw new Error("Browser is not launched. This may occur if the browser was automatically closed by a non-`browser_action` tool.");
		}
		const logs = [];
		let lastLogTs = Date.now();
		const consoleListener = (msg) => {
			logs.push(msg.type() === "log" ? msg.text() : `[${msg.type()}] ${msg.text()}`);
			lastLogTs = Date.now();
		};
		const errorListener = (err) => {
			logs.push(`[Page Error] ${err.toString()}`);
			lastLogTs = Date.now();
		};
		page.on("console", consoleListener);
		page.on("pageerror", errorListener);
		try {
			await action(page);
		} catch (err) {
			const message = err instanceof Error ? err.message : String(err);
			if (err?.name !== "TimeoutError") logs.push(`[Error] ${message}`);
			else if (logs.length === 0) logs.push(`[Timeout] ${message}`);
		}
		await waitFor(() => Date.now() - lastLogTs >= 500, { timeout: 3_000, interval: 100 });
		let shot = await page.screenshot({ encoding: "base64", type: "png" });
		if (!shot) shot = await page.screenshot({ encoding: "base64", type: "png" });
		if (!shot) throw new Error("Failed to take screenshot.");
		page.off("console", consoleListener);
		page.off("pageerror", errorListener);
		return { screenshot: `data:image/png;base64,${shot}`, logs: logs.join("\n"), currentUrl: page.url(), currentMousePosition };
	}

	async function waitTillHTMLStable(p, timeout = 5_000) {
		const checkMs = 500;
		const maxChecks = timeout / checkMs;
		let lastSize = 0;
		let checks = 1;
		let stable = 0;
		while (checks++ <= maxChecks) {
			const size = (await p.content()).length;
			stable = lastSize !== 0 && size === lastSize ? stable + 1 : 0;
			if (stable >= 3) break;
			lastSize = size;
			await delay(checkMs);
		}
	}

	return {
		launchBrowser,
		closeBrowser,
		navigateToUrl: (url) =>
			doAction(async (p) => {
				await p.goto(url, { timeout: 7_000, waitUntil: ["domcontentloaded", "networkidle2"] });
				await waitTillHTMLStable(p);
			}),
		click: (coordinate) => {
			const [x, y] = coordinate.split(",").map(Number);
			return doAction(async (p) => {
				let network = false;
				const onRequest = () => {
					network = true;
				};
				p.on("request", onRequest);
				await p.mouse.click(x, y);
				currentMousePosition = coordinate;
				await delay(100);
				if (network) {
					await p.waitForNavigation({ waitUntil: ["domcontentloaded", "networkidle2"], timeout: 7000 }).catch(() => {});
					await waitTillHTMLStable(p);
				}
				p.off("request", onRequest);
			});
		},
		type: (text) => doAction(async (p) => p.keyboard.type(text)),
		scrollDown: () =>
			doAction(async (p) => {
				await p.evaluate(() => window.scrollBy({ top: 600, behavior: "auto" }));
				await delay(300);
			}),
		scrollUp: () =>
			doAction(async (p) => {
				await p.evaluate(() => window.scrollBy({ top: -600, behavior: "auto" }));
				await delay(300);
			}),
	};
}

const LOAD_TIMEOUT_MS = 20_000;
const SETTLE_TIMEOUT_MS = 3_000;
const MAX_CAPTURE_PIXELS = 16_000;
const VIEWPORT_HEIGHT = 800;

/**
 * The extension's renderWithChrome: a Chrome started for one render and
 * closed after it, for render_image and transform_image.
 */
export function createRenderDriver({ root = BROWSER_ROOT, renderRootId, puppeteer } = {}) {
	return {
		async render(request) {
			const executablePath = chromePath(root);
			if (!executablePath) throw new Error(`no Chrome in this lane image (looked for ${join(root, "chrome-linux64", "chrome")})`);
			const mermaidScriptPath = mermaidPath(root);
			if (request.mermaid && !mermaidScriptPath) throw new Error("this build has no Mermaid to draw the diagram with");
			const scratch = await mkdtemp(join(os.tmpdir(), "cerebriline-render-"));
			const pagePath = join(scratch, "page.html");
			await writeFile(pagePath, request.html, "utf8");
			const lib = puppeteer ?? loadPuppeteer(root);
			const browser = await lib.launch({
				executablePath,
				headless: "shell",
				args: [...CONTAINER_ARGS],
				defaultViewport: { width: request.width, height: request.height ?? VIEWPORT_HEIGHT, deviceScaleFactor: request.scale },
			});
			const stop = () => {
				void browser.close().catch(() => undefined);
			};
			request.signal?.addEventListener("abort", stop, { once: true });
			try {
				const page = await browser.newPage();
				try {
					await page.goto(pathToFileURL(pagePath).href, { waitUntil: "load", timeout: LOAD_TIMEOUT_MS });
				} catch (error) {
					if (!(error instanceof Error) || !/timeout/i.test(error.message)) throw error;
				}
				await page.waitForNetworkIdle({ idleTime: 300, timeout: SETTLE_TIMEOUT_MS }).catch(() => undefined);
				if (request.mermaid && mermaidScriptPath) {
					await page.addScriptTag({ path: mermaidScriptPath });
					const failure = await page.evaluate(
						async (rootId, theme) => {
							const rootEl = document.getElementById(rootId);
							const text = rootEl?.querySelector("pre.mermaid")?.textContent ?? "";
							const mermaid = window.mermaid;
							if (!rootEl || !mermaid) return "Mermaid did not load in the page";
							mermaid.initialize({ startOnLoad: false, theme, securityLevel: "strict" });
							try {
								const { svg } = await mermaid.render("cerebriline-diagram", text);
								rootEl.innerHTML = svg;
							} catch (error) {
								return error instanceof Error ? error.message : String(error);
							}
							const drawing = rootEl.querySelector("svg");
							const box = drawing?.viewBox?.baseVal;
							if (drawing && box && box.width > 0 && box.height > 0) {
								drawing.setAttribute("width", String(Math.ceil(box.width)));
								drawing.setAttribute("height", String(Math.ceil(box.height)));
								drawing.style.maxWidth = "none";
							}
							return undefined;
						},
						renderRootId,
						request.mermaid.theme,
					);
					if (failure) throw new Error(failure);
				}
				await page.evaluate(() => document.fonts.ready.then(() => true)).catch(() => undefined);
				const drawn = await page.evaluate((rootId) => {
					const rootEl = document.getElementById(rootId);
					if (rootEl) {
						const box = rootEl.getBoundingClientRect();
						return {
							x: Math.max(0, Math.floor(box.left + window.scrollX)),
							y: Math.max(0, Math.floor(box.top + window.scrollY)),
							width: Math.ceil(Math.max(box.width, rootEl.scrollWidth)),
							height: Math.ceil(Math.max(box.height, rootEl.scrollHeight)),
						};
					}
					const doc = document.documentElement;
					return { x: 0, y: 0, width: Math.max(doc.scrollWidth, doc.clientWidth), height: Math.max(doc.scrollHeight, document.body?.scrollHeight ?? 0) };
				}, renderRootId);
				const asked =
					request.crop ?? (request.fitContent ? drawn : { x: 0, y: 0, width: request.width, height: request.height ?? Math.max(drawn.height, 1) });
				const limit = Math.floor(MAX_CAPTURE_PIXELS / request.scale);
				const clip = { x: asked.x, y: asked.y, width: Math.min(asked.width, limit), height: Math.min(asked.height, limit) };
				if (clip.width <= 0 || clip.height <= 0) return { data: Buffer.alloc(0), width: 0, height: 0 };
				const format = request.format ?? "png";
				const capture = await page.screenshot({
					type: format,
					...(format !== "png" && request.quality ? { quality: Math.round(request.quality) } : {}),
					clip,
					omitBackground: request.transparent,
					captureBeyondViewport: true,
				});
				const svg = request.wantSvg
					? await page.evaluate((rootId) => {
							const drawing = document.getElementById(rootId)?.querySelector("svg");
							return drawing ? new XMLSerializer().serializeToString(drawing) : undefined;
						}, renderRootId)
					: undefined;
				return { data: Buffer.from(capture), width: clip.width, height: clip.height, ...(svg ? { svg } : {}) };
			} finally {
				request.signal?.removeEventListener("abort", stop);
				await browser.close().catch(() => undefined);
				await rm(scratch, { recursive: true, force: true }).catch(() => undefined);
			}
		},
	};
}
