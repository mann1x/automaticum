# 070 — Diagnostics: metrics, detectors, statistics

**Status:** draft 0.14, 2026-10-14.

## 1. The rule: no custom script

Everything in this file runs automatically at ingest, is stored as metrics and
findings on the run, and is available as a CLI command, an API call and a
dashboard view. Each item replaces a script someone had to write (100 §5).

Definitions here are shared with `omk_eval`, so a number from automaticum and a
number from omnimergekit mean the same thing.

### 1.1 Per-run report

`automaticum run show <id>`: verdict and evidence; the arm, with the fields
that differ from the batch's baseline highlighted; timeline; the metrics of §2;
the findings of §3 and §4; links to every artefact.

## 2. Metrics per run

Token counts come from the server's own `usage` block whenever it exists, not
from re-tokenizing afterwards. Where the server gives no split, the split
method is recorded.

- **Time:** wall time (paused intervals excluded, 050 §4), time to first
  token, time per turn, time in tools against time in the model, idle gaps.
- **Volume:** requests, turns, tool calls by tool, tool failures by tool and by
  error class.
- **Tokens:** prompt, cached, fresh, completion; completion split into
  reasoning, visible text and tool input; tokens per second for prompt and
  generation, per request and overall, with the concurrency it was measured at.
- **Length distributions** for completion tokens and characters: n, sum, p10,
  p50, p90, max. The same for reasoning alone and visible text alone.
- **Finish reasons:** histogram; empty completions (nothing visible and no tool
  call).
- **Thinking:** blocks, tokens per block (median, p90, max), budget hits, turns
  with no thinking, thinking-only turns.
- **Context:** prompt tokens at the first and last request, peak occupancy
  against the window, share of prompt served from cache, compactions (count,
  size before and after, duration, tokens spent, trigger), overflow errors,
  full prompt re-reads per request with the cause when known.
- **Reasoning carried between turns:** per request, whether history contains
  earlier reasoning and how many tokens of it. It changes fresh reasoning
  length (median 252 against 467 characters measured).
- **Edits:** count, files, churn (lines written against lines surviving),
  edits to the same region, reverts, edit-without-check sequences.
- **Change protocol:** transactions kept, discarded, empty; checks proposed and
  their outcome; rollbacks.
- **Escalation:** offers, calls, expert cost, whether it helped.
- **Sub-agents:** spawned, per-agent verdict and spend, wait for placement,
  retries, agents that reported nothing.
- **Cost** in tokens and, for priced endpoints, money.

### 2.1 Per cell

- Every distribution above over the cell's runs.
- **Conditioned on the verdict:** token p50 and p90 over `PASS` runs only, and
  **tokens per solved task** = total completion tokens of the cell ÷ number of
  `PASS`. A median over all runs rewards a model that gives up early.
- Throughput over the whole cell. A rate from the easy first tasks is not the
  rate of the suite.
- **Sanity warnings:** n differs from expected; empty completions above 5 %;
  p10 of visible characters under the suite's declared floor.

## 3. Detectors: degeneration and looping

**Decided by the owner (2026-10-12):** one detector library, written in **Go,
in automaticum**, exposed by CLI and API (`automaticum detect` over a
transcript or a text). omnimergekit calls it in place of its own Python
implementation, whose thresholds and fixtures become the library's first test
set. Two implementations of one detector drift.

Detectors are versioned plugins, evaluated per assistant block (reasoning and
visible text separately), per run and per cell. They are never re-declared in a
report.

| detector | definition | lesson behind it |
|---|---|---|
| short-unit repeat | a 2 to 40 character unit repeated 8 or more times, where **the repeated unit itself** contains 3 or more alphabetic characters | on code-bearing transcripts every firing without that rule was indentation or a number printout |
| character run | one character repeated 41 or more times, **only when that character is alphabetic** | a run of `=` or `-` is a separator and is clean |
| sentence repeat | see §3.1 | character detectors saw 1 and 9 blocks where this saw 29 and 89. Count alone is not enough: 3 to 6 repeats with thousands of new characters between them is iterative drafting |
| runaway | a single block whose length in characters is **at or above** a declared size (per suite; no default, D75) | healthy length differs by suite |
| repeated tool call | identical name and arguments N times | |
| near-duplicate rounds | repeated rounds hidden behind a changing ordinal | |
| edit oscillation | edit and revert of the same region | |
| re-read loop | the same range read again and again; search repeated after "no match" | |

The first four are omnimergekit's reference detectors, and their definitions
here were checked against its code on 2026-10-12. The per-block result keeps
each detector's flag separate, plus `looped` as their OR. **The OR alone is
never what is stored.**

### 3.1 Sentence repeat, exactly

- Evaluated **within one block only**. The same sentence across separate turns
  is normal and does not count.
- **Units:** split on whitespace after `.`, `!` or `?`, and on newlines; then
  strip. A unit qualifies when it has 60 or more characters and 3 or more
  letters.
- **Identity:** exact string equality after strip. No normalisation of case or
  spacing.
- **Share:** count of the most frequent unit ÷ number of qualifying units in
  the block.
- **Fires** when count ≥ 3 and (share ≥ 0.30 or count ≥ 8).

Requirements:

- **Positive and negative controls, per domain.** Each detector ships a fixture
  it must fire on and healthy long text (prose, code, tool output) it must stay
  silent on. The self-test runs at start (050 §3.2) and its result is in the
  result file. A zero from a detector with no proven non-zero floor is not a
  measurement.
- `loop_rate` = runs with at least one firing ÷ runs, reported with its
  components: which detector, reasoning or text, turn index.
- **Always cross-tabulated** with the verdict and with cap hits: loops among
  passes, loops among failures, loops that ended in a cap.
- A suite subset may declare `loop_rate` as its headline metric.
- Interventions are arm fields (030 §5.1), and the report says which runs had a
  guard fire.
- When a detector changes, findings are recomputed over the whole corpus
  (`automaticum reanalyse`).
  **Decided by the owner (2026-10-13):** the imported old corpus gets the
  text detectors this way; metrics that need the wire capture exist for new
  runs only.

### 3.2 The library, as built

`internal/detect`, version 1, holds the four reference detectors and the
repeated tool call. `automaticum detect <file>` runs them on a text file
taken as one block, `automaticum detect <run>` on a stored run's blocks and
calls, and `automaticum detect --self-test` on the library's own controls.

- **Short-unit repeat** takes the unit the reference pattern
  `(.{2,40}?)\1{7,}` takes: the shortest unit that repeats at the leftmost
  place. The letters rule is applied to that unit, and a repeat whose unit
  fails it is passed over whole. So `abab…` (unit `ab`, two letters) and
  `a1 a1 …` do not fire, however long. The library's tests pin its answers
  to that pattern's.
- **Letters** are Unicode letters; lengths and offsets are in characters,
  not bytes.
- **Controls:** texts a detector must fire on, healthy ones (prose, a long
  block, code with indentation and separators, tool output, iterative
  drafting, number tables, the empty block) on which none may, and two call
  sequences.
  The daemon runs them at start. A detector that misses a control is
  **unavailable**: ingest writes `detector.<name>.available = 0` and no
  count for it (050 §3.2).
- **At ingest** each block of reasoning and of visible text goes through the
  text detectors. One finding per detector and channel, with the number
  of blocks it fired on, out of how many, the turns, and the first firing.
  Counts are also metrics (`detector.<name>.<channel>`), so that a zero from
  an available detector is on record. `detector.looped_blocks` is the count
  of blocks where any of the three looping detectors fired; it is derived
  next to its components and never replaces them.
- **Imported runs** (D65) are read in the older stream format. Where a block's
  end does not repeat the whole block, the block is put together from its
  pieces. They get the detectors' findings only: their tool calls stay as
  the importer wrote them and they have no wire-based metrics.

**Decided by the owner (2026-10-13), D75: no threshold is chosen here.**
Where the agent's own SDK has a guard for the same thing, the detector is
that guard read after the run, with the SDK's definition and thresholds,
which were set on recorded sessions. Library version 2 (this closes O19 and
O20):

| detector | definition, as in the SDK's source | source file |
|---|---|---|
| `reasoning_repetition` | in a block of 4,000 characters or more, among the paragraphs of 60 characters or more that carry a sentence (3 or more common words; a bare fenced quote does not count): one paragraph 4 or more times **and** duplicates making up 25% or more of them | `packages/agents/src/reasoning-repetition.ts` |
| `reasoning_loop` | from 4,000 characters of a block, any of: 4 or fewer distinct lines in a full window of 60 lines (`low-uniqueness`); the same 1 to 6 lines repeating with an exact period 20 or more times (`line-cycle`); in one unbroken line of 2,000 characters or more, a unit of 4 to 120 characters 13 or more times in a row (`phrase-cycle`) | `packages/agents/src/reasoning-loop-guard.ts` |
| `repeated_tool_call` | identical name and arguments in a row: reported from 2 (where the SDK warns the model), a warning from 5 (where it escalates) | `packages/core/src/runtime/safety/loop-detection.ts` |

