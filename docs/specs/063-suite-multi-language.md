# 063 — Suite: our own multi-language agentic suite

**Status:** draft 0.5, 2026-10-12. The tasks themselves are omnimergekit's to
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
