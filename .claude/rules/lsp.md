---
paths:
  - runner/lsp.mjs
  - runner/editor.mjs
  - runner/test/lsp.test.mjs
  - runner/test/editor.test.mjs
  - runner/test/fixtures/fake-lsp.mjs
  - images/lsp/**
---

# Language servers in the runner (D149)

- `runner/lsp.mjs` is the session's language-server host (ported from the SDK CLI's `apps/cli/src/runtime/lsp-client.ts` and `code-intel-support.ts` at 4.100.276): `createLspHost` gives `provider` (ask_lsp's `CodeIntelProvider`), `touch(path, {save})`, `readDiagnostics()` (`{filePath, diagnostics, pending}`), `serves`, `prewarm`, `status`, `dispose`. `runner/editor.mjs` ports the extension's `check_file` and diagnostics after edits (`apps/vscode/src/sdk/check-file-tool.ts`, `editor-diagnostics.ts`, `integrations/diagnostics/index.ts`). The texts the model reads (the check_file description, `<editor_diagnostics>`, the root-cause hint, `describePosition`) are the extension's word for word, and so are the budgets; change them only when the extension's source at the SDK release under test changes.
- Neither file imports the SDK: `runner.mjs` passes `sdk.createTool`, `sdk.describeDelimiterBalance`, `sdk.createAskLspTool` and `sdk.mergeAgentHooks`, so the tests run on Node alone (`cd runner && node --test test/`, with `test/fixtures/fake-lsp.mjs` as the server).
- Answer every server request (`answer` in `LspConnection`), with a settings object, never null: null makes the CSS validator, and the HTML server's page validation with it, report nothing. A changed file is `didChange` with a new version, never a second `didOpen` (the CLI's way, which a server ignores). A pull-diagnostics server (csharp-ls) is pulled on every `touch`; a server that refuses the pull (`Unhandled method`) is not asked again (`noPull`).
- A diagnostic's `source` is relabelled to the editor's name through the spec's `sources` (`typescript` -> `ts`, as VS Code's TypeScript extension labels it).
- Verify a server change in the image, not only on the fake: the probe pattern is the base or layer image with the SDK build mounted read-only at `/opt/automaticum/runner/node_modules` and the workspace owned by uid 1000 (rust-analyzer's `cargo check` writes `Cargo.lock` and `target/` there; a root-owned directory makes it report nothing).
