import assert from "node:assert/strict";
import { test } from "node:test";
import { ALREADY_IN_ACT, createSwitchToActModeTool, SWITCH_TO_ACT_MODE_TOOL_NAME, SWITCHED_TO_ACT, withSwitchTool } from "../plan.mjs";

// createTool as the SDK's: it hands back what it was given.
const createTool = (def) => def;

test("the switch tool is the extension's: plan mode only, ends the run, queues the switch", async () => {
	let mode = "plan";
	let queued = 0;
	const tool = createSwitchToActModeTool(createTool, { currentMode: () => mode, queue: () => queued++ });
	assert.equal(tool.name, SWITCH_TO_ACT_MODE_TOOL_NAME);
	assert.deepEqual(tool.inputSchema, { type: "object", properties: {} });
	assert.deepEqual(tool.lifecycle, { completesRun: true });
	assert.equal(tool.timeoutMs, 5000);
	assert.equal(tool.retryable, false);
	assert.equal(tool.maxRetries, 0);
	assert.match(tool.description, /^Switch from plan mode to act mode\./);
	assert.equal(await tool.execute({}), SWITCHED_TO_ACT);
	assert.equal(queued, 1);
	mode = "act";
	assert.equal(await tool.execute({}), ALREADY_IN_ACT);
	assert.equal(queued, 1);

	const other = { name: "check_file" };
	const plan = withSwitchTool({ extraTools: [other] }, "plan", tool);
	assert.deepEqual(plan.extraTools.map((t) => t.name), ["check_file", SWITCH_TO_ACT_MODE_TOOL_NAME]);
	// Started again in act mode, the tool is gone and the others stay.
	assert.deepEqual(withSwitchTool(plan, "act", tool).extraTools.map((t) => t.name), ["check_file"]);
	assert.deepEqual(withSwitchTool({}, "act", tool).extraTools, []);
});
