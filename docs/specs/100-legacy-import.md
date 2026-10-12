# 100 — Import of the old harness's corpus

**Status:** draft 0.5, 2026-10-12. Figures are from a read-only survey of the
old harness on 2026-10-12. The old harness is paused, not retired: the import
reads it and changes nothing there.

## 1. What exists

| set | runs | size | transcripts | session state kept |
|---|---|---|---|---|
| native loop | 438 directories (418 finished) | 2.2 GB | compressed for all but the newest few | 8 |
| atomic loop | 21 (13 finished) | 5.5 GB | one stream per transaction | per transaction |
| teams loop | 6 | 7.8 GB | raw, one of 7.5 GB | 6 |
| abandoned and discarded | 9 | small | raw | 9 |
| earlier campaigns (archive) | 30 named campaigns | 23 GB | mixed | mixed |
| external state archive | 166 runs | 54 MB | one compressed tar of session state per run | 166 |

Verdict lines for the native loop: 424, of which 284 passed, 96 failed, 24
timed out, 19 aborted and 1 is malformed. A live VS Code session archive from
the Windows host also exists, as copied session files and notes.

## 2. Mapping

- **Run ids are kept.** Every imported run carries its original id, so old
  findings stay checkable.
- **Verdicts:** `FIXED` → `PASS`; `broken` → `FAIL`; `TIMEOUT` and `ABORTED`
  unchanged; the atomic loop's `ROLLBACK-FAILED` → `ABORTED`. The original word
  is kept in a separate field.
- **Known-invalid runs** recorded by the old harness's own notes (a swallowed
  template, a stale lane script, a model unloaded mid-run, a non-pristine
  workspace) are imported as `INVALID` with the reason and the original verdict
  beside it.
- **The event stream is the CLI's wrapped format**, not the SDK's: each event is
  wrapped, the CLI adds its own events, teammate events are dropped except
  usage, and an accumulated-text field was stripped to save disk. The importer
  maps it to the SDK event shape and records what is missing.
- **Per-run text files** (build, prompt, oracle output, exit line, transactions,
  checks, escalations, server and runtime logs, before and after files,
  compaction snapshots) map to provenance fields and blobs.
- **The arm** is reconstructed from the build file and the driver script that
  launched the run. Fields that cannot be recovered are recorded as `unknown`,
  never as a default.
- Imported runs are marked `imported`, with the importer's version.

## 3. Comparability boundaries

The oracle changed twice, and each change is a task version (020 §4):

| task version | oracle | from |
|---|---|---|
| 1 | 30 frames; never left the countdown; scored broken code as passing | the first run |
| 2 | 400 frames, must reach play | run number 0213 of the native loop |
| 3 | compiled oracle with game-logic traps | run number 0411 of the native loop; 21 runs |

There is no "oracle v2" under that name in the old harness; the three versions
above are numbered here for the first time. Runs under version 1 are imported
with a finding that their oracle could not fail.

Change days recorded in the old harness's change logs (a scoring change, a
harness change) are imported as further boundaries.

## 4. What cannot be recovered

- **What was sent on the wire.** The old harness never captured request bodies.
  Its `state/` directory is the CLI's data directory: session messages, a
  settings file and logs. It shows the conversation, not the requests.
- That directory survives for 8 native runs plus the 166 archived ones. For the
  rest, sampler, thinking and context are known only as configured.
- Reasoning deltas made up almost all events in the sampled stream; where the
  accumulated field was stripped, text is rebuilt from deltas.
- Runs from the loop that had no oracle keep their analysis-script verdict,
  marked as such.

Reports treat "as configured" values from imported runs as weaker than "as
sent" values, and say so when a comparison mixes the two.

## 5. Scripts this replaces

| old | what it did | here |
|---|---|---|
| the four loop scripts | the loops | scheduler and runner (050, 040) |
| about forty one-arm driver scripts | one arm each, with gates in bash | arm files and the gate engine (030, 050) |
| about twelve chain and launch scripts | sequences of arms | campaigns |
| tag and parameter check scripts | tag intact, parameters exact | gates (050 §3) |
| the per-run analysis script | tool sequence, budget, logs | run report (070 §1.1) |
| run-metrics and thinking-statistics scripts | comparable numbers per run | metrics (070 §2) |
| the statistics script | rates, streaks, medians | statistics (070 §6) |
| A/B report and auto-score scripts | A/B scoring | A/B report |
| the transaction watcher | live transaction and stall watch | live events and watchdogs (050 §5) |
| tool-analysis scripts | tool failure census | census (070 §6) |
| the escalation study | trigger recall and false alarms | struggle signals (070 §4) |
| struggle calibration (extract, replay) | replay a rule over transcripts | reanalyse |
| the census scripts | leaks, missing paths, per-model census | findings (070 §4) |
| the server-log fetcher and its hook | fetch the server's log | endpoint log source (030 §4) |
| the state archivers | keep the session state | store (080) |
| compaction and tool-schema snapshot scripts | snapshots | artefacts (020 §2) |
| the accumulated-field stripper | shrink the stream | storage compression (080 §4) |
| the server run recorder | record a server during a run | endpoint recorder |
| the log grabber on the Windows host | copy a live log through a shared handle | native Windows agent (010 §4) |
| per-day change logs | comparability boundaries | task and harness versions (020 §4) |

## 6. Order

The import is also the store spike's data (080 §7), so the importer is written
first, against the store interface, and its output is checked against the old
verdict files: the counts in §1 must be reproduced exactly.
