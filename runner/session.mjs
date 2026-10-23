// From the resolved arm to the SDK's session configuration, and back: what
// the SDK says it built is compared with what the arm states.
//
// Nothing here imports the SDK, so both directions can be tested without it.
// Every guard and every compaction setting is given a value: the SDK turns
// most of them on when a field is left out, and an arm states them all.

const SAMPLER_FIELDS = {
	temperature: "temperature",
	top_p: "topP",
	top_k: "topK",
	min_p: "minP",
	typical_p: "typicalP",
	repeat_penalty: "repeatPenalty",
	repeat_last_n: "repeatLastN",
	presence_penalty: "presencePenalty",
	frequency_penalty: "frequencyPenalty",
	seed: "seed",
	stop: "stop",
};

// The bundled skills the SDK ships switched on. An arm lists those it wants;
// the runner switches the others off.
export const BUNDLED_SKILLS_ON_BY_DEFAULT = ["media-audio", "media-images", "media-video"];

/** unsupported lists what an arm asks for that this runner cannot do. */
export function unsupported(arm) {
	const out = [];
	if (arm.agent.approval !== "auto") out.push(`agent.approval "${arm.agent.approval}": only "auto" exists in protocol 2`);
	if (arm.agent.escalation) out.push("agent.escalation: not supported in protocol 2");
	if (arm.agent.sub_agents.spawn || arm.agent.sub_agents.teams) out.push("agent.sub_agents: not supported in protocol 2");
	if (!["act", "plan"].includes(arm.agent.mode)) out.push(`agent.mode "${arm.agent.mode}" is not act or plan`);
	if (!["off", "on", "static"].includes(arm.protocol.mode)) out.push(`protocol.mode "${arm.protocol.mode}" is not known`);
	for (const s of arm.agent.bundled_skills ?? []) {
		if (!BUNDLED_SKILLS_ON_BY_DEFAULT.includes(s)) out.push(`agent.bundled_skills: "${s}" is not a skill this runner can switch on`);
	}
	return out;
}

/**
 * buildConfig returns { config, localRuntime, toolPolicies } for
 * ClineCore.start, from the job alone.
 */
export function buildConfig(job, { systemPrompt, fetch, onConfigResolved }) {
	const arm = job.arm;
	const config = {
		sessionId: job.session_id,
		providerId: arm.model.provider,
		modelId: arm.model.model,
		baseUrl: job.endpoint.base_url,
		cwd: job.workspace,
		workspaceRoot: job.workspace,
		systemPrompt,
		mode: arm.agent.mode,
		contextWindow: arm.model.context_window,
		storedProviderSettings: "ignore",
		enableTools: true,
		enableSpawnAgent: false,
		enableAgentTeams: false,
		maxToolResultChars: arm.agent.tool_result_cap,
		// Guards that are fields of the session itself.
		strongNudges: arm.guards.strong_nudges.enabled,
		toolIntentGuard: arm.guards.tool_intent.enabled,
		staleReadRewrites: arm.guards.stale_read_rewrites.enabled,
		editVerification: { mode: arm.guards.edit_verification },
		execution: {
			maxConsecutiveMistakes: arm.agent.mistake_limit,
			loopDetection: arm.guards.loop_detection.enabled
				? { softThreshold: arm.guards.loop_detection.soft, hardThreshold: arm.guards.loop_detection.hard }
				: false,
			// An empty object is "on, with the SDK's own numbers".
			reasoningLoopDetection: arm.guards.reasoning_loop.enabled ? {} : false,
			reasoningRepetition: arm.guards.reasoning_repetition.enabled ? {} : false,
		},
		taskProgress: arm.agent.task_progress.enabled
			? { enabled: true, reminderInterval: arm.agent.task_progress.interval }
			: { enabled: false },
		checkpoint: { enabled: arm.agent.checkpoints },
		atomicProtocol:
			arm.protocol.mode === "off"
				? { mode: "off" }
				: { mode: arm.protocol.mode, maxChanges: arm.protocol.max_changes, maxTransactions: arm.protocol.max_transactions },
		compaction: arm.compaction.enabled
			? {
					enabled: true,
					strategy: arm.compaction.strategy,
					keepRecentMessages: arm.compaction.keep_recent_messages,
					councilEnabled: arm.compaction.council,
					thinkingSummaryEnabled: arm.compaction.thinking_summary,
					cappedThinkingEnabled: arm.compaction.capped_thinking,
					forceFullFromCompaction: arm.compaction.force_full_from,
				}
			: { enabled: false },
		fetch,
		onConfigResolved,
	};
	if (job.endpoint.api_key) config.apiKey = job.endpoint.api_key;
	if (arm.agent.max_iterations > 0) config.maxIterations = arm.agent.max_iterations;
	if (arm.model.output_tokens > 0) config.maxTokensPerTurn = arm.model.output_tokens;

	// Thinking and sampler: owned by the model tag (send nothing) or by the arm.
	if (arm.thinking.owner === "arm") {
		config.thinking = arm.thinking.enabled;
		if (arm.thinking.effort) config.reasoningEffort = arm.thinking.effort;
		if (arm.thinking.budget_tokens !== undefined) config.thinkingBudgetTokens = arm.thinking.budget_tokens;
	}
	// How much earlier reasoning goes back in the history (D103). The SDK
	// reads it from the provider configuration; the arm leaves it out when
	// it is "auto" with inlining, which is what the SDK does unasked.
	const history = {};
	if (arm.thinking.reasoning_history !== undefined) history.reasoningHistory = arm.thinking.reasoning_history;
	if (arm.thinking.reasoning_inline !== undefined) history.reasoningInline = arm.thinking.reasoning_inline;
	if (arm.sampler.owner === "arm") {
		const sampling = {};
		for (const [from, to] of Object.entries(SAMPLER_FIELDS)) {
			if (arm.sampler[from] !== undefined) sampling[to] = arm.sampler[from];
		}
		// The SDK drops a provider configuration whose provider is not the
		// session's own.
		config.providerConfig = { providerId: arm.model.provider, modelId: arm.model.model, sampling, ...history };
	} else if (Object.keys(history).length > 0) {
		config.providerConfig = { providerId: arm.model.provider, modelId: arm.model.model, ...history };
	}

	const toolPolicies = {};
	for (const name of arm.agent.disabled_tools ?? []) toolPolicies[name] = { enabled: false };

	return { config, localRuntime: { configExtensions: [...(arm.agent.config_extensions ?? [])] }, toolPolicies };
}

