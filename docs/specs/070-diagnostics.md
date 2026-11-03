# 070 — Diagnostics: metrics, detectors, statistics

**Status:** draft 0.29, 2026-11-03.

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

**As built (2026-11-02), the timeline:** `automaticum run timeline <run>`
reads the run's event stream: one line per turn with its time, how long
after the run's first event it came, its tool calls and how many failed
(by tool and error class), and between the turns every guard trip (with
its cause; the SDK's notice of the same trip is folded into it), every
other notice of the SDK but the context report each turn carries, session
forks, mode switches and the agent's end (`--json` gives the moments).

**Decided by the owner (2026-11-02), D130:** the baseline is the
baseline arm of the campaign or cohort the run's batch belongs to where
there is one, else the arm the run's arm extends; the fields in which the
run's arm differs from it are highlighted.

**As built (2026-11-02):** a campaign file may name `baseline = "<arm>"`,
one of its batches' arms (a campaign's cohort shares it). `run show`
(`GET /v1/runs/{id}`, `baseline`) names the run's baseline and where it
comes from (`campaign <name>` or `parent`, the arm its file extends) and
lists every field in which the run's arm, as the run recorded it, differs
from the baseline as its file is now, each line marked `*`; a run of the
baseline arm says so.

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
- **Scripted user** (040 §6.2): `user.actions`, `user.denials`,
  `user.failed`, and since analysis version 4 `user.forks` and
  `user.mode_switches`. **Decided by the owner (2026-10-16), D88:** hidden
  where zero: `compare` leaves out one that is zero in every run of every
  group, `run show` a zero one. The same rule hides
  `mode_switches.by_agent` (the agent's `switch_to_act_mode`, 040 §6.2)
  where zero.
- **Guard trips** (analysis version 5, D99): `guard_trips` and
  `guard_trips.<guard>.<phase>` count the trips the runner noted, and
  `guard_stop.<guard>.<phase>` is 1 on a run its arm's `guards.stop_on`
  stopped (D100). A nudge's phase is which nudge it was. Hidden where zero, by the same rule.
  From analysis version 6 a trip whose guard says what was repeating is
  also counted by that cause, beside the guard-and-phase total:
  `guard_trips.<guard>.<phase>.<cause>` and, on a stopped run,
  `guard_stop.<guard>.<phase>.<cause>`. In SDK 4.100.259 that is the
  reasoning-loop guard: `reasoning_loop` (the reasoning repeated),
  `answer_loop` (the reply) and `argument_loop` (a tool call's arguments).

  From analysis version 8 (2026-11-01) the partial score of the primary
  oracle is `oracle.tests_passed`, `oracle.tests_total` and
  `oracle.partial_share`, where its output says them (060 §2), and the
  time per turn is `time.turn_median_ms` and `time.turn_max_ms` (from one
  turn's start to the next, the last to the stream's end), with the
  longest silence in the stream `time.gap_max_ms` (a paused run's hold is
  inside its turns and gaps).

  From analysis version 9 (2026-11-01): a failed tool call's class is
  the head of the tool's own error sentence ("No replacement performed",
  "Read before editing": its `<name> operation failed:` prefix off, cut at
  the first ": ", ". " or line end, digits as N, at most 60 characters;
  `tool_error` when it says nothing), counted as
  `tool_calls.failed.by_class.<class>` and kept on each call for `report
  census`. Per request: `rate.generation_tok_s.per_request` and
  `rate.prompt_tok_s.per_request` (distributions), `thinking.tokens.per_block`
  and `tokens.completion.text.per_request` (the SDK's split),
  `thinking.budget_hits` (a request whose reasoning reached the budget
  served), `context.reasoning_carried.requests` and
  `context.reasoning_carried.chars` (earlier reasoning sent back in the
  history), and `context.rereads` with `context.rereads.<cause>`
  (`compaction`, `reload`, `unknown`: a request after the first with
  nothing served from cache, on a server that reports its cache).
  Compactions: `context.compaction.tokens_before`, `.tokens_after`,
  `.duration_ms` and `context.compactions.by_cause.<cause>` from the SDK's
  completed notice. Escalation: `escalation.ended`, `.failed`, `.changed`,
  `escalation.expert_tokens.input|output`; offers are
  `notices.status.struggle.offer`, calls `notices.status.escalation_started`.
  Edits (D127): `edits`, `edits.failed`, `edits.files`, `edits.restores`,
  `edits.without_check`. Turns: `turns.thinking_only`,
  `turns.no_call_no_answer` (and its share), `turns.short_text` (visible
  text under five characters and no call), `turns.ended_thinking_only`
  (D128). Not built: churn (lines written against lines surviving), edits
  to the same region, sub-agents (the arms run none).

  From analysis version 10 (2026-11-02): the change protocol, from the
  SDK's `atomic_transaction` notice (a transaction settled), its
  `atomic_empty_attempt` and its `atomic_plan`: `transactions.settled`,
  `.kept`, `.discarded` (put back: neither kept nor carried on),
  `.carried`, `.checked` (its check gave output), `.files_put_back`,
  `.empty_attempts`, `.plans` and `transactions.elapsed_ms`
  (distribution). The struggle signals of §4 (D126): `struggle.<signal>`
  and `struggle.<signal>.turn` where one held, `struggle.signals` (how
  many held) on every run, and `struggle.replay.<nudge|suggest>.<reason>`.

  From analysis version 11 (2026-11-02): cost in money for a priced
  endpoint (030 §4.2): `cost.money`, `cost.money.prompt` and
  `cost.money.completion`, the run's fresh prompt, cached prompt and
  completion tokens at the price the run recorded, in its currency. And
  the integrity metrics of §4: `integrity.outside_workspace` with its
  `.added`, `.changed` and `.deleted`.

  From analysis version 12 (2026-11-02): `tool_calls.swallowed_argument`
  and `tool_calls.swallowed_argument.by_tool.<tool>` (D131, §4), also a
  column of `compare`.

  **Decided by the owner (2026-10-27, D116):** from analysis version 7 a
  notice of the SDK is counted by what kind it is, never by its sentence:
  `notices.<type>.<kind>.<phase>` from the kind and phase it states
  (`notices.status.reasoning_loop.cut`, `notices.status.loop_detection.soft`),
  else `notices.<reason>`, else `notices.<type>`. Before, a notice without a
  reason was counted under its type and its whole sentence, which carries
  counts and quoted text, so every run had metrics of its own and none
  pooled. The sentences are read with `automaticum notices` (090 §1), by
  the key a report prints. `context.compactions` counts a compaction once,
  when the SDK says it completed (`auto_compaction`, `manual_compaction`,
  `overflow_recovery_compaction` at phase `completed`, as the SDK counts
  them), and a notice of an older SDK without a kind by its reason, as
  before.

### 2.1 Per cell

- Every distribution above over the cell's runs.
- **Conditioned on the verdict:** token p50 and p90 over `PASS` runs only, and
  **tokens per solved task** = total completion tokens of the cell ÷ number of
  `PASS`. A median over all runs rewards a model that gives up early.
- Throughput over the whole cell. A rate from the easy first tasks is not the
  rate of the suite.
- **Sanity warnings:** n differs from expected; empty completions above 5 %;
  p10 of visible characters under the suite's declared floor. The floor
  is declared in the suite's definition (`suites/*.toml`, 030 §1), which
  comes with milestone 5; until then no floor is checked.

**As built (2026-11-01):** every summary of a cell or a comparison has p10
and p90 beside the quartiles. A cell's result file has `token_stats_pass`
(the token distributions over its `PASS` runs only), `tokens_per_solved`,
`throughput_tok_s` (its completion tokens over its time in the model) and
`empty_completion_share` in `scores`, with the warning above 5 %, and
`loop_by_verdict`: per verdict, the runs, those that looped, those that hit
their output limit and those that did both (§3.1), and (2026-11-02)
`loop_components`: the runs `loop_rate` counts, by detector
(`reasoning_loop`, `reasoning_repetition`), by channel (reasoning, text)
and the turn of each one's first loop (distribution). n always equals
the runs the cell queued, so it cannot differ. The floor of visible
characters comes with the suite definitions (milestone 5).

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
- A suite subset may declare `loop_rate` as its headline metric. The
  declaration is the suite definition's (`suites/*.toml`), milestone 5;
  until then a cell's headline is its pass rate.
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
taken as one block, `automaticum detect <run>` every detector on stored
runs (§3.3), and `automaticum detect --self-test` on the library's own
controls.

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
  size (the requirement it comes from says "per suite"). The size is
  declared in the suite's definition (`suites/*.toml`), milestone 5; until
  then it fires only with `automaticum detect --runaway N`.
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
- **Library version 4 (2026-11-02).** The swallowed-argument detector
  (D131, §4) and its two controls.
- **Library version 3 (2026-11-01).** The SDK's call guard is replayed
  after the run (`internal/detect/callguard.go`, from class `Bu` of
  @cerebriline/core 4.100.269): before each call it looks at what the same
  call (name and arguments, keys sorted) gave before. `failed_identical_call`:
  a call refused in advance ("No change: ", "Duplicated instead of
  replaced: ") and sent again, warned on its 1st to 3rd repeat and
  escalated on the 4th; a call that failed before, warned on and escalated
  once it has failed 6 times. `repeated_call_result`: the same result
  (revision numbers `#N` blanked) 2 times in a row, warned on at the next
  call, escalated from 5 for a writing tool (`editor`, `apply_patch`,
  `sed`, `awk`, `write_to_file`, `new_file`, `restore_file`). A success
  clears the call's failures and, for a writing tool, those of every call
  naming one of its files; a parallel sibling (the same call twice in one
  turn) is neither looked at nor counted, here and in
  `repeated_tool_call`; `run_commands` and the agents' and teams' status
  tools are never looked at; a fork of the session starts every count
  again. A call's outcome is read as the guard reads it: failed when its
  result is an error, every item says `success: false`, or one says
  `regressed`. **Decided by the owner (2026-11-01), D129:**
  `edit_oscillation` is the guard's firings on writing tools and
  `reread_loop` its same-result firings on every other tool, each a
  finding of its own; near-duplicate rounds is not built until defined.
- **The reasoning-loop guard of 4.100.258 on (library version 3).** From
  release 4.100.258 to 4.100.269 the guard is one code
  (`internal/detect/guards258.go`): line cycles over the last 126 lines;
  a block of 7 to 400 lines (200 characters or more) repeating 3 times
  (`block-cycle`); and each signal passed over when the repetition is
  already as often in what the model was given (the request's messages
  other than its own). A run is read with the rules of its SDK build
  (`detect.RulesFor`: 4.100.258 on, else the 4.100.250 rules above);
  the finding says which (`rules`). What the model was given is rebuilt
  from the run: the task's prompt and every tool result before the block,
  their strings as the guard collects them; text the SDK adds itself
  (nudges, compaction summaries) is not in it. `automaticum detect <file>
  --sdk <release>` reads a file with a build's rules. The repetition guard
  is the same in every build.

- `detect.Spans(text, hit)` (2026-10-25, for the export of 110 §3.5) says
  where a hit's repeats stand in its block, the first occurrence left out:
  a short unit or a letter run from its second unit to the end of the run,
  every later occurrence of a repeated sentence or paragraph, a reasoning
  loop's cycle from its second repeat (`CycleStart`, lines read as the
  SDK's guard reads them) or, for low uniqueness, from the first line of
  the window that repeats an earlier one. A runaway has no span. No
  detector's answer changes, so `Version` stays.

Not built: near-duplicate rounds (D129).

- **Findings lost before analysis version 14 (fixed 2026-11-03):** on a
  run with an arm on record (every run of this harness), the findings of
  the wire against the arm replaced every finding written before them,
  where they should have been added. So `cap_hit`, `guard_trip` and
  `control_markers` were never stored, though their metrics were
  (`cap.hits`, `guard_trips`, `hygiene.*`). A client's run whose request
  ended by length at 96,000 tokens had `cap.hits` 1 and no `cap_hit`
  finding. From analysis version 14 they are kept. A stored run shows them
  after `automaticum reanalyse` or `detect --record`, and `detect` shows
  the difference as "(stored 0)".

- **Blocks cut off before their end, lost before analysis version 16
  (fixed 2026-11-03):** a block is what the model wrote between its
  first delta and its `content_end`. When the output limit cut a turn,
  or the wall limit stopped the run, no `content_end` came, and the next
  block's end threw the deltas away: the detectors never read the text
  that ran into the cap. A client's run wrote a visible-text loop of
  360,271 characters (5,781 lines, 114 distinct) to the 96,000-token
  limit and had no loop finding. From analysis version 16 such a block
  is kept as unfinished, in the turn it started, and read like any
  other; `reasoning_repetition` fires on that loop (one paragraph 837
  times). The metric `blocks.unfinished` counts them; an unfinished
  block is left out of `hygiene.unclosed_thinking`, as it may stop
  inside its thinking.

- **Answers only the wire has (analysis version 17, 2026-11-03):** the
  SDK's retry after the output limit (`max_tokens_turn_recovery`) is
  never streamed as events: the same client's retry wrote 94,187
  characters of reasoning and 106,517 of text in ten minutes of an event
  stream with no line. When the requests were captured, an agent
  request's answer that no block of the same kind was printed during is
  read from the wire as a block of the turn that had started when it was
  asked; by time, not by text, as a retry may begin as the turn it
  retries did. A stream without times adds none. On that run
  `reasoning_repetition` then fires on the retry's reasoning (one
  paragraph 212 times) as well as on both text blocks. The metric
  `blocks.from_wire` counts them, on a run with a capture.

### 3.3 Detectors over runs, on demand (D133, as built 2026-11-03)

**Decided by the owner (2026-11-03), D133:** "we should not need a script
for this kind of checks, there should be a command in automaticum to do it
at any time, either on a single run or scanning multiple results, cohorts,
lanes, etc.", and "make detect broad and covering all possible use cases".
A client had been scanning its gates with its own script.

`automaticum detect` runs every detector of this build, or the ones named,
again on the stored material of any set of runs, with the same analysis
ingest applies (`ingest.Load` and `ingest.Derive`). By default it writes
nothing.

- **Which runs:** the union of the runs named (bare or `--run`), batches
  (`--batch`, a glob), cohorts (`--cohort`) and campaigns (`--campaign`).
  Then every narrowing given: `--lane`, `--arm` (name or id), `--task`
  (`suite/task` or the task), `--suite`, `--model`, `--verdict` (globs,
  any number of each), and `--since`/`--until`. With nothing named, every
  run the narrowing leaves; every run at all takes `--all`. Imported runs
  only with `--imported`; skipped runs never.
- **Runs in flight** are included (not with `--finished`, `--model` or
  `--verdict`). Each is read from the lines it has printed so far, with its
  arm as the configuration says it now, and marked partial: its wire
  capture is written only at its end, so argument names come from its own
  calls. If the daemon no longer holds its first lines, the mark says so.
- **What each run gives:**
  - its state, verdict, blocks and calls;
  - each detector's firings now (a finding counts its `Count`, at least
    one), and, for a finished run, what its stored findings say, so a
    changed rule shows what it changes;
  - the metrics counted per tool (`….by_tool.<tool>`) and any metric asked
    with `--metric` (a name, a prefix or a glob);
  - its calls by tool: failed, by their 070 §2 error class; refused; with
    arguments that are not a JSON object; of a tool no request offered
    (said only when the requests were captured); and with an argument name
    holding a control token or a piece of one (`<|`, `|>`, the markers of
    `control_markers`).
- **`--firings`** adds each finding with its evidence, and every call with
  a swallowed argument (the finding quotes five), with the text around it.
  **`--calls`** prints the calls by tool for each run.
- **Totals:** by batch, arm and cohort by default, or by each `--group`:
  `batch`, `arm`, `arm_id`, `cohort`, `campaign`, `lane`, `task`, `model`,
  `state`, `verdict`, or one of compare's axes (`arm.<field>`,
  `label.<label>`, `finding.<detector>`). Each total gives the runs, calls,
  failed calls, and per detector the firings and the runs it fired in;
  the overall total also gives the failed calls by error class, and each
  metric asked, summed and averaged.
