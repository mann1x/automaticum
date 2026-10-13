package blob

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"

	"github.com/klauspost/compress/zstd"
)

// On-disk framing. FORMAT.md is the normative description; keep both in step.
const (
	// skippableMagic is the zstd skippable-frame magic used for the index
	// (any of 0x184D2A50..0x184D2A5F is skipped by zstd decoders).
	skippableMagic uint32 = 0x184D2A5B
	// formatVersion is the version written into the index footer.
	formatVersion uint32 = 1

	skipHeaderLen = 8  // magic + payload size
	entryLen      = 12 // offset u64 + crc32c u32
	footerLen     = 32

	// maxFrames keeps the index payload within the 32-bit size field of a
	// skippable frame.
	maxFrames = (math.MaxUint32 - footerLen) / entryLen

	tailRead = 64 << 10 // bytes read from the end of a file to find the index
)

// footerMagic ends every blob file.
var footerMagic = [8]byte{'A', 'M', 'B', 'L', 'O', 'B', 'I', 'X'}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// index is the decoded seek table of one blob file.
type index struct {
	chunkSize int64
	total     int64    // uncompressed length
	offsets   []int64  // len frames+1: start of every frame, then the index start
	crcs      []uint32 // crc32c of every uncompressed chunk
}

func (ix *index) frames() int { return len(ix.crcs) }

// chunkLen is the uncompressed length of chunk i.
func (ix *index) chunkLen(i int) int64 {
	if i < ix.frames()-1 {
		return ix.chunkSize
	}
	return ix.total - int64(i)*ix.chunkSize
}

// frameCount is the number of frames of a blob: one per started chunk, and
// one empty frame for an empty blob.
func frameCount(total, chunkSize int64) int64 {
	if total == 0 {
		return 1
	}
	n := total / chunkSize
	if total%chunkSize != 0 {
		n++
	}
	return n
}

// appendIndex appends the skippable index frame to dst. offsets holds the
// start of each data frame.
func appendIndex(dst []byte, chunkSize int, total int64, offsets []int64, crcs []uint32) []byte {
	n := len(offsets)
	dst = binary.LittleEndian.AppendUint32(dst, skippableMagic)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(n*entryLen+footerLen))
	start := len(dst)
	for i := range offsets {
		dst = binary.LittleEndian.AppendUint64(dst, uint64(offsets[i]))
		dst = binary.LittleEndian.AppendUint32(dst, crcs[i])
	}
	dst = binary.LittleEndian.AppendUint32(dst, formatVersion)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(chunkSize))
	dst = binary.LittleEndian.AppendUint64(dst, uint64(total))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(n))
	dst = binary.LittleEndian.AppendUint32(dst, crc32.Checksum(dst[start:], castagnoli))
	return append(dst, footerMagic[:]...)
}

func corruptf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}

