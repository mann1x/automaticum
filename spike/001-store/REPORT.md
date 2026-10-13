# Store spike: report

**Run on 2026-10-12 and 2026-10-13.** The question (spec 080 §7): does the
shape "DuckDB for state, Lance for search, blobs on disk" hold up, built into
one Go binary, on the real corpus of the old harness and at ten times its size?

**Answer: yes, with four things to change in the spec and two measurements
that could not be made properly.** All three hard gates pass, and so do the
twelve other thresholds, T9 and T10 with a condition each (§3.1).

The decision on the shape (D6) is the owner's; §6 lists what to decide. The
raw output of every measurement is in [`results/`](results/).

## 1. Setup

| | |
|---|---|
| State | DuckDB 1.5.6 through `duckdb-go` v2.10506.0, extension autoload and autoinstall off |
| Search | Lance 9.1.0-beta.3 through `lance-c` v0.1.9, behind our own cgo wrapper (`internal/store/lance`), rank fusion in Go |
| Blobs | content-addressed by SHA-256, zstd in independent 4 MiB frames with an index, two tiers (`internal/store/blob`) |
| Build | `manylinux_2_28` link image; both engines static, glibc and libstdc++ dynamic |
| Host | `linux-gpu-1`: 12 cores, 125 GB memory, one 24 GB GPU; **shared**, with a load average of 6 to 10 from other work throughout |
| Live tier and indexes | an NVMe disk that is neither the development disk nor the old harness's disk |
| Archive tier | the large disk array (spinning disks behind an SSD cache) |
| Limits on each job | 1 to 8 cores and 16 to 48 GB, set per container |

Because the host was busy, every latency below includes interference from
other work. None of the results is close enough to its threshold for that to
change a verdict, except where §2 says so.

## 2. Results against the thresholds

| # | measurement | threshold | result | |
|---|---|---|---|---|
| T1 | one binary with both engines, linux amd64 and arm64 | links, opens both, round-trips, no symbol or allocator fault | both pass; see [`linkcheck/README.md`](linkcheck/README.md) | **pass** |
| T2 | `kill -9` during writes, 20 times | reopens 20/20, nothing committed is lost | 20 of 20 reopened; 320 runs acknowledged, 320 present; Lance held only whole appends (316) | **pass** |
| T3 | import of the real corpus | verdict counts exact, run ids kept | 474 runs, none failed; all three verdict files reproduced line by line | **pass** |
| T4 | ingest rate | 10 MB/s of raw stream on one core | 102 MB/s of stream (147 MB/s of all files) on one core | **pass** |
| T5 | size on disk | at most 15 % of raw | 2.0 % (43.4 GB raw, 0.88 GB stored) | **pass** |
| T6 | batch statistics, 5,000 runs | under 1 s warm | 1.7 ms idle; 5.9 ms p95 during the T12 load | **pass** |
| T7 | tool-failure census, 2,000,000 tool calls | under 5 s warm | 9.4 ms idle; 23 ms p95 during the T12 load | **pass** |
| T8 | full-text search over all chunks | p95 under 1 s warm, 5 s cold | 389k chunks: 7.6 ms warm, 15 ms first pass. 3.9M chunks: 5.4 ms warm, 54 ms first pass (slowest single query 355 ms) | **pass** |
| T9 | regex over raw blobs, whole live corpus | under 10 minutes | 526 s for 30 GB with 4 workers; 17 s when streaming deltas are skipped | **pass, but see §3.1** |
| T10 | vector search, 1M chunks, 1024 dimensions | p95 under 200 ms, recall@10 at least 0.90 | recall 0.954 at p95 30 ms (20 probes, refine 30); 0.892 with refine 10 | **pass, with the right setting** |
| T11 | vector search, 5M chunks, 1024 dimensions | p95 under 500 ms, recall@10 at least 0.85, build under 2 h | recall 0.906 at p95 43 ms (20 probes, refine 30); index built in 9.0 min on 4 cores | **pass** |
| T12 | 10 lane-state updates per second for 1 h, queries running | p99 under 100 ms, growth under 100 MB | p99 15 ms (27 of 35,989 updates took over 100 ms, the slowest 470 ms); file 15.0 MB before, 22.7 MB after, 15.0 MB after a checkpoint | **pass** |
| T13 | 1 MiB range read from a 4 GiB blob | 200 ms live, 2 s archive | p95 8.1 ms live, 7.6 ms archive | **pass** |
| T14 | memory | 500 MB at rest, 16 GB at peak | 39 MB with the 3.9M-chunk dataset open and idle; highest peak 5.5 GB, loading and indexing 5M vectors | **pass** |
| T15 | search with half the blobs archived | latency within 10 %, archived passage under 2 s | search unchanged within run-to-run spread (§3.4); archived passage p95 4.2 ms with the file cache dropped for those files | **pass** |

