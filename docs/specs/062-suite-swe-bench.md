# 062 — Suite: SWE-bench and SWE-bench-style sets

**Status:** draft 0.5, 2026-10-12.

## 1. Subsets

- Verified, Lite and full.
- **Fixed named subsets** with their instance-id list stored and hashed. A
  50-instance cell is what fits a single GPU per arm. The subset is part of the
  basis.

## 2. How it runs

The agent works in the instance's repository in a lane container. automaticum
then writes the `predictions.jsonl` the official harness consumes
(`instance_id`, `model_name_or_path`, `model_patch`), runs the official
evaluation images, and ingests their reports. This is the predictions-file
adapter (064 §3.1).

Needs: instance-level caching of environments, sharding across lanes, resume of
a half-finished evaluation, and budgets per instance (wall time, requests,
tokens) recorded as arm fields.

## 3. Scoring

Only by the official harness images, version-pinned and recorded by digest.
Per instance the harness reports one of five results, with the test logs:

| harness result | verdict | note |
|---|---|---|
| `resolved` | `PASS` | |
| `unresolved` | `FAIL` | |
| `empty_patch` | `FAIL` | reason recorded as a finding |
| `patch_apply_failed` | `FAIL` | reason recorded as a finding |
| `error` | `ABORTED` | counted as errored, never as a failure |

`empty_patch_rate` is reported next to the headline, because an agent that
gives up scores like one that tries and fails.

## 4. Validation and flakiness

- **Gold-patch validation** on the subset before the first cell (050 §3.2).
  Instances whose gold patch does not resolve on this host exist; they are
  excluded by a **recorded list**, not silently, and the list is part of the
  basis.
- **Flakiness:** an instance whose result differs between two evaluations of
  the same patch is flagged and excluded from paired comparisons.

## 5. Degeneration

The long transcripts here are where degeneration shows. Every detector of
070 §3 runs per turn, and a degeneration cohort may declare `loop_rate` as its
headline metric instead of `resolved_rate`.

## 6. Sets for other languages

**Decided by the owner (2026-10-12):** SWE-bench-style sets for other languages
(Multi-SWE-bench, SWE-bench-java and similar) are **external suites**, each
with its own official scorer, kept apart from our own multi-language suite
(063). **C/C++ comes first.**

**Open:** which published set is adopted for C/C++, at which pinned version,
and the order of the languages after it.
