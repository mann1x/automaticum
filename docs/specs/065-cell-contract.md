# 065 — The cell contract and exports

**Status:** draft 0.6, 2026-10-12.

## 1. Purpose

`omk_eval` already drives its non-lm-eval backends the same way: a subprocess
writes one JSON file and one function reads it. automaticum fits that shape
(mode C, 000 §2), and the same command is the way to run any cell.

## 2. Invocation

One blocking command, safe to re-run:

```
automaticum cell run \
  --suite <suite[:subset]> --arm <arm file or id> \
  --endpoint-url http://<host>:<port>/v1 --served-model <name> \
  --out <dir> --idempotency-key <key> --json
```

- **Decided by the owner (2026-10-12):** in mode C the endpoint is passed in,
  already serving, and **`omk_eval` always owns serving**. automaticum probes
  the endpoint and gates on it, and never starts, stops, loads or unloads
  anything there. GPU placement, context per slot and the serving library stay
  protocol-locked in omnimergekit.
- Arm fields the caller resolves (sampler, thinking, output budget, context)
  arrive as explicit values with their source recorded as the caller's
  template. They are still verified on the wire (040 §4).
- **Resume:** re-running with the same key does not repeat finished runs and
  restarts unfinished ones. Every long run dies at least once.
- A line-oriented **progress stream** on stdout (`started`, `n_done/n_total`,
  `finished`), so the caller's log shows movement.

### 2.1 Exit codes

Stable, and distinguishing these five outcomes. The omnimergekit session
confirmed on 2026-10-12 that these numbers work for `omk_eval`, which maps a
child's code to its own:

| code | meaning |
|---|---|
| 0 | finished and clean |
| 10 | finished with warnings (the result file lists them) |
| 20 | a gate refused; nothing ran |
| 30 | incomplete; resume is possible with the same key |
| 70 | internal error |

On **20** and **30** the reason is machine-readable: `<out>/refusal.json` names
the gate that refused (code 20) or the unfinished run ids (code 30), so the
caller's log line says why.

## 3. The result file

`<out>/automaticum_result.json`, written once, atomically, at the end. It is
the only file the caller reads for the score.

```json
{
  "schema": "automaticum.cell/1",
  "suite": "swe-bench", "subset": "verified-50", "suite_version": "…",
  "task_set_sha256": "…", "n_expected": 50,
  "arm_id": "…", "cohort": "…", "declared_variable": "…",
  "headline": {"metric": "resolved_rate", "value": 0.42, "higher_is_better": true},
  "counts": {"pass": 21, "fail": 26, "timeout": 2, "aborted": 1, "invalid": 0, "skipped": 0},
  "scores": {"resolved_rate": 0.42, "loop_rate": 0.06, "empty_patch_rate": 0.04},
  "basis": {},
  "basis_sha256": "…",
  "token_stats": {},
  "generation_caps": {},
  "degeneration": {},
  "tool_calls": {},
  "detector_self_test": {},
  "warnings": [],
  "complete": true,
  "runs": "runs.jsonl",
  "started_at": "…Z", "finished_at": "…Z", "duration_s": 0
}
```

`basis` is 020 §5; `token_stats`, `generation_caps`, `degeneration` and
`tool_calls` are 070 §2, §5, §3 and §4.

Rules, each from an incident:

- The headline is computed over `n_expected`, and `counts` sums to it. A score
  over a different denominator than its sibling is not a delta.
- **Errored tasks are never silently dropped and never silently counted as
  failures.** The file says how each was counted.
- `warnings` is machine-readable, and non-empty when a cap, a basis mismatch, a
  detector self-test failure or an incomplete set was seen. A warning is a
  reason not to tabulate, so it is in the file and not only in a log.
- `complete: false` always comes with a non-empty `warnings`, so a reader that
  only checks warnings still stops.
- Fields may be added within a schema version; readers ignore keys they do not
  know.
- `runs.jsonl` has one row per run: task id, verdict, the per-run numbers of
  070, and the run id to drill into.
- Timestamps are UTC with offset.
- The schema is versioned. A change that removes or re-defines a field is a new
  schema version.

## 4. Re-scoring

Results, raw transcripts and adapters are permanent (080 §5). A scorer or
detector change can re-score a cell from stored material without the model:
`automaticum reanalyse` for detectors and metrics, `automaticum rescore` for
scorers. A re-scored cell gets a new result file and keeps the old one.

## 5. Exports

- `automaticum_result.json` and `runs.jsonl` per cell.
- **The basis in plain text** per cell, so the diff of two arms' files is the
  audit of a cohort.
- Raw per-run artefacts by run id on request: transcript, requests as sent,
  patch or final workspace diff, scorer logs.
- `automaticum export cohort <name> --format parquet|jsonl` with one row per
  run and every per-run number as a column.
- A layout option matching omnimergekit's result tree,
  `<results>/<suite>/<served-model>/`, so existing tabulators find the files.
- A **per-campaign export** that can be mirrored to another host: every result
  exists on the master host too.
- Any slice as JSONL, CSV or Parquet, and a self-contained HTML report
  (090 §6).

Private tasks are never exported to a cloud destination (060 §7).
