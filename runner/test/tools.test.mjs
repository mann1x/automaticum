import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { privateTools } from "../tools.mjs";

test("the SDK's calls of a private tool find it, the shell's PATH does not change", async () => {
	const dir = mkdtempSync(join(tmpdir(), "tools-"));
	const rg = join(dir, "rg");
	writeFileSync(rg, "#!/bin/sh\necho private-rg \"$@\"\n");
	chmodSync(rg, 0o755);
	const path = process.env.PATH;
	assert.deepEqual(privateTools(dir), { rg });
	assert.equal(process.env.PATH, path);

	// Imported after the routing, as the SDK is, and by named import.
	const { spawnSync, execFileSync, execFile, spawn } = await import("node:child_process");
	assert.equal(spawnSync("rg", ["--version"], { encoding: "utf8" }).stdout, "private-rg --version\n");
	assert.equal(execFileSync("rg", ["-a"], { encoding: "utf8" }), "private-rg -a\n");
	const { stdout } = await promisify(execFile)("rg", ["-b"]);
	assert.equal(stdout, "private-rg -b\n");
	const out = await new Promise((resolve) => {
		let s = "";
		const p = spawn("rg", ["-c"]);
		p.stdout.on("data", (d) => (s += d));
		p.on("close", () => resolve(s));
	});
	assert.equal(out, "private-rg -c\n");
	// A command through the shell is the agent's: rg there is the PATH's.
	const sh = spawnSync("sh", ["-c", "command -v rg || echo none"], { encoding: "utf8" }).stdout.trim();
	assert.notEqual(sh, rg);
});

test("no private tools, nothing routed", () => {
	const dir = mkdtempSync(join(tmpdir(), "tools-"));
	assert.deepEqual(privateTools(dir), {});
});