These are the numbers of the SDK's release tags `v4.100.250` and
`v4.100.251` (equal in both, as the SDK's session confirmed on 2026-10-13).
The source of truth is the release tag of the SDK build under test, not a
development tree. A build with other numbers needs its own set and a new
library version; `internal/detect/guards.go` states which builds a version
holds. A session may also set a guard's numbers itself: what was in effect
for a run is its resolved configuration, which the run record holds; the
detector always reads with the release's defaults, so that runs compare.

- **`runaway` has no default size.** The SDK's own data says length is
  never the signal: healthy reasoning runs to hundreds of thousands of
  characters a turn. The detector reports only where a suite declares a
  size (the requirement it comes from says "per suite").
- The SDK's guards look while the stream arrives; here the whole block is
  at hand, and it is looked at after every line and, inside a line, after
  every character. What fires in the SDK fires here.
- With a window of 60 lines and 20 cycles asked for, the SDK's `line-cycle`
  can only be reached by a period of 1 or 2 lines; periods of 3 to 6 are in
  its definition and cannot fire. The SDK's session confirmed it; the
  detector says what the guard does, and whether the guard changes is the
  owner's decision there.
- `short_unit_repeat`, `char_run` and `sentence_repeat` stay as the
  requirements define them (their thresholds are that document's, with its
  reference pattern). Where they and the SDK's guards disagree, both are on
  record: on code quoted again and again, `sentence_repeat` fires and
  `reasoning_repetition`, which asks for a sentence, does not.
- **Decided by the owner (2026-10-13), D76: every match is the reference.**
  The older reference code of the requirements looks only at the first
  repeat in a block, so a block that opens with layout (indentation, a
  separator line) is never flagged, whatever follows. That is a blind spot
  and is not reproduced: `short_unit_repeat` and `char_run` pass over a
  repeat that fails the letters rule and keep looking. Loop rates published
  with the older code stay as they were published and are not recomputed.
  Every detector finding states which way made it (`matching`:
  `every_match`, or `first_match` for the older way), next to the library
  version, so a count can always be told from an older one.
  `automaticum detect <file> --first-match` reads a text the older way, to
  reproduce an older count; ingest never does.
- **Against the reference code** (criterion 8 of 110 §1): the reference's
  own fixtures are tests here (`internal/detect/reference_test.go`), and a
  second test runs the reference code itself and compares block by block.
  On 2026-10-13, over 907 blocks (the fixtures, this library's controls,
  and every block of one run of this harness and four looping runs of the
  old corpus): the older way of matching gives the reference's three flags
  on every block, with no difference; the reference way finds 3 firings
  more (2 on texts made for it, 1 on a real block) and loses none.
  On 2026-10-14 the reference code became version 2 (omnimergekit
  `01f5ed3`): it matches every repeat too, and says so in its output. The
  test now compares in the way the reference reports: over the same 907
  blocks, this library and the reference agree on every block, with no
  difference (11 short-unit, 4 letter-run and 19 sentence firings).
  The same day the reference's owners packaged two real cohorts (9,611
  blocks of model transcripts on benchmark tasks, kept outside this
  repository) with their code's expected output for both of its versions.
  `TestCohorts` (`AUTOMATICUM_COHORTS`) compares every block: no difference
  on any detector or any block, in either way of matching, with the
  runaway size of 10,000 characters given explicitly (6 short-unit, 1
  letter-run, 363 sentence and 529 runaway firings). Criterion 8 of 110 §1
  is met for the detectors the requirements define.
- Not ported yet from the same source: the count of failed identical calls
  (6, and 4 for a call the tool refused in advance) and the repeated pair
  of call and result (3, and 6 for writing tools). Both need each call's
  result.

Not built: near-duplicate rounds, edit oscillation, re-read loop.

## 4. Other findings

Each has a severity, evidence (event ids, quoted text) and a count.

- **Guards:** every guard that fired, when, what it cut or said, and what the
  model did next; guards that should have fired by their own rule and did not
  (the rule replayed over the transcript); a false-positive review queue.
- **Tool calls:** calls per run and per tool; parse path per call (native,
  repaired, from text, from reasoning); unreadable calls; calls truncated by a
  cap; unknown tool names; schema violations; arrays sent as strings; an
  `intent` used as scratch space; server parse errors (HTTP 5xx on a
  generation) counted apart from model errors; share of turns with no call and
  no final answer.
- **Output hygiene:** empty turns; visible text under five characters; control
  tokens or channel markers in visible text or in history; thinking that
  stopped after a leak; thinking that never closed; reasoning emitted as text
  or text as reasoning; markdown fences where the scorer executes raw code;
  answers present only inside the reasoning; a stop sequence that fired inside
  the reasoning.