## 3. What the numbers mean

### 3.1 Search

**The old streams are almost all repetition.** 30 GB of event streams hold
0.45 GB of distinct text: 389,279 chunks of at most 2,000 bytes (167k
reasoning, 29k text, 73k tool arguments, 120k tool results). Nearly every line
is a streaming delta, and the closing event of each block repeats its text in
full. This is why the corpus compresses to 2 %.

**Full-text search is not a concern.** Building the index takes 21 s for the
real corpus and 273 s for ten copies of it with one word in twenty replaced.
Queries stay under 10 ms warm at both sizes. "Cold" here means the first query
of a new process: the operating system's file cache was not dropped, because
the host is shared.

**Regex over raw streams is too slow as specified.** Go's regular-expression
engine ran a case-insensitive alternation at 57 MB/s over four workers: 526 s
for this corpus, so about 90 minutes at ten times the size. A plain phrase
runs at 980 MB/s on one worker. Skipping the delta lines, which carry nothing
their closing event does not repeat, brings the same pattern to 17 s with
identical matches. **Proposal:** regex search runs over the non-delta lines of
a stream (or over the chunk text), not over every byte; a "raw, every byte"
mode stays available and is slow.

**Vector search passes on real embeddings, and the setting matters.**

| data | probes, refine | recall@10 | p50 | p95 |
|---|---|---|---|---|
| 50k real chunks, 768 dimensions (recall by identical ids) | 50, 30 | 0.912 | 11.2 ms | 21.2 ms |
| 100k real chunks, 1024 dimensions | 20, 10 | 0.966 | 6.6 ms | 11.6 ms |
| | 50, 30 | 0.991 | 12.3 ms | 18.0 ms |
| | exact scan | 1.000 | 331 ms | 385 ms |
| 1M, real vectors plus noise | 20, 10 | 0.892 | 13.8 ms | 25.7 ms |
| | 20, 30 | 0.954 | 14.6 ms | 30.0 ms |
| | 100, 100 | 0.982 | 38.0 ms | 55.9 ms |
| | exact scan | 1.000 | 3.9 s | 4.5 s |
| 5M, real vectors plus noise | 20, 10 | 0.866 | 17.4 ms | 29.8 ms |
| | 20, 30 | 0.906 | 22.8 ms | 43.0 ms |
| | 100, 100 | 0.960 | 46.8 ms | 79.1 ms |
| | exact scan | 1.000 | 14.0 s | 14.6 s |
| 1M, synthetic clusters (worst case) | 100, 30 | 0.882 | 51.8 ms | 78.8 ms |
| 5M, synthetic clusters (worst case) | 100, 30 | 0.630 | 212 ms | 420 ms |

- Without a refine step recall is poor everywhere (0.58 on the 1M set, 0.30 on
  the 5M set, 0.09 and 0.05 on the worst-case sets): the compressed index alone cannot order near
  neighbours. **The refine factor must default to 30 or more**; more probes do
  not help.
- The first synthetic sets (2,000 and 5,000 tight, evenly spread clusters)
  are a worst case that real embeddings do not resemble, and at 5M that case
  misses the recall threshold (0.63). If a future embedder produced vectors
  like that, a larger refine factor or a different index type would be
  needed. Real chunks are nothing like it: real chunks have a mean cosine of
  0.64 between random pairs and 0.95 to the nearest neighbour. The second
  synthetic set is real vectors with noise added, as the plan asked.
- Many real chunks are near-copies, so the ten nearest are often tied. Recall
  is counted as "at least as close as the true tenth neighbour"; demanding the
  same ids gives 0.92 even for an exact scan.
- Hybrid search (full-text and vector, fused by rank in our code): p95 24 ms
  on the 100k real set.
