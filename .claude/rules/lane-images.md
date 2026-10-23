---
paths:
  - images/**
  - cmd/automaticum/image_cmds.go
---

# Lane image conventions

- `automaticum image build [name...]` (`cmd/automaticum/image_cmds.go`) builds `images/base/Dockerfile` as `automaticum-lane:base`, then every directory under `images/layers/` as `automaticum-lane:<name>` from `images/layers/<name>/Dockerfile` (`imageNames` lists them, `LaneImage` in `internal/daemon/daemon.go` names the tag); a new layer is a new directory with a `Dockerfile`, nothing else to register. A task names the layers it needs in `layers = [...]` of its `task.toml` (`Task.Layers` in `internal/task/task.go`, e.g. `tasks/custom/manic_miner/task.toml`).
- The layers are `python` (3.12, standard library only), `go` (1.23), `rust` (1.83), `cpp` (GCC 14 with make), `dotnet` (.NET 8 SDK) and `typescript` (npm packages on the base). Each toolchain is copied with `COPY --from=` out of the image, pinned by digest, that the suite's oracles run in (`image = "<name>@sha256:..."` in `task.toml`), so the agent and the oracle compile with the same build: change a layer's digest and the tasks' oracle `image` together. Pin by digest only, never by tag; the fresh-host install pulls every pinned digest before `image build` (`.claude/skills/learned-new-host-install/SKILL.md`).
- Every layer is offline when a run starts: no package fetch, no toolchain download (`GOPROXY=off GOTOOLCHAIN=local`, `CARGO_NET_OFFLINE=true`, no pip or NuGet packages). Nothing is left running after a build either: `dotnet` sets `DOTNET_CLI_USE_MSBUILD_SERVER=0` and `MSBUILDDISABLENODEREUSE=1` so no MSBuild server outlives a `dotnet build` in the lane or the oracle. End the Dockerfile with a `RUN` that prints the toolchain version so the build proves the copy works.
- `cpp` is the exception to "on the base": the GCC 14 image is Debian 13 and its compiler needs a newer C library than the base's Debian 12, so it starts from the gcc image and copies Node, `/opt/automaticum` (the runner) and the `node` user (uid and gid 1000) from `automaticum-lane:base`. Rebuild the base before `cpp`, and keep its apt list in step with `images/base/Dockerfile` (`libatomic1` is needed by the compiled task checkers and comes from the base elsewhere).
- The lane's and the oracle's `/tmp` are tmpfs mounted `exec` (`Execute` in `internal/run/run.go`, `RunOracle` in `internal/task/exec.go`): a compiled checker and the agent's `go test` or `cargo test` build and run programs there. Do not mount either `noexec` again.
- No layer but `typescript` carries a language server yet (063 §2): the runner does not read diagnostics (040 §6), so one is added with that work, not before.