- **Turn pathologies:** thinking-only turns, a report after work that ended the
  run early, a plan written to reasoning with an empty reply, premature "done",
  refusal to continue.
- **Parameter drift:** any request whose sampler, thinking, context or output
  budget differs from the arm; values the server reported differently from
  what was sent; a tag parameter that silently won or lost.
- **Server side:** finish reason per request, truncations, slot or session
  reuse, checkpoint restores, evictions, admission refusals, load and unload
  events, restarts, GPU residency changes, errors in the window of the run.
- **Integrity:** oracle tampering, workspace not pristine, files written
  outside the workspace, sandbox escapes or denials, the agent changing its
  environment to get past an obstacle (editing PATH, chmod, deleting a guard).
- **Struggle signals:** the triggers of the escalation study, evaluated on
  every run, with recall and false-alarm rate per trigger across a set.
- **Outcome mismatches:** claimed success against the oracle; fixed then broken
  again; correct at some point but not at the end (needs oracle sampling,
  060 §3.1).

### 4.1 Wire against arm, as built

The detector `wire_arm` runs at ingest on every run this harness made. It
reads the wire capture and compares each agent request (a chat request that
offers tools; the SDK's own probes are left out) with the run's resolved
arm. One finding per field and kind:

| kind | meaning |
|---|---|
| `stated_not_sent` | the arm states a value and the request does not carry it; the finding names the value in effect and where it was read (the model tag's parameters recorded before the run, or the server's own answer) |
| `sent_not_stated` | the request carries a sampler or thinking value the arm does not state, or one the arm leaves to the tag |
| `differs` | both have the field, with different values |
| `drift` | the field did not have the same value in every request of the run |

Each finding counts the requests that show it, out of how many, and names
the first. It never changes a verdict (D63). A drift is a warning for a
field the arm governs and a note otherwise; an output budget the arm leaves
to the SDK is a note. `think: true` with thinking left to the tag is not a
finding: it says "on, budget from the tag".

Two more findings are written at ingest from what the endpoint itself said:
`model_reloaded`, when the server reports a load of a second or more on any
chat request after the run's first (a model in memory reports milliseconds),
and `gpu_residency`, when the probe after the run shows the model only
partly on the GPU.

**Decided by the owner (2026-10-13), D73:** either makes the run `INVALID`,
with the reason "endpoint: ..." and the verdict it had kept beside it: its
times are not the arm's. An arm may state that it accepts one or both
(030 §5.3), when that condition is what it tests; its runs then keep their
verdict and the finding is a note. The rule is applied at ingest, so
`reanalyse` applies it to earlier runs, and takes back an `INVALID` it set
itself when the cause is no longer found. It never touches an operator's
`INVALID`.

**Decided by the owner (2026-10-14), D85:** a reload after a pause keeps the
verdict. A pause (050 §4, D83) drops the model on purpose, so the first chat
request to start after each release loads it again. That reload is listed
apart in the finding's evidence (`after_pause`, with the request and the
seconds), in the metric `endpoint.reloads_after_pause`, and with severity
`note`; it is not counted in `endpoint.reloads` and does not make the run
`INVALID`. A reload on any other request still does. The run keeps its
`paused` flag, so reports can still leave paused runs out. `model_reloaded`
is detector version 2 from here on.

Metrics written at ingest so far: time (wall, agent, first token, in tools,
in the model), turns, requests, tool calls and failures by tool, tokens as
the server counted them (prompt, cached, fresh, completion) with the SDK's
split of the completion, rates, completion length per request (n, sum, p10,
p50, p90, max), finish reasons, empty completions, thinking and text block
lengths, turns without thinking, context (first, last, peak, share of the
window, cached share, compactions), the SDK's notices by reason, and the
values the SDK derived from the arm. The rest of §2 is not written yet.

## 5. Generation-cap audit

- Cap hits are counted per run **at any turn**. In an agent loop the mid-loop
  cap is the failure.
- Per cell: capped runs, capped requests, and how many capped runs still
  passed.
- **Declared limit:** when the output budget is known, a completion is capped
  when its length ≥ cap − 16 tokens. For a derived answer allowance (output
  budget minus thinking budget) it is a band: |length − allowance| ≤ 16.
- **Pile-up test** for servers that do not report the finish reason honestly,
  all of:
  - tie band: completions within 16 tokens of the longest;
  - at least 2 ties, and at least 10 rows. Under 10 rows the answer is
    "unverified", never "capped";
  - plausible limit: longest ≥ 256 tokens, and (longest − 16) ≥ 2.0 × the
    median of the rows outside the tie band.
