# 100 — Import of the old harness's corpus

**Status:** draft 0.8, 2026-10-13. The importer exists
(`internal/legacyimport`) and has imported the run directories of the real
corpus; §7 has the result. The figures in §1 are from a read-only survey on
2026-10-12, corrected where the import showed otherwise. The old harness is
paused, not retired: the import reads it and changes nothing there.

## 1. What exists

| set | runs | size | transcripts | session state kept |
|---|---|---|---|---|
| native loop | 438 directories (418 finished) | 2.2 GB | compressed for all but the newest few | 8 |
| atomic loop | 21 (13 finished) | 5.5 GB | one stream per transaction | per transaction |
| teams loop | 6 | 7.8 GB | raw, one of 7.5 GB | 6 |
| abandoned and discarded | 9 | small | raw | 9 |
| earlier campaigns (archive) | 30 named campaigns | 23 GB | mixed | mixed |
| external state archive | 168 files (166 runs, 2 aborted) | 54 MB | one compressed tar of session state per run | 168 |

Verdict lines for the native loop: 424, of which 284 passed, 96 failed, 24
timed out, 19 aborted, and 1 is not a run line: it is an operator's comment
that voids one run. A live VS Code session archive from the Windows host also
exists, as copied session files and notes.

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

## 7. Result of the first import (2026-10-13)

Importer version `legacyimport/0.1.1`, into the spike's store.

| | |
|---|---|
| runs imported | 474 of 474 directories; none failed |
| verdict files | all three reproduced exactly, every run line matched to an imported run by its original id |
| mapped verdicts, native loop (447 with discarded and abandoned) | 284 `PASS`, 91 `FAIL`, 24 `TIMEOUT`, 41 `ABORTED`, 7 `INVALID` |
| mapped verdicts, atomic and teams | 13 `PASS`, 8 `ABORTED`; 5 `PASS`, 1 `FAIL` |
| task versions, native loop | 220 runs under version 1, 204 under version 2, 23 under version 3 |
| findings written | 247: every run under version 1, "the oracle could not fail" |
| tool calls extracted | 64,801 |
| size | 43.4 GB uncompressed read, 0.88 GB stored |
| time | 295 s on one core |
| old harness before and after | 17,304 files both times; it was mounted read-only |

**What the corpus showed that this spec did not say:**

- The native verdict file's 423 run lines are 417 directories of the native
  set, one whose directory was renamed with a void marker, and 5 runs that now
  sit among the abandoned runs. The `exit` files of the native set alone give
  279 passes, not 284.
- Six run directories carry a marker after the run id (a void note, a
  reason). The id is read from inside the name.
- Run numbers repeat across days, so a boundary is compared on the full run
  id, never on the number.
- Version 3 has 23 directories, of which 21 finished.
- The old harness's file of invalid runs lists one group of five. Two more
  invalid runs come from void markers. The other reasons named in §2 (a stale
  lane script, a model unloaded mid-run, a non-pristine workspace) are not in
  that file, so those runs are **not** marked `INVALID` by the import.
- No `ROLLBACK-FAILED` verdict exists in the corpus.
- State archives: 168 files in the external archive and 15 more inside the old
  harness's directory; 182 runs have one. Session state is still on disk for 44
  run directories, 8 of them native.
- The streams hold 30 GB, of which about 0.45 GB is distinct text: nearly
  every line is a streaming delta that the closing event of the same block
  repeats in full.

**Fields recorded as `unknown`, by number of runs (of 474):** what was sent on
the wire, thinking, host, arm id, driver script and claimed success for all
474; sampler 458; session state 277; accumulated text 256; protocol arm 128;
context window 47; agent end 46; verdict 30; endpoint 28; provider 16; start
and end time 12; wall time 2.

**Choices the importer makes that the owner has not seen** (each can be
changed and the import re-run; it is idempotent per run):

- A run directory with no `exit` file is imported as `ABORTED`, with the
  verdict listed among its missing fields.
- Streams are stored exactly as the old harness wrote them. The mapping to the
  SDK's event shape (§2) is applied when a stream is read, not at import, and
  text is not rebuilt from deltas at import.
