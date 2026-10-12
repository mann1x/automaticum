# Store spike, step 1: the link gate (threshold T1)

**Run on 2026-10-12/13.** T1 asks: can DuckDB and Lance live in one Go binary,
on linux amd64 and arm64, open both in one process and round-trip data, with no
duplicate-symbol or allocator fault?

**Result: T1 passes for both Lance candidates tested.** The choice between
them is the owner's; a recommendation is at the end.

## What was tested

Each program opens a DuckDB file (extension autoload and autoinstall switched
off), creates a table, inserts 1,000 rows and runs a percentile query; then
creates a Lance table of 200 rows with a text column and a 64-dimension vector
column, builds a full-text index, and runs a full-text and a vector query; then
opens the DuckDB file again in the same process.

| | L1 | L2 |
|---|---|---|
| Lance binding | `lancedb/lancedb-go`, main at `fa14ce29` (unreleased), built from source | `lance-format/lance-c` v0.1.9, built from source, behind our own cgo wrapper using the Arrow C Data Interface |
| Lance engine underneath | lancedb 0.24.0, lance 1.0.3 | lance 9.1.0-beta.3 |
| Rust compiler needed | 1.89 works | 1.91 or later (1.99 used) |
| DuckDB | `duckdb/duckdb-go` v2.10506.0, DuckDB 1.5.6, prebuilt static archive | same |
| Source | [`l1/`](l1/) | [`l2/`](l2/) |