- Verdict per cell: `CLEAN`, `CAPPED`, `UNVERIFIED`. A `CAPPED` cell is not
  comparable to a cell at another budget.
- **Cap asymmetry between two arms**, over their shared task ids. Let hi be the
  larger cap-hit count and lo the smaller. The asymmetry is *material* when
  hi ≥ max(5, 1 % of shared tasks). When material, the comparison is confounded
  if lo = 0 or hi ÷ lo ≥ 1.5, and the report refuses to call the score
  difference clean: the bench has become a length meter. A bare 1.5 ratio
  would cry wolf on 1 against 0.
- The reasoning-budget regime is recorded (unlimited, budgeted, off): a budget
  relocates rumination rather than removing it.

**As built (milestone 4, first part, 2026-10-14):** at ingest every request
of the agent's loop is audited, every turn: it is capped when the server
says it ended by length, or when its completion reached the limit the
request declared (`num_predict`) less 16 tokens. Each run gets the metric
`cap.hits`, the limit it declared (`cap.declared_tokens`) and, when any
request is capped, the finding `cap_hit` (severity warning) with each
request, its tokens, its limit and how it is known. `automaticum compare`
gives each arm a verdict: `CAPPED` when any request is, with capped
requests, capped runs and how many of those still passed; `CLEAN` when none
is and the server gave a finish reason for every request; `UNVERIFIED` when
it did not, or when a run was analysed before the audit existed. It also
shows the declared limits and the thinking budgets the server applied (the
reasoning-budget regime). The 16-token tolerance is omnimergekit's.
**Not built yet:** the derived answer allowance band, the pile-up test (for
servers that give no honest finish reason; ingest does not yet keep every
completion's length per arm) and the asymmetry between two arms: their
definitions are being confirmed against omnimergekit's code (what a row
is, and whether the counts are of requests or of runs) before they are
ported.

## 6. Across runs

**Decided by the owner (2026-10-13), D78:** a report gives every statistic
and draws no conclusion. No significance level is set: with small models
the swing from run to run is very large, and the reader judges, with the
history of earlier runs beside it. What reports are for: between
quantizations of one model, and between versions of the SDK on the same tag
and lane, is the pass rate the same, does it think longer, compact more,
loop less, need more wall time.

**As built (milestone 4, first part):** `automaticum compare <batch> ...`
puts the arms of some batches side by side. Above the numbers it prints
what differs between the arms besides the runs. Then, per arm: runs
counted, runs left out with the reason (`SKIPPED` and `INVALID` say nothing
of the arm), verdicts, the pass count with its Wilson interval, tokens per
solved task, the runs with each finding, and for every metric the median,
the least and the most and the swing (standard deviation over mean).
Between two arms: Fisher's exact test of the pass counts, and for every
metric the share of pairs of runs in which one arm's value is the larger,
with the Mann-Whitney p (exact without ties, else the approximation,
marked). Intervals are at 95%, stated in the output; that level is the
usual convention and not a threshold for anything. A batch of several arms
is started with `automaticum run start --arm A --arm B -n N`, queued one
run of each arm in turn (`--order blocks` for all of one, then the next).

- **Pass rate per arm** with a Wilson interval, streaks, and "how many more
  runs to tell these two arms apart".
- **Paired comparison** when two arms ran the same task ids: McNemar on the
  discordant pairs (exact binomial), reported with the two discordant counts.
  Errored tasks are excluded from the pairs and listed.
- **Unpaired comparison** of any metric with the test stated (Fisher for rates,
  Mann-Whitney or bootstrap for times and counts) and a plain statement of
  whether the difference is distinguishable at this n.
- **Jitter band:** a repeat of the same arm on the same basis. A difference
  inside the band is reported as not distinguishable, whatever its sign. One
  repeat per cohort is the minimum to state a band.
- **A zero or a null result carries its power:** n, and the smallest difference
  this n could have shown.
- Above any difference it prints, a report states the basis diff (020 §5), the
  cap comparison (§5) and each arm's denominator.
- Medians and ranges by default, not means; distributions on request.
- **Group by** any arm field, task label or finding. Attribution across
  categories is done on balanced axes with per-category n shown.
- **Census** of tool failures across the corpus by tool, model and error class.
- **Regression view** across harness builds: what changed between two SDK
  versions, on the same arm, for every metric and finding.
- **Comparability:** refusal to pool across task, oracle, scoring or basis
  boundaries, with the excluded runs and the reason.
- **Lane skew:** the same arm on two hosts, to separate the model from the
  machine.
