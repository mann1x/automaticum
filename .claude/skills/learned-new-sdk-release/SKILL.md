---
name: learned-new-sdk-release
description: Provision, pin, verify and adopt a new @cerebriline/sdk release as an automaticum SDK build
---
# Provisioning a new SDK release

Use this when a new `@cerebriline/sdk` version is on npm and an arm is to run on it.

## 1. Confirm the release

```bash
curl -s -m 15 https://registry.npmjs.org/@cerebriline%2fsdk | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['dist-tags']['latest'], d['time'].get('<ver>'))"
```

## 2. Write the build definition (`runner/sdk/<ver>/`)

```bash
NI=$(grep -n 'NodeImage = ' internal/sdk/sdk.go | sed 's/.*= "\(.*\)"/\1/')
mkdir -p runner/sdk/<ver>
sed 's/<prev>/<ver>/' runner/sdk/<prev>/package.json > runner/sdk/<ver>/package.json
docker run --rm -v "$PWD/runner/sdk/<ver>:/w" -w /w --user "$(id -u):$(id -g)" -e npm_config_cache=/tmp/npmcache "$NI" \
  npm install --package-lock-only --ignore-scripts --no-audit --no-fund
```

Compare the new lockfile with the previous one in Python: count the packages, list what was added and removed, and read the `@cerebriline/*` versions.

Write `runner/sdk/<ver>/sdk.toml`. Start with a comment saying what the release changes relative to the previous one, with X/D ids, then:

```toml
version = "<ver>"
source = "registry"
```

## 3. Install, pin and verify

`-repo` must be an absolute path. With `.`, Docker rejects the bind mount as an invalid volume name.

```bash
CGO_ENABLED=0 go build -o <scratch>/am ./cmd/automaticum
export AUTOMATICUM_CONFIG=<local config dir>
<scratch>/am sdk add -repo "$PWD" <ver>     # first install writes bundle_sha256 into sdk.toml: commit it
<scratch>/am sdk verify -repo "$PWD" <ver>  # reinstalls offline from the archived packages
```

## 4. Check that the bundle carries the change you need

Use Python to slice `<SPIKE_ROOT>/m2/sdk/<ver>/node_modules/@cerebriline/{core,agents}/dist/index.js`, because ugrep chokes on minified bundles. Check `core` and `agents` separately; a field the agent reads may never be forwarded by core. Also check the `.d.ts` types (`core/dist/types/config.d.ts`).

## 5. Arms, runner and docs

- Add `examples/config/arms/stub-sdk-<ver>.toml` (`extends = "stub"`, `[harness] sdk_release = "<ver>"`) for the harness's own tests.
- Move a real arm only on the owner's go. Set `[harness] sdk_release`, then run `arm validate` and `arm show | grep ^id`. Note old id → new id and the date in the arm's header comment, because the identity changes and runs no longer pool.
- If the release changes notice metadata or hook behaviour, update `runner/guards.mjs` and its tests (`cd runner && node --test test/`), along with `runner/PROTOCOL.md`, `docs/specs/040-runner.md` and the X row in `docs/specs/decisions.md`.
- Update `CLAUDE.md` Layout (`runner/sdk/<ver>/`, the stub arm) and `.wolf/anatomy.md`. Then run `make test`.

## 6. Lane host

The lane host keeps its own SDK builds under `<data_root>/sdk/`. List that directory to see what is installed. After deploying the commit (see the idle-gated binary swap), run `automaticum sdk add -repo <repo> <ver>` there too. Rebuild the lane images if `runner/` changed. Then run a stub smoke run on `stub-sdk-<ver>` before any real batch uses the build.


## When the fix is ours: change the SDK, release it, then adopt it

Use this when a client reports a failure that comes from the SDK itself. For example, a tool executor reads a 150 MB binary whole and the runner dies with a Node heap OOM: exit 134, `ABORTED`, the stack in `automaticum logs <run> --runner`.

