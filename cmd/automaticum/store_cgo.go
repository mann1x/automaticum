//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/legacyimport"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/store/duck"
)

// storeFlags are the flags every command that opens the store shares.
type storeFlags struct{ data, archive *string }

func addStoreFlags(fs *flag.FlagSet) storeFlags {
	return storeFlags{
		data:    fs.String("data", os.Getenv("AUTOMATICUM_DATA"), "data root (blobs and state)"),
		archive: fs.String("archive", os.Getenv("AUTOMATICUM_ARCHIVE"), "archive root for old blobs (optional; must exist)"),
	}
}

func (f storeFlags) open(ctx context.Context) (*blob.Store, *duck.State, error) {
	if *f.data == "" {
		return nil, nil, errors.New("no data root: pass -data or set AUTOMATICUM_DATA")
	}
	if err := os.MkdirAll(*f.data, 0o755); err != nil {
		return nil, nil, err
	}
	blobs, err := blob.Open(blob.Options{Root: *f.data, ArchiveRoot: *f.archive})
	if err != nil {
		return nil, nil, err
	}
	st, err := duck.Open(ctx, filepath.Join(*f.data, "state.duckdb"))
	if err != nil {
		blobs.Close()
		return nil, nil, err
	}
	return blobs, st, nil
}

// sink writes an import through the store.
type sink struct {
	blobs *blob.Store
	store.State
}

func (s sink) PutBlob(ctx context.Context, r io.Reader) (string, int64, error) {
	h, n, err := s.blobs.Put(ctx, r)
	return string(h), n, err
}

func (s sink) ImportedFingerprint(ctx context.Context, runID string) (string, bool, error) {
	r, err := s.GetRun(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	fp := legacyimport.Fingerprint(r)
	return fp, fp != "", nil
}

// laneFlags collects the repeatable -lane name=path flag.
type laneFlags []legacyimport.Lane

func (l *laneFlags) String() string { return fmt.Sprint(len(*l), " lanes") }

func (l *laneFlags) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	if !ok || name == "" || path == "" {
		return errors.New("want name=path")
	}
	*l = append(*l, legacyimport.Lane{Name: name, Root: path})
	return nil
}

func legacyImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("legacy import", flag.ExitOnError)
	sf := addStoreFlags(fs)
	root := fs.String("legacy-root", os.Getenv("AUTOMATICUM_LEGACY_ROOT"), "the old harness's directory (read only)")
	state := fs.String("state-archive", os.Getenv("AUTOMATICUM_LEGACY_STATE_ARCHIVE"), "directory of archived session state (optional)")
	var lanes laneFlags
	fs.Var(&lanes, "lane", "a sibling lane of the old harness as name=path (read only; repeatable)")
	dry := fs.Bool("dry-run", false, "read and map everything, write nothing")
	meta := fs.Bool("metadata-only", false, "with -dry-run: skip event streams and state archives")
	workers := fs.Int("workers", 1, "runs processed at once")
	report := fs.String("report", "", "write the full report as JSON to this file")
	quiet := fs.Bool("quiet", false, "no line per run")
	fs.Parse(args)
	if *root == "" {
		return errors.New("no legacy root: pass -legacy-root or set AUTOMATICUM_LEGACY_ROOT")
	}
	opts := legacyimport.Options{LegacyRoot: *root, StateArchive: *state, Lanes: lanes, DryRun: *dry, MetadataOnly: *meta, Workers: *workers}
	if !*quiet {
		opts.Progress = func(r legacyimport.RunSummary) {
			fmt.Fprintf(os.Stderr, "%-9s %-8s %s\n", r.Status, r.Verdict, r.ID)
		}
	}
	var st *duck.State
	if !*dry {
		blobs, s, err := sf.open(ctx)
		if err != nil {
			return err
		}
		defer blobs.Close()
		defer s.Close()
		st = s
		opts.Sink = sink{blobs: blobs, State: s}
	}
	rep, err := legacyimport.Import(ctx, opts)
	if rep != nil {
		fmt.Print(rep)
		if *report != "" {
			if b, jerr := json.MarshalIndent(rep, "", "  "); jerr == nil {
				if werr := os.WriteFile(*report, b, 0o644); werr != nil {
					fmt.Fprintln(os.Stderr, "automaticum: report:", werr)
				}
			}
		}
	}
	if err != nil {
		return err
	}
	if st != nil {
		if err := st.Checkpoint(ctx); err != nil {
			return err
		}
	}
	ver, err := legacyimport.VerifyAgainstLegacy(rep, *root, lanes...)
	if err != nil {
		return err
	}
	fmt.Print(ver)
	if !ver.OK() {
		return errors.New("the old verdict counts were not reproduced")
	}
	if len(rep.Failed) > 0 {
		return fmt.Errorf("%d runs failed to import", len(rep.Failed))
	}
	return nil
}

func storeCheck(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("store check", flag.ExitOnError)
	sf := addStoreFlags(fs)
	fs.Parse(args)
	blobs, st, err := sf.open(ctx)
	if err != nil {
		return err
	}
	defer blobs.Close()
	defer st.Close()

	runs, err := st.CountRuns(ctx, store.RunFilter{})
	if err != nil {
		return err
	}
	rep, err := blobs.Check(ctx)
	if err != nil {
		return err
	}
	for _, table := range []string{"tool_calls", "findings"} {
		var n int64
		rows, err := st.Query(ctx, "SELECT count(*) FROM "+table)
		if err != nil {
			return err
		}
		if rows.Next() {
			err = rows.Scan(&n)
		}
		rows.Close()
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d\n", table, n)
	}
	fmt.Printf("runs: %s\nblobs verified: %s live, %s archived, %s uncompressed\n", human.Count(int64(runs)), human.Count(int64(rep.Live)), human.Count(int64(rep.Archived)), human.Bytes(int64(rep.Bytes)))
	for _, f := range rep.Corrupt {
		fmt.Printf("corrupt: %s (%s) %s: %v\n", f.Hash, f.Tier, f.Path, f.Err)
	}
	for _, p := range rep.StrayTemps {
		fmt.Println("stray temporary file:", p)
	}
	for _, p := range rep.StrayFiles {
		fmt.Println("stray file:", p)
	}
	if rep.ArchiveErr != nil {
		fmt.Println("archive not checked:", rep.ArchiveErr)
	}

	// Every reference must point at a blob one of the tiers holds.
	rows, err := st.Query(ctx, `SELECT DISTINCT hash FROM blob_refs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var refs, missing int
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return err
		}
		refs++
		ok, err := blobs.Has(blob.Hash(h))
		if err != nil {
			return err
		}
		if !ok {
			missing++
			fmt.Println("missing blob:", h)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Printf("distinct blobs referenced: %d, missing: %d\n", refs, missing)
	if !rep.OK() || missing > 0 {
		return errors.New("the store is not consistent")
	}
	fmt.Println("store check: OK")
	return nil
}
