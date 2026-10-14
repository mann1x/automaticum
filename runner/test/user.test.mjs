import assert from "node:assert/strict";
import { test } from "node:test";
import { APPROVED_ACT, createUser, DENIED, EDIT_DENIED } from "../user.mjs";

const categories = { read: ["read_files", "grep"], edit: ["editor"], commands: ["run_commands"] };

const play = (user) => {
	const lines = [];
	const acted = [];
	const u = createUser({ default_answer: "Continue.", user }, { out: (k, f) => lines.push({ k, ...f }), act: async (r) => acted.push(r.do) });
	return { u, lines, acted };
};

test("without a script every question gets the task's default answer and no approval is asked", () => {
	const { u, lines } = play(undefined);
	assert.equal(u.scripted, false);
	assert.equal(u.question("which file?", []), "Continue.");
	assert.equal(u.counts.questions, 1);
	assert.deepEqual(lines[0], { k: "user", on: "question", rule: 0, do: "answer", question: "which file?", options: [], answer: "Continue." });
});

test("approvals follow the rules in order, then the default", () => {
	const { u, lines } = play({
		default: { approval: "approve", answer: "Go on." },
		categories,
		rules: [
			{ on: "approval", category: "commands", match: "rm -rf", do: "deny", text: "  Do not delete anything. ", times: 0 },
			{ on: "approval", tool: "editor", do: "deny", times: 1 },
		],
	});
	assert.equal(u.scripted, true);
	// Only the tools of a category ask, as in the extension.
	assert.deepEqual(u.asking.sort(), ["editor", "grep", "read_files", "run_commands"]);
	const rm = { toolName: "run_commands", toolCallId: "c1", iteration: 2, input: { commands: ["rm -rf /workspace"] } };
	const denied = u.approval(rm);
	assert.equal(denied.approved, false);
	assert.equal(denied.reason, `${DENIED} The user provided the following feedback:\n<feedback>\nDo not delete anything.\n</feedback>`);
	// times = 0: every time.
	assert.equal(u.approval(rm).approved, false);
	assert.deepEqual(u.approval({ toolName: "run_commands", input: { commands: ["ls"] } }), { approved: true });
	// times = 1: once, then the default. An edit tool's denial is the
	// extension's sentence for an edit.
	assert.deepEqual(u.approval({ toolName: "editor", input: {} }), { approved: false, reason: EDIT_DENIED });
	assert.deepEqual(u.approval({ toolName: "editor", input: {} }), { approved: true });
	assert.deepEqual(u.counts, { approvals: 2, denials: 3, questions: 0, actions: 3 });
	assert.equal(lines[0].on, "approval");
	assert.equal(lines[0].rule, 1);
	assert.equal(lines[0].category, "commands");
	assert.equal(lines.at(-1).rule, 0);
});

test("a default denial carries its feedback", () => {
	const { u } = play({ default: { approval: "deny", feedback: "Ask first.", answer: "x" }, categories, rules: [] });
	assert.match(u.approval({ toolName: "grep", input: {} }).reason, /<feedback>\nAsk first.\n<\/feedback>$/);
});

test("questions are answered by text, by option, or by default", () => {
	const { u, lines } = play({
		default: { approval: "approve", answer: "Use your judgement." },
		rules: [
			{ on: "question", match: "which file", do: "option", option: 2, times: 1 },
			{ on: "question", match: "colour", do: "answer", text: "Blue.", times: 1 },
			{ on: "question", match: "level", do: "option", option: 5, times: 1 },
		],
	});
	assert.equal(u.question("which file should I edit?", ["a.js", "b.js"]), "b.js");
	assert.equal(u.question("what colour?", []), "Blue.");
	// An option the agent did not offer is the default answer.
	assert.equal(u.question("which level?", ["1", "2"]), "Use your judgement.");
	assert.equal(lines.at(-1).offered, 2);
	assert.equal(u.question("anything else?", []), "Use your judgement.");
	assert.equal(u.counts.questions, 4);
});