- Search filtered to one run: p95 13 ms at 1M and 12 ms at 5M.
- An exact scan, which is what rows not yet in the index cost, takes 4 s per
  query at 1M and 14 s at 5M. Unindexed rows must stay few (§7).

**Embedding is the slow step, not search.** The 1024-dimension model embedded
45 chunks per second on the GPU: 37 minutes for 100k chunks, so about 2.4
hours for this corpus and a day for ten times it. This was one request at a
time with 32 chunks each and no tuning.

### 3.2 State

DuckDB did everything asked of it with a wide margin. The one-hour test wrote
10 lane-state updates per second while two reporting queries looped; the file
did not grow after a checkpoint. Generating the 5,000 runs with 400 tool calls
each took 159 s (31 runs per second), which is the cost of one transaction per
run and is no concern for a harness that finishes a run every few minutes.

### 3.3 Blobs

- Writing: 125 to 165 MB/s per stream. Reading back and verifying all 43.4 GB:
  36 s.
- A range read costs one 4 MiB frame: a few milliseconds on either tier.
- **The archive mover is slow on many small files:** 2.5 MB/s, 145 s for 4,257
  blobs (364 MB), because each blob is copied, synced, verified and removed on
  its own. One 598 MB blob moved in 8 s. Acceptable for a nightly job; worth
  batching the syncs later.

### 3.4 Archive tier

After the older half of the blobs moved to the array, the store check passed
(4,258 live, 4,257 archived, none missing), search needed no change, and
passages opened from the archive in milliseconds. Full-text p95 over five
runs was 3.4 to 7.6 ms with no relation to whether blobs were archived (3.6
and 7.6 ms all-live; 7.1, 6.1 and 3.4 ms half-archived), so the "within 10 %"
test cannot be read at this scale: the spread between identical runs is
larger than 10 %. Lance and DuckDB never touch the archive tier, so no effect
is expected. The array has an SSD cache; a plain spinning disk would open a
passage in tens of milliseconds, still far inside 2 s.

## 4. Size, memory and build

| | |
|---|---|
| real corpus, raw | 43.4 GB (30.0 GB of it event streams) |
| blobs | 0.87 GB |
| DuckDB file, 474 runs and 64,801 tool calls | 5 MB |
| DuckDB file, 5,000 runs and 2,000,000 tool calls | 15 MB |
| Lance, 389k chunks with full-text index, no vectors | 0.35 GB |
| Lance, 3.9M chunks with full-text index, no vectors | 4.0 GB |
| Lance, 100k chunks with 1024-dimension vectors and both indexes | 0.48 GB |
| Lance, 1M chunks with 1024-dimension vectors and both indexes | 4.0 GB |
| Lance, 5M chunks with 1024-dimension vectors and both indexes | 19.9 GB |
| vector index build, 100k / 1M / 5M | 1.7 min / 2.1 to 4.3 min / 6.7 min on 8 cores, 9.0 min on 4 |
| loading vectors | 36,000 to 43,000 rows per second |
| peak memory: import | 0.5 GB of the process's own memory (one worker) |
| peak memory: chunking and indexing 3.9M chunks | 3.6 GB |
| peak memory: 1M vectors, load, index and queries | 3.0 to 3.7 GB |
| peak memory: 5M vectors, load, index and queries | 5.3 to 5.5 GB |
| memory at rest, 3.9M-chunk dataset open | 39 MB |
| memory after a round of full-text queries on it | 1.05 GB |
| binary, `automaticum` with DuckDB only | 74 MB |
| binary with both engines, stripped | 348 MB |

## 5. Deduplication of wire captures (O8)

**This could not be measured on real data:** the old harness never captured
request bodies. The figures below are for bodies *simulated* from the event
streams of 83 streams (one run in six): each request is a fixed 60 KiB preamble
plus every message so far, and a compaction replaces the oldest messages with
a summary. That reproduces the one property that matters here (each request
repeats nearly all of the previous one) and nothing else about a real body.

9,070 requests, 6.32 GiB of bodies:

| scheme | stored | of raw |
|---|---|---|
| each body compressed whole | 1.18 GiB | 18.7 % |
| by message part: each part stored once, a request is a list of part hashes | 0.098 GiB | 1.5 % |
| content-defined pieces of about 8 KiB | 0.073 GiB | 1.2 % |
| content-defined pieces of about 64 KiB | 0.246 GiB | 3.9 % |

