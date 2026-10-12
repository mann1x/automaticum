# 080 — The store

**Status:** draft 0.5, 2026-10-12. The shape is the owner's preference; the
spike in §7 confirms it or sends us to a fallback.

## 1. What the store has to do

- Hold everything: streams and logs, resolved configurations, lane and queue
  state, metrics, findings, statistics and reports.
- Scale to thousands of runs, compressed. A run wrote about 25 MB of log per
  1000 s in the old harness; one old stream is 7.5 GB. zstd got about 30 times
  on the archived session state.
- Vector search over text chunks (likely millions), full-text and regex search,
  and analytical queries (group by any arm field, percentiles, joins).
- Be simple to run: no separate database server to administer.
- Work from Go, on Linux and Windows, with only the Go application on the host.
- One writer (the daemon), many readers, and survive a kill during a write.

## 2. Shape

**Owner's preference (2026-10-12):** three parts, each doing what it is good
at.

| part | holds | role |
|---|---|---|
| **Blob store on disk** | event streams, wire captures, logs, snapshots, session data | the source of truth for raw material |
| **DuckDB** | runs, arms, tasks, cells, cohorts, lane and queue state, metrics, findings, statistics, reports | state and analytics |
| **Lance** (through `lancedb-go`) | text chunks, their vectors, the full-text index | search |

**Decided by the owner (2026-10-12):** raw event streams, wire captures and
logs live as **content-addressed blobs on disk**, referenced by hash from
DuckDB. They are not database columns.

## 3. Consistency

- The **run id** is the join key across all three.
- Order of writes: blobs first, then the DuckDB rows that reference them, then
  Lance chunks.
- **Lance is derived data.** Its chunks can always be rebuilt from the blobs,
  so a failure between writes loses nothing.
- **DuckDB's metrics and findings are derived data** too: `reanalyse` rebuilds
  them from blobs. What is *not* derivable (verdicts and their evidence,
  resolved arms, probe results, queue state, operator actions) is written
  transactionally and backed up.
- A consistency check command verifies that every referenced blob exists and
  hashes correctly, and that every run has its chunks.

## 4. Blobs

- Content-addressed by sha256, compressed with zstd.
- **Deduplicated:** system prompts and tool schemas repeat thousands of times.
  Wire captures are stored so that repeated parts are shared. **Open:** the
  chunking scheme (whole request bodies deduplicated by part, or content-defined
  chunking).
- Size is handled here, never by dropping fields at source (020 §2).
- A blob is written to a temporary name and renamed, so a kill during a write
  leaves no half file under a valid name.
- Streams are readable by range, so a transcript viewer never loads a whole
  multi-gigabyte stream.

## 5. Retention

- **Results are never deleted.** Evaluation results, raw transcripts and
  adapters are permanent.
- Nothing is pruned by default. Retention is an explicit policy with an archive
  tier, and a deletion happens only behind a **verified archive**.
- Per run, enough is kept to re-run the scorer without the model (020 §2).
- **Open:** the archive tier's location and the policy for moving cold blobs to
  it.

## 6. The store interface

The daemon reaches the store through one small Go interface, so the choice of
engines can be revisited. The spike's test set becomes that interface's test
suite.

Also required: schema migrations, backup, and import of the old corpus (100).

Never write large data to a RAM-backed directory. Never put the store on a
volume that has stalled under log writes; the data root is a configured path
with a free-space floor.

## 7. The spike

Run before the first milestone that needs a store (110 §3). It confirms the
shape and finds its limits; it no longer chooses among three.

**Fallbacks** if one engine fails its part:
1. Lance for everything. Risk: the Go SDK is young, and small frequent updates
   make a new table version each.
2. DuckDB for everything, with vectors searched exactly or the index rebuilt
   from the table at start. Risk: full-text needs scheduled rebuilds; vector
   cost at millions of chunks.
3. ClickHouse embedded (chDB) only if log volume defeats the others.

**Known limits going in** (as read on 2026-10-12, to be re-checked):
`lancedb-go` is a CGO binding at an early version; `duckdb-go` is CGO and
actively maintained; DuckDB's vector index persistence is experimental and its
full-text index does not update automatically. Both engines need CGO, so **the
first measurement is whether the two link into one static binary** on Linux
(amd64, arm64) and Windows (amd64).

**Data:** the old corpus (100) imported, then scaled synthetically to 5,000
runs.

**Measurements:**

- the build: one static binary with both engines, and what cross-compiling
  takes;
- ingest rate, and size on disk against the raw and the zstd figures;
- a batch statistics query (pass rate and percentiles grouped by three arm
  fields) and a tool-failure census over all events;
- regex and full-text search across all transcripts, cold and warm, including
  regex over blobs that are not in Lance;
- vector search over 100 k, 1 M and 5 M chunks at 768 and 1024 dimensions:
  latency, recall against exact search, memory, index build time;
- 10 updates per second to a lane-state table for an hour while the queries
  above run: latency, file growth, what compaction costs;
- `kill -9` during a write, twenty times: does it reopen, and is anything lost;
- range reads from a multi-gigabyte blob;
- memory at rest and at peak.

**Pass criteria** are set before the spike runs. **Open:** the thresholds.
