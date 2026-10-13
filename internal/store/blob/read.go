package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// blobFile is an open blob file with its index.
type blobFile struct {
	f    *os.File
	ix   *index
	info Info
	path string
}

// openBlobFile opens one file as blob h. A missing file is reported with
// fs.ErrNotExist, a bad index with ErrCorrupt.
func openBlobFile(path string, h Hash, tier Tier) (*blobFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	ix, err := readIndex(f, st.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &blobFile{f: f, ix: ix, path: path, info: Info{
		Hash:           h,
		Size:           ix.total,
		CompressedSize: st.Size(),
		ChunkSize:      int(ix.chunkSize),
		Tier:           tier,
		ModTime:        st.ModTime(),
	}}, nil
}

// open resolves the tier of blob h: live first, then archive. A live copy
// whose index cannot be read does not hide a good archive copy.
func (s *Store) open(h Hash) (*blobFile, error) {
	if !h.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidHash, string(h))
	}
	livePath := blobPath(s.live, h)
	bf, liveErr := openBlobFile(livePath, h, TierLive)
	if liveErr == nil {
		return bf, nil
	}
	if s.archive == "" {
		if errors.Is(liveErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s (expected at %s)", ErrNotFound, h, livePath)
		}
		return nil, fmt.Errorf("blob %s: %w", h, liveErr)
	}
	arcPath := blobPath(s.archive, h)
	bf, arcErr := openBlobFile(arcPath, h, TierArchive)
	if arcErr == nil {
		return bf, nil
	}
	switch {
	case !errors.Is(liveErr, fs.ErrNotExist):
		return nil, fmt.Errorf("blob %s: %w", h, liveErr)
	case !errors.Is(arcErr, fs.ErrNotExist):
		return nil, fmt.Errorf("blob %s: not on the live tier; archive copy: %w", h, arcErr)
	}
	// Absent from both. Say so when the archive itself could not be
	// looked at: the blob may well be there (spec 080 §5.1).
	if rerr := s.archiveReachable(); rerr != nil {
		return nil, fmt.Errorf("%w: %s (not at %s; archive copy expected at %s could not be looked for: %w)",
			ErrNotFound, h, livePath, arcPath, rerr)
	}
	return nil, fmt.Errorf("%w: %s (expected at %s or %s)", ErrNotFound, h, livePath, arcPath)
}

// Stat describes blob h on the tier that would serve a read. A missing blob
// returns an error wrapping ErrNotFound.
func (s *Store) Stat(h Hash) (Info, error) {
	bf, err := s.open(h)
	if err != nil {
		return Info{}, err
	}
	bf.f.Close()
	return bf.info, nil
}

// Has reports whether a file for blob h exists on either tier. It does not
// read the file; use Stat or Verify to learn whether it is intact.
func (s *Store) Has(h Hash) (bool, error) {
	if !h.Valid() {
		return false, fmt.Errorf("%w: %q", ErrInvalidHash, string(h))
	}
	roots := []string{s.live}
	if s.archive != "" {
		roots = append(roots, s.archive)
	}
	for _, root := range roots {
		_, err := os.Stat(blobPath(root, h))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("blob %s: %w", h, err)
		}
	}
	return false, nil
}

// Reader opens blob h for reading its whole content from the start. Info
// says which tier serves it. The reader checks every chunk against the
// index and fails with an error wrapping ErrCorrupt rather than return
// damaged bytes; it does not check the sha256 (Verify does). It must be
// closed and is not safe for concurrent use.
func (s *Store) Reader(ctx context.Context, h Hash) (io.ReadCloser, Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, fmt.Errorf("blob %s: %w", h, err)
	}
	bf, err := s.open(h)
	if err != nil {
		return nil, Info{}, err
	}
	return &reader{s: s, ctx: ctx, bf: bf, comp: s.getBuf(0), buf: s.getBuf(int(min(bf.ix.chunkSize, max(bf.ix.total, 1))))}, bf.info, nil
}

type reader struct {
	s    *Store
	ctx  context.Context
	bf   *blobFile
	next int    // next chunk to decode
	cur  []byte // undelivered part of the current chunk
	comp []byte
	buf  []byte
	err  error
	done bool
}

