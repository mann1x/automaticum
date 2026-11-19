import assert from "node:assert/strict";
import { test } from "node:test";
import { describeTemplate, resolveTemplate, TemplateError } from "../template.mjs";

// An SDK build as 4.100.260 on exports it, and @cerebriline/shared: every
// function records what it was given.
function fakes({ async = true, workspace = [] } = {}) {
	const calls = {};
	const builtins = [
		{ name: "default", source: "builtin", fileName: "default.md", tools: {} },
		{ name: "qwen", source: "builtin", fileName: "qwen.md", match: { family: ["qwen*"] }, tools: {} },
	];
	const sdk = {
		resolveSessionPromptTemplateFrom: (req) => {
			calls.from = req;
			return { rendered: { name: "default", source: "builtin", overlaid: false }, errors: [], warnings: [] };
		},
		getBuiltinPromptTemplates: () => builtins,
		resolvePromptTemplateDirectories: (dirs) => {
			calls.dirs = dirs;
			return [{ path: dirs.globalDir, source: "global" }];
		},
		loadPromptTemplates: () => ({ templates: workspace, errors: [{ fileName: "broken.md" }], warnings: [] }),
		parsePromptTemplate: (input) => {
			calls.parse = input;
			if (input.raw.includes("BROKEN")) return { error: "no frontmatter" };
			const name = /name: (\S+)/.exec(input.raw)?.[1] ?? input.fileName.replace(/\.md$/, "");
			return { template: { name, source: input.source, fileName: input.fileName, match: { model: ["x"] }, tools: {} }, warnings: [{ message: "w" }] };
		},
	};
	if (async)
		sdk.resolveSessionPromptTemplate = async (req) => {
			calls.async = req;
			return { rendered: { name: "qwen", fileName: "qwen.md", source: "builtin", overlaid: true }, errors: [], warnings: [], family: "qwen35moe" };
		};
	const shared = {
		isOllamaNativeProvider: (p) => p === "ollama" || p === "xollama",
		renderPromptTemplate: (list, target) => {
			calls.render = { list, target };
			const top = list[list.length - 1];
			return { name: top.name, fileName: top.fileName, source: top.source, overlaid: top.name !== "default" };
		},
	};
	return { sdk, shared, calls };
}

const armOf = (provider, prompt_template) => ({ model: { provider, model: "a3b-coder_tb:Q4_K_M" }, agent: { prompt_template } });
const dirs = { baseUrl: "http://e:11434", fetch: () => {}, globalDir: "/out/data/templates", workspaceDir: "/workspace/.clinerules/templates" };

test("auto on Ollama: the SDK asks the server for the family, through the runner's fetch", async () => {
	const { sdk, shared, calls } = fakes();
	for (const pt of [undefined, { use: "auto" }]) {
		const t = await resolveTemplate({ sdk, shared, arm: armOf("ollama", pt), ...dirs });
		assert.deepEqual(calls.async, { providerId: "ollama", modelId: "a3b-coder_tb:Q4_K_M", ...dirs });
		assert.equal(t.family, "qwen35moe");
		assert.equal(t.lookedUp, true);
		assert.equal(t.lookup, "asked");
		assert.equal(t.rendered.name, "qwen");
	}
});

test("auto on a provider the SDK does not ask: no lookup, the record says so", async () => {
	const { sdk, shared } = fakes();
	const t = await resolveTemplate({ sdk, shared, arm: armOf("openai", { use: "auto" }), ...dirs });
	assert.equal(t.lookedUp, false);
	assert.equal(t.lookup, "not_asked");
});

test("auto on a build without the lookup (before 4.100.260): name and provider only", async () => {
	const { sdk, shared, calls } = fakes({ async: false });
	const t = await resolveTemplate({ sdk, shared, arm: armOf("ollama"), ...dirs });
	assert.deepEqual(calls.from, { providerId: "ollama", modelId: "a3b-coder_tb:Q4_K_M", globalDir: dirs.globalDir, workspaceDir: dirs.workspaceDir });
	assert.equal(t.lookup, "not_in_build");
	assert.equal(t.family, undefined);
	const other = await resolveTemplate({ sdk, shared, arm: armOf("openai"), ...dirs });
	assert.equal(other.lookup, "not_asked", "a provider no build asks");
});