### 1. Find the code
- Read the minified bundle of the arm's release (`<data_root>/sdk/<ver>/node_modules/@cerebriline/core/dist/index.js`). Search for literal strings such as tool names or error text, never for minified identifiers.
- Then open the source: the cline monorepo beside this checkout. The executors are in `sdk/packages/core/src/extensions/tools/executors/`.
- Before you write code, read that repo's `CLAUDE.md` and the Do-Not-Repeat list in its `.wolf/cerebrum.md`.

### 2. Fix and test, on cline `main`
- Put a Vitest beside the source. For a large file, make it sparse (`fs.truncate`), which costs no disk.
- Run the tests from the package directory:
  `cd sdk/packages/core && bunx vitest run --config vitest.config.ts src/extensions/tools/executors/`
- Keep the behaviour existing tests pin down. A guard that changes an old answer, such as grep passing a binary by, fails an existing test, and that test is the spec.
- `bunx biome check --write <files>`, then `bun run types`.
- Log the bug in cline's `.wolf/buglog.json` (a `{version, bugs}` object) and its memory and anatomy files, then commit. The pre-commit hook runs types, biome and gitleaks.

### 3. Release: outward-facing, so ask the owner first (AskUserQuestion)
Follow cline's `docs/protocols/BUILD-RELEASE-DEPLOY.md`:
- Bump `apps/vscode/package.json` with a Python `re.subn` that asserts it made one change, never a blind sed.
- Write `release-notes/<ver>.md`, starting at `##`.
- `bun install --frozen-lockfile` must report no changes.
- Preview the notes with `node apps/vscode/scripts/generate-fork-changelog.mjs --pending-version <ver> --emit-notes <file>`. Then `git checkout -- apps/vscode/CHANGELOG.md`, because the preview rewrites it.
- Commit `release: <ver>`. Then `git push origin main`, `git push origin main:mann1x/full-build-release`, `git tag -a v<ver> -m v<ver>` and `git push origin v<ver>`.
- The tag starts two workflows: "Cerebriline SDK Publish" (npm) and "Fork Release" (VSIX). Wait on them with `gh run view <id> --json status,conclusion`.
- Confirm that all five packages were published (`Published @cerebriline/{agents,core,llms,sdk,shared}@<ver>` in the log).
- The registry's `dist-tags` lags behind. Use `npm view @cerebriline/sdk@<ver> version`, and grep the `npm pack`ed core bundle for a string from the fix.

### 4. Adopt it in automaticum
- Copy `runner/sdk/<prev>/package.json` with the version changed.
- Make the lockfile in the Node image named by `NodeImage` in `internal/sdk/sdk.go`:
  `docker run --rm -v $PWD/runner/sdk/<ver>:/w -w /w --user $(id -u):$(id -g) -e npm_config_cache=/tmp/npmcache <image> npm install --package-lock-only --ignore-scripts --no-audit --no-fund`
- Diff it against the previous lockfile: same package count, and say which versions changed.
- Write `sdk.toml` with a comment saying what changed since the previous pinned build.
- Check that the code the detectors replay is unchanged: in cline, `git diff --stat v<prev> v<ver> -- sdk/packages`. The guards, struggle and fault patterns live in `@cerebriline/agents`, `@cerebriline/llms` and core. If they changed, `detect.RulesFor` and the ports need work.
- Build a scratch CLI with `CGO_ENABLED=0 go build`, outside the repo, then:
  `AUTOMATICUM_CONFIG=<local config> <cli> sdk add -repo $PWD <ver>`
  `<cli> sdk verify -repo $PWD <ver>`
  `sdk add` writes `bundle_sha256` into `sdk.toml`.
- Add `examples/config/arms/stub-sdk-<ver>.toml` (`extends = "stub"`, `[harness] sdk_release`), then `arm validate`.
- Add an X-row to the 040 §8 table and to `decisions.md` §4, and bump both `**Status:**` lines.
- Add the build to the `runner/sdk/` list in `CLAUDE.md` and the new files to `.wolf/anatomy.md`. Run `make test`.
- Run the private-name scan, filtering `registry.npmjs.org/` out of the lockfile lines, then commit.
- Mail the client that the release exists. Moving their arms' `sdk_release` changes the arm identity, so that decision is theirs.