---
paths:
  - runner/browser.mjs
  - runner/test/browser.test.mjs
  - images/browser/**
---

# The browser in the runner (D150)

- `runner/browser.mjs` gives the SDK's `browser`, `render_image` and `transform_image` their drivers: `createBrowserDriver` is the extension's `BrowserSession` local mode (`apps/vscode/src/services/browser/BrowserSession.ts` at 4.100.276) and `createRenderDriver` its `renderWithChrome` (`services/browser/render-page.ts`). Port the extension's, never the CLI's `browser-support.ts`: the CLI's console text, timeouts and headless mode differ, and the model reads them.
- Only `CONTAINER_ARGS` (`--no-sandbox`, `--disable-dev-shm-usage`) is ours; everything else the model or the page sees (user agent, viewport, waits, the `[Page Error]`, `[Timeout]`, `[Error]` lines) is the extension's. puppeteer-core is loaded from `/opt/automaticum/browser` with `createRequire`, on first use; the tests pass a fake `puppeteer` and a temporary prefix with an empty `chrome-linux64/chrome`.
- Do not patch what the SDK's tools build (the 4.100.276 `transform_image` page calls `planTransform` under a minified name and fails): parity keeps the SDK's defect, and the fix is asked of the SDK (X15).
- Prove a change in the base image with the SDK's real tools (`sdk.createBrowserTool` etc.) against a workspace owned by uid 1000, then with a stub session through `runner.mjs`.
