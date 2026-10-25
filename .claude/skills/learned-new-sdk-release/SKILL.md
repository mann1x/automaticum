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

- Add `arms/stub-sdk-<ver>.toml` (`extends = "stub"`, `[harness] sdk_release = "<ver>"`) for the harness's own tests.
- Move a real arm only on the owner's go. Set `[harness] sdk_release`, then run `arm validate` and `arm show | grep ^id`. Note old id → new id and the date in the arm's header comment, because the identity changes and runs no longer pool.
- If the release changes notice metadata or hook behaviour, update `runner/guards.mjs` and its tests (`cd runner && node --test test/`), along with `runner/PROTOCOL.md`, `docs/specs/040-runner.md` and the X row in `docs/specs/decisions.md`.
- Update `CLAUDE.md` Layout (`runner/sdk/<ver>/`, the stub arm) and `.wolf/anatomy.md`. Then run `make test`.

## 6. Lane host

The lane host keeps its own SDK builds under `<data_root>/sdk/`. List that directory to see what is installed. After deploying the commit (see the idle-gated binary swap), run `automaticum sdk add -repo <repo> <ver>` there too. Rebuild the lane images if `runner/` changed. Then run a stub smoke run on `stub-sdk-<ver>` before any real batch uses the build.