---
name: learned-lane-host-deploy
description: Deploy a new automaticum build (daemon, CLI, repo checkout, unit changes) onto a running lane host without disturbing runs
---
# Deploying a new automaticum build to a lane host

Use this when committed code has to go live on a lane host that already runs `automaticumd` (for example blackswan-2). The host's real ssh alias and data root are in `docs/local/hosts.local.md`; they never go in a tracked file. Below, `<root>` is the data root (`bin/`, `config/`, `repo/`, `data/`, `work/`).

## 0. Make sure you may touch the daemon

- Client sessions run their own batches on the lane. If one has asked for a hold on daemon changes, wait for its release. Never restart the daemon while a run is live or queued.
- Claim the build host's resource for the build with `claude-hooks-res run <resource> -r "<why>" -- ...`.

## 1. Build

```bash
make build SPIKE_ROOT=<dir> > <dir>/work/make-build-<sha>.log 2>&1; echo rc=$?
./bin/automaticum version
```

The version prints `<sha>-dirty` when the build host has untracked or modified state files (`.wolf/cron-state.json` and the like). The code is still exactly `<sha>`; say so when you report the deploy.

## 2. Stage. Never copy onto the live names

```bash
scp bin/automaticumd <alias>:<root>/bin/automaticumd.new
scp bin/automaticum  <alias>:<root>/bin/automaticum.new
ssh <alias> 'sha256sum <root>/bin/*.new'; sha256sum bin/automaticum bin/automaticumd
```

Copying straight to `bin/automaticumd` fails with `Text file busy` while the daemon runs. Copying straight to `bin/automaticum` succeeds but leaves no `.prev` to roll back to.

## 3. Idle-gated swap, in one ssh script

The CLI is not on PATH on the host. Call it by path, with the configuration set:

```bash
ssh <alias> 'set -e; B=<root>; export AUTOMATICUM_CONFIG=$B/config
S=$($B/bin/automaticum status 2>&1); echo "$S"
echo "$S" | grep -q "^lane-1 .* idle" && echo "$S" | grep -q "^0 queued" || { echo NOT-IDLE; exit 3; }
cp -p /etc/systemd/system/automaticumd.service /etc/systemd/system/automaticumd.service.pre-<sha>
systemctl stop automaticumd
for n in automaticumd automaticum; do mv $B/bin/$n $B/bin/$n.prev; mv $B/bin/$n.new $B/bin/$n; done
systemctl daemon-reload; systemctl start automaticumd
sleep 3; systemctl is-active automaticumd; $B/bin/automaticum status | head -3'
```

- The unit should match `examples/systemd/automaticumd.service`: `TimeoutStopSec=infinity`, so that a stop waits for the daemon to wind down (050). An older installed unit with `TimeoutStopSec=120` can be fixed in the same script with `sed -i`. Keep the backup.
- Run `daemon-reload` after any change to the unit file. Otherwise systemd warns that the unit changed on disk.

## 4. Repo checkout on the host

```bash
ssh <alias> 'cd <root>/repo && git status --short | head -5 && git fetch -q origin && git checkout -q <sha> && git log --oneline -1'
```

The host reads tasks and images from this checkout, so it must match the binaries.

## 5. Verify

- `automaticum status` shows the new version and the lane idle.
- `automaticum detect <a recent run>` prints `detector library version N`; check that N is the new one.
- `automaticum doctor` shows gate refusals. A `model_tag` refusal can just mean that a client has another model on the shared endpoint. Report it and leave it alone.
- Any new SDK build needs `automaticum sdk add` first (see `learned-new-sdk-release`).

## 6. Afterwards

- Stored runs keep their old analysis until `automaticum reanalyse` runs on them. The client batches belong to their owners, so do not reanalyse them unless asked.
- Tell the client sessions what changed: the version, the new findings and metrics, the unit change, and that their arm, model, endpoint and lane files were not touched.
- Record the deploy in `.wolf/memory.md`.

## Rollback

Run `systemctl stop automaticumd`, `mv bin/automaticumd.prev bin/automaticumd` (and the same for the CLI), restore the `.pre-<sha>` unit if it changed, run `daemon-reload`, then start the daemon again.

## Token file check (before or after the swap)

- `automaticum.toml` on the host usually has no `token_file` line. The daemon then reads `<root>/config/token` (the default in `internal/config/local.go`). Grepping the TOML for `token_file` finds nothing, and that is not an error.
- Check the file directly: `stat -c '%a %s' <root>/config/token; head -c7 <root>/config/token`. A file that starts `sha256:` holds the hash (D132), so the CLI needs `client_token_file` for the plain token. Any other file holds the plain token and keeps working with a new build.
- Do not rewrite the token file during a deploy. It belongs to the installation, and client sessions use it.

