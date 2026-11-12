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

## Proving a tool fix in the installed bundle

Once `sdk add` and `sdk verify` succeed for a release that fixes an SDK tool (sed, awk, grep, read_files), check the fix in the bundle that runs will actually load. The SDK repo's tests are not enough.

1. Get the Node image: `NI=$(grep 'NodeImage = ' internal/sdk/sdk.go | sed 's/.*= "\(.*\)"/\1/')`.
2. In a scratch directory (small files, then delete it), make the inputs. For a large file use `truncate -s 150400128 w/big` (sparse, so it costs no disk). For a binary use `printf '\x7fELF\x02\x01\x01\x00\x00' > w/small_elf`. Add a text file as the control.
3. Write a probe that imports `/sdk/<ver>/node_modules/@cerebriline/core/dist/index.js`. Call `core.createDefaultExecutors({})` and then `ex.sed({ script: "1,10p", files, quiet: true }, "/w")`. Print the result and `process.memoryUsage().heapUsed`. There are no per-tool exports (`createSedExecutor` does not exist). To list the factories, parse the bundle's last `export{` block for names that match `Executor|DefaultTools`.
4. Run `docker run --rm -v <scratch>/w:/w -v <scratch>/probe.mjs:/probe.mjs:ro -v <data_root>/sdk:/sdk:ro "$NI" node /probe.mjs <ver>`. Run it for the new release and for the previous one, so the mail can show the change side by side.
5. Quote the tool's exact refusal text and the heap figure in the mail to the client.

## Changing the harness default to the new release

Only do this when the owner says so, and record it as a D-row. Follow the default-release pattern in CALIBER_LEARNINGS.md: `base.toml`, the regenerated `runner/test/arm-stub.json`, `internal/run/run_test.go` (`const release` and the expected build string), and `internal/daemon/daemon_test.go`. Then run `make test` and `cd runner && node --test test/`. Before you touch a lane host, list each arm's id and release with `automaticum arm show <a> --json | grep -o '"sdk_release":"[^"]*"'`. A client's arms move only when the client chooses.

## A release that is not on the registry: a local build from its packs

Use this when the SDK owner sends the release's packed packages because the release's npm publish did not run (for example, Actions is disabled on the SDK repo).

1. **Check the packs.** Run `sha256sum -c SHA256SUMS` in the packs directory. Read each package's version and its `@cerebriline/*` dependencies with `tar -xOzf <tgz> package/package.json`. The packs carry a prerelease version such as `4.100.275-g<commit>`, so they never share a version with a later registry release.
2. **Copy the packs to a gitignored directory**, `docs/local/sdk-<release>/packs/`. Never put them in a tracked path or in /tmp.
3. **Make the definition directory** `runner/sdk/<release>+<commit>/`. In its `package.json`, name only `@cerebriline/sdk` under `dependencies`, as `file:../packs/cerebriline-sdk-<ver>.tgz`. Point the other four packages (`shared`, `llms`, `agents`, `core`) at their own `.tgz` files with `file:../packs/...` entries under `overrides`. Without the overrides, npm resolves those four from the registry.
4. **Make the lockfile in the Node image**, with no install scripts:
   `docker run --rm -v "$PWD/<def>:/w/def" -v "$PWD/<packs>:/w/packs:ro" -w /w/def --user "$(id -u):$(id -g)" -e HOME=/tmp -e npm_config_cache=/tmp/npmcache <NodeImage from internal/sdk/sdk.go> npm install --package-lock-only --ignore-scripts --no-audit --no-fund`
   Then compare its `packages` keys with the previous release's lockfile. The count should match, and only patch versions should differ.
5. **Write `sdk.toml`** with a comment saying what the build is and why it is local, plus `version = "<release>-g<commit>"`, `source = "local"` and `commit = "<commit>"`. Leave out `bundle_sha256`: the first install writes it.
6. **Install and verify** with a CLI built by `CGO_ENABLED=0 go build -o <tmp>/am ./cmd/automaticum` and the local installation's `AUTOMATICUM_CONFIG`:
   `<tmp>/am sdk add -repo "$PWD" -packs "$PWD/docs/local/sdk-<release>/packs" <release>+<commit>`, then `<tmp>/am sdk verify -repo "$PWD" <release>+<commit>`. The add step writes the hash into `sdk.toml`, which you commit. The verify step must report "reproduced".
