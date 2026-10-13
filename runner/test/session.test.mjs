import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { buildConfig, compareResolved, derivedFromArm, unsupported } from "../session.mjs";

const stub = () => JSON.parse(readFileSync(new URL("./arm-stub.json", import.meta.url), "utf8"));
const jobOf = (arm) => ({
	run_id: "r1", session_id: "run-r1", arm, workspace: "/workspace",
	endpoint: { name: "stub", kind: "ollama", base_url: "http://model:11434" },
});
const build = (arm) => buildConfig(jobOf(arm), { systemPrompt: "SP", fetch: () => {}, onConfigResolved: () => {} });

// What an honest SDK would report for a configuration.
function reportOf(config, more = {}) {
	const session = JSON.parse(JSON.stringify(config));
	delete session.systemPrompt;
	delete session.providerConfig;
	return {
		sessionId: config.sessionId, providerId: config.providerId, modelId: config.modelId, baseUrl: config.baseUrl,
		contextWindow: config.contextWindow,
		thinking: { enabled: config.thinking, effort: config.reasoningEffort, budgetTokens: config.thinkingBudgetTokens },
		sampling: config.providerConfig?.sampling,
		provider: {}, session,
		sources: { storedProviderSettings: "ignored", inheritedFromStoredProfile: [], globalSettingsPresent: false },
		...more,
	};
}

test("every guard and compaction setting is given a value", () => {
	const { config, localRuntime, toolPolicies } = build(stub());
	assert.equal(config.storedProviderSettings, "ignore");
	assert.equal(config.contextWindow, 32768);
	assert.equal(config.baseUrl, "http://model:11434");
	assert.equal(config.strongNudges, true);
	assert.equal(config.toolIntentGuard, true);
	assert.equal(config.staleReadRewrites, true);
	assert.deepEqual(config.editVerification, { mode: "nudge" });
	assert.deepEqual(config.execution, {
		maxConsecutiveMistakes: 6,
		loopDetection: { softThreshold: 3, hardThreshold: 5 },
		reasoningLoopDetection: {},
		reasoningRepetition: {},
	});
	assert.equal(config.compaction.enabled, true);
	assert.equal(config.compaction.strategy, "agentic");
	assert.equal(config.compaction.councilEnabled, true);
	assert.deepEqual(config.atomicProtocol, { mode: "off" });
	assert.deepEqual(config.taskProgress, { enabled: true, reminderInterval: 2 });
	assert.deepEqual(config.checkpoint, { enabled: true });
	assert.equal(config.maxToolResultChars, 64000);
	// Owned by the model tag: nothing is sent.
	for (const k of ["thinking", "reasoningEffort", "providerConfig", "maxIterations", "maxTokensPerTurn", "apiKey"]) {
		assert.equal(k in config, false, k);
	}
	assert.deepEqual(localRuntime.configExtensions, []);
	assert.deepEqual(toolPolicies, {});
});

test("switched off means off, not left out", () => {
	const arm = stub();
	for (const g of ["loop_detection", "reasoning_loop", "reasoning_repetition", "strong_nudges", "tool_intent", "stale_read_rewrites"]) {
		arm.guards[g] = { enabled: false };
	}
	arm.compaction = { enabled: false };
	arm.agent.task_progress = { enabled: false };
	arm.agent.checkpoints = false;
	const { config } = build(arm);
	assert.equal(config.execution.loopDetection, false);
	assert.equal(config.execution.reasoningLoopDetection, false);
	assert.equal(config.execution.reasoningRepetition, false);
	assert.equal(config.strongNudges, false);
	assert.equal(config.toolIntentGuard, false);
	assert.equal(config.staleReadRewrites, false);
	assert.deepEqual(config.compaction, { enabled: false });
	assert.deepEqual(config.taskProgress, { enabled: false });
	assert.deepEqual(config.checkpoint, { enabled: false });
});