## Shared lane host: coordinate, don't auto-swap

When the lane host is shared with a client running batches and with other GPU users, the owner wants a coordinated deploy, not an unattended idle-gated watcher. If you started a watcher script, stop it (TaskStop) before it swaps, and say so to the client.

1. Stage the new build as `bin/automaticumd.new` and `bin/automaticum.new`, and compare sha256 on both ends. This is safe at any time.
2. The client drains and pauses the lane. Ask them by mail; don't pause it yourself.
3. Anyone else who claimed the host's GPU resource (see `claude-hooks-res status`) finishes and releases it.
4. Claim it yourself: `claude-hooks-res acquire <host>/gpu<N>:exclusive -r "automaticum deploy"`.
5. Check again that `automaticum status` shows the lane idle with `0 queued`, then mail the client that the deploy is starting.
6. If `main` has moved past the staged commit, rebuild (`make build`) and stage again first.
7. Do the swap in one `set -e` ssh script: back up the unit, `systemctl stop`, `mv` each binary to `.prev` and each `.new` into place, `daemon-reload`, `start`, check `is-active`, check out the commit in the repo, and run `automaticum status`.
8. Run `claude-hooks-res release <host>/gpu<N>`, then mail the client DONE with the new version string, so they can resume the lane and queue batches again.

While you wait, set a recurring mail check (CronCreate, e.g. hourly at a fixed minute). Its prompt holds the whole sequence and the rules: never disturb a running batch, never restart the daemon with a run live or queued, and make no owner decisions on the client's behalf.

## Deploying a runner protocol bump: lane images and a stub smoke

Use this when the commit changes `const PROTOCOL` in `runner/runner.mjs`. A run started on a lane image built with the old protocol is SKIPPED, so the deploy has three steps.

1. **Ask the client first.** Mail what changes, that the protocol goes N→N+1, the steps, how long it takes, and that no GPU is used. Wait for their OK. Mail START (and "queue nothing until DONE") and DONE with the deployed version.
2. **Swap the binaries**, gated on the lane being idle, as usual: `status` must show `lane-1 .* idle` and `0 running, 0 queued`. Keep the old binaries as `.prev` and back up the unit. Then check out the repo at the commit (`git fetch -q origin && git checkout -q <sha>`).
3. **Rebuild every lane image.** `automaticum image build` with no arguments builds them all, base first, then the layers in name order. That is 11 images today: base, cpp, dotnet, go, python, rust, typescript and the 4 `tb2-*` layers, which takes about 2 minutes. Run it in the background with its log under `$B/work/`, never /tmp.
4. **Verify**:
   - `automaticum image ls`: every CREATED time is new.
   - `docker run --rm --entrypoint sh automaticum-lane:base -c 'grep -n "PROTOCOL = " /opt/automaticum/runner/runner.mjs'` shows the new number.
   - `systemctl show automaticumd -p NRestarts` is 0, and `detect <a finished run>` reports the expected library and analysis versions.
5. **Stub smoke, one run of your own.**
   - The `stub` endpoint does not answer (`endpoint stub does not answer /api/tags`) until you start the stub server yourself. It must listen on the docker bridge gateway named in `config/endpoints/stub.toml`'s `base_url`. The scripts live in `$B/work/stub/` (`fix-answer.json` for `demo/answer`):
     `cd $B/work; nohup $B/bin/automaticum stub serve -listen <gateway>:7412 -script stub/fix-answer.json > stub-smoke.log 2>&1 & echo $! > stub.pid`
   - `automaticum run start --arm stub-sdk-<release> --task demo/answer` (there is no `--batch` flag), then `timeout 300 automaticum wait run <id>`. Expect PASS in about 30 s.
   - Check the new feature on that run (`run show`, `run timeline -requests`).
   - Stop the stub with `kill $(cat stub.pid)` and leave the lane idle with 0 queued.

A CLI-only change (a new verb, nothing in the daemon or the runner) needs only the CLI binary swapped, with no restart and no image rebuild.

## Addendum: checks learned from the D144 swap (2026-11-09)

### Before staging
- Commit first, then build. A binary built before `git commit` reports the previous sha plus `-dirty` in `version`, `status` and the log. Check that `./bin/automaticum version` shows the commit you are deploying.

### After staging, before asking for a window
- Check the host's live configuration with the staged CLI while the old daemon still serves:

