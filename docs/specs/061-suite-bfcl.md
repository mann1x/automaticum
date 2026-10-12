# 061 — Suite: BFCL (Berkeley Function-Calling Leaderboard)

**Status:** draft 0.5, 2026-10-12.

## 1. What is kept comparable

omnimergekit runs BFCL today and those cells must stay comparable:

- `inspect_evals/bfcl`, pinned **by commit** (the project re-cuts version tags
  over a moving main).
- Category `multi_turn_base`, n = 200, plus `multi_turn_long_context` and
  `multi_turn_composite` for degeneration work. Single, multiple and parallel
  calls, irrelevance and the executable categories are supported as further
  subsets.
- The V3 scorer compares the final state of the backend objects and the
  execution results with ground truth. **No model judge.** This stays the
  scorer: it is why the cell is usable when an effect is a few points.
- Two arms are compared **paired** on sample ids (070 §6).

## 2. Scoring

BFCL's own evaluator, version-pinned, runs in its own container. Gold answers
are validated against it before the first cell (050 §3.2).

## 3. Two cells

**Decided by the owner (2026-10-12):** BFCL is run both ways, as two
separately named cells that are never pooled.

| cell | loop | what it measures | comparable to |
|---|---|---|---|
| **proxy cell** | the harness's | model + chat template + Cerebriline provider and tool-call parser | omnimergekit's existing BFCL cells, if the parser is the only change |
| **agent cell** | Cerebriline's | the whole engine on stateful tool use | only itself, across arms |

The proxy cell uses the OpenAI-compatible proxy adapter (064 §3.4): the
Cerebriline provider layer and parser exposed as an endpoint, with no agent
loop of ours. The agent cell uses the Python shim adapter (064 §3.3).

## 4. Recorded per entry

- the raw model output;
- how each call was obtained: native tool call, repaired, read out of text,
  read out of reasoning;
- the number of turns; cap hits at any turn; reasoning length per turn;
- the scorer's result and its evidence.

## 5. Known property, carried into reports

BFCL multi-turn reasoning is short (median 252 to 480 characters measured), so
it is a tool-use cell and a poor degeneration cell. A null loop rate on BFCL
says nothing about long-context looping, and the report header says so.