- **`--fail-if "EXPR"`** (any number) exits 1 when one holds. `EXPR` is
  `NAME OP NUMBER`, with `OP` one of `>`, `>=`, `<`, `<=`, `==`, `!=`.
  `NAME` is a detector (its firings), `runs_with.<detector>`,
  `share.<detector>` (runs with it over runs), `metric.<name>` (summed),
  or `runs`, `partial`, `calls`, `failed`, `refused`, `unreadable`,
  `control_token_names`, `not_offered`. `--fail-scope` says what is tested:
  `total` (the default), `run` (each run), or a totals axis (each group).
- **`--record`** stores the new analysis on the finished runs, as
  `reanalyse` does (it needs the daemon's token); runs in flight are never
  recorded.
- **Output:** one line per run as it is read, then the totals; `--quiet`
  prints the totals alone; `--json` prints the header, the runs, the
  totals, the groups and the tests that held, as one document. The header
  names the detector library version and the analysis version the runs
  were read with.
- **API:** `GET /v1/detect` reads and `POST /v1/detect` records, with the
  same fields in the query. The answer is a stream of JSON lines: a header,
  one line per run in the order of their ids (runs in flight last), and an
  end line. `detect <file>` and `detect --self-test` are unchanged.
  `GET /v1/runs/{id}/detect` stays for older command lines.

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

  **As built (2026-11-02, analysis version 11):** the agent's container
  is asked for its filesystem changes when it stops (the engine's
  `changes`: its own filesystem, never its mounts; the mount points and
  the directories above them are left out), and the run records them
  (`container.changes`: how many, the first 500). `outside_workspace`
  (info, version 1) lists what was added, changed or deleted there by
  directory (three levels), a directory changed only because something
  under it changed not counted. It judges nothing yet: which paths the
  runner and the SDK write on every run is to be read from the runs
  before any of it is called a fault. Oracle tampering is a verdict (060
  §3) and a workspace not pristine a gate (050 §3.1). Not built: sandbox
  escapes or denials, and changes inside the workspace that get past an
  obstacle (a mode changed, a check deleted), which need the task to say
  what its obstacles are.
- **Struggle signals:** the triggers of the escalation study, evaluated on
  every run, with recall and false-alarm rate per trigger across a set.
  **Decided by the owner (2026-11-01), D126:** the triggers are listed
  there.

  **As built (2026-11-02, analysis version 10):** `struggle_signals`
  (note, version 1) lists the signals that held on a run, each with the
  turn it first held: `sdk_nudge` and `sdk_offer` (the SDK's struggle
  notices), `compaction_emergency`, `loop_hard` (`loop_detection` `hard`
  or `stopped`), `reasoning_aborted`, `second_compaction`, and
  `replay_nudge` and `replay_suggest`: the SDK's struggle detector
  (`detect.Struggle`, the class of @cerebriline/core 4.100.250 to
  4.100.269 with its own numbers: a window of 10 turns, 6 failed calls
  with 2 distress phrases or hedging at the opening rate from turn 20, at
  most 2 suggestions a task and one a transaction; a nudge one failure
  short, on every 3rd failed edit in a row and on every 2nd attempt
  thrown away) replayed over the event stream. The SDK runs its detector
  only when the agent can escalate or spawn agents; the replay runs on
  every run. The stream does not say when a transaction opens, so the
  first notice naming it stands for that. `automaticum report struggle
  [<batch>...] [--where ...]` (`GET /v1/report/struggle`) sets each signal
  against "did not pass" over the runs it selects: the runs it held on
  that did not pass and that passed, recall, false-alarm rate, precision,
  and the turn it first held (median, p10, p90) on the runs that did not
  pass; the row `any` is any signal. PASS against FAIL and TIMEOUT;
  ABORTED, INVALID and SKIPPED runs are left out, and runs analysed before
  version 10 are counted apart until they are reanalysed.

  **The review queue (as built 2026-11-02):** `automaticum report guards
  [<batch>...] [--kind unconfirmed|missed] [--where ...]` (`GET
  /v1/report/guards`) gathers the `guard_replay` findings of the runs it
  selects into one list, a row per turn: the trips the rule does not
  confirm (possible false alarms) first, then the turns where the rule
  holds and the guard did not trip.

**As built (2026-11-01, analysis version 9):** `guard_trip` (note,
version 1) lists every guard's trip with its turn, guard, phase, cause and
what the model did next (`tool` with its name, `text`, `reasoning`,
`end`), also counted as `guard_trips.next.<act>`. `guard_replay` (version
1; warning when a trip is to review, else note) sets each guard against
its rule replayed: `loop_detection` against the call guard and identical
calls in a row, `reasoning_loop` `cut`/`aborted` against `reasoning_loop`
(also read on the stream the guard cut off, which the SDK threw away),
`reasoning_loop` `removed` against `reasoning_repetition`; per guard the
turns where the rule holds and the guard did not trip (`missed`) and trips
the replay does not confirm (`unconfirmed`, the review queue), also as
`guard_replay.<guard>[.<phase>].missed|unconfirmed`. On a real run of SDK
4.100.266 the replay matched every trip once read with that build's
rules; read with the 4.100.250 rules, the block cycle the guard cut on
was unconfirmed, which is how the change of rules was found.
`control_markers` (warning, version 1): visible text holding a control
token or channel marker (`<|name|>`, `<|name>`, `<name|>`, `<think>`) or
opening a thinking tag it never closes (`hygiene.control_markers`,
`hygiene.unclosed_thinking`). Not built: the rest of output hygiene and
tool-call parse paths, which the SDK records on its messages and not in
the event stream.
- **Swallowed arguments (D131, as built 2026-11-02, library version 4,
  analysis version 12; widened 2026-11-03, library version 5, analysis
  version 13):** `swallowed_argument` (warning) is a tool call
  one of whose string arguments holds another argument of the same tool,
  missing from the call: its name at the start of the string or after
  whitespace (a newline included), a quote, a backtick, a comma or a
  closing brace, and before a colon (a closing quote between or not). The
  start and whitespace came from a client's run on which version 4 found
  none of 18 such calls: an intent ending `*/\nnew_text:`, new_text
  missing. That run's calls are the test `TestSwallowedNewline`, which
  must find all 18. An optional argument named in code with a colon after
  it (`{ start_line: 3 }`) in a call that left it out also fires. The
  names are the tools' schemas the run's requests offered (`ReadWire`
  keeps them), and the arguments the run's calls of the tool did pass. The
  finding gives the calls per tool, the arguments swallowed and up to five
  calls with the text around the name. A client's own count on its gates
  (a fixed list of editor argument names, found whether or not the call
  passed them) and this rule agreed on the runs read (2 and 19 calls).
- **Duplicate definitions (D134, as built 2026-11-03, library version 6,
  analysis version 15):** `duplicate_definitions` (warning) is a tool call
  one of whose string arguments defines names it already defined, more
  than 3 times. A definition is a line holding `function <name>`, or
  starting with `def`, `fn` or `func` and a name, or starting with a name,
  its parameters and an opening brace (a method), keywords left out. A run
  whose requests were captured is read from the wire's calls, as the
  server streamed them. A call the output limit cut has only `{}` in the
  event stream; the wire has it as far as it went, and the finding marks
  it cut. Other runs are read from the event stream's calls. The finding
  gives the calls per tool, and up to five arguments with their
  definitions, names, repeated definitions and most-defined names. The
  metric is `tool_calls.duplicate_definitions` and its `.by_tool.<tool>`.
  - **The 3 is measured, not chosen.** On 2026-11-03 a scan covered the 637
    string arguments of 20 lines or more in the 371 runs then on the
    client's lane host. Every base-model run (413 strings, 15 tasks) and
    the harness's stub wrote at most 3. The reference game file itself has
    3: methods of one name in several classes. The trained models that
    broke wrote 6, 18, 33 and 492. The positive control is the call that
    started it (`TestDuplicateDefinitionsCanary`): a trained model's editor
    call of 215,471 characters (218,643 as the escaped JSON of the arguments), cut at 96,000 tokens, rewriting a file of
    about 130 lines, with 1,541 definitions of 1,049 names.
  - **Not covered:** C, C++, C# and Java functions, whose lines start with
    a type, and the argument's size against the file it edits.
- **Calls on the wire (analysis version 15):** `ReadWire` keeps the tool
  calls of each answer (`Request.Calls`): OpenAI's streamed pieces joined
  by index, and Ollama's whole calls.
- **Oracles and the agent's own end (as built 2026-11-01, analysis version
  8):** `secondary_oracle` (warning, version 1), one per secondary oracle
  that did not pass or could not run (060 §2), and
  `claimed_success_oracle_no` (note, version 1), the agent claiming success
  on a `FAIL` or `TIMEOUT` (020 §3). The partial score is the metrics
  `oracle.tests_passed`, `oracle.tests_total` and `oracle.partial_share`.
