#!/usr/bin/env node
// The runner: one agent session for one run, inside the lane's container.
//
//   node runner.mjs /job/job.json
//
// It reads the job, checks that the SDK installed is the release the job
// names, starts a session built from the resolved arm and nothing else,
// compares what the SDK says it built with the arm, and prints everything
// as JSON lines. It never runs the oracle and never sees it.
// The contract is in PROTOCOL.md.

import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, statSync, writeSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { BUNDLED_SKILLS_ON_BY_DEFAULT, buildConfig, compareResolved, unsupported } from "./session.mjs";
import { createWire, describe } from "./wire.mjs";

const PROTOCOL = 1;
const SDK_PACKAGE = "@cerebriline/sdk";
const EXIT = { completed: 0, error: 1, refused: 3, invalid: 4, wall_limit: 5 };

// An Error inside an event serialises to {} unless it is described.
const plain = (_key, value) => (value instanceof Error ? describe(value) : typeof value === "bigint" ? String(value) : value);

// Lines are written synchronously: a line is whole or absent, and nothing is
// lost when the process is ended right after its last line.
function out(kind, fields) {
	writeSync(1, JSON.stringify({ t: new Date().toISOString(), k: kind, ...fields }, plain) + "\n");
}
const note = (what, fields = {}) => out("runner", { note: what, ...fields });

function end(outcome, fields = {}) {
	out("end", { outcome, ...fields });
	process.exit(EXIT[outcome] ?? 1);
}

// installedSDK finds the package on disk and reads its release from its own
// manifest. The SDK's build constant carries another number.
function installedSDK() {
	let dir = dirname(fileURLToPath(import.meta.resolve(SDK_PACKAGE)));
	for (;;) {
		try {
			const manifest = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
			if (manifest.name === SDK_PACKAGE) return { dir, release: manifest.version };
		} catch {}
		const up = dirname(dir);
		if (up === dir) throw new Error(`${SDK_PACKAGE} has no manifest on disk`);
		dir = up;
	}
}

// packageHash is the sha256 over the name and hash of every file of the
// installed package, in name order.
function packageHash(dir) {
	const files = [];
	const walk = (d) => {
		for (const name of readdirSync(d).sort()) {
			if (name === "node_modules") continue;
			const p = join(d, name);
			const st = statSync(p);
			if (st.isDirectory()) walk(p);
			else if (st.isFile()) files.push(p);
		}
	};
	walk(dir);
	const all = createHash("sha256");
	for (const f of files) all.update(f.slice(dir.length) + "\0" + createHash("sha256").update(readFileSync(f)).digest("hex") + "\n");
	return all.digest("hex");
}

