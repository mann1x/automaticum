// Builds the oracle the agent may run but not read: run_game.logic.js as a
// Node single executable.
//
//   node build.mjs <source.js> <output>
//
// Inside the binary the source is deflated and masked with a keystream, stored
// reversed, and the loader spells its module and function names as masked
// character codes, so `strings` on the binary shows neither the checks nor a
// readable recipe for unmasking them. This keeps an agent from reading the
// answer in its own workspace; it is not secrecy (the source is public).
//
// The seed and the salt come from the source's sha256, so the same source and
// the same Node binary give the same executable, byte for byte.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, copyFileSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { deflateRawSync } from "node:zlib";

const [source, output] = process.argv.slice(2);
if (!source || !output) {
	console.error("usage: node build.mjs <source.js> <output>");
	process.exit(2);
}

const src = readFileSync(source);
const digest = createHash("sha256").update(src).digest();
const seed = digest.readUInt32BE(0);
const salt = 1 + (digest[4] % 249);

const raw = deflateRawSync(src, { level: 9 });
let s = seed >>> 0;
for (let i = 0; i < raw.length; i++) {
	s = (Math.imul(s, 1103515245) + 12345) >>> 0;
	raw[i] ^= (s >>> 16) & 255;
}
const blob = raw.toString("base64").split("").reverse().join("");
const mask = (text) => `[${[...text].map((ch, i) => ch.charCodeAt(0) ^ ((i * 7 + salt) & 255)).join(",")}]`;

const loader =
	`const q=(a)=>String.fromCharCode(...a.map((c,i)=>c^((i*7+${salt})&255)));` +
	`const z=require(q(${mask("node:zlib")}));` +
	`let s=${seed}>>>0;const b=Buffer.from("${blob}".split("").reverse().join(""),q(${mask("base64")}));` +
	`for(let i=0;i<b.length;i++){s=(Math.imul(s,1103515245)+12345)>>>0;b[i]^=(s>>>16)&255;}` +
	`new Function(q(${mask("require")}),q(${mask("process")}),q(${mask("console")}),z[q(${mask("inflateRawSync")})](b).toString())(require,process,console);\n`;

// Relative names: the main script's path is embedded in the binary.
const dir = mkdtempSync(join(tmpdir(), "oracle-build-"));
try {
	writeFileSync(join(dir, "main.js"), loader);
	writeFileSync(
		join(dir, "sea-config.json"),
		JSON.stringify({ main: "main.js", output: "run_game", disableExperimentalSEAWarning: true, useCodeCache: false }),
	);
	execFileSync(process.execPath, ["--build-sea", "sea-config.json"], { cwd: dir, stdio: ["ignore", "inherit", "inherit"] });
	copyFileSync(join(dir, "run_game"), output);
	chmodSync(output, 0o555);
} finally {
	rmSync(dir, { recursive: true, force: true });
}
const out = readFileSync(output);
console.log(JSON.stringify({ source_sha256: digest.toString("hex"), node: process.version, sha256: createHash("sha256").update(out).digest("hex"), bytes: out.length }));