function same(a, b) {
	if (a === b) return true;
	if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) return false;
	if (Array.isArray(a) !== Array.isArray(b)) return false;
	const ka = Object.keys(a).filter((k) => a[k] !== undefined);
	const kb = Object.keys(b).filter((k) => b[k] !== undefined);
	return ka.length === kb.length && ka.every((k) => same(a[k], b[k]));
}

const show = (v) => (v === undefined ? "nothing" : JSON.stringify(v));

// The session fields the runner sets and expects to read back as it set them.
const READ_BACK = [
	"mode", "contextWindow", "maxToolResultChars", "maxIterations", "maxTokensPerTurn",
	"enableTools", "enableSpawnAgent", "enableAgentTeams",
	"strongNudges", "toolIntentGuard", "staleReadRewrites", "editVerification",
	"execution", "taskProgress", "checkpoint", "atomicProtocol", "compaction",
	"thinking", "reasoningEffort", "thinkingBudgetTokens",
];

/**
 * compareResolved returns what differs between what the runner asked for and
 * what the SDK reports it built. An empty list means the session is the arm.
 *
 * `wroteGlobalSettings` says the runner itself wrote the global settings
 * file (to switch bundled skills off); a file it did not write is a leftover.
 */
export function compareResolved(job, config, resolved, { wroteGlobalSettings = false } = {}) {
	const arm = job.arm;
	const out = [];
	const differ = (what, want, got) => {
		if (!same(want, got)) out.push(`${what}: the arm gives ${show(want)}, the session has ${show(got)}`);
	};
	differ("model.provider", arm.model.provider, resolved.providerId);
	differ("model.model", arm.model.model, resolved.modelId);
	differ("endpoint address", config.baseUrl, resolved.baseUrl);
	differ("model.context_window", arm.model.context_window, resolved.contextWindow);

	const src = resolved.sources ?? {};
	if (src.storedProviderSettings === "merged") out.push("a stored provider profile was merged into the session");
	for (const field of src.inheritedFromStoredProfile ?? []) out.push(`${field} was inherited from a stored provider profile`);
	if (src.globalSettingsPresent && !wroteGlobalSettings) out.push("a global settings file was present that the runner did not write");

	if (arm.thinking.owner === "arm") {
		differ("thinking.enabled", arm.thinking.enabled, resolved.thinking?.enabled);
		differ("thinking.effort", arm.thinking.effort || undefined, resolved.thinking?.effort);
		// A budget the arm states must survive. One the arm does not state
		// is derived by the SDK from the effort; that is reported by
		// derivedFromArm, not refused.
		if (arm.thinking.budget_tokens !== undefined) {
			differ("thinking.budget_tokens", arm.thinking.budget_tokens, resolved.thinking?.budgetTokens);
		}
	} else {
		for (const [k, v] of Object.entries(resolved.thinking ?? {})) {
			if (v !== undefined) out.push(`thinking is owned by the model tag, but the session has thinking.${k} = ${show(v)}`);
		}
	}
	const sampling = Object.fromEntries(Object.entries(resolved.sampling ?? {}).filter(([, v]) => v !== undefined));
	if (arm.sampler.owner === "arm") {
		differ("sampler", config.providerConfig.sampling, sampling);
	} else if (Object.keys(sampling).length > 0) {
		out.push(`the sampler is owned by the model tag, but the session has ${show(sampling)}`);
	}

	// How much earlier reasoning goes back (D103), where the SDK reports it
	// (4.100.260 on): the setting must be the arm's. What "auto" resolved
	// to is the record's, in this note, and is not compared.
	const history = resolved.reasoningHistory;
	if (history && typeof history === "object") {
		differ("thinking.reasoning_history", arm.thinking.reasoning_history ?? "auto", history.setting);
	}

	const session = resolved.session ?? {};
	for (const key of READ_BACK) differ(`session ${key}`, config[key], session[key]);
	return out;
}

/**
 * derivedFromArm lists values the SDK worked out from what the arm states,
 * so the record holds them although no arm field does.
 */
export function derivedFromArm(job, resolved) {
	const out = {};
	const arm = job.arm;
	if (arm.thinking.owner === "arm" && arm.thinking.budget_tokens === undefined && resolved.thinking?.budgetTokens !== undefined) {
		out.thinking_budget_tokens = resolved.thinking.budgetTokens;
	}
	if (!(arm.model.output_tokens > 0) && resolved.maxOutputTokens !== undefined) out.max_output_tokens = resolved.maxOutputTokens;
	return out;
}
