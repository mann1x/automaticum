---
paths:
  - internal/task/**
  - internal/config/local.go
  - tasks/**
---

# Task loader conventions

- Tasks are read from `task.Roots` (`internal/task/task.go`): `Public` is the repository's `tasks/`, `Private` the root the local configuration names in `private_tasks` (`Daemon.PrivateTasks` in `internal/config/local.go`, shown in `examples/config/automaticum.toml`; empty means none). `LoadAll` and `Find` take a `Roots`, never a bare path; tests pass `task.Roots{Public: "../../tasks"}` (`internal/gate/gate_test.go`, `internal/run/run_test.go`).
- The daemon gets its roots from `taskRoots()` in `internal/daemon/daemon.go` and the command line from `place.tasks()` in `cmd/automaticum/config_cmds.go`; a new reader of tasks calls one of those, never `filepath.Join(repo, "tasks")`.
- Both roots use the `<suite>/<task>/task.toml` layout. A task in the private root must say `private = true`; a private task needs a `canary` of 16 characters or more (`validate` in `internal/task/task.go`); the same suite and id in both roots is an error. Prove a loader rule in `internal/task/task_test.go`.
- A training-pool task (D105, revised 2026-10-23) is public, in the repository's `tasks/<suite>/`, and says `training = true` (`Task.Training` in `internal/task/task.go`, proven by `TestTrainingFlag`): its runs are rollouts for training, not measurements, and it is never a held-out gate (`manic_miner` is). The private root is not used for the pool.
- Private task text never leaves the owner's hosts (060 §7): not exported, embedded or sent to a cloud endpoint. The private root and its tasks are never committed to the public repository; its path lives only in the untracked local configuration.
