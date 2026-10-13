package blob

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// entry is one thing found under a tier's blobs directory.
type entry struct {
	hash  Hash
	path  string
	tier  Tier
	stray bool // not a blob under its proper name and directory
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	return Hash(s + strings.Repeat("0", 64-n)).Valid()
}

// walkTier calls fn for everything under root's blobs directory, in hash
// order. A root without a blobs directory is empty.
func walkTier(ctx context.Context, root string, tier Tier, fn func(entry) error) error {
	base := filepath.Join(root, blobsDir)
	l1, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, d1 := range l1 {
		p1 := filepath.Join(base, d1.Name())
		if !d1.IsDir() || !isHex(d1.Name(), 2) {
			if err := fn(entry{path: p1, tier: tier, stray: true}); err != nil {
				return err
			}
			continue
		}
		l2, err := os.ReadDir(p1)
		if err != nil {
			return err
		}
		for _, d2 := range l2 {
			p2 := filepath.Join(p1, d2.Name())
			if !d2.IsDir() || !isHex(d2.Name(), 2) {
				if err := fn(entry{path: p2, tier: tier, stray: true}); err != nil {
					return err
				}
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			files, err := os.ReadDir(p2)
			if err != nil {
				return err
			}
			for _, f := range files {
				e := entry{path: filepath.Join(p2, f.Name()), tier: tier}
				h := Hash(strings.TrimSuffix(f.Name(), blobExt))
				if f.IsDir() || !strings.HasSuffix(f.Name(), blobExt) || !h.Valid() ||
					string(h[:2]) != d1.Name() || string(h[2:4]) != d2.Name() {
					e.stray = true
				} else {
					e.hash = h
				}
				if err := fn(e); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// tiers lists the roots to walk and whether the archive could not be.
func (s *Store) tiers() (roots []string, tiers []Tier, archiveErr error) {
	roots, tiers = []string{s.live}, []Tier{TierLive}
	if s.archive == "" {
		return roots, tiers, nil
	}
	if err := s.archiveReachable(); err != nil {
		return roots, tiers, err
	}
	return append(roots, s.archive), append(tiers, TierArchive), nil
}

// Walk calls fn for every blob, the live tier first, then the archive tier,
// each in hash order. A blob present on both tiers is reported once per
// tier, with Info.Tier telling them apart. Walk reads each blob's index but
// not its content.
//
// An error from fn stops the walk and is returned. Blobs whose index cannot
// be read are not passed to fn; the walk continues, and Walk then returns an
// error joining one ErrCorrupt per such blob. The same goes for a configured
// archive root that cannot be reached (ErrArchiveUnavailable): the live tier
// is still walked.
func (s *Store) Walk(ctx context.Context, fn func(Hash, Info) error) error {
	roots, tiers, archiveErr := s.tiers()
	var bad []error
	if archiveErr != nil {
		bad = append(bad, fmt.Errorf("blob: walk: archive tier skipped: %w", archiveErr))
	}
	for i, root := range roots {
		err := walkTier(ctx, root, tiers[i], func(e entry) error {
			if e.stray {
				return nil
			}
			bf, err := openBlobFile(e.path, e.hash, e.tier)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) { // gone: archived or replaced meanwhile
					bad = append(bad, fmt.Errorf("blob %s (%s tier): %w", e.hash, e.tier, err))
				}
				return nil
			}
			bf.f.Close()
			return fn(e.hash, bf.info)
		})
		if err != nil {
			return err
		}
	}
	return errors.Join(bad...)
}

// CheckReport is the outcome of Check.
type CheckReport struct {
	Live     int   // live blobs that verified
	Archived int   // archive blobs that verified
	Bytes    int64 // uncompressed bytes verified
	// Corrupt lists blobs that failed verification, with tier and path.
	Corrupt []Failure
	// StrayTemps lists temporary files no running operation of this
	// process owns: leftovers of a crash, removed by the next Open.
	StrayTemps []string
	// StrayFiles lists files under blobs/ that are not blobs under their
	// proper name.
	StrayFiles []string
	// ArchiveErr is set when an archive root is configured but could not
	// be checked. The live tier was still checked.
	ArchiveErr error
}

// OK reports whether the check found nothing wrong on the tiers it could
// reach and could reach every configured tier.
func (r CheckReport) OK() bool {
	return len(r.Corrupt) == 0 && len(r.StrayTemps) == 0 && len(r.StrayFiles) == 0 && r.ArchiveErr == nil
}

// Check verifies every blob on both tiers (full decompression and sha256)
// and lists corrupt blobs, stray files and stray temporaries. Findings go in
// the report; the error is for a check that could not run to the end.
func (s *Store) Check(ctx context.Context) (CheckReport, error) {
	var rep CheckReport
	roots, tiers, archiveErr := s.tiers()
	rep.ArchiveErr = archiveErr
	for i, root := range roots {
		tier := tiers[i]
		err := walkTier(ctx, root, tier, func(e entry) error {
			if e.stray {
				rep.StrayFiles = append(rep.StrayFiles, e.path)
				return nil
			}
			info, err := s.verifyPath(ctx, e.path, e.hash, tier)
			switch {
			case err == nil:
				if tier == TierLive {
					rep.Live++
				} else {
					rep.Archived++
				}
				rep.Bytes += info.Size
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, fs.ErrNotExist): // gone: archived or replaced meanwhile
			default:
				rep.Corrupt = append(rep.Corrupt, Failure{Hash: e.hash, Tier: tier, Path: e.path, Err: err})
			}
			return nil
		})
		if err != nil {
			return rep, fmt.Errorf("blob: check %s tier: %w", tier, err)
		}
		tmp := filepath.Join(root, tmpDir)
		ents, err := os.ReadDir(tmp)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return rep, fmt.Errorf("blob: check %s tier: %w", tier, err)
		}
		for _, e := range ents {
			p := filepath.Join(tmp, e.Name())
			if s.isInflight(p) {
				continue
			}
			// A temporary is untracked only after it is renamed or
			// removed, so one that finished since the listing is gone.
			if _, err := os.Lstat(p); err == nil {
				rep.StrayTemps = append(rep.StrayTemps, p)
			}
		}
	}
	return rep, nil
}
