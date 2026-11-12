import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { CONTAINER_ARGS, DEFAULT_VIEWPORT, chromePath, createBrowserDriver, createRenderDriver } from "../browser.mjs";

/** A lane prefix with a Chrome in it (a file is enough: the fake puppeteer never runs it). */
function lane() {
	const root = mkdtempSync(join(tmpdir(), "browser-test-"));
	mkdirSync(join(root, "chrome-linux64"));
	writeFileSync(join(root, "chrome-linux64", "chrome"), "");
	return root;
}

/** A puppeteer whose page plays what a test tells it to. */
function fakePuppeteer(script = {}) {
	const launches = [];
	const handlers = {};
	const page = {
		url: () => "file:///workspace/index.html",
		on: (event, fn) => ((handlers[event] ??= []).push(fn)),
		off: (event, fn) => (handlers[event] = (handlers[event] ?? []).filter((h) => h !== fn)),
		emit: (event, payload) => (handlers[event] ?? []).forEach((h) => h(payload)),
		goto: async (url, options) => {
			page.went = { url, options };
			await script.goto?.(page);
		},
		content: async () => "<html>same</html>",
		screenshot: async () => "BASE64",
		mouse: { click: async (x, y) => (page.clicked = [x, y]) },
		keyboard: { type: async (t) => (page.typed = t) },
		evaluate: async () => undefined,
	};
	return {
		launches,
		page,
		launch: async (options) => {
			launches.push(options);
			return { newPage: async () => page, close: async () => (page.closed = true) };
		},
	};
}

test("no Chrome in the lane: the tool's driver says where it looked", async () => {
	const root = mkdtempSync(join(tmpdir(), "browser-none-"));
	assert.equal(chromePath(root), undefined);
	await assert.rejects(createBrowserDriver({ root, puppeteer: fakePuppeteer() }).launchBrowser(), /no Chrome in this lane image/);
});

test("the browser launches as the extension's local mode, with what a container needs", async () => {
	const root = lane();
	const pp = fakePuppeteer();
	const driver = createBrowserDriver({ root, puppeteer: pp });
	await driver.launchBrowser();
	const [opts] = pp.launches;
	assert.equal(opts.headless, "shell");
	assert.deepEqual(opts.defaultViewport, DEFAULT_VIEWPORT);
	assert.equal(opts.executablePath, join(root, "chrome-linux64", "chrome"));
	assert.match(opts.args[0], /^--user-agent=Mozilla\/5\.0 \(Macintosh; Intel Mac OS X 10_15_7\)/);
	assert.deepEqual(opts.args.slice(1), CONTAINER_ARGS);
});

test("an action reports the console as the extension does: log bare, others tagged, page errors with their name", async () => {
	const root = lane();
	const pp = fakePuppeteer({
		goto: async (page) => {
			page.emit("console", { type: () => "log", text: () => "ready" });
			page.emit("console", { type: () => "warning", text: () => "slow" });
			page.emit("pageerror", new SyntaxError("missing ) after argument list"));
		},
	});
	const driver = createBrowserDriver({ root, puppeteer: pp });
	await driver.launchBrowser();
	const res = await driver.navigateToUrl("file:///workspace/index.html");
	assert.deepEqual(pp.page.went.options, { timeout: 7_000, waitUntil: ["domcontentloaded", "networkidle2"] });
	assert.equal(res.logs, "ready\n[warning] slow\n[Page Error] SyntaxError: missing ) after argument list");
	assert.equal(res.screenshot, "data:image/png;base64,BASE64");
	assert.equal(res.currentUrl, "file:///workspace/index.html");
	// The listeners are gone after the action: a later action does not see these lines.
	pp.page.emit("console", { type: () => "log", text: () => "late" });
	const again = await driver.type("x");
	assert.equal(again.logs, "");
	assert.equal(pp.page.typed, "x");
});

test("a timeout alone is said; other failures are errors", async () => {
	const root = lane();
	const timeout = Object.assign(new Error("Navigation timeout of 7000 ms exceeded"), { name: "TimeoutError" });
	const pp = fakePuppeteer({ goto: async () => { throw timeout; } });
	const driver = createBrowserDriver({ root, puppeteer: pp });
	await driver.launchBrowser();
	assert.equal((await driver.navigateToUrl("http://localhost:1")).logs, "[Timeout] Navigation timeout of 7000 ms exceeded");
	const broken = fakePuppeteer({ goto: async () => { throw new Error("net::ERR_FILE_NOT_FOUND"); } });
	const d2 = createBrowserDriver({ root, puppeteer: broken });
	await d2.launchBrowser();
	assert.equal((await d2.navigateToUrl("file:///nope")).logs, "[Error] net::ERR_FILE_NOT_FOUND");
	const clicked = await driver.click("10,20");
	assert.deepEqual(pp.page.clicked, [10, 20]);
	assert.equal(clicked.currentMousePosition, "10,20");
	await driver.closeBrowser();
	assert.equal(pp.page.closed, true);
	await assert.rejects(driver.type("x"), /Browser is not launched/);
});

test("a render launches its own Chrome and says when there is no Mermaid", async () => {
	const root = lane();
	const pp = fakePuppeteer();
	const render = createRenderDriver({ root, renderRootId: "root", puppeteer: pp });
	await assert.rejects(render.render({ html: "<p>x</p>", width: 100, scale: 1, fitContent: true, mermaid: { theme: "default" } }), /no Mermaid/);
	assert.equal(pp.launches.length, 0);
});