L3 (DuckDB's `lance` extension) was not tested: the plan stops at the first
candidate that passes, and two did.

## Results

| check | L1 | L2 |
|---|---|---|
| linux amd64: links, runs in the build image (glibc 2.28) | pass | pass |
| linux amd64: runs directly on a Debian 11 host (glibc 2.31) | pass | pass |
| linux arm64: links and runs (under emulation, glibc 2.28) | pass | pass |
| full-text query returns rows | pass | pass |
| vector query returns rows | pass | pass |
| hybrid query with rank fusion, in the binding | pass | not offered by the binding |
| DuckDB reopened after Lance ran in the same process | pass | pass |
| duplicate symbols or allocator faults at link or run time | none | none |
| highest glibc symbol version required | 2.28 | 2.28 |
| highest libstdc++ symbol version required | GLIBCXX_3.4.22 | GLIBCXX_3.4.22 |
| Lance static archive, amd64 / arm64 | 596 MB / 595 MB | 990 MB / 990 MB |
| Lance archive build time (12 cores), amd64 / arm64 cross | 6.5 min / 6.3 min | 10.1 min / 11.8 min |
| binary size amd64, unstripped / stripped | 322 MB / 259 MB | 477 MB / 348 MB |
| final link time, amd64 native / arm64 emulated | 1.0 min / 8.0 min | 1.0 min / 7.1 min |
| windows amd64 | see below | not attempted |

Dynamic dependencies of every Linux binary: `libc`, `libm`, `libdl`,
`libpthread`, `libresolv`, `libstdc++`, `libgcc_s`. Nothing else.

## What was learned about building

1. **Debian 11's own compiler cannot link DuckDB's prebuilt archive.** It needs
   C++ runtime symbols from GCC 11 and later (`std::__throw_bad_array_new_length`
   and others); Debian 11 has GCC 10. The link fails with hundreds of undefined
   references.
2. **The fix is the base DuckDB itself is built on:** `manylinux_2_28` (glibc
   2.28, GCC 14, newer C++ runtime parts linked in statically). A binary linked
   there needs only glibc 2.28 and an old system libstdc++, and runs on our
   Debian 11 host. This is [`build/Dockerfile.build`](../../../build/Dockerfile.build).
   **It replaces "built on Debian 11" in decision D39.**
3. **The Rust archives can be cross-compiled on Debian 11** with its GNU cross
   toolchains ([`build/Dockerfile.cross`](../../../build/Dockerfile.cross)) and
   then linked in the manylinux image of the target architecture. An archive
   compiled against glibc 2.31 headers linked cleanly against glibc 2.28 on
   both architectures.
4. **The arm64 final link runs under emulation** on an amd64 host and takes
   about 8 minutes. A native arm64 builder would be faster; it is not needed.
5. **Fully static linking is not available** in this image (no static glibc),
   and DuckDB's own link flags name the shared C++ runtime, so making that
   static changes nothing. The binary is "both engines static, glibc and
   libstdc++ dynamic", as D39 intended.
6. **Debian 11's package mirrors have moved to the archive**; the cross image
   points apt there.
7. **lance-c's prebuilt libraries need glibc 2.39**, far newer than our hosts,
   so they are no use to us; it is built from source.

## Windows amd64

Not required (D40). Attempted for L1 with MinGW-w64 from Debian 11.

**Result: does not link with this toolchain. The failure is on the DuckDB
side, not in Lance.**

- The Lance archive cross-compiles for `x86_64-pc-windows-gnu` without trouble
  (408 MB, 6.3 minutes).
- The final link fails with thousands of undefined references of two kinds:
  `pthread_mutex_*` and `sched_yield`, and `std::__throw_bad_array_new_length`.
  DuckDB's prebuilt Windows archive expects a MinGW-w64 with POSIX threads and
  a C++ runtime from GCC 11 or later. Debian 11's MinGW is GCC 10 with Win32
  threads.
- So this is the same cause as finding 1 above, in its Windows form. A current
  MinGW-w64 (POSIX threads, as MSYS2's UCRT toolchain provides) is the likely
  fix. It was not tried: a store on Windows is not required, and Windows gets
  agent mode and the CLI without it.
- L2 was not attempted on Windows; it would meet the same DuckDB failure first.

## How to reproduce

`SPIKE_ROOT` is a directory with room for sources and caches (about 12 GB after
both candidates are built for both architectures).

```
git clone https://github.com/lancedb/lancedb-go  $SPIKE_ROOT/src/lancedb-go   # fa14ce29
git clone https://github.com/lance-format/lance-c $SPIKE_ROOT/src/lance-c      # v0.1.9

docker build -t automaticum-build:deb11 -f build/Dockerfile.cross build
docker build -t automaticum-build:ml228-amd64 -f build/Dockerfile.build build
docker build -t automaticum-build:ml228-arm64 --platform linux/arm64 \
    --build-arg ARCH=aarch64 --build-arg GOARCH=arm64 -f build/Dockerfile.build build
```

Archives, in the `deb11` image with `$SPIKE_ROOT/src` at `/work` and a target
directory at `/target`:

```
cd /work/lancedb-go/rust && cargo         build --release --target <triple>
cd /work/lance-c         && cargo +1.99.0 build --release --target <triple>
```

Link, in the `ml228-<arch>` image with this repository at `/src`:

```
cd /src/spike/001-store/linkcheck/l1
CGO_CFLAGS=-I/work/lancedb-go/include \
CGO_LDFLAGS="/target/lancedb-go/<triple>/release/liblancedb_go.a -lm -ldl -lpthread" \
go build -o linkcheck-l1 .

cd /src/spike/001-store/linkcheck/l2
CGO_CFLAGS=-I/work/lance-c/include \
CGO_LDFLAGS="/target/lance-c/<triple>/release/liblance_c.a -lm -ldl -lpthread" \
go build -o linkcheck-l2 .
```

Each binary prints one JSON line and `linkcheck: PASS` or `FAIL`.

## Recommendation: L2, `lance-c` behind our own wrapper

Both pass, so the gate does not decide. What does:

| | L1 `lancedb-go` main | L2 `lance-c` |
|---|---|---|
| Maintenance | no release since September 2025; last commit May 2026; the original author says he was removed as maintainer; we would pin an unreleased commit of an orphaned repository | official project of the Lance format, release in September 2026, commits in October 2026 |
| Engine age (upstream lance was at 14 beta when read) | lance 1.0.3 | lance 9.1 beta |
| What we own | nothing, until it breaks; then a fork of about 600 MB of someone else's Rust and Go glue | a Go wrapper over a documented C header, a few hundred lines, which we can read and test |
| API fit | a table-oriented Go API, with hybrid ranking built in | a dataset-oriented C API through Arrow streams; hybrid ranking is ours to write (rank fusion over two result lists) |
| Capabilities we need | write, update, delete, merge-insert, vector and full-text indexes, versions, compaction | the same, by the header; only the calls this check uses have been exercised |
| Cost | smaller binary (259 MB against 348 MB stripped), faster build | larger binary, slower build, needs a recent Rust compiler, and a 0.x C interface that states its ABI is not yet stable |

L1 is the smaller and more convenient today and the worse bet over a year: it
is a dependency nobody maintains, on the oldest engine of the two. L2 costs us
a wrapper and puts us on the maintained path. Since the store sits behind one
interface and Lance holds only derived data, the wrapper is small and a change
of binding later loses nothing.

**Not measured yet, for either:** crash safety, concurrency, performance at
scale, memory. Those are T2 and T4 to T15, and they run on whichever candidate
is chosen. If L2 fails one of them, L1 is the fallback and is known to link.