- **Session forks (D86, as built 2026-10-14, analysis version 4):** the
  finding `session_fork` (severity note, version 1) lists each fork a
  restore of messages started: its time, the session it left and the one it
  started, the checkpoint, whether the files were restored too, and what
  the SDK started again from nothing (iterations, consecutive mistakes,
  loop guards, compaction state). Every counter and guard metric of such a
  run spans more than one session; a report filters on the finding. It
  judges nothing.
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

Version 2 (2026-10-14, analysis version 2): when every agent request is
OpenAI-compatible, the arm's context window is not expected on the wire.
That protocol has no such field; the server holds the context, and a mode
C cell checks it against `/props` (065 §3.1). Version 1 reported it as
`stated_not_sent` on every such request.

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
window, cached share, compactions), the SDK's notices by kind (D116), and the
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
**Whose rules, as checked against omnimergekit's code (2026-10-14,
`eval/omk_eval.py` `cap_report`, `eval/test_cap_report.py`,
`eval/agentic/summarize_arms.py`, commit `01f5ed3`):** for agent cells
their code trusts the reported stop reason only and gives `CAPPED` or
`CLEAN`. Two parts above are automaticum's own and are labelled so: the
declared-limit check on the agent's requests (the length there is the
server's `eval_count`, thinking included), and `UNVERIFIED` (no finish
reason, or a run analysed before the audit). The **cap asymmetry** between
two arms is theirs as written: the capped requests of each arm summed over
the shared tasks, material when the larger ≥ max(5, 1% of the shared
*tasks*), confounded when material and the smaller is 0 or the ratio
≥ 1.5. `compare` shows it per pair. The **derived answer allowance** and
the **pile-up test** belong to their single-turn audit, where a row is one
sample per task (deduplicated by task id), the length is the raw answer
text re-tokenized with the model's tokenizer, the allowance applies only
when 0 < thinking budget < output limit and the thinking is not inline in
the text, and a pile-up on fewer than 10 rows is only logged. They are
ported with the single-turn suites (milestone 5), with `test_cap_report.py`
as the fixture set.

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
  boundaries, with the excluded runs and the reason. Built 2026-11-02 for
  the task version (020 §4): `compare`, `report ab` and `basis show` count
  only the runs at the latest task version in the view, leave the others
  out by name ("task version N, not the latest here (M)"), and say which
  tasks were mixed; `--pool-versions` (`?pool_versions=1`) counts them all
  and says so. A cell keeps every run and warns instead. Groups never pool
  across tasks; a basis difference is shown, not refused.