7. **Check the bundle itself.** Grep `<data_root>/sdk/<name>/node_modules/@cerebriline/core/dist/index.js` for strings the fixes added. Then compare the sha256 of `agents`, `llms` and `shared` `dist/index.js` with the previous release's. If `agents` and `llms` are unchanged, the reasoning-loop and struggle code the detectors replay has not moved.
8. **Check the guard code in the SDK repo**, read-only: `git diff --name-only <old commit> <new commit> -- sdk | grep -i -E 'struggle|guard|loop|repetit'`. For each changed file, check whether `internal/detect` (`callguard.go`, `guards258.go`, `struggle.go`) replays it. Record any behaviour the replay does not model yet in the release's X row. Do not change the detectors without the owner.
9. **Add the stub arm.** Name the file `examples/config/arms/stub-sdk-<release>.toml`, without `+<commit>`, because arm names may not contain `+`. Give it `extends = "stub"` and `[harness] sdk_release = "<release>+<commit>"`. Validate it with `AUTOMATICUM_CONFIG=$PWD/examples/config <tmp>/am arm validate stub-sdk-<release>`.
10. **Record it.** Add an X row in `docs/specs/decisions.md` and in the 040 external-dependency table, and bump both Status lines. List the new directory in the CLAUDE.md layout and in `.wolf/anatomy.md`. Run `make test`. Changing the default release (base.toml) is a separate owner decision.

## Installing a pinned build on a running lane host

Use this after the build is pinned and committed in the repo (for example a local build `<release>+<commit>`). It needs no binary swap and no daemon restart, so the lane-host deploy skill is not needed. `<root>` is the installation directory on the host. Never write the host's private name or paths in tracked files.

