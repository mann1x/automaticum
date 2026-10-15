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

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, statSync, writeSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { BUNDLED_SKILLS_ON_BY_DEFAULT, buildConfig, compareResolved, derivedFromArm, unsupported } from "./session.mjs";
import { createUser } from "./user.mjs";
import { createWire, describe } from "./wire.mjs";

const PROTOCOL = 3;
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
	// The scripted user (040 §6.2). Its actions need the session, which is
	// made below; they are only ever taken once it runs.
	let core;
	const user = createUser(job, {
		out,
		act: async (rule) => {
			const failed = (err) => note("user_action_failed", { rule: rule.index, do: rule.do, error: describe(err) });
			switch (rule.do) {
				case "steer":
				case "queue":
					// Not awaited: a queued message waits for the turn to end.
					core.send({ sessionId: session.id, prompt: rule.text, delivery: rule.do }).catch(failed);
					return;
				case "cancel":
				case "mode":
					// A switch is made once the running work has stopped.
					await core.abort(session.id, "the scripted user " + (rule.do === "mode" ? "switches mode" : "cancelled")).catch(failed);
					return;
				case "restore":
					if (rule.restore !== "workspace") {
						// A fork is made once the running work has stopped.
						await core.abort(session.id, "the scripted user restores a checkpoint").catch(failed);
						return;
					}
					try {
						const res = await core.restore({ sessionId: session.id, checkpointRunCount: rule.checkpoint, restore: { messages: false, workspace: true } });
						note("restored", { rule: rule.index, checkpoint: res?.checkpoint ?? null });
					} catch (err) {
						failed(err);
					}
					return;
			}
		},
	});
	core = await sdk.ClineCore.create({
		backendMode: "local",
		dataDir,
		clientName: "automaticum-runner",
		fetch: wire.fetch,
		capabilities: {
			// One record of the files read, for every session of the run, as
			// the extension keeps one for its controller's lifetime: the
			// read-before-edit guard does not forget across a mode switch or
			// a fork (Cerebriline session, 2026-10-15). Without it core
			// makes a fresh one per session.
			readReceipts: sdk.createReadReceipts(),
			requestToolApproval: (request) => user.approval(request),
			toolExecutors: {
				askQuestion: async (question, options) => user.question(question, options),
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
	const promptFor = (mode) =>
		sdk.getClineDefaultSystemPrompt({
			ide: arm.agent.host_ide,
			workspaceRoot: job.workspace,
			workspaceName: basename(job.workspace),
			mode,
			providerId: arm.model.provider,
			platform: process.platform,
			basePrompt: template.rendered?.system,
		});
	const sha = (text) => createHash("sha256").update(text).digest("hex");
	const hooks = sdk.createPromptTemplateHooks({ rendered: template.rendered, ideName: arm.agent.host_ide, log: (m) => note("prompt_template", { message: m }) });
	const firstPrompt = promptFor(arm.agent.mode);
	note("prepared", {
		sdk: sdkFound,
		node: process.version,
		bundled_skills_switched_off: switchedOff,
		prompt_template: template.rendered ? { name: template.rendered.name, source: template.rendered.source, overlaid: template.rendered.overlaid } : null,
		prompt_template_errors: template.errors?.length ?? 0,
		model_family_looked_up: false,
		system_prompt_sha256: sha(firstPrompt),
		system_prompt_bytes: Buffer.byteLength(firstPrompt),
	});

	// 5a. The hold (PROTOCOL.md): while the file "hold" stands next to the
	// job, the run waits before its next model request. The request being
	// answered has finished and its tools have run; nothing is cut. The SDK
	// awaits this hook before every request and keeps every counter and
	// guard as it is. Time held is not the run's: the wall limit stops.
	const holdFile = join(dirname(process.argv[2]), "hold");
	// The session the run talks to now. A mode switch starts it again under
	// the same id; a restore of the messages (D86) starts another.
	const session = { id: job.session_id, mode: arm.agent.mode, forks: 0 };
	let wall;
	let wallLeft = job.wall_s * 1000;
	let wallFrom = 0;
	let wallFired = false;
	const runFrom = Date.now();
	let heldMs = 0;
	const armWall = () => {
		wallFrom = Date.now();
		wall = setTimeout(() => {
			wallFired = true;
			note("wall_limit", { wall_s: job.wall_s });
			core.abort(session.id, "wall limit").catch((err) => note("abort_failed", { error: describe(err) }));
		}, wallLeft);
	};
	const beforeModel = async () => {
		if (existsSync(holdFile)) {
			clearTimeout(wall);
			wallLeft = Math.max(0, wallLeft - (Date.now() - wallFrom));
			const since = Date.now();
			note("held", { requests_so_far: wire.count() });
			let beat = since;
			while (existsSync(holdFile)) {
				await new Promise((r) => setTimeout(r, 500));
				if (Date.now() - beat >= 60000) {
					beat = Date.now();
					note("holding", { held_ms: beat - since });
				}
			}
			note("released", { held_ms: Date.now() - since });
			heldMs += Date.now() - since;
			armWall();
		}
		await user.beforeModel(Date.now() - runFrom - heldMs);
	};

	// 5. The session, from the arm alone; what the SDK built is read back
	// before the first request and a difference stops the start. A session
	// started again in another mode, or forked, is built and read back the
	// same way, for its own id and mode.
	let problems = null;
	const startInput = (id, mode) => {
		const own = { ...job, session_id: id, arm: { ...arm, agent: { ...arm.agent, mode } } };
		const built = buildConfig(own, {
			systemPrompt: mode === arm.agent.mode ? firstPrompt : promptFor(mode),
			fetch: wire.fetch,
			onConfigResolved: (resolved) => {
				note("config_resolved", { session: id, mode, resolved, derived: derivedFromArm(own, resolved) });
				const found = compareResolved(own, built.config, resolved, { wroteGlobalSettings: switchedOff.length > 0 });
				problems = [...(problems ?? []), ...found];
				if (found.length > 0) throw new Error("the session is not the arm: " + found.join("; "));
			},
		});
		const prior = hooks?.beforeModel;
		built.config.hooks = {
			...(hooks ?? {}),
			beforeModel: async (context) => {
				await beforeModel();
				return prior ? prior(context) : undefined;
			},
		};
		// With a script, the tools of the extension's approval categories
		// ask and the script decides; any other tool runs without asking, as
		// in the extension. Without a script every tool runs, as before. A
		// tool the arm disables stays disabled.
		if (user.scripted) for (const tool of user.asking) built.toolPolicies[tool] = { ...(built.toolPolicies[tool] ?? {}), autoApprove: false };
		return { config: built.config, localRuntime: built.localRuntime, toolPolicies: built.toolPolicies, interactive: user.continues };
	};

	let unsubscribe = () => {};
	const follow = (id) => {
		unsubscribe();
		unsubscribe =
			core.subscribe(
				(event) => {
					out("event", { event });
					const inner = event?.payload?.event;
					if (event?.type !== "agent_event" || (event.payload.teamRole ?? "lead") !== "lead") return;
					if (inner?.type === "iteration_start") user.iteration();
					if (inner?.type === "content_start" && inner.contentType === "tool") user.tool(inner.toolName);
				},
				{ sessionId: id },
			) ?? (() => {});
	};
	follow(session.id);

	// A mode switch as the extension makes it (sdk-mode-coordinator.ts):
	// the live messages and the saved compaction state, a session of the
	// same id started again in the new mode with them, the old one stopped.
	const switchMode = async (to) => {
		const from = session.mode;
		const messages = await core.readLiveMessages(session.id);
		const compaction = await core.readSessionCompactionState(session.id).catch(() => undefined);
		await core.stop(session.id).catch((err) => note("stop_failed", { error: describe(err) }));
		session.mode = to;
		follow(session.id);
		await core.start({ ...startInput(session.id, to), initialMessages: messages, initialCompactionState: compaction });
		note("mode_switched", { session: session.id, from, to, messages: messages.length, compaction_carried: compaction !== undefined });
	};
	// A restore of the messages (D86): a new session, the conversation up to
	// the checkpoint, the old one stopped. Every counter and guard of the SDK
	// starts again in it.
	const fork = async ({ checkpoint, workspace }) => {
		const from = session.id;
		const id = `${job.session_id}-fork${++session.forks}`;
		const res = await core.restore({ sessionId: from, checkpointRunCount: checkpoint, start: startInput(id, session.mode), restore: { messages: true, workspace } });
		const to = res?.sessionId ?? res?.startResult?.sessionId ?? id;
		await core.stop(from).catch((err) => note("stop_failed", { error: describe(err) }));
		session.id = to;
		follow(to);
		note("fork", {
			from,
			to,
			checkpoint,
			checkpoint_ref: res?.checkpoint?.ref ?? null,
			workspace,
			messages: res?.messages?.length ?? null,
			restarted: ["iterations", "consecutive mistakes", "loop guards", "compaction state"],
		});
	};

	// 5b. A task that asks for it gets its workspace as a git repository,
	// the seed its first commit: the SDK's checkpoints are commits in it,
	// and a workspace that is not a repository has none.
	if (job.workspace_git) {
		const git = (...args) => execFileSync("git", args, { cwd: job.workspace, encoding: "utf8" }).trim();
		try {
			// In the runner's own fresh home, so the SDK's git commands read
			// it too: the workspace is mounted from the host and may belong
			// to another user, and a checkpoint is a commit, which needs an
			// author.
			git("config", "--global", "--add", "safe.directory", "*");
			git("config", "--global", "user.name", "automaticum");
			git("config", "--global", "user.email", "automaticum@localhost");
			git("init", "-q");
			git("add", "-A");
			git("commit", "-q", "--allow-empty", "-m", "the task's seed");
			note("workspace_git", { head: git("rev-parse", "HEAD") });
		} catch (err) {
			end("refused", { reason: "the workspace cannot be made a git repository: " + describe(err).message, sdk: sdkFound });
		}
	}

	// 6. The wall limit is ours: the SDK has none.
	armWall();
	// A session the user may speak to again after it ends stays open; it is
	// stopped below.
	if (user.continues) note("interactive", { why: "the scripted user may speak after a run of the session ends (a resume, a switch, a fork, a plan or a completion)" });

	let started;
	let failure;
	try {
		started = await core.start({ ...startInput(session.id, session.mode), prompt: job.prompt });
		// The user may speak again when a run of the session ends: the second
		// half of its own cancel, switch or fork, or what a plan or a
		// completion rule says.
		let last = started?.result;
		for (let say = user.next(last, session.mode); say !== null && !wallFired; say = user.next(last, session.mode)) {
			if (say.mode && say.mode !== session.mode) await switchMode(say.mode);
			if (say.fork) await fork(say.fork);
			const more = await core.send({ sessionId: session.id, prompt: say.prompt });
			if (more) last = more;
		}
		if (started && last) started = { ...started, result: last };
		if (user.continues) await core.stop(session.id).catch((err) => note("stop_failed", { error: describe(err) }));
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
		questions_answered: user.counts.questions,
		user: user.scripted ? user.counts : null,
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
