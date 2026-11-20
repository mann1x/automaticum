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
// "auto" may leave templates out by name (`disabled`, D157), as the
// extension's On checkbox per template does (SDK 4.100.288,
// disabledPromptTemplates): the list is always passed, empty included, so
// the global settings file in the container never decides it; a build
// before 4.100.288 does not read it, and the runner leaves them out itself
// the same way (withoutDisabledPromptTemplates: by name, default never).
//
// checkTemplate is the template's check (D157): the SDK's own
// validatePromptTemplate on what the session is given, the rendered
// template, so a required placeholder missing ({{CLINE_RULES}}, the
// plan-mode contract; {{CWD}}), an unknown one, an empty or repeated
// section or a compaction section without its token is reported; and
// toolCoverage names, at the first request and whenever the tools change,
// every tool the session offers that no section of the template describes.
//
// Nothing here imports the SDK: the caller passes `sdk` (@cerebriline/sdk)
// and `shared` (@cerebriline/shared, for renderPromptTemplate,
// isOllamaNativeProvider and validatePromptTemplate), so the tests run on
// Node alone.

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
		const disabled = disabledNames(pt.disabled);
		// The SDK leaves them out itself from 4.100.288; before, the runner.
		const bySdk = typeof sdk.resolveDisabledPromptTemplateNames === "function";
		const off = { disabled, disabledBy: disabled.length === 0 ? null : bySdk ? "sdk" : "runner" };
		let r;
		let lookup;
		if (typeof sdk.resolveSessionPromptTemplate === "function") {
			const asked = shared.isOllamaNativeProvider(providerId);
			r = await sdk.resolveSessionPromptTemplate({ providerId, modelId, baseUrl, fetch, globalDir, workspaceDir, disabledTemplateNames: disabled });
			lookup = asked ? "asked" : "not_asked";
		} else {
			r = { ...sdk.resolveSessionPromptTemplateFrom({ providerId, modelId, globalDir, workspaceDir, disabledTemplateNames: disabled }), family: undefined };
			lookup = shared.isOllamaNativeProvider(providerId) ? "not_in_build" : "not_asked";
		}
		if (off.disabledBy === "runner") {
			const all = [...sdk.getBuiltinPromptTemplates(), ...sdk.loadPromptTemplates(sdk.resolvePromptTemplateDirectories({ globalDir, workspaceDir })).templates];
			const left = all.filter((t) => t.name.trim().toLowerCase() === DEFAULT_TEMPLATE || !disabled.includes(t.name.trim().toLowerCase()));
			r = { ...r, rendered: shared.renderPromptTemplate(left, { providerId, modelId, family: r.family }) };
		}
		return { ...r, use, family: r.family, lookedUp: lookup === "asked", lookup, ...off };
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

/** disabledNames is a `disabled` list as the SDK reads it. */
export function disabledNames(list) {
	const names = (list ?? []).map((n) => String(n).trim().toLowerCase()).filter((n) => n && n !== DEFAULT_TEMPLATE);
	return [...new Set(names)].sort();
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
		...(template.disabled?.length ? { disabled: template.disabled, disabled_by: template.disabledBy } : {}),
	};
}

/**
 * checkTemplate is what the `prepared` note says of the template's check:
 * `checked` (false on a build without validatePromptTemplate, or when no
 * template applies and the SDK's built-in prompt does), `problems` (the
 * SDK's warnings on the rendered template: `code`, `section`, `message`)
 * and `files` (the template files the directories hold that did not parse,
 * and the warnings on those that did, whether or not they won).
 */
export function checkTemplate({ shared, template }) {
	const files = {
		errors: (template.errors ?? []).map((e) => ({ file_name: e.fileName ?? null, source: e.source ?? null, message: e.message ?? String(e) })),
		warnings: (template.warnings ?? []).flatMap((w) =>
			(w.warnings ?? []).map((x) => ({ file_name: w.fileName ?? null, source: w.source ?? null, code: x.code ?? null, section: x.section ?? null, message: x.message ?? String(x) })),
		),
	};
	const r = template.rendered;
	if (!r || typeof shared.validatePromptTemplate !== "function") return { checked: false, problems: [], files };
	const problems = shared.validatePromptTemplate({ ...r }, {}).map(({ code, section, message }) => ({ code, section, message }));
	return { checked: true, problems, files };
}

/**
 * toolCoverage gives a `beforeModel` that only looks: at the first request,
 * and again whenever the set of tools changes, it notes how many tools the
 * request offers and which of them no section of the rendered template
 * describes (`prompt_template_tools`), which keep their built-in text.
 */
export function toolCoverage({ rendered, note }) {
	let seen;
	return (context) => {
		const names = (context?.request?.tools ?? []).map((t) => t.name);
		const key = names.join("\n");
		if (names.length === 0 || key === seen) return;
		seen = key;
		const sections = rendered?.tools ?? {};
		note("prompt_template_tools", { template: rendered?.name ?? null, tools: names.length, uncovered: names.filter((n) => !Object.hasOwn(sections, n)) });
	};
}
