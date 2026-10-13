# Blob file format, version 1

This is the on-disk format of one blob in `internal/store/blob`. The code in
`format.go` implements it; this file is the reference.

## Where a blob lives

```
<root>/blobs/<h[0:2]>/<h[2:4]>/<h>.zst      a blob
<root>/tmp/<prefix>-<16 hex>.tmp            a file being written
```

`h` is the lowercase hex sha256 of the blob's **uncompressed** content, 64
characters. The layout is the same on the live root and on the archive root.
A file appears under `blobs/` only by a rename of a complete, fsynced file
from `tmp/` on the same root, so everything under `blobs/` is complete.
Everything under `tmp/` is disposable and is removed when the store is opened.

## File layout

```
+---------+---------+-- ... --+-----------+----------------------------+
| frame 0 | frame 1 |         | frame N-1 | index (zstd skippable frame)|
+---------+---------+-- ... --+-----------+----------------------------+
```

All integers are little-endian.

### Data frames

The content is cut into chunks of `chunk_size` uncompressed bytes; the last
chunk holds the remainder (1 to `chunk_size` bytes). Each chunk is compressed
on its own into one standard zstd frame (RFC 8878) carrying its content size
and the zstd content checksum. Frames do not share history or a dictionary, so
any frame can be decompressed alone.

- `N = ceil(total_len / chunk_size)`, except that an empty blob has `N = 1`:
  a single frame with empty content.
- Frame `i` holds the bytes `[i * chunk_size, min((i + 1) * chunk_size, total_len))`.
- Frames follow each other with nothing in between; frame 0 starts at offset 0.

### Index

The index is one zstd skippable frame at the end of the file. zstd decoders
skip it, so `zstd -d` on the file yields exactly the content.

| offset in frame | size | field |
|---|---|---|
| 0 | 4 | skippable-frame magic `0x184D2A5B` (bytes `5B 2A 4D 18`) |
| 4 | 4 | payload size in bytes, `12 * N + 32` |
| 8 | 12 * N | entries, one per data frame, in order |
| 8 + 12 * N | 32 | footer |

Entry `i` (12 bytes):

| offset | size | field |
|---|---|---|
| 0 | 8 | `offset`: position of frame `i` from the start of the file |
| 8 | 4 | `crc`: CRC-32C (Castagnoli) of the **uncompressed** bytes of chunk `i` |

Footer (32 bytes, the last 32 bytes of the file):

| offset | size | field |
|---|---|---|
| 0 | 4 | `version` = 1 |
| 4 | 4 | `chunk_size`, 1 to 2^30 |
| 8 | 8 | `total_len`: uncompressed length of the blob |
| 16 | 4 | `frame_count` = N |
| 20 | 4 | `index_crc`: CRC-32C of all entries followed by footer bytes 0 to 19 |
| 24 | 8 | magic, ASCII `AMBLOBIX` |

The compressed length of frame `i` is `offset[i + 1] - offset[i]`, where
`offset[N]` is the start of the index, `file_size - (8 + 12 * N + 32)`.

## Reading

1. Read the last 32 bytes. Require the magic and `version == 1`.
2. Require `1 <= chunk_size <= 2^30` and `frame_count == N` as defined above.
3. Locate the index at `file_size - (8 + 12 * N + 32)`; require the
   skippable-frame magic, the payload size, and `index_crc`.
4. Require `offset[0] == 0`, offsets strictly increasing, and all of them
   before the start of the index.
5. To read uncompressed bytes `[a, b)`: decompress frames `a / chunk_size` to
   `(b - 1) / chunk_size`. For each, require that it decodes to exactly its
   expected length and that the CRC-32C of the result equals the entry's `crc`.

Any failed requirement means the file is corrupt. A reader must not return
bytes from a frame that failed step 5.

## What protects what

- Truncation at any point removes or damages the footer: step 1 fails.
- A changed bit in the index fails the magic, size, or `index_crc` check.
- A changed bit in a data frame fails zstd decoding, the zstd checksum, or the
  entry's `crc` when that frame is read, unless the frame still decodes to the
  same bytes. Other frames stay readable.
- The file name is the sha256 of the whole content; a full verification
  decompresses every frame and compares.

## Compatibility

- `zstd -d` and `zstd -t` accept the file.
- The index is not the "zstd seekable format" of the zstd contrib directory
  and uses a different skippable magic nibble (`B`, not `E`), so tools for
  that format do not mistake one for the other.
- A reader meeting a `version` it does not know must refuse the file. A new
  version may change the entry or footer layout; the last 8 bytes (magic) and
  the position of `version` (32 bytes before the end) stay fixed.
