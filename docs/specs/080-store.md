# 080 — The store

**Status:** draft 1.0, 2026-10-13. The shape is the owner's preference. The
spike has run (§7.4, and `spike/001-store/REPORT.md`): the hard gates pass and
the evidence is for confirming the shape. The confirmation itself is the
owner's and is still open.

## 1. What the store has to do

- Hold everything: streams and logs, resolved configurations, lane and queue
  state, metrics, findings, statistics and reports.
- Scale to thousands of runs, compressed. A run wrote about 25 MB of log per
  1000 s in the old harness; one old stream is 7.5 GB. zstd got about 30 times
  on the archived session state.
- Vector search over text chunks (likely millions), full-text and regex search,
  and analytical queries (group by any arm field, percentiles, joins).
- Be simple to run: no separate database server to administer.
- Work from Go on Linux (amd64 and arm64), with only the Go application on the
  host. **Decided by the owner (2026-10-12):** only the daemon holds the store,
  so Windows builds agent mode and the CLI without it; a Windows build with the
  store is attempted in the spike and reported, not required. The daemon runs
  on Linux for now.
- One writer (the daemon), many readers, and survive a kill during a write.

## 2. Shape

**Owner's preference (2026-10-12):** three parts, each doing what it is good
at.

| part | holds | role |
|---|---|---|
| **Blob store on disk** | event streams, wire captures, logs, snapshots, session data | the source of truth for raw material |
| **DuckDB** | runs, arms, tasks, cells, cohorts, lane and queue state, metrics, findings, statistics, reports | state and analytics |
| **Lance** (binding chosen by the spike, §7.1) | text chunks, their vectors, the full-text index | search |

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
  chunking). The spike could only simulate request bodies, because the old
  corpus has no wire capture; on those, both schemes store 1 to 2 % of the raw
  bytes, against 19 % for compressing each body whole. Recommendation: by
  message part, with the per-request lists of part hashes compressed, to be
  confirmed on the first real captures of milestone 2 (§7.4).
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
1. Lance for everything. Risk: small frequent updates make a new table version
   each, and the binding risk of §7.1 is concentrated.
2. DuckDB for everything, with vectors searched exactly or the index rebuilt
   from the table at start. Risk: full-text needs scheduled rebuilds; vector
   cost at millions of chunks.
3. ClickHouse embedded (chDB) only if log volume defeats the others.

### 7.1 Lance binding and linking

Read on 2026-10-12 from each project's repository; nothing below is a
measurement yet.

- `lancedb-go` **cannot be used as released**: v0.1.2 (September 2025) returns
  "not supported" for full-text queries, has no hybrid search, merge-insert or
  compaction, and ships no linux-arm64 or Windows library. Its unreleased main
  branch has them, must be built from source, and the repository looks
  unmaintained.
- `lance-c` is the official C interface to Lance and is active. It covers
  write, update, delete, merge-insert, vector and scalar indexes, full-text,
  versions and compaction, exchanging data through the Arrow C Data Interface.
  Its prebuilt libraries need a newer glibc than our oldest host, so it is
  built from source too.
- DuckDB has a `lance` extension, which would remove the second native library.
- `duckdb-go` is healthy and ships static archives. Its `vss` and `fts`
  extensions are downloaded at run time; this shape does not need them, and
  the store switches extension autoload and autoinstall off.

**Decided by the owner (2026-10-12):**

- The Lance binding is **chosen by the spike**. Candidates in order: L1
  `lancedb-go` main at a pinned commit; L2 `lance-c` behind a thin cgo wrapper
  of our own; L3 DuckDB's `lance` extension.
- **Linking posture:** one binary, both engines linked statically, glibc and
  libstdc++ dynamic. A fully static binary is measured and reported, not
  required.
  **Decided by the owner (2026-10-13), after the link gate:** the build base is
  `manylinux_2_28`, not Debian 11. DuckDB's prebuilt archive needs a C++
  runtime newer than Debian 11's compiler provides, so it cannot be linked
  there. The resulting binary needs glibc 2.28 or later and runs on Debian 11.
  Fully static linking is not available.
- **Hybrid ranking** (reciprocal rank fusion) is our own Go code if the chosen
  binding lacks it.

### 7.1.1 Result of the link gate (T1)

Run on 2026-10-12/13; details and reproduction in
`spike/001-store/linkcheck/README.md`.

- **T1 passes for L1 and for L2**, on linux amd64 and arm64: both engines in
  one process, data round-tripped, full-text and vector queries answered, no
  duplicate symbol and no allocator fault. L3 was not tested.
- Binaries need glibc 2.28 and are 259 MB (L1) and 348 MB (L2) stripped.
- A Windows build with the store does not link with the toolchain tried; the
  cause is DuckDB's Windows archive, and it is not required (D40).
- **Decided by the owner (2026-10-13):** the binding is **L2, `lance-c` behind
  a thin cgo wrapper of our own**, with our own rank fusion for hybrid search.
  It is the maintained path. L1 stays as a fallback that is known to link.

### 7.2 Data and measurements

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

### 7.3 Pass thresholds