```bash
ssh <alias> 'B=<root>; AUTOMATICUM_CONFIG=$B/config $B/bin/automaticum.new config check --repo $B/repo; grep -n ExecReload /etc/systemd/system/automaticumd.service || echo no-ExecReload'
```

- Put the result in the mail asking for a window, for example: your configuration passes with the new build, N arms, N models, N tasks.

### Unit without ExecReload (hosts installed before D144)
In the same idle-gated ssh script, after the idle check and before `systemctl stop`:

```bash
U=/etc/systemd/system/automaticumd.service
cp -p $U $U.pre-<sha>
grep -q "^ExecReload=" $U || sed -i "/^ExecStart=/a ExecReload=/bin/kill -HUP \$MAINPID" $U
```

Then run `systemctl daemon-reload` before `systemctl start`. When `systemctl stop` warns "unit file changed on disk", that is expected.

### After the swap
- `automaticum lane show <lane>` for every lane the client named. It lists the endpoints that are now loaded. Clients often install endpoint files ahead of time and wait for this before they queue.
- `systemctl reload automaticumd; sleep 2; automaticum config show`, then `journalctl -u automaticumd --since -2min | grep reload` should show `configuration reloaded: nothing changed source=signal`.
- `systemctl show automaticumd -p NRestarts` should print 0.
- The DONE mail names the rollback files: `bin/*.prev` and `automaticumd.service.pre-<sha>`.
## A commit that raises the store's schema version

Check `git diff <deployed>..<sha> -- internal/store/duck/duck.go | grep SchemaVersion`. A build refuses a database of a newer version than its own. So once the new daemon has migrated `state.duckdb`, `bin/automaticumd.prev` will not start on it. In the swap script, after `systemctl stop` and before `start`, copy the database:

```bash
cp -p $B/data/state.duckdb $B/data/state.duckdb.pre-<sha>
[ -f $B/data/state.duckdb.wal ] && cp -p $B/data/state.duckdb.wal $B/data/state.duckdb.wal.pre-<sha>
```

Rollback then means stopping the daemon, restoring both files and the `.prev` binaries, and starting it again. Runs recorded after the swap are lost in a rollback, so say that in the DONE mail. Before the window, rehearse the migration on a copy of the host's database with the new build.

## Smoke runs and cleanup after the swap (learned 2026-11-12)

- Before any change, mail START with the steps. Ask the client to queue nothing until DONE. Hold until their explicit "go" mail names the runs that must finish first.
- Keep every backup in `<install>/work/` (`base.toml.pre-<sha>`, the unit as `/etc/systemd/system/automaticumd.service.pre-<sha>`, binaries as `bin/*.prev`). Never put a backup under `<install>/config/`: the daemon watches that directory and reloads it live.
- A protocol bump (`internal/run/protocol.go`) needs every lane image rebuilt: `automaticum image build -repo <repo>` with nohup, its log in `work/`, its pid in a file. Poll it from a background command; do not end that command with a bare `grep -c` (it exits 1 on 0 hits). Then check `automaticum image ls` and `grep 'PROTOCOL = ' /opt/automaticum/runner/runner.mjs` inside the base, then run `docker image prune -f` to get the old layers' disk back.
- A new SDK release on the host: `automaticum sdk add -repo <repo> <ver>`, then `automaticum sdk verify -repo <repo> <ver>`, which must say "reproduced".
- When the repo checkout moves, run `automaticum oracle verify --repo <repo> <suite>/<task>` for every task you will smoke on. Without it, `run start` refuses with "was not proven against its fixtures".
- Write each smoke as one `ssh <host> bash -s <<'EOF' ... EOF` block. Start the stub with `nohup automaticum stub serve -listen 172.17.0.1:7412 -script work/stub/<script>.json &`. Parse the run id with `grep -oE '^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$'`. Then `timeout 300 automaticum wait run $ID`, then `run show $ID`, and only then kill the stub. A stub stopped early gives SKIPPED `gate model_tag: endpoint stub does not answer /api/version`.
- Smoke set: the default stub arm on `demo/answer` (script `fix-answer.json`), and for LSP a temporary arm `extends = "stub-sdk-<ver>"` with `[agent.lsp] enabled = true` on `custom/manic_miner` (script `fix-manic.json`). Check `lsp.source` and `lsp.started` in the `prepared` note (`automaticum logs <id> | grep '"note":"prepared"'`) and the `lsp.*` metrics in `run show`. Remove the temporary arm afterwards.
- Release the build claim. Mail DONE with the versions, image ids, smoke run ids and verdicts (including any SKIPPED run of your own and why), and the rollback files.