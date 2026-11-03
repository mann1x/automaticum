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