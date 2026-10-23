---
paths:
  - runner/**
  - images/base/**
  - internal/run/protocol.go
  - arms/*.toml
---

# Runner protocol conventions

- `const Protocol` in `internal/run/protocol.go` must equal `const PROTOCOL` in `runner/runner.mjs` (currently 5). Bump both together and describe the change in `runner/PROTOCOL.md` under its dated protocol entry.
- The lane image copies runner modules one by one: a new `runner/*.mjs` (for example `runner/guards.mjs`) must be added to the `COPY runner/...` line in `images/base/Dockerfile`, then `./bin/automaticum image build` before an end-to-end run.
- Every runner module has a test in `runner/test/<name>.test.mjs` (Node test runner); `runner/test/guards.test.mjs` pairs with `runner/guards.mjs`.
- A new field on the runner's last line (`End` in `internal/run/protocol.go`) that a harness rule depends on is a pointer or `json.RawMessage` with `omitempty`, so `nil` means an older runner did not report it and the old rule still holds. Add `run_test.go` rows for the new value, the old value and the field missing.
- A new run outcome (such as `guard_stop`) gets an `Outcome*` constant in `internal/run/protocol.go`, a verdict mapping in `internal/run/verdict.go`, a row in the outcome table of `runner/PROTOCOL.md` and its spec in `docs/specs/040-runner.md`.
- Guard trips are `notice` events of the SDK whose `metadata.kind` names the guard and `metadata.phase` what it did (for a `nudge`, `metadata.nudge`); `GUARD_KINDS` in `runner/guards.mjs` and `guardPhases` in `internal/config/arm.go` hold the same table and change together. Never invent a guard kind, phase or threshold: confirm it in the installed SDK build or on the stub.
- A new arm setting under `[guards]` (such as `stop_on`) is stated in `arms/base.toml` and required by `need(...)` in `internal/config/arm.go`; an empty or false value is dropped from the resolved arm so older arms keep their identity. Run behaviour the owner chooses (which guards stop a run) is arm configuration, never runner code: a `stop_on` selector is a guard or `guard.phase`, validated against `guardPhases` by `Arm.Validate()` and matched by `stopsOn` in `runner/guards.mjs`.
- Metrics derived from a new runner note (`guard_trips`, `guard_stop.*`) are added in `internal/ingest/events.go` and hidden at zero in `cmd/automaticum/client_cmds.go` like the `user.` and `mode_switches.` metrics.
