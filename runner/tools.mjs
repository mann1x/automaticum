// The runner's own tools (D113). In a lane layer on a task's own image the
// runner brings what the SDK runs by itself (ripgrep, for its search) under
// its prefix, /opt/automaticum/bin, which is on no PATH: the agent's
// commands find the image's tools and nothing of the runner's. privateTools
// makes the SDK's own calls of those programs, by name, find them there.
// A command the agent runs goes through the shell, which is not one of
// them, and is left as it is.

import childProcess from "node:child_process";
import { existsSync } from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import { join } from "node:path";
import { promisify } from "node:util";

// The programs the SDK starts by name.
export const TOOLS = ["rg"];

const CALLS = ["spawn", "spawnSync", "execFile", "execFileSync"];

// privateTools routes calls of the tools found in dir to them, for every
// module that imports node:child_process, the SDK included: it must run
// before the SDK is imported. It returns the tools found, by name; with
// none, nothing is changed.
export function privateTools(dir, names = TOOLS) {
	const found = {};
	for (const name of names) {
		const p = join(dir, name);
		if (existsSync(p)) found[name] = p;
	}
	if (Object.keys(found).length === 0) return found;
	const to = (file) => (typeof file === "string" && Object.hasOwn(found, file) ? found[file] : file);
	for (const call of CALLS) {
		const orig = childProcess[call];
		const routed = function (file, ...rest) {
			return orig.call(this, to(file), ...rest);
		};
		// execFile's promisified form resolves to { stdout, stderr }, as
		// the SDK's own git calls expect: keep it.
		if (orig[promisify.custom]) {
			routed[promisify.custom] = (file, ...rest) => orig[promisify.custom](to(file), ...rest);
		}
		childProcess[call] = routed;
	}
	syncBuiltinESMExports();
	return found;
}