test("default: default.md alone, whatever the model", async () => {
	const { sdk, shared, calls } = fakes();
	const t = await resolveTemplate({ sdk, shared, arm: armOf("ollama", { use: "default" }), ...dirs });
	assert.deepEqual(
		calls.render.list.map((x) => x.name),
		["default"],
	);
	assert.equal(calls.async, undefined, "a forced template asks the server nothing");
	assert.equal(t.rendered.overlaid, false);
	assert.equal(t.lookup, "not_asked");
	assert.equal(t.errors.length, 1, "the directories' parse errors are kept");
});

test("a name: that template over default.md, the nearest source winning, claimed on the provider", async () => {
	const ws = { name: "qwen", source: "workspace", fileName: "qwen.md", match: { model: ["*nothing*"] }, tools: {} };
	const { sdk, shared, calls } = fakes({ workspace: [ws] });
	const t = await resolveTemplate({ sdk, shared, arm: armOf("ollama", { use: "qwen" }), ...dirs });
	const [d, q] = calls.render.list;
	assert.equal(d.name, "default");
	assert.equal(q.source, "workspace");
	assert.deepEqual(q.match, { provider: ["ollama"] });
	assert.deepEqual(calls.render.target, { providerId: "ollama", modelId: "a3b-coder_tb:Q4_K_M" });
	assert.equal(t.rendered.name, "qwen");
	assert.equal(t.rendered.overlaid, true);
});

test("a name no source has: refused, naming the ones there are", async () => {
	const { sdk, shared } = fakes();
	await assert.rejects(resolveTemplate({ sdk, shared, arm: armOf("ollama", { use: "gemma" }), ...dirs }), (e) => {
		assert.ok(e instanceof TemplateError);
		assert.match(e.message, /no prompt template is named "gemma".*there are: default, qwen/);
		return true;
	});
});

test("custom: the arm's text parsed as a template, over default.md", async () => {
	const { sdk, shared, calls } = fakes();
	const pt = { use: "custom", file_name: "mine.md", text: "---\nname: mine\n---\n# system\nBe brief.\n", sha256: "ab" };
	const t = await resolveTemplate({ sdk, shared, arm: armOf("ollama", pt), ...dirs });
	assert.equal(calls.parse.raw, pt.text);
	assert.equal(calls.parse.fileName, "mine.md");
	assert.deepEqual(
		calls.render.list.map((x) => x.name),
		["default", "mine"],
	);
	assert.deepEqual(calls.render.list[1].match, { provider: ["ollama"] }, "its own claim is replaced: the arm chose it");
	assert.equal(t.warnings.length, 1);
	assert.deepEqual(describeTemplate(t, armOf("ollama", pt)), {
		use: "custom",
		name: "mine",
		file_name: "mine.md",
		source: "global",
		overlaid: true,
		family: null,
		lookup: "not_asked",
		sha256: "ab",
	});
});

test("custom named default replaces default.md; a file that does not parse is refused", async () => {
	const { sdk, shared, calls } = fakes();
	await resolveTemplate({ sdk, shared, arm: armOf("ollama", { use: "custom", file_name: "d.md", text: "name: default\n" }), ...dirs });
	assert.equal(calls.render.list.length, 1);
	assert.equal(calls.render.list[0].match, undefined);
	await assert.rejects(
		resolveTemplate({ sdk, shared, arm: armOf("ollama", { use: "custom", file_name: "b.md", text: "BROKEN" }), ...dirs }),
		/the custom prompt template b.md does not parse: no frontmatter/,
	);
});

test("the prepared note for auto", async () => {
	const { sdk, shared } = fakes();
	const arm = armOf("ollama");
	const t = await resolveTemplate({ sdk, shared, arm, ...dirs });
	assert.deepEqual(describeTemplate(t, arm), {
		use: "auto",
		name: "qwen",
		file_name: "qwen.md",
		source: "builtin",
		overlaid: true,
		family: "qwen35moe",
		lookup: "asked",
	});
});