async function main() {
	const job = JSON.parse(readFileSync(process.argv[2], "utf8"));
	const arm = job.arm;
	const sdkFound = { release: "", package_sha256: "" };
	if (job.protocol !== PROTOCOL) end("refused", { reason: `the job speaks protocol ${job.protocol}, this runner ${PROTOCOL}`, sdk: sdkFound });

	// 1. The SDK release, before anything else.
	let pkg;
	try {
		pkg = installedSDK();
		sdkFound.release = pkg.release;
		sdkFound.package_sha256 = packageHash(pkg.dir);
	} catch (err) {
		end("refused", { reason: "the SDK release cannot be established: " + describe(err).message, sdk: sdkFound });
	}
	if (sdkFound.release !== job.sdk_release) {
		end("refused", { reason: `the installed SDK is release ${sdkFound.release}, the arm names ${job.sdk_release}`, sdk: sdkFound });
	}
	const cannot = unsupported(arm);
	if (cannot.length > 0) end("refused", { reason: "the arm asks for what this runner cannot do", problems: cannot, sdk: sdkFound });

	// 2. A clean place: a home nobody wrote to, a data directory that is
	// collected afterwards, and no setting carried in by the environment.
	for (const key of Object.keys(process.env)) {
		if (/^(CLINE|CEREBRILINE|OLLAMA|XOLLAMA)_/.test(key)) delete process.env[key];
	}
	const home = mkdtempSync(join(tmpdir(), "home-"));
	process.env.HOME = home;
	process.env.USERPROFILE = home;
	const dataDir = join(job.out, "data");
	mkdirSync(dataDir, { recursive: true });

	const wire = createWire({ file: join(job.out, "wire.jsonl") });
	const sdk = await import(SDK_PACKAGE);
	let asked = 0;
	const core = await sdk.ClineCore.create({
		backendMode: "local",
		dataDir,
		clientName: "automaticum-runner",
		fetch: wire.fetch,
		capabilities: {
			toolExecutors: {
				// The scripted user of protocol 1: the task's default answer.
				askQuestion: async (question, options) => {
					asked++;
					note("ask_question", { question, options, answer: job.default_answer });
					return job.default_answer;
				},
			},
		},
	});

	// 3. Bundled skills the arm does not list are switched off. This writes
	// the global settings file, which is therefore the runner's own.
	const switchedOff = BUNDLED_SKILLS_ON_BY_DEFAULT.filter((s) => !(arm.agent.bundled_skills ?? []).includes(s));
	for (const skill of switchedOff) sdk.setBundledSkillEnabled(skill, false);

	// 4. The system prompt, built as the hosts build it, with the IDE name
	// the arm states. The model family is not looked up on the server, so a
	// template chosen by family alone is not chosen here; the record says so.
	const template = sdk.resolveSessionPromptTemplateFrom({
		providerId: arm.model.provider,
		modelId: arm.model.model,
		globalDir: join(dataDir, "templates"),
		workspaceDir: join(job.workspace, ".clinerules", "templates"),
	});
	const systemPrompt = sdk.getClineDefaultSystemPrompt({
		ide: arm.agent.host_ide,
		workspaceRoot: job.workspace,
		workspaceName: basename(job.workspace),
		mode: arm.agent.mode,
		providerId: arm.model.provider,
		platform: process.platform,
		basePrompt: template.rendered?.system,
	});
	const hooks = sdk.createPromptTemplateHooks({ rendered: template.rendered, ideName: arm.agent.host_ide, log: (m) => note("prompt_template", { message: m }) });
	note("prepared", {
		sdk: sdkFound,
		node: process.version,
		bundled_skills_switched_off: switchedOff,
		prompt_template: template.rendered ? { name: template.rendered.name, source: template.rendered.source, overlaid: template.rendered.overlaid } : null,
		prompt_template_errors: template.errors?.length ?? 0,
		model_family_looked_up: false,
		system_prompt_sha256: createHash("sha256").update(systemPrompt).digest("hex"),
		system_prompt_bytes: Buffer.byteLength(systemPrompt),
	});

	// 5. The session, from the arm alone; what the SDK built is read back
	// before the first request and a difference stops the start.
	let problems = null;
	const built = buildConfig(job, {
		systemPrompt,
		fetch: wire.fetch,
		onConfigResolved: (resolved) => {
			note("config_resolved", { resolved });
			problems = compareResolved(job, built.config, resolved, { wroteGlobalSettings: switchedOff.length > 0 });
			if (problems.length > 0) throw new Error("the session is not the arm: " + problems.join("; "));
		},
	});
	if (hooks) built.config.hooks = hooks;

	core.subscribe((event) => out("event", { event }), { sessionId: job.session_id });

	// 6. The wall limit is ours: the SDK has none.
	let wallFired = false;
	const wall = setTimeout(() => {
		wallFired = true;
		note("wall_limit", { wall_s: job.wall_s });
		core.abort(job.session_id, "wall limit").catch((err) => note("abort_failed", { error: describe(err) }));
	}, job.wall_s * 1000);

	let started;
	let failure;
	try {
		started = await core.start({
			config: built.config,
			prompt: job.prompt,
			interactive: false,
			localRuntime: built.localRuntime,
			toolPolicies: built.toolPolicies,
		});
	} catch (err) {
		failure = err;
	}
	clearTimeout(wall);
	const cut = await wire.close();
	if (cut > 0) note("wire_cut", { responses_still_being_read: cut });
	await core.dispose("run over").catch((err) => note("dispose_failed", { error: describe(err) }));

	const result = started?.result;
	const fields = {
		sdk: sdkFound,
		read_back: problems === null ? "the SDK did not report its resolved configuration" : "compared",
		requests: wire.count(),
		questions_answered: asked,
		agent_end: result?.finishReason ?? "",
		// The agent claims success when it ends its task by itself.
		claimed_success: result ? result.finishReason === "completed" : null,
		usage: result?.usage ?? null,
		iterations: result?.iterations ?? null,
	};
	if (problems !== null && problems.length > 0) end("invalid", { ...fields, reason: "the session is not the arm", problems });
	if (problems === null && !failure) end("invalid", { ...fields, reason: fields.read_back, problems: [fields.read_back] });
	if (wallFired) end("wall_limit", { ...fields, reason: `the wall limit of ${job.wall_s} s` });
	if (failure) end("error", { ...fields, reason: describe(failure).message, error: describe(failure) });
	end("completed", fields);
}

main().catch((err) => {
	// Anything not foreseen: say so in the last line rather than die silent.
	out("end", { outcome: "error", reason: "the runner failed: " + describe(err).message, error: describe(err), sdk: { release: "", package_sha256: "" } });
	process.exit(1);
});