- The arm is taken from the build file, the saved settings and the stream. The
  driver scripts are not read.

**Not imported yet:**

- the 30 earlier campaigns under the old harness's archive (23 GB);
- the per-loop result files beside the verdict files;
- the VS Code session archive from the Windows host.

**Decided by the owner (2026-10-13):** the earlier campaigns are imported only
when a question needs them. They have their own layouts and no verdict file
of the same form; the importer's parsers cover the files they share with the
loops.

Build files and saved settings in the corpus hold endpoint addresses and
absolute paths, and these are now in the imported arms. They stay in the store.
An export of an imported run must pass them through the same redaction as any
other export (010 §5).

## 8. Second pass (decided 2026-10-13)

The Cerebriline session assembled the invalid runs by id from the old
harness's notes, change logs, directory markers and exit files.

**Decided by the owner (2026-10-13):**

- Runs the old harness's own records call invalid are `INVALID` (§2): the
  five with a swallowed template, the two with a void marker, six runs of
  2026-09-11 that were partial states and operator kills (numbers 0207 to
  0212), and one of 2026-09-13 killed in flight (0296).
- **Verdicts contradicted by how the run ended are re-mapped**, with the
  original word kept beside the new verdict:
  - recorded as failed, ended by an operator's kill: `ABORTED`;
  - recorded as failed, ended by the CLI's own clock before a timeout verdict
    existed: `TIMEOUT`;
  Each carries a finding that says whether the cause is **documented** in the
  old notes, **reported** by the session that ran the old harness without a
  trace in the corpus, or **inferred** from exit code and wall time.
- **The two sibling lanes are imported**: a second lane of 51 runs, of which
  18 ran under a lane script that was four days stale (old oracle, no timeout
  or abort verdicts, other limits), and a third of 5 runs. Their run numbers
  restart at 0001, so their run ids carry a lane prefix.
- A boundary for the four runs of one build with two defects since fixed was
  offered and **not** chosen. Those runs stay as they are.

Two cases follow from §2 as it already stood, and are not new decisions: the
one run whose model was unloaded mid-run by another model's load, and the 18
runs under the stale lane script, are `INVALID`, each with its original
verdict beside it.

The verdict files are still reproduced exactly on the *original* words; the
re-mapping changes the mapped verdict only.

**Result of the second pass (importer `legacyimport/0.2.0`, 2026-10-13):**

| | |
|---|---|
| runs | 474 in the main lane, 52 and 5 in the sibling lanes; none failed |
| verdict files | the three of the main lane and one per sibling lane, all reproduced exactly |
| `INVALID` by the old records | 8 more, 15 in all in the main native loop |
| `INVALID` for the stale lane script | 18, all in the larger sibling lane |
| re-mapped to `ABORTED` | 6: one documented, one reported, four inferred |
| re-mapped to `TIMEOUT` | 9, all inferred: four at 21,601 to 21,602 s and five at 5,402 to 5,405 s, exit code 1 |
| main native loop, mapped, before and after | `PASS` 284 to 282, `FAIL` 91 to 75, `TIMEOUT` 24 to 33, `ABORTED` 41 to 42, `INVALID` 7 to 15 |
| larger sibling lane, mapped | 12 `PASS`, 11 `FAIL`, 5 `TIMEOUT`, 6 `ABORTED`, 18 `INVALID` |
| smaller sibling lane, mapped | 1 `PASS`, 4 `ABORTED` |

- A sibling lane's run id is the lane's name, a hyphen, and the original id.
  Every run now carries its lane; the main lane's ids are unchanged.
- Task versions in the sibling lanes come from each run's own files, not from
  the main lane's boundaries: the 18 stale-script runs are version 1, the
  other 39 are version 2.
- The larger lane has 52 run directories, not 51: one sits among its
  abandoned runs. Both sibling lanes have a verdict file.
- The two `PASS` that became `INVALID` are runs of 2026-09-11 that the old
  change log says must not be read as an arm.