func (r *reader) Read(p []byte) (int, error) {
	if r.done {
		return 0, fs.ErrClosed
	}
	if len(p) == 0 {
		return 0, r.err
	}
	for len(r.cur) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		if r.next >= r.bf.ix.frames() {
			r.err = io.EOF
			return 0, io.EOF
		}
		if err := r.ctx.Err(); err != nil {
			return 0, fmt.Errorf("blob %s: %w", r.bf.info.Hash, err)
		}
		var err error
		r.cur, r.comp, r.buf, err = readChunk(r.s.dec, r.bf.f, r.bf.ix, r.next, r.comp, r.buf)
		if err != nil {
			r.err = fmt.Errorf("blob %s (%s): %w", r.bf.info.Hash, r.bf.path, err)
			return 0, r.err
		}
		r.next++
	}
	n := copy(p, r.cur)
	r.cur = r.cur[n:]
	return n, nil
}

func (r *reader) Close() error {
	if r.done {
		return nil
	}
	r.done = true
	r.cur = nil
	r.s.putBuf(r.comp)
	r.s.putBuf(r.buf)
	return r.bf.f.Close()
}

// ReadRange returns up to n bytes of blob h starting at uncompressed offset
// off, decompressing only the frames that overlap the range. The range is
// clipped to the blob: fewer than n bytes come back when it reaches past the
// end, and none (with a nil error) when off is at or past the end. Negative
// off or n is an error.
func (s *Store) ReadRange(ctx context.Context, h Hash, off, n int64) ([]byte, Info, error) {
	if off < 0 || n < 0 {
		return nil, Info{}, fmt.Errorf("blob %s: read range: negative offset %d or length %d", h, off, n)
	}
	bf, err := s.open(h)
	if err != nil {
		return nil, Info{}, err
	}
	defer bf.f.Close()
	ix := bf.ix
	if off >= ix.total || n == 0 {
		return []byte{}, bf.info, nil
	}
	n = min(n, ix.total-off)
	out := make([]byte, 0, n)
	comp, buf := s.getBuf(0), s.getBuf(int(min(ix.chunkSize, ix.total)))
	defer func() { s.putBuf(comp); s.putBuf(buf) }()
	for i := int(off / ix.chunkSize); int64(len(out)) < n; i++ {
		if err := ctx.Err(); err != nil {
			return nil, bf.info, fmt.Errorf("blob %s: read range: %w", h, err)
		}
		var chunk []byte
		chunk, comp, buf, err = readChunk(s.dec, bf.f, ix, i, comp, buf)
		if err != nil {
			return nil, bf.info, fmt.Errorf("blob %s (%s): %w", h, bf.path, err)
		}
		start := max(off-int64(i)*ix.chunkSize, 0)
		end := min(int64(len(chunk)), start+n-int64(len(out)))
		out = append(out, chunk[start:end]...)
	}
	return out, bf.info, nil
}

// Verify decompresses blob h completely, on the tier that serves reads, and
// compares the sha256 of the content with h. A mismatch or any damage
// returns an error wrapping ErrCorrupt; a missing blob wraps ErrNotFound.
func (s *Store) Verify(ctx context.Context, h Hash) error {
	bf, err := s.open(h)
	if err != nil {
		return err
	}
	defer bf.f.Close()
	return s.verifyOpen(ctx, bf)
}

// verifyPath verifies one file as blob h, whatever tier it is on.
func (s *Store) verifyPath(ctx context.Context, path string, h Hash, tier Tier) (Info, error) {
	bf, err := openBlobFile(path, h, tier)
	if err != nil {
		return Info{}, fmt.Errorf("blob %s: %w", h, err)
	}
	defer bf.f.Close()
	return bf.info, s.verifyOpen(ctx, bf)
}

func (s *Store) verifyOpen(ctx context.Context, bf *blobFile) error {
	h := bf.info.Hash
	sum := sha256.New()
	comp, buf := s.getBuf(0), s.getBuf(int(min(bf.ix.chunkSize, max(bf.ix.total, 1))))
	defer func() { s.putBuf(comp); s.putBuf(buf) }()
	for i := 0; i < bf.ix.frames(); i++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("blob %s: verify: %w", h, err)
		}
		var chunk []byte
		var err error
		chunk, comp, buf, err = readChunk(s.dec, bf.f, bf.ix, i, comp, buf)
		if err != nil {
			return fmt.Errorf("blob %s (%s): %w", h, bf.path, err)
		}
		sum.Write(chunk)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != string(h) {
		return fmt.Errorf("blob %s (%s): %w: content hashes to %s", h, bf.path, ErrCorrupt, got)
	}
	return nil
}