**Decided by the owner (2026-10-12).** T1 to T3 are hard gates. A miss on T4 to
T15 is reported with its number and does not by itself reject the shape; the
owner decides.

| # | measurement | pass |
|---|---|---|
| T1 | one binary with both engines, linux amd64 and arm64, built on Debian 11 | opens DuckDB and a Lance dataset in one process and round-trips data on both; no duplicate-symbol or allocator fault |
| T2 | `kill -9` during writes, 20 times | reopens 20/20; no committed DuckDB transaction lost; no partial blob under a valid name; Lance reopens or is rebuilt from blobs |
| T3 | import of the real corpus | the old verdict counts reproduced exactly; original run ids kept |
| T4 | ingest rate | at least 10 MB/s of raw stream on one core |
| T5 | size on disk | streams and logs at most 15 % of raw; figures for both deduplication schemes |
| T6 | batch statistics (pass rate and percentiles by three arm fields, 5,000 runs) | under 1 s warm |
| T7 | tool-failure census over all tool calls | under 5 s warm |
| T8 | full-text search over all chunks | p95 under 1 s warm, under 5 s cold |
| T9 | regex scan over raw blobs, whole live corpus | under 10 minutes; throughput reported |
| T10 | vector search, 1 M chunks at 1024 dimensions | p95 under 200 ms; recall@10 at least 0.90 against exact |
| T11 | vector search, 5 M chunks at 1024 dimensions | p95 under 500 ms; recall@10 at least 0.85; index build under 2 h |
| T12 | 10 updates per second to lane state for 1 h under the query load | p99 under 100 ms; file growth under 100 MB after checkpoint |
| T13 | range read of 1 MB from a multi-gigabyte blob | under 200 ms live, under 2 s from the archive tier |
| T14 | memory | under 500 MB at rest, under 16 GB at peak |
| T15 | search with half the corpus archived | full-text and vector latency within 10 % of all-live; opening an archived passage under 2 s |

If T1 fails for every Lance candidate, the spike falls back to DuckDB for
everything, re-runs T6 to T15 on that, and reports.

Vectors: about 100k real chunks are embedded with local embedders at 768 and
1024 dimensions; the 1 M and 5 M sets are synthetic vectors drawn to match the
real sample.

### 7.4 Result of the spike

Run on 2026-10-12/13 on `linux-gpu-1`, a shared and busy host. Every number,
its conditions and its limits are in `spike/001-store/REPORT.md`.

| # | result | |
|---|---|---|
| T1 | both candidates link and run on amd64 and arm64 | pass |
| T2 | 20 of 20 reopened after `kill -9`; nothing acknowledged was lost | pass |
| T3 | 474 runs imported; every verdict file reproduced exactly | pass |
| T4 | 102 MB/s of stream on one core | pass |
| T5 | 2.0 % of raw | pass |
| T6 | 1.7 ms | pass |
| T7 | 9.4 ms over 2,000,000 tool calls | pass |
| T8 | p95 under 10 ms warm at 389k and at 3.9M chunks | pass |
| T9 | 526 s over every raw byte; 17 s skipping streaming deltas | pass, see below |
| T10 | recall 0.954 at p95 30 ms, with a refine factor of 30 | pass |
| T11 | recall 0.906 at p95 43 ms with a refine factor of 30; index built in 9 minutes | pass |
| T12 | p99 15 ms; no growth after a checkpoint | pass |
| T13 | p95 8 ms on both tiers | pass |
| T14 | 39 MB at rest; 5.5 GB at peak | pass |
| T15 | search unchanged; archived passage p95 4 ms | pass |

What the spike changes or adds. Each is a proposal until the owner decides.

- **Open:** confirmation of the shape (D6). The evidence is for confirming.
- **Open:** regex search runs over the lines of a stream that are not
  streaming deltas, not over every byte. A delta carries nothing that the
  closing event of its block does not repeat, and the old corpus is 30 GB of
  stream for 0.45 GB of distinct text. A raw mode over every byte stays, and
  is slow.
- **Open:** vector search uses a refine step by default, with a factor of at
  least 30. Without it recall is between 0.05 and 0.6 on every set tried, and
  more probes do not help. A deliberately hostile synthetic set (evenly spread
  tight clusters) still reaches only 0.63 at 5M; real embeddings are not like
  it, and recall should be checked on a sample whenever the embedder changes.
- **Open:** every chunk carries a locator (blob hash and byte offset of its
  source line), so a hit can open its passage. The spike packed this into the
  chunk id.
- Embedding ran at 45 chunks per second with a 1024-dimension model on the
  GPU. Embedding, not search, limits how fresh semantic search can be; it needs
  its own plan before milestone 6.
- Rows written after an index is built are searched without the index until
  it is rebuilt, and `lance-c` has no incremental update. The daemon rebuilds
  indexes on a schedule.
- The archive mover handles many small blobs at 2.5 MB/s. Enough for a nightly
  job; batching its syncs is an improvement, not a requirement.
- The 5,000-run blob set of §7.2 was not generated: costs that are linear were
  measured on the real 43 GB, and the parts that are not were measured at scale
  (state tables at 5,000 runs, full-text at ten times the corpus, vectors at
  1M and 5M).