// readIndex reads and validates the index of a blob file of the given size.
// Every failure wraps ErrCorrupt, except I/O errors, which are returned
// wrapped as they are.
func readIndex(r io.ReaderAt, size int64) (*index, error) {
	const minLen = 1 + skipHeaderLen + entryLen + footerLen
	if size < minLen {
		return nil, corruptf("file is %d bytes, shorter than any blob", size)
	}
	tail := make([]byte, min(size, tailRead))
	if err := readFullAt(r, tail, size-int64(len(tail))); err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	foot := tail[len(tail)-footerLen:]
	if [8]byte(foot[24:32]) != footerMagic {
		return nil, corruptf("no index footer at end of file (truncated or not a blob)")
	}
	if v := binary.LittleEndian.Uint32(foot[0:4]); v != formatVersion {
		return nil, corruptf("unsupported format version %d", v)
	}
	chunkSize := int64(binary.LittleEndian.Uint32(foot[4:8]))
	utotal := binary.LittleEndian.Uint64(foot[8:16])
	n := int64(binary.LittleEndian.Uint32(foot[16:20]))
	sum := binary.LittleEndian.Uint32(foot[20:24])
	if chunkSize < 1 || chunkSize > MaxChunkSize {
		return nil, corruptf("chunk size %d out of range", chunkSize)
	}
	if utotal > math.MaxInt64 {
		return nil, corruptf("uncompressed length %d out of range", utotal)
	}
	total := int64(utotal)
	if n != frameCount(total, chunkSize) {
		return nil, corruptf("index has %d frames, length %d with chunk size %d needs %d",
			n, total, chunkSize, frameCount(total, chunkSize))
	}
	skipLen := skipHeaderLen + n*entryLen + footerLen
	indexStart := size - skipLen
	if indexStart < n { // every frame takes at least one byte
		return nil, corruptf("index of %d frames does not fit a file of %d bytes", n, size)
	}
	skip := tail
	if skipLen <= int64(len(tail)) {
		skip = tail[int64(len(tail))-skipLen:]
	} else {
		skip = make([]byte, skipLen)
		if err := readFullAt(r, skip, indexStart); err != nil {
			return nil, fmt.Errorf("read index: %w", err)
		}
	}
	if m := binary.LittleEndian.Uint32(skip[0:4]); m != skippableMagic {
		return nil, corruptf("bad index frame magic %#x", m)
	}
	if l := int64(binary.LittleEndian.Uint32(skip[4:8])); l != skipLen-skipHeaderLen {
		return nil, corruptf("index frame size %d, want %d", l, skipLen-skipHeaderLen)
	}
	if got := crc32.Checksum(skip[skipHeaderLen:len(skip)-12], castagnoli); got != sum {
		return nil, corruptf("index checksum mismatch")
	}
	ix := &index{
		chunkSize: chunkSize,
		total:     total,
		offsets:   make([]int64, n+1),
		crcs:      make([]uint32, n),
	}
	ent := skip[skipHeaderLen:]
	for i := int64(0); i < n; i++ {
		off := binary.LittleEndian.Uint64(ent[i*entryLen:])
		if off >= uint64(indexStart) {
			return nil, corruptf("frame %d offset %d beyond data end %d", i, off, indexStart)
		}
		ix.offsets[i] = int64(off)
		ix.crcs[i] = binary.LittleEndian.Uint32(ent[i*entryLen+8:])
		if (i == 0 && off != 0) || (i > 0 && ix.offsets[i] <= ix.offsets[i-1]) {
			return nil, corruptf("frame %d offset %d out of order", i, off)
		}
	}
	ix.offsets[n] = indexStart
	return ix, nil
}

// readFullAt fills p from r at off; a short read is an error.
func readFullAt(r io.ReaderAt, p []byte, off int64) error {
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// readChunk decompresses chunk i. comp and dst are scratch buffers that are
// reused when large enough; the (possibly grown) buffers are returned so the
// caller can keep them. The returned chunk is checked against the length and
// the crc32c recorded in the index, so wrong bytes are never returned.
func readChunk(dec *zstd.Decoder, r io.ReaderAt, ix *index, i int, comp, dst []byte) (chunk, compOut, dstOut []byte, err error) {
	want := ix.chunkLen(i)
	clen := ix.offsets[i+1] - ix.offsets[i]
	// A zstd frame never expands its content by more than a few bytes per
	// block; anything larger is not something Put wrote.
	if clen > ix.chunkSize+ix.chunkSize/8+1024 {
		return nil, comp, dst, corruptf("frame %d is %d bytes compressed for a chunk of %d", i, clen, ix.chunkSize)
	}
	if int64(cap(comp)) < clen {
		comp = make([]byte, clen)
	}
	comp = comp[:clen]
	if err := readFullAt(r, comp, ix.offsets[i]); err != nil {
		return nil, comp, dst, fmt.Errorf("read frame %d: %w", i, err)
	}
	if int64(cap(dst)) < max(want, 1) {
		dst = make([]byte, 0, max(want, 1))
	}
	// The decoder is created with WithDecodeAllCapLimit, so a frame claiming
	// more content than the chunk fails instead of allocating.
	out, derr := dec.DecodeAll(comp, dst[:0:max(want, 1)])
	if derr != nil {
		return nil, comp, dst, corruptf("frame %d: %v", i, derr)
	}
	if int64(len(out)) != want {
		return nil, comp, dst, corruptf("frame %d decodes to %d bytes, want %d", i, len(out), want)
	}
	if crc32.Checksum(out, castagnoli) != ix.crcs[i] {
		return nil, comp, dst, corruptf("frame %d content checksum mismatch", i)
	}
	return out, comp, dst, nil
}
