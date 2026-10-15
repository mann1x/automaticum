// The extension's switch_to_act_mode tool, ported from Cline 4.100.250 and
// 4.100.251 (the same in both): apps/vscode/src/sdk/sdk-session-config-builder.ts
// (the tool, offered in plan mode only), sdk-mode-coordinator.ts (the switch
// it queues, applied when the session goes idle, then an automatic
// continuation) and sdk-user-message-mapping.ts (the continuation's text).
// It is in no SDK release: the extension builds it, so the runner does too.
// The texts below are the extension's, word for word.

export const SWITCH_TO_ACT_MODE_TOOL_NAME = "switch_to_act_mode";

export const SWITCH_TO_ACT_MODE_TOOL_DESCRIPTION =
	"Switch from plan mode to act mode. Switching to act mode immediately starts executing the plan, so only call this after the user has explicitly approved the plan in a message sent AFTER you presented it (e.g. 'looks good', 'go ahead', 'switch to act mode'). " +
	"Never call this in the same turn you present a plan, never call it proactively, and never treat the original task request as approval. " +
	"Output: a one-line confirmation, as plain text. This call ends the current run and the next one starts in act mode with the file and command tools available, so it is a handover, not a failure — carry on with the plan there.";

export const SWITCHED_TO_ACT = "You successfully switched to act mode, proceed with the plan. You now have access to editing files and running commands. (The switch_to_act_mode tool is only available in plan mode.)";
export const ALREADY_IN_ACT = "Already in act mode.";

// The text sent when the session is started again in act mode: the
// extension's ACT_MODE_CONTINUATION_PROMPT, the same sentence a user's switch
// without a message of its own sends.
export const ACT_MODE_CONTINUATION_PROMPT = "The user approved switching to act mode. Continue with the approved plan now.";

// createSwitchToActModeTool builds the tool with the SDK's createTool.
// currentMode says the session's mode; queue asks for the switch, which the
// runner makes once the run has ended (the tool ends it: completesRun).
export function createSwitchToActModeTool(createTool, { currentMode, queue }) {
	return createTool({
		name: SWITCH_TO_ACT_MODE_TOOL_NAME,
		description: SWITCH_TO_ACT_MODE_TOOL_DESCRIPTION,
		inputSchema: { type: "object", properties: {} },
		timeoutMs: 5000,
		retryable: false,
		maxRetries: 0,
		lifecycle: { completesRun: true },
		execute: async () => {
			if (currentMode() === "act") return ALREADY_IN_ACT;
			queue();
			return SWITCHED_TO_ACT;
		},
	});
}

// withSwitchTool is the extension's config step: in plan mode the tool is
// added to the session's extra tools; in any other mode it is taken out.
export function withSwitchTool(config, mode, tool) {
	const others = (config.extraTools ?? []).filter((t) => t.name !== SWITCH_TO_ACT_MODE_TOOL_NAME);
	return { ...config, extraTools: mode === "plan" ? [...others, tool] : others };
}