1. `claude-hooks-res status`, then `claude-hooks-res acquire <host>/build:exclusive -r "<why>" --ttl 2h`. Check `automaticum status` on the host: nothing running or queued on the lane you will use. Leave a client's GPU work alone.
2. Mail the client START. Say what you install, that there is no restart and the GPUs are untouched, that the arm template's default does not move, and any analysis caveats (for example a guard change the replay does not model yet).
3. Copy the packed `.tgz` files and `SHA256SUMS` to `<root>/work/sdk-<release>/packs/` and run `sha256sum -c SHA256SUMS` there.
4. `git -C <root>/repo fetch -q origin && git -C <root>/repo merge --ff-only -q origin/main`.
5. With `AUTOMATICUM_CONFIG=<root>/config`, run `automaticum sdk add -repo <root>/repo -packs <packs> <build>`. Expect "the bundle is the one its definition pins". Then run `automaticum sdk verify -repo <root>/repo <build>` and expect "is reproduced: installed again with no network". The flags take a single dash.
6. Copy `<root>/repo/examples/config/arms/stub-sdk-<release>.toml` into `<root>/config/arms/` (the host's config is its own copy) and run `automaticum arm validate stub-sdk-<release>`.
7. Stub smoke run:
   - The port must be free: `curl -s -m 3 http://<docker-bridge>:7412/api/version` must fail. Abort if it answers.
   - `nohup automaticum stub serve -listen <docker-bridge>:7412 -script stub/fix-answer.json > stub-smoke.log 2>&1 & echo $! > stub.pid`, then check `/api/version`.
   - `automaticum run start --arm stub-sdk-<release> --task demo/answer`. The batch line comes first, so take the run id with `grep -v batch | grep -oE '^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}'`.
   - `timeout 300 automaticum wait run <id>` must return 0. In `automaticum run show <id>`, check that the verdict is PASS and that `SDK release` is the package version (`<release>-g<commit>`).
   - Stop the stub only after the wait returns, then check that its pid is gone.
8. Run `systemctl show automaticumd -p NRestarts -p ActiveState`. It should show the same restart count and `active`. Release the claim (`claude-hooks-res release --claim <id>`).
9. Mail the client DONE with the run id, the bundle hash, and the facts that there was no restart and nothing is queued. Tell them how to adopt the build: `[harness] sdk_release = "<build>"` in an arm. Arm ids change, so runs on the new build do not pool with older ones. Moving client arms or the template default is the client's or the owner's call, never yours.
10. Log a row in `.wolf/memory.md` using neutral host names.

## Addendum (2026-11-12, adopting 4.100.277)

### Pinning
1. Confirm the release is on the registry: `curl -s https://registry.npmjs.org/@cerebriline%2fsdk | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['dist-tags']['latest'], d['time'].get('<ver>'))"`.
2. `mkdir runner/sdk/<ver>`; copy the previous pin's `package.json` with the version changed (`sed 's/<prev>/<ver>/'`).
3. Make the lockfile in the pinned Node image (`NodeImage` in `internal/sdk/sdk.go`) as your own uid, with an npm cache on persistent disk (never /tmp): `docker run --rm -v "$PWD/runner/sdk/<ver>:/w" -v <cache>:/npmcache -w /w --user "$(id -u):$(id -g)" -e npm_config_cache=/npmcache <NodeImage> npm install --package-lock-only --ignore-scripts --no-audit --no-fund`.
4. Diff the lockfile against the previous pin's (packages removed, added, changed) in Python over `json.load(...)['packages']`. Check the packages the SDK owner was asked to bump (Dependabot alerts).
5. **Write a stub `sdk.toml` first** (`version = "<ver>"`, `source = "registry"`); `sdk add` fails without it. Then, with `AUTOMATICUM_CONFIG=<local config>`: `am sdk add -repo "$PWD" <ver>` (appends `bundle_sha256`) and `am sdk verify -repo "$PWD" <ver>` (must say "reproduced"). Use a CLI built with `CGO_ENABLED=0 go build -o <scratch>/am ./cmd/automaticum`.
6. Replace the stub comment in `sdk.toml` with what the release changes, its dependency changes and whether the replayed guard sources changed.

### Checking the build
- List the exports: `docker run --rm -v <data_root>/sdk/<ver>/node_modules:/x/node_modules:ro -w /x <NodeImage> node --input-type=module -e "const s=await import('@cerebriline/sdk'); for (const k of [...]) console.log(k, typeof s[k]);"`. Importing the bundle by file path fails with ERR_MODULE_NOT_FOUND.
- Read the declarations under `node_modules/@cerebriline/core/dist/**/*.d.ts`; read an implementation in the minified `dist/index.js` by finding its local name with `(\w+) as <exportName>\b`.
- Check that the replayed guards did not change: `git -C <sdk repo> diff --stat v<old> v<new> -- sdk/packages/core/src/runtime/safety sdk/packages/agents sdk/packages/core/src/runtime/session-runtime-orchestrator.ts` (empty means CallGuard/Rules mappings carry over).
- If the runner uses new exports, keep the older builds' path too and prove both give the model the same text: run one probe script in the lane image with each build's `node_modules` mounted, and diff the outputs with timings stripped.
- Run an end-to-end stub session through `runner.mjs` with a stub arm on the new release (see the CALIBER_LEARNINGS e2e pattern), and read the tool results off `out/wire.jsonl`.

### Adopting as the default
Read the previous adoption commit (`git show --stat <commit>`) and change the same files: `examples/config/arms/base.toml`, a new `stub-sdk-<ver>.toml`, the old stub arm's comment, `internal/daemon/daemon_test.go`, `internal/run/run_test.go` (`const release` and the "build <ver> of the arm" string), `internal/detect/callguard_test.go` (map row), `runner/test/arm-stub.json` (regenerate with `am arm show -json stub`). Then run the Go tests for detect, daemon, run and config, then `make test` in the background, and `node --test test/` in `runner/`. Update 040 §3 and the X row in both 040 §8 and decisions.md §4, add the D row, the CLAUDE.md layout (`runner/sdk/` list and the default release), `.wolf/anatomy.md`, and mirror the decision in `.wolf/cerebrum.md`. If the job and the last line are unchanged, the protocol number stays; note this in `runner/PROTOCOL.md`.