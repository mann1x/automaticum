package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ArchiveReport is the outcome of one ArchiveOlderThan pass.
type ArchiveReport struct {
	Scanned         int   // live blobs looked at
	Eligible        int   // of those, older than the age
	Moved           int   // copied to the archive, verified there, freed on live
	AlreadyArchived int   // a verified archive copy existed; only the live copy was freed
	BytesMoved      int64 // compressed bytes copied to the archive
	BytesFreed      int64 // compressed bytes removed from the live root
	// Failed lists the blobs that could not be moved. Each is still
	// readable from the live tier.
	Failed []Failure
}

// ArchiveOlderThan moves every live blob whose modification time is more
// than age before now to the archive root.
//
// Each blob is copied to a temporary name on the archive root, fsynced,
// verified there by decompressing the copy and comparing its sha256 with the
// name, renamed into place, and only then removed from the live root. A move
// interrupted at any point leaves the blob readable from at least one tier;
// a blob left on both is finished by the next pass.
//
// If the archive root is not configured (ErrNoArchive) or not reachable
// (ErrArchiveUnavailable), nothing is moved, nothing is deleted, and the
// error is returned. Failures of single blobs are listed in the report and
// do not stop the pass or make it return an error.
func (s *Store) ArchiveOlderThan(ctx context.Context, age time.Duration, now time.Time) (ArchiveReport, error) {
	var rep ArchiveReport
	s.archiveMu.Lock()
	defer s.archiveMu.Unlock()

	if err := s.prepareArchive(); err != nil {
		return rep, fmt.Errorf("blob: archive: nothing moved: %w", err)
	}
	err := walkTier(ctx, s.live, TierLive, func(e entry) error {
		if e.stray {
			return nil
		}
		st, err := os.Stat(e.path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			rep.Failed = append(rep.Failed, Failure{Hash: e.hash, Tier: TierLive, Path: e.path, Err: err})
			return nil
		}
		rep.Scanned++
		if now.Sub(st.ModTime()) <= age {
			return nil
		}
		rep.Eligible++
		if err := s.archiveOne(ctx, e.hash, e.path, st, &rep); err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			rep.Failed = append(rep.Failed, Failure{Hash: e.hash, Tier: TierLive, Path: e.path, Err: err})
		}
		return nil
	})
	if err != nil {
		return rep, fmt.Errorf("blob: archive: %w", err)
	}
	return rep, nil
}

// prepareArchive makes sure the archive root can take blobs: it exists, its
// working directories can be created, and a file can be written. Leftover
// temporaries of an interrupted pass are removed.
func (s *Store) prepareArchive() error {
	if err := s.archiveReachable(); err != nil {
		return err
	}
	tmp := filepath.Join(s.archive, tmpDir)
	for _, d := range []string{filepath.Join(s.archive, blobsDir), tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("%w: %v", ErrArchiveUnavailable, err)
		}
	}
	if err := removeTemps(tmp); err != nil {
		return fmt.Errorf("%w: %v", ErrArchiveUnavailable, err)
	}
	probe, f, err := s.createTemp(tmp, "probe")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArchiveUnavailable, err)
	}
	f.Close()
	err = os.Remove(probe)
	s.untrack(probe)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArchiveUnavailable, err)
	}
	return nil
}

// archiveOne moves one live blob. The live file is removed only after a
// verified copy sits under its final name on the archive root.
func (s *Store) archiveOne(ctx context.Context, h Hash, src string, st fs.FileInfo, rep *ArchiveReport) error {
	dst := blobPath(s.archive, h)

	// Left on both tiers by an interrupted move, or archived and stored
	// again since: free the live copy if the archive copy verifies.
	if _, err := os.Stat(dst); err == nil {
		if _, verr := s.verifyPath(ctx, dst, h, TierArchive); verr == nil {
			if err := s.removeLive(src); err != nil {
				return err
			}
			rep.AlreadyArchived++
			rep.BytesFreed += st.Size()
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		// The archive copy is damaged: replace it from the live one.
	}

	tmp, out, err := s.createTemp(filepath.Join(s.archive, tmpDir), "arc")
	if err != nil {
		return fmt.Errorf("create temporary on archive: %w", err)
	}
	renamed := false
	defer func() {
		_ = out.Close()
		if !renamed {
			_ = os.Remove(tmp)
		}
		s.untrack(tmp)
	}()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, ctxReader{ctx, in})
	in.Close()
	if err != nil {
		return fmt.Errorf("copy to %s: %w", tmp, err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	// Keep the blob's age visible on the archive. Not essential.
	_ = os.Chtimes(tmp, st.ModTime(), st.ModTime())

	// Verify what the destination holds, by reading it back.
	if _, err := s.verifyPath(ctx, tmp, h, TierArchive); err != nil {
		return fmt.Errorf("verify copy at destination: %w", err)
	}
	if err := s.callHook("verified"); err != nil {
		return err
	}
	if err := mkdirBlobDir(dst); err != nil {
		return err
	}
	// As in Put: the name is the content hash, so a readable file already
	// at the destination (Windows refusing to replace an open file) holds
	// the same bytes; only then is a failed rename acceptable.
	if err := os.Rename(tmp, dst); err != nil {
		if _, verr := s.verifyPath(ctx, dst, h, TierArchive); verr != nil {
			return fmt.Errorf("rename into place on archive: %w", err)
		}
	} else {
		renamed = true
	}
	syncDir(filepath.Dir(dst))
	if err := s.callHook("renamed"); err != nil {
		return err
	}
	if err := s.removeLive(src); err != nil {
		return err
	}
	rep.Moved++
	rep.BytesMoved += n
	rep.BytesFreed += st.Size()
	return nil
}

// removeLive frees a live file whose content is safe on the archive. On
// Windows this fails while a reader has the file open; the blob then stays
// on both tiers and the next pass frees it.
func (s *Store) removeLive(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("archived, but the live copy could not be removed: %w", err)
	}
	syncDir(filepath.Dir(path))
	return nil
}

func (s *Store) callHook(stage string) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(stage)
}

// ctxReader stops a copy when its context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
