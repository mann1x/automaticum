# 080 — The store

**Status:** draft 0.6, 2026-10-12. The shape is the owner's preference; the
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
- **Decided by the owner (2026-10-12):** archiving is **automatic by age**.
  Blobs older than a configured age move to a configured archive path. Each is
  copied, its hash verified at the destination, and only then freed at the
  source. Both the age and the path are daemon settings (030 §1).
- An archived blob stays addressable by its hash: reads fall through to the
  archive tier, and reports say when a query waited on it.
- If the archive path is unreachable, nothing is moved and nothing is deleted;
  the daemon notifies and the free-space floor (010 §6) still stops new runs.

### 5.1 Archived logs stay searchable

**Decided by the owner (2026-10-12):** archiving must not take anything out of
search. The model is the episodic memory of the owner's `claude-hooks`
project, where a transcript that has been compressed into the archive is still
found, shown and read exactly as before.

What that model does, and what is required here:

| episodic memory | automaticum |
|---|---|
| the index (text, vectors, summaries) stays in the database when a transcript is archived | **only raw blobs move.** DuckDB rows, Lance chunks, vectors and the full-text index never go to the archive tier |
| the transcript keeps its canonical name; only opening it resolves to the compressed file | a blob keeps its hash as its address; the store resolves the tier on open. No caller knows or cares where a blob is |
| search, show, indexing and the read tool open archived transcripts transparently | every reader does: the transcript viewer, `logs`, `find`, replay, `reanalyse`, `rescore`, export and the MCP tools |
| each indexed row carries the version of the encoder that embedded it; a pipeline change triggers re-embedding | each chunk carries the embedder's identity and version; changing the embedder re-embeds from the blobs, archived ones included |
| a marker in a conversation keeps it archived but not indexed | private tasks are indexed locally and never sent to a cloud embedder (060 §7) |

Further requirements:

- Full-text and semantic search hit the index and are **as fast on archived
  runs as on live ones**. Only opening the matched passage reads the archive.
- **Regex over raw text** that is not in the index scans blobs, archived ones
  too. It is slower there; the result says how many blobs were read from the
  archive, and a query may be limited to live data explicitly, never silently.
- A hit always shows enough context from the index (the chunk and its
  neighbours) to be useful before the blob is opened.
- An archived blob that cannot be read is reported as such per result, with its
  hash and expected location. It is never an empty result.
- The consistency check (§3) covers both tiers.

## 6. The store interface

The daemon reaches the store through one small Go interface, so the choice of
engines can be revisited. The spike's test set becomes that interface's test
suite.

Also required: schema migrations, backup, and import of the old corpus (100).

Never write large data to a RAM-backed directory. Never put the store on a
volume that has stalled under log writes; the data root is a configured path
with a free-space floor.

## 7. The spike

**Decided by the owner (2026-10-12):** the spike runs **first**, before the
walking skeleton (110 §3). It confirms the
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
- search with part of the corpus moved to an archive tier on a slower disk:
  full-text and semantic latency unchanged, regex scan cost, and opening a
  matched passage from an archived blob;
- memory at rest and at peak.

**Pass criteria** are set before the spike runs. **Open:** the thresholds.