test("a sampler and thinking owned by the arm are sent, under the session's provider", () => {
	const arm = stub();
	arm.sampler = { owner: "arm", temperature: 0.8, repeat_last_n: 96, repeat_penalty: 1.05, presence_penalty: 1.1 };
	arm.thinking = { owner: "arm", enabled: true, effort: "xhigh" };
	arm.model.output_tokens = 4096;
	arm.agent.disabled_tools = ["web_fetch"];
	const { config, toolPolicies } = build(arm);
	assert.deepEqual(config.providerConfig, {
		providerId: "ollama", modelId: "stub:latest",
		sampling: { temperature: 0.8, repeatLastN: 96, repeatPenalty: 1.05, presencePenalty: 1.1 },
	});
	assert.equal(config.thinking, true);
	assert.equal(config.reasoningEffort, "xhigh");
	assert.equal(config.maxTokensPerTurn, 4096);
	assert.deepEqual(toolPolicies, { web_fetch: { enabled: false } });
	assert.deepEqual(compareResolved(jobOf(arm), config, reportOf(config)), []);

	// The SDK derives a thinking budget from the effort. The arm states
	// none, so that is recorded, not refused; a stated one must survive.
	const report = reportOf(config);
	report.thinking.budgetTokens = 76800;
	report.maxOutputTokens = 24576;
	assert.deepEqual(compareResolved(jobOf(arm), config, report), []);
	assert.deepEqual(derivedFromArm(jobOf(arm), report), { thinking_budget_tokens: 76800 });
	arm.thinking.budget_tokens = 4096;
	const again = build(arm).config;
	const problems = compareResolved(jobOf(arm), again, { ...reportOf(again), thinking: { enabled: true, effort: "xhigh", budgetTokens: 76800 } });
	assert.equal(problems.length, 1);
	assert.ok(problems[0].startsWith("thinking.budget_tokens"));
});

test("an honest report compares clean; each difference is named", () => {
	const arm = stub();
	const job = jobOf(arm);
	const { config } = build(arm);
	assert.deepEqual(compareResolved(job, config, reportOf(config)), []);

	const cases = [
		[(r) => { r.contextWindow = 8192; }, "model.context_window"],
		[(r) => { r.baseUrl = "http://localhost:11434"; }, "endpoint address"],
		[(r) => { r.session.compaction.enabled = false; }, "session compaction"],
		[(r) => { delete r.session.strongNudges; }, "session strongNudges"],
		[(r) => { r.session.execution.loopDetection = { softThreshold: 2, hardThreshold: 5 }; }, "session execution"],
		[(r) => { r.sources.storedProviderSettings = "merged"; }, "stored provider profile was merged"],
		[(r) => { r.sources.inheritedFromStoredProfile = ["sampling.temperature"]; }, "sampling.temperature was inherited"],
		[(r) => { r.sources.globalSettingsPresent = true; }, "global settings file"],
		[(r) => { r.sampling = { temperature: 0.2 }; }, "sampler is owned by the model tag"],
		[(r) => { r.thinking = { enabled: true }; }, "thinking is owned by the model tag"],
	];
	for (const [change, want] of cases) {
		const report = reportOf(config);
		change(report);
		const problems = compareResolved(job, config, report);
		assert.equal(problems.length, 1, `${want}: ${JSON.stringify(problems)}`);
		assert.ok(problems[0].includes(want), `${want}: ${problems[0]}`);
	}
	// The runner's own global settings file is not a leftover.
	const report = reportOf(config);
	report.sources.globalSettingsPresent = true;
	assert.deepEqual(compareResolved(job, config, report, { wroteGlobalSettings: true }), []);
});

test("what protocol 1 cannot do is refused, not approximated", () => {
	assert.deepEqual(unsupported(stub()), []);
	const arm = stub();
	arm.agent.approval = "ask";
	arm.agent.escalation = true;
	arm.agent.sub_agents.spawn = true;
	arm.agent.bundled_skills = ["media-audio", "sdd-plan"];
	assert.equal(unsupported(arm).length, 4);
});