- Either deduplicating scheme is 12 to 16 times smaller than compressing whole
  bodies. Storing whole bodies is not an option at this request size.
- The two are close. Three quarters of the by-part figure is the lists of
  hashes themselves (275 parts per request on average), which compress or
  delta-encode well; that was not done here.
- With deduplication, a run's wire capture costs about as much disk as its
  event stream does (1.1 times, in this simulation).

**Recommendation: by message part.** It is as small as content-defined pieces
once the lists are compressed, and it keeps what the questions in 110 §2
need: a request is a list of named parts, so "what changed between request 40
and 41" and "which part re-read the prompt" are comparisons of lists, not of
bytes. The owner decides; the first real captures in milestone 2 should
confirm the sizes before the format is frozen.

## 6. For the owner

1. **Confirm the store shape (D6)** or fall back. The evidence is for
   confirming.
2. **O8:** deduplicate wire captures by message part (§5)?
3. **Regex search** over non-delta lines instead of every raw byte (§3.1)?
4. **Refine factor of at least 30** as the default for vector search, and
   recall checked as part of `store check` on a sample (§3.1)?
5. **A locator in every chunk.** A hit must lead to its place in a blob. The
   spike packed blob hash and byte offset into the chunk id; the store
   interface should carry them as fields.
6. **Embedding throughput** (§3.1) decides how fresh semantic search can be.
   It needs its own plan before milestone 6: batching, a smaller model, or
   embedding only reasoning and text.

## 7. Not measured, and limits of what was

- **Wire-capture deduplication on real bodies** (§5): simulated only.
- **A cold file cache** for full-text and vector search: the host is shared,
  so the cache was dropped only for the archived blob files.
- **The 5,000-run blob set** of the plan was not generated. It would have been
  460 GB of derived streams to prove costs that are linear and were measured on
  the real 43 GB (ingest, verification, regex). What does not scale linearly
  was measured at scale: the state tables at 5,000 runs, full-text at ten
  times the corpus, vectors at 1M and 5M.
- **768 dimensions** were measured on 50k real chunks only, with an earlier
  build of the benchmark that counts recall by identical ids: 0.912 at 50
  probes and refine 30 (an exact scan scores 0.929 by that count), p95 21 ms,
  hybrid p95 32 ms. That model embedded 54 chunks per second. No 1M or 5M set
  was built at 768 dimensions; it is the cheaper case.
- **Rows written after an index is built** are searched without the index
  until it is rebuilt; `lance-c` offers no incremental index update. Correct
  results were verified by a test; the cost at scale was not measured. The
  daemon will need a periodic rebuild, minutes at 1M chunks.
- **`lance-c` is version 0.1 with an ABI its authors call unstable**, on a beta
  of the engine. It did not fail once in these runs. `lancedb-go` main remains
  the tested fallback (D44).
- **arm64** was verified for linking and a round trip only (T1); no
  performance was measured there.
- **Concurrency:** one writer process throughout, as the design intends. Two
  processes on one store were not tried.

## 8. How to reproduce

```
make images
make lance-c SPIKE_ROOT=<dir>
make build bench SPIKE_ROOT=<dir>
make test-store SPIKE_ROOT=<dir>            # includes T2

bin/automaticum legacy import -data <data> -archive <archive> \
    -legacy-root <old harness> -state-archive <state archive>     # T3, T4, T5
bin/automaticum store check -data <data> -archive <archive>

bin/bench state    -dir <dir> -runs 5000 -calls 400 -duration 1h    # T6, T7, T12
bin/bench chunks   -data <data> -lance <dir> -sample-out <file>     # T8 (add -copies 10)
bin/bench text     -data <data> -archive <archive> -lance <dir>     # T8, T15
bin/bench regex    -data <data> [-skip-deltas]                      # T9
bin/bench embed    -sample <file> -lance <dir> -model <model> -vectors-out <f32>
bin/bench vectors  -dir <dir> -n 1000000 -dim 1024 -base <f32> -noise 0.3   # T10, T11
bin/bench range    -root <dir> -archive <dir> -gb 4                 # T13
bin/bench archive-half -data <data> -archive <archive>              # T15
bin/bench dedup    -data <data> -every 6                            # O8
```

The binaries run inside the link image or on any Linux with glibc 2.28 or
later. Each `bench` subcommand prints one JSON object.
