# 063 — Suite: our own multi-language agentic suite

**Status:** draft 0.6, 2026-10-23. The tasks themselves are omnimergekit's to
write. This file states what automaticum provides so that such a suite can
exist.

## 1. Form

**Decided by the owner (2026-10-12):** the suite is **agentic only**: a small
repository, an instruction, hidden tests. The agent edits and may run the
visible tests only. Completion-form tasks are out of scope (060 §1).

## 2. Languages

**Decided by the owner (2026-10-12):** first set **C# (dotnet), C/C++, Python,
TypeScript, Go, Rust**, the same answer as for the language layers (030 §3).

- One language layer per language, pinned by digest, with the compiler or
  interpreter version in the basis.
- **Per-language time limits**, calibrated from the reference solution's run
  time on the lane.

As built (2026-10-23): the layers are `python`, `go`, `rust`, `cpp`, `dotnet`
and `typescript` under `images/layers/`, each built by `automaticum image
build <name>` as `automaticum-lane:<name>`. Each takes its toolchain from the
very image, pinned by digest, that the training tasks' oracles run in, so the
agent and the oracle use the same compiler: Python 3.12 (standard library
only), Go 1.23, Rust 1.83, GCC 14 with make, the .NET 8 SDK. None fetches
packages when a run starts (Go and Cargo are set offline). `libatomic1`, which
the compiled task checkers need, comes from the base.

- The `cpp` layer is the exception to "on the base": the GCC 14 image is
  Debian 13, and its compiler needs a newer C library than the base's
  Debian 12. It starts from that image and copies Node, the runner and the
  `node` user's id from the base, so the base is rebuilt first.
- The layers carry no language server yet: the runner does not read
  diagnostics (040 §6), so 030 §3's language server is added with that work.

## 3. Tests

- **Hidden tests** live outside the workspace and are run by the harness
  (060 §3). Visible tests, when given, are a separate file set.
- **A reference solution and a known-bad solution per task per language.** The
  suite is admitted only when `oracle verify` passes for all of them, and the
  reference is run several times to reject flaky tests.
- **Partial credit** (tests passed ÷ tests) is recorded and is never the
  headline. The headline is all-tests-pass.

## 4. Structure

- **Parallel tasks:** the same problem in several languages shares a problem id
  and has its own task id. Reports show both per-language and per-problem
  views. The same problem in two languages is two draws, not a repeat.
- **Tiers and families** as task labels (difficulty; kind: algorithmic,
  refactor, bug fix, API use, build and configuration), so a score change can
  be attributed. Attribution is done on balanced axes with per-category n
  shown (070 §6).
- **Versioning:** any change to a task, a test, a limit or a toolchain is a new
  task version and a comparability boundary (020 §4).
- **Leakage:** tasks carry a canary string and a private flag (060 §7).

## 5. Diagnostics

Everything in 070 applies unchanged. Additionally, per language: compile
errors, test failures and timeouts counted apart.
