// The session's prompt template: the system prompt and tool descriptions
// the model is given (040 §6.1.4, D155).
//
// "auto" is the extension's choice: the SDK's resolveSessionPromptTemplate,
// which asks an Ollama or xOllama server for the model's family
// (/api/show, through the wire, so the request is recorded) and then
// matches name, family and provider over the builtins, the runner's data
// directory and the workspace's `.clinerules/templates`, merged over
// default.md. A build older than 4.100.260 has no lookup: the match is on
// the name and provider alone, and the record says so.
//
// Any other choice is forced, whatever the model: "default" is default.md
// alone; a name is that template (the nearest of the builtins, the data
// directory and the workspace); "custom" is the arm's own file, whose text
// the job carries. A forced template is merged over default.md by the
// SDK's own renderPromptTemplate, the function the resolver ends in, given
// default.md and the template with a claim on the session's provider, so
// it outranks default.md and nothing else is in the list.
//
// Nothing here imports the SDK: the caller passes `sdk` (@cerebriline/sdk)
// and `shared` (@cerebriline/shared, for renderPromptTemplate and
// isOllamaNativeProvider), so the tests run on Node alone.

export const DEFAULT_TEMPLATE = "default";

/** The arm asks for a template this build or workspace does not have. */
export class TemplateError extends Error {}

/**
 * resolveTemplate gives `{ rendered, errors, warnings, use, family,
 * lookedUp, lookup }` for the arm's `[agent.prompt_template]`. `lookup` is
 * "asked" (the server was asked, `family` its answer or undefined when it
 * gave none), "not_asked" (a provider the SDK does not ask, or a forced
 * template) or "not_in_build" (auto on a build without the lookup).
 */
export async function resolveTemplate({ sdk, shared, arm, baseUrl, fetch, globalDir, workspaceDir }) {
	const pt = arm.agent.prompt_template ?? {};
	const use = pt.use || "auto";
	const providerId = arm.model.provider;
	const modelId = arm.model.model;
	if (use === "auto") {
		if (typeof sdk.resolveSessionPromptTemplate === "function") {
			const asked = shared.isOllamaNativeProvider(providerId);
			const r = await sdk.resolveSessionPromptTemplate({ providerId, modelId, baseUrl, fetch, globalDir, workspaceDir });
			return { ...r, use, family: r.family, lookedUp: asked, lookup: asked ? "asked" : "not_asked" };
		}
		const r = sdk.resolveSessionPromptTemplateFrom({ providerId, modelId, globalDir, workspaceDir });
		const lookup = shared.isOllamaNativeProvider(providerId) ? "not_in_build" : "not_asked";
		return { ...r, use, family: undefined, lookedUp: false, lookup };
	}
	const loaded = sdk.loadPromptTemplates(sdk.resolvePromptTemplateDirectories({ globalDir, workspaceDir }));
	// Builtins first, the nearest source last, as the resolver reads them.
	const all = [...sdk.getBuiltinPromptTemplates(), ...loaded.templates];
	const warnings = [...(loaded.warnings ?? [])];
	let chosen;
	if (use === "custom") {
		const fileName = pt.file_name || "custom.md";
		const parsed = sdk.parsePromptTemplate({ raw: pt.text ?? "", source: "global", fileName });
		if (parsed.error) throw new TemplateError(`the custom prompt template ${fileName} does not parse: ${parsed.error}`);
		chosen = parsed.template;
		if (parsed.warnings?.length) warnings.push({ fileName, source: "global", warnings: parsed.warnings });
	} else if (use !== DEFAULT_TEMPLATE) {
		chosen = all.findLast((t) => t.name === use);
		if (!chosen) {
			const names = [...new Set(all.map((t) => t.name))].sort().join(", ");
			throw new TemplateError(`no prompt template is named "${use}" in this SDK build or the workspace (there are: ${names})`);
		}
	}
	const defaults = all.filter((t) => t.name === DEFAULT_TEMPLATE);
	let list;
	if (!chosen) list = defaults;
	else if (chosen.name === DEFAULT_TEMPLATE) list = [{ ...chosen, match: undefined }];
	else list = [...defaults, { ...chosen, match: { provider: [providerId] } }];
	const rendered = shared.renderPromptTemplate(list, { providerId, modelId });
	return { rendered, errors: loaded.errors ?? [], warnings, use, family: undefined, lookedUp: false, lookup: "not_asked" };
}

/** What the `prepared` note says of the template. */
export function describeTemplate(template, arm) {
	const pt = arm.agent.prompt_template ?? {};
	const r = template.rendered;
	return {
		use: template.use,
		name: r?.name ?? null,
		file_name: r?.fileName ?? null,
		source: r?.source ?? null,
		overlaid: r?.overlaid ?? false,
		family: template.family ?? null,
		lookup: template.lookup,
		...(template.use === "custom" ? { sha256: pt.sha256 ?? null } : {}),
	};
}
