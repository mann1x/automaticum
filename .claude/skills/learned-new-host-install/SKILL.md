---
name: learned-new-host-install
description: Bring up a stable automaticum installation on a fresh host (e.g. blackswan-2)
---
# Installing automaticum on a fresh host

Used to stand up a self-contained automaticum install on a new main host (D93/D95: the host runs its own daemon, store, lanes and data root; it is not reached through the dev host).

## Layout and prerequisites

- Put the whole tree on a **data disk**, never on the root filesystem that holds Docker's root (D95/D96). A typical tree: `<root>/{bin,config/endpoints,config/lanes,data,work,archive,repo}`.
- The host needs: git, a running Docker engine, systemd. Check the engine's API range: `docker version --format '{{.Server.Version}} api {{.Server.APIVersion}} min {{.Server.MinAPIVersion}}'`. Docker 29.1+ enforces a minimum API of 1.44 (the client negotiates this, bug-027, but confirm the engine is reachable).

## Steps

1. **Tree + repo.** `mkdir -p` the tree; `git clone` this repo into `<root>/repo` and `git checkout` the exact commit the dev host built from (`H=$(git rev-parse HEAD)`).
2. **Binaries.** `scp` the dev host's `bin/automaticum` and `bin/automaticumd`. They are built on the glibc-2.28 baseline (manylinux_2_28), so they run on newer glibc hosts unchanged — no rebuild needed. Confirm with `./bin/automaticum version`.
3. **SDK bundles.** `rsync` the dev host's `data/sdk/` (the installed bundles) and `archive/sdk-packages/` (the archived packages, ~226 MB). These let `sdk verify` reproduce with no network.
4. **Config.** Write `config/automaticum.toml` (data_root, archive_root, free_floor_gb, repo), `config/lanes/lane-1.toml` (host = neutral name, endpoints, user/cpus/memory/pids), and `config/endpoints/stub.toml` (`kind="ollama"`, `base_url="http://172.17.0.1:7412"`). Generate the daemon token: `(umask 077; head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > config/token)` — it must be mode 0600 and >= 32 chars.
5. **Pull pinned images.** Before any verify or build, pull every pinned digest: `grep -rhoE '[a-z0-9./-]+@sha256:[0-9a-f]{64}' tasks images internal --include=*.toml --include=Dockerfile --include=*.go | sort -u`, then `docker pull` each `node@sha256:`/`debian@sha256:`. `sdk verify` and `image build` need the pinned node image; `oracle verify` needs the debian/node oracle images.
6. **Verify SDK + build images.** `export AUTOMATICUM_CONFIG=<root>/config`; for each build `./bin/automaticum sdk verify <build>` (expects "reproduced ... with no network ... as pinned"); then `./bin/automaticum image build` and check `./bin/automaticum image ls`.
7. **Oracles.** `./bin/automaticum task build custom/manic_miner` (rebuilds `run_game` to the pinned hash), then `./bin/automaticum oracle verify <task>` for every task — otherwise runs are `SKIPPED` by the oracle gate.
8. **Daemon as systemd.** Install `examples/systemd/automaticumd.service` with `Requires=docker.service`, `RequiresMountsFor=<data disk>`, `Environment=AUTOMATICUM_CONFIG=<root>/config`, `Restart=on-failure`, `KillSignal=SIGTERM`. `systemctl daemon-reload && systemctl enable --now automaticumd`, then poll `curl -sf http://127.0.0.1:7411/v1/status` and check `automaticum status`.
9. **Smoke test.** Start the stub (`automaticum stub serve --script <work>/stub/fix-answer.json --listen 172.17.0.1:7412`), then `automaticum run start -arm stub -task demo/answer` (task is a FLAG, not positional) and wait for PASS. Stop the stub with `kill -TERM` of its pid.

## Updating an existing install

Check out a newer commit in `repo` and copy that commit's binaries, with the lane idle and the queue empty. Never kill the sshd. Record the install facts in `docs/local/hosts.local.md` (untracked) and the roadmap spec, with neutral names only in tracked files.


## Updating an existing install (new binaries, new commit, new endpoints)

Used on blackswan-2 on 2026-10-23 to roll out protocol 4 and 5 binaries and the Gemma-4 endpoints.

1. Commit and push first: the host's `<install>/repo/` is a plain checkout the daemon reads arms from. `git fetch -q && git checkout -q <commit>` there; `git log --oneline -1` to confirm.
2. Claim the host's build resource before building images there: `claude-hooks-res acquire <host>/build:shared -r "<why>" --ttl 1h`; release it with `claude-hooks-res release --claim <id>` when the image build is done.
3. Copy the binaries without touching the running ones: `scp bin/automaticum bin/automaticumd <host>:<install>/bin/.new/`.
4. `systemctl stop automaticumd`; wait until `systemctl is-active -q automaticumd` fails; move the two binaries from `bin/.new/` into `bin/`; `rmdir bin/.new`.
5. With `AUTOMATICUM_CONFIG=<install>/config` exported, run `../bin/automaticum image build` from `<install>/repo/` (the lane images must match the runner protocol of the new binary).
6. `systemctl start automaticumd`, then poll `curl -sf http://127.0.0.1:7411/v1/status` before the first command.
7. Endpoints live in `<config>/endpoints/<name>.toml` (`kind`, `base_url`, `host`, `capacity`, and a comment describing the server: build, template sha, flags, model file sha); lanes in `<config>/lanes/<lane>.toml` with `endpoints = [...]` listing every endpoint the lane may use. Any change there needs `systemctl restart automaticumd`.
8. Validate: `../bin/automaticum status`, `arm validate <arm>` for each arm, and `doctor -all -arm <arm> -task <task>` before the first batch on a new endpoint. Expect the milestone-7 gates (`gpu_ownership`, `server_log_level`, `free`, `gpu_residency`, `expert`) as `unchecked`.
9. Prove the runner on the stub there once: `stub serve --script ../work/stub/<script>.json --listen 172.17.0.1:7412`, then `run start -arm stub-stop-sdk-<v> -task demo/answer -lane <lane>` and wait for `finished`.
