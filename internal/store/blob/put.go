package blob

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// Put stores the content of r and returns its hash and uncompressed size.
//
// The content is streamed: it is hashed and compressed chunk by chunk into a
// temporary file under <root>/tmp, which is fsynced and only then renamed to
// its final name. A process killed at any moment therefore leaves no partial
// file under a valid name; the temporary is removed by the next Open.
//
// If a readable copy of the blob already exists on either tier, the new copy
// is discarded. ctx is checked between chunks; a Read that blocks is not
// interrupted.
func (s *Store) Put(ctx context.Context, r io.Reader) (Hash, int64, error) {
	tmp, f, err := s.createTemp(filepath.Join(s.live, tmpDir), "put")
	if err != nil {
		return "", 0, fmt.Errorf("blob: put: create temporary: %w", err)
	}
	renamed := false
	defer func() {
		_ = f.Close() // no-op error after the explicit Close below
		if !renamed {
			_ = os.Remove(tmp)
		}
		s.untrack(tmp)
	}()

	buf := s.getBuf(s.chunkSize)[:s.chunkSize]
	comp := s.getBuf(0)
	defer func() { s.putBuf(buf); s.putBuf(comp) }()

	var (
		sum     = sha256.New()
		w       = bufio.NewWriterSize(f, 256<<10)
		offsets []int64
		crcs    []uint32
		total   int64
		pos     int64
	)
	for done := false; !done; {
		if err := ctx.Err(); err != nil {
			return "", 0, fmt.Errorf("blob: put: %w", err)
		}
		n, rerr := fillChunk(r, buf)
		if rerr == io.EOF {
			done = true
		} else if rerr != nil {
			return "", 0, fmt.Errorf("blob: put: read content: %w", rerr)
		}
		// An empty blob still gets one (empty) frame, so the file is a
		// regular zstd stream for any decoder.
		if n == 0 && len(offsets) > 0 {
			continue
		}
		if len(offsets) >= maxFrames {
			return "", 0, fmt.Errorf("blob: put: content needs more than %d frames of %d bytes", maxFrames, s.chunkSize)
		}
		chunk := buf[:n]
		sum.Write(chunk)
		comp = s.enc.EncodeAll(chunk, comp[:0])
		if _, err := w.Write(comp); err != nil {
			return "", 0, fmt.Errorf("blob: put: write %s: %w", tmp, err)
		}
		offsets = append(offsets, pos)
		crcs = append(crcs, crc32.Checksum(chunk, castagnoli))
		pos += int64(len(comp))
		total += int64(n)
	}
	if _, err := w.Write(appendIndex(nil, s.chunkSize, total, offsets, crcs)); err != nil {
		return "", 0, fmt.Errorf("blob: put: write %s: %w", tmp, err)
	}
	if err := w.Flush(); err != nil {
		return "", 0, fmt.Errorf("blob: put: write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		return "", 0, fmt.Errorf("blob: put: fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return "", 0, fmt.Errorf("blob: put: close %s: %w", tmp, err)
	}

	h := Hash(hex.EncodeToString(sum.Sum(nil)))
	final := blobPath(s.live, h)

	mu := s.lock(h)
	mu.Lock()
	defer mu.Unlock()

	// Deduplicate. A copy counts only if its index reads back with the same
	// length; a damaged live copy is replaced by the rename below.
	if usable(final, total) || (s.archive != "" && usable(blobPath(s.archive, h), total)) {
		return h, total, nil
	}
	if err := mkdirBlobDir(final); err != nil {
		return "", 0, fmt.Errorf("blob: put: %w", err)
	}
	// Rename semantics differ by platform. On POSIX, rename atomically
	// replaces an existing destination. On Windows, Go asks for replacement
	// too, but the call fails when the destination is open in a reader (a
	// sharing violation) and is not atomic on every filesystem. Since the
	// name is the content hash, an existing readable destination holds the
	// same bytes, so "already exists" in any form is success.
	if err := os.Rename(tmp, final); err != nil {
		if usable(final, total) {
			return h, total, nil
		}
		return "", 0, fmt.Errorf("blob: put: rename into place: %w", err)
	}
	renamed = true
	syncDir(filepath.Dir(final))
	return h, total, nil
}

// fillChunk reads until buf is full or r ends. It returns io.EOF only when r
// itself reported io.EOF, so a source failing with io.ErrUnexpectedEOF is
// not mistaken for a clean end.
func fillChunk(r io.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		if m > 0 {
			n += m
		}
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// usable reports whether path is a blob file whose index is intact and
// records the given uncompressed length.
func usable(path string, total int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	ix, err := readIndex(f, st.Size())
	return err == nil && ix.total == total
}