test("turn and elapsed rules act once when due, before the next request", async () => {
	const { u, acted, lines } = play({
		default: { approval: "approve", answer: "x" },
		rules: [
			{ on: "turn", turn: 2, do: "steer", text: "Run the checker.", times: 1 },
			{ on: "elapsed", after_s: 60, do: "queue", text: "Wrap up.", times: 1 },
			{ on: "turn", turn: 3, do: "restore", restore: "workspace", checkpoint: 1, times: 0 },
		],
	});
	u.iteration();
	await u.beforeModel(1000);
	assert.deepEqual(acted, []);
	u.iteration();
	await u.beforeModel(2000);
	await u.beforeModel(2500); // a retried request of the same turn
	assert.deepEqual(acted, ["steer"]);
	u.iteration();
	await u.beforeModel(61000);
	assert.deepEqual(acted, ["steer", "queue", "restore"]);
	u.iteration();
	await u.beforeModel(70000);
	assert.deepEqual(acted, ["steer", "queue", "restore"]);
	assert.equal(lines.find((l) => l.do === "restore").checkpoint, 1);
	assert.equal(lines.find((l) => l.on === "elapsed").active_ms, 61000);
});

test("a cancel is followed by its resume message, a completion by the queued one, then nothing", async () => {
	const { u, acted, lines } = play({
		default: { approval: "approve", answer: "x" },
		rules: [
			{ on: "turn", turn: 2, do: "cancel", text: "Start again from the failing test.", times: 1 },
			{ on: "completion", do: "queue", text: "Now add a test.", times: 1 },
		],
	});
	u.iteration();
	u.iteration();
	await u.beforeModel(0);
	assert.deepEqual(acted, ["cancel"]);
	assert.deepEqual(u.next({ finishReason: "aborted" }, "act"), { prompt: "Start again from the failing test." });
	assert.deepEqual(u.next({ finishReason: "completed" }, "act"), { prompt: "Now add a test." });
	assert.equal(u.next({ finishReason: "completed" }, "act"), null);
	assert.deepEqual(lines.map((l) => l.on), ["turn", "resume", "completion"]);
	// Only rules count as actions; the resume is the cancel's second half.
	assert.equal(u.counts.actions, 2);
});

test("turns are counted over the whole run, across a resume", async () => {
	const { u, acted } = play({
		default: { approval: "approve", answer: "x" },
		rules: [
			{ on: "turn", turn: 2, do: "cancel", text: "Again.", times: 1 },
			{ on: "turn", turn: 4, do: "restore", restore: "workspace", checkpoint: 1, times: 1 },
		],
	});
	for (let i = 0; i < 2; i++) u.iteration();
	await u.beforeModel(0);
	u.next({ finishReason: "aborted" }, "act");
	// The SDK numbers the turns of the resumed run from 1 again.
	for (let i = 0; i < 2; i++) {
		u.iteration();
		await u.beforeModel(0);
	}
	assert.deepEqual(acted, ["cancel", "restore"]);
});

test("in plan mode an end is a plan: a switch to act with the extension's sentence", () => {
	const { u, lines } = play({
		default: { approval: "approve", answer: "x" },
		rules: [
			{ on: "plan", do: "mode", mode: "act", times: 1 },
			{ on: "completion", do: "queue", text: "Check it once more.", times: 1 },
		],
	});
	assert.equal(u.continues, true);
	u.tool("switch_to_act_mode");
	assert.deepEqual(u.next({ finishReason: "completed" }, "plan"), { prompt: APPROVED_ACT, mode: "act" });
	assert.equal(lines.at(-1).on, "plan");
	assert.equal(lines.at(-1).switch_asked, true);
	// Back in act mode, an end is a completion.
	assert.deepEqual(u.next({ finishReason: "completed" }, "act"), { prompt: "Check it once more." });
	assert.equal(u.next({ finishReason: "completed" }, "act"), null);
});

test("a switch and a fork at a turn stop the work, then are said when it has stopped", async () => {
	const { u, acted, lines } = play({
		default: { approval: "approve", answer: "x" },
		rules: [
			{ on: "turn", turn: 1, do: "mode", mode: "plan", text: "Plan it first.", times: 1 },
			{ on: "turn", turn: 2, do: "restore", restore: "both", checkpoint: 1, text: "Start over.", times: 1 },
		],
	});
	u.iteration();
	await u.beforeModel(0);
	assert.deepEqual(acted, ["mode"]);
	assert.deepEqual(u.next({ finishReason: "aborted" }, "act"), { prompt: "Plan it first.", mode: "plan" });
	u.iteration();
	await u.beforeModel(0);
	assert.deepEqual(acted, ["mode", "restore"]);
	assert.deepEqual(u.next({ finishReason: "aborted" }, "plan"), { prompt: "Start over.", fork: { checkpoint: 1, workspace: true } });
	assert.deepEqual(
		lines.map((l) => [l.on, l.do]),
		[["turn", "mode"], ["resume", "mode"], ["turn", "restore"], ["resume", "restore"]],
	);
});