- **Lane skew:** the same arm on two hosts, to separate the model from the
  machine.

**As built (2026-11-01), statistics:** each group has its longest streaks
(passes, and runs counted that did not pass) in the order the runs
started. Each pair on one task has McNemar's exact test with its two
discordant counts, the i-th counted run of one arm paired with the i-th of
the other in the order they ran, and the runs left without a partner.
**Decided by the owner (2026-11-01, O39):** "how many more runs to tell
these two arms apart", the power of a null result and the jitter band's
"not distinguishable" are not adopted: each needs a significance level,
and D78 sets none and draws no conclusion.

**As built (2026-11-01):** `automaticum report regression <batch>...` is
`compare` with the runs grouped by arm name, task and harness build (SDK
release and package, runner protocol, lane image, automaticum version, 020
§5) instead of arm id, so an arm whose id changed with its SDK release
stays one arm. Only the groups of one arm are paired, each pair with every
metric's rank test and the basis diff. Every group names its build.
`automaticum report census [<batch>...] [--where ...]` counts the tool calls
of the runs it selects by tool and model: calls, runs, failures by error
class, and calls by parse path, most failures first, and the runs that made
no call. Error classes come from analysis version 9 (§2); ingest records no
parse path yet (`not recorded`): the SDK records it on its messages, not in
the event stream. `report struggle` and `report guards` are in §4. `report
run|cell|batch|ab` are `run show`, `cell show` and `compare`, whose paired
test is McNemar's on the task ids the two groups share. The unpaired
test of a metric is Mann-Whitney's (the "or bootstrap" of the list above
is not needed beside it).

**As built (2026-11-02), group-by and lane skew:** `automaticum compare
<batch>... --group <axis>` (`GET /v1/compare?split=<axis>`) groups the
runs on each task by an arm field as the run recorded its arm
(`arm.thinking.level`, a dotted path; `not stated` where the arm has
none), a task label as the task's files say now (`label.difficulty`;
`not labelled`), or a finding (`finding.reasoning_loop`: `with` or
`without`). Each group shows its n and the arm ids it holds, so the reader
sees whether an axis is balanced; groups on one task are paired as usual,
with no arm diff, since a group may hold several arms. `automaticum report
skew <batch>...` (`split=lane`) is lane skew: one group per arm, task and
the host its lanes ran on (the run's `lane_host`), and only the groups of
one arm id are paired, so what differs is the machine.
