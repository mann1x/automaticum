package blob

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// twoTiers opens a store with an existing archive root.
func twoTiers(t *testing.T, chunk int) *Store {
	t.Helper()
	dir := t.TempDir()
	arc := filepath.Join(dir, "archive")
	if err := os.Mkdir(arc, 0o755); err != nil {
		t.Fatal(err)
	}
	return open(t, Options{Root: filepath.Join(dir, "live"), ArchiveRoot: arc, ChunkSize: chunk})
}

// age sets the modification time of a live blob to d before now.
func age(t *testing.T, s *Store, h Hash, now time.Time, d time.Duration) {
	t.Helper()
	if err := os.Chtimes(blobPath(s.Root(), h), now.Add(-d), now.Add(-d)); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestArchiveMove(t *testing.T) {
	s := twoTiers(t, 512)
	now := time.Now()
	oldData, youngData := textBytes(51, 5000), textBytes(52, 5000)
	old, young := put(t, s, oldData), put(t, s, youngData)
	age(t, s, old, now, 48*time.Hour)
	age(t, s, young, now, time.Hour)
	live, arc := blobPath(s.Root(), old), blobPath(s.ArchiveRoot(), old)
	size := fileSize(t, live)

	// The order of the move, observed from inside it: the copy is verified
	// before it gets its name, and the live file outlives the rename.
	var stages []string
	s.hook = func(stage string) error {
		stages = append(stages, stage)
		switch stage {
		case "verified":
			if !exists(live) || exists(arc) {
				t.Errorf("at %q: live exists=%v, archive exists=%v; want true, false", stage, exists(live), exists(arc))
			}
			if tmps := tmpEntries(t, s.ArchiveRoot()); len(tmps) != 1 {
				t.Errorf("at %q: archive temporaries = %v, want one", stage, tmps)
			}
		case "renamed":
			if !exists(live) || !exists(arc) {
				t.Errorf("at %q: live exists=%v, archive exists=%v; want both", stage, exists(live), exists(arc))
			}
		}
		return nil
	}
	rep, err := s.ArchiveOlderThan(ctx, 24*time.Hour, now)
	s.hook = nil
	if err != nil {
		t.Fatalf("ArchiveOlderThan: %v", err)
	}
	if rep.Scanned != 2 || rep.Eligible != 1 || rep.Moved != 1 || rep.AlreadyArchived != 0 ||
		rep.BytesMoved != size || rep.BytesFreed != size || len(rep.Failed) != 0 {
		t.Fatalf("report = %+v (file is %d bytes)", rep, size)
	}
	if len(stages) != 2 || stages[0] != "verified" || stages[1] != "renamed" {
		t.Fatalf("stages = %v", stages)
	}
	if exists(live) || !exists(arc) {
		t.Fatalf("after the move: live exists=%v, archive exists=%v", exists(live), exists(arc))
	}
	if !exists(blobPath(s.Root(), young)) || exists(blobPath(s.ArchiveRoot(), young)) {
		t.Fatal("a blob younger than the age was moved")
	}
	if left := tmpEntries(t, s.ArchiveRoot()); len(left) != 0 {
		t.Fatalf("temporaries left on the archive: %v", left)
	}

	// Reads fall through to the archive tier and say so.
	got, info := readAll(t, s, old)
	if !bytes.Equal(got, oldData) || info.Tier != TierArchive || info.Size != 5000 || info.CompressedSize != size {
		t.Fatalf("Reader from archive: equal=%v, Info=%+v", bytes.Equal(got, oldData), info)
	}
	part, info, err := s.ReadRange(ctx, old, 500, 1500)
	if err != nil || !bytes.Equal(part, oldData[500:2000]) || info.Tier != TierArchive {
		t.Fatalf("ReadRange from archive: %v, Info=%+v", err, info)
	}
	if st, err := s.Stat(old); err != nil || st.Tier != TierArchive {
		t.Fatalf("Stat of an archived blob: %+v, %v", st, err)
	}
	if ok, err := s.Has(old); err != nil || !ok {
		t.Fatalf("Has of an archived blob: %v, %v", ok, err)
	}
	if err := s.Verify(ctx, old); err != nil {
		t.Fatalf("Verify of an archived blob: %v", err)
	}
	if _, info := readAll(t, s, young); info.Tier != TierLive {
		t.Fatalf("live blob served from %s", info.Tier)
	}

	// Storing archived content again deduplicates against the archive.
	put(t, s, oldData)
	if exists(live) {
		t.Fatal("Put of archived content created a live copy")
	}

	seen := map[Hash]Tier{}
	if err := s.Walk(ctx, func(h Hash, i Info) error { seen[h] = i.Tier; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[old] != TierArchive || seen[young] != TierLive {
		t.Fatalf("Walk saw %v", seen)
	}
	if rep, err := s.Check(ctx); err != nil || !rep.OK() || rep.Live != 1 || rep.Archived != 1 || rep.Bytes != 10000 {
		t.Fatalf("Check: %+v, %v", rep, err)
	}

	// A second pass has nothing to do.
	rep, err = s.ArchiveOlderThan(ctx, 24*time.Hour, now)
	if err != nil || rep.Scanned != 1 || rep.Eligible != 0 || rep.Moved != 0 {
		t.Fatalf("second pass: %+v, %v", rep, err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func TestArchiveRootMissing(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "not-mounted")
	s := open(t, Options{Root: filepath.Join(dir, "live"), ArchiveRoot: arc, ChunkSize: 512})
	now := time.Now()
	data := randBytes(61, 3000)
	h := put(t, s, data)
	age(t, s, h, now, 48*time.Hour)

	assertUntouched := func(what string) {
		t.Helper()
		got, info := readAll(t, s, h)
		if !bytes.Equal(got, data) || info.Tier != TierLive {
			t.Fatalf("%s: live blob not intact (tier %s)", what, info.Tier)
		}
	}
	rep, err := s.ArchiveOlderThan(ctx, time.Hour, now)
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("missing archive root: err = %v, want ErrArchiveUnavailable", err)
	}
	if rep.Moved != 0 || rep.BytesFreed != 0 || rep.AlreadyArchived != 0 {
		t.Fatalf("missing archive root: report = %+v", rep)
	}
	if exists(arc) {
		t.Fatal("the missing archive root was created")
	}
	assertUntouched("archive root missing")

	// A file where the directory should be is just as unreachable.
	if err := os.WriteFile(arc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveOlderThan(ctx, time.Hour, now); !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("archive root is a file: err = %v", err)
	}
	assertUntouched("archive root is a file")
	os.Remove(arc)

	// A missing blob is reported as possibly archived, not just absent.
	_, err = s.Stat(hashOf([]byte("elsewhere")))
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("missing blob with unreachable archive: %v", err)
	}
	// Walk and Check still cover the live tier and report the gap.
	n := 0
	if err := s.Walk(ctx, func(Hash, Info) error { n++; return nil }); n != 1 || !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("Walk with unreachable archive: %d blobs, %v", n, err)
	}
	crep, err := s.Check(ctx)
	if err != nil || crep.Live != 1 || !errors.Is(crep.ArchiveErr, ErrArchiveUnavailable) || crep.OK() {
		t.Fatalf("Check with unreachable archive: %+v, %v", crep, err)
	}

	// No archive configured at all.
	s2 := open(t, Options{Root: filepath.Join(dir, "live"), ChunkSize: 512})
	if _, err := s2.ArchiveOlderThan(ctx, time.Hour, now); !errors.Is(err, ErrNoArchive) {
		t.Fatalf("no archive root: err = %v, want ErrNoArchive", err)
	}
	assertUntouched("no archive root")

	// Once the archive root is there, the same store moves the blob.
	if err := os.Mkdir(arc, 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err = s.ArchiveOlderThan(ctx, time.Hour, now)
	if err != nil || rep.Moved != 1 {
		t.Fatalf("after the archive root appeared: %+v, %v", rep, err)
	}
	if _, info := readAll(t, s, h); info.Tier != TierArchive {
		t.Fatalf("served from %s, want archive", info.Tier)
	}
}

// A move interrupted at either point leaves the blob readable, and the next
// pass finishes the job.
func TestArchiveInterrupted(t *testing.T) {
	for _, stage := range []string{"verified", "renamed"} {
		t.Run("stopped after "+stage, func(t *testing.T) {
			s := twoTiers(t, 512)
			now := time.Now()
			data := randBytes(71, 4000)
			h := put(t, s, data)
			age(t, s, h, now, 48*time.Hour)
			live, arc := blobPath(s.Root(), h), blobPath(s.ArchiveRoot(), h)

			crash := errors.New("simulated crash")
			s.hook = func(st string) error {
				if st == stage {
					return crash
				}
				return nil
			}
			rep, err := s.ArchiveOlderThan(ctx, time.Hour, now)
			s.hook = nil
			if err != nil || rep.Moved != 0 || len(rep.Failed) != 1 || !errors.Is(rep.Failed[0].Err, crash) || rep.Failed[0].Hash != h {
				t.Fatalf("interrupted pass: %+v, %v", rep, err)
			}
			if !exists(live) {
				t.Fatal("the live copy was removed by an unfinished move")
			}
			if want := stage == "renamed"; exists(arc) != want {
				t.Fatalf("archive copy exists=%v, want %v", exists(arc), want)
			}
			// Present on both tiers (or only live): served from live.
			got, info := readAll(t, s, h)
			if !bytes.Equal(got, data) || info.Tier != TierLive {
				t.Fatalf("after interruption: equal=%v tier=%s", bytes.Equal(got, data), info.Tier)
			}
			if stage == "renamed" {
				tiers := 0
				if err := s.Walk(ctx, func(Hash, Info) error { tiers++; return nil }); err != nil || tiers != 2 {
					t.Fatalf("Walk reported the blob %d times (%v), want once per tier", tiers, err)
				}
				if c, err := s.Check(ctx); err != nil || !c.OK() || c.Live != 1 || c.Archived != 1 {
					t.Fatalf("Check with the blob on both tiers: %+v, %v", c, err)
				}
			}

			rep, err = s.ArchiveOlderThan(ctx, time.Hour, now)
			if err != nil || len(rep.Failed) != 0 || rep.Moved+rep.AlreadyArchived != 1 {
				t.Fatalf("next pass: %+v, %v", rep, err)
			}
			if stage == "renamed" && (rep.AlreadyArchived != 1 || rep.BytesMoved != 0) {
				t.Fatalf("next pass copied a blob that was already archived: %+v", rep)
			}
			if exists(live) || !exists(arc) {
				t.Fatalf("after the next pass: live exists=%v, archive exists=%v", exists(live), exists(arc))
			}
			if got, info := readAll(t, s, h); !bytes.Equal(got, data) || info.Tier != TierArchive {
				t.Fatalf("after the next pass: tier=%s", info.Tier)
			}
		})
	}
}

// A process killed during the copy leaves a temporary on the archive root;
// it is never mistaken for a blob and is removed by Open and by the next pass.
func TestArchiveLeftoverTemporary(t *testing.T) {
	s := twoTiers(t, 512)
	now := time.Now()
	data := randBytes(72, 4000)
	h := put(t, s, data)
	age(t, s, h, now, 48*time.Hour)
	full, err := os.ReadFile(blobPath(s.Root(), h))
	if err != nil {
		t.Fatal(err)
	}
	arcTmp := filepath.Join(s.ArchiveRoot(), tmpDir)
	if err := os.MkdirAll(arcTmp, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(arcTmp, "arc-0123456789abcdef"+tmpExt)
	if err := os.WriteFile(stale, full[:len(full)/3], 0o644); err != nil {
		t.Fatal(err)
	}
	if got, info := readAll(t, s, h); !bytes.Equal(got, data) || info.Tier != TierLive {
		t.Fatal("blob not readable from live with a half copy on the archive")
	}
	if c, err := s.Check(ctx); err != nil || len(c.StrayTemps) != 1 || c.StrayTemps[0] != stale {
		t.Fatalf("Check: %+v, %v", c, err)
	}
	s2 := open(t, Options{Root: s.Root(), ArchiveRoot: s.ArchiveRoot(), ChunkSize: 512})
	if left := tmpEntries(t, s.ArchiveRoot()); len(left) != 0 {
		t.Fatalf("Open left archive temporaries: %v", left)
	}
	if err := os.WriteFile(stale, full[:len(full)/3], 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := s2.ArchiveOlderThan(ctx, time.Hour, now)
	if err != nil || rep.Moved != 1 {
		t.Fatalf("pass with a leftover temporary: %+v, %v", rep, err)
	}
	if left := tmpEntries(t, s.ArchiveRoot()); len(left) != 0 {
		t.Fatalf("the pass left archive temporaries: %v", left)
	}
}

// A live blob that does not verify is not archived and not deleted.
func TestArchiveRefusesCorruptSource(t *testing.T) {
	s := twoTiers(t, 512)
	now := time.Now()
	good, bad := put(t, s, randBytes(81, 4000)), put(t, s, randBytes(82, 4000))
	path := blobPath(s.Root(), bad)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[700] ^= 0xff // inside the second frame
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	age(t, s, good, now, 48*time.Hour)
	age(t, s, bad, now, 48*time.Hour)

	rep, err := s.ArchiveOlderThan(ctx, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Eligible != 2 || rep.Moved != 1 || len(rep.Failed) != 1 || rep.Failed[0].Hash != bad || !errors.Is(rep.Failed[0].Err, ErrCorrupt) {
		t.Fatalf("report = %+v", rep)
	}
	if !exists(path) || exists(blobPath(s.ArchiveRoot(), bad)) {
		t.Fatal("a blob that failed verification was moved or deleted")
	}
	if left := tmpEntries(t, s.ArchiveRoot()); len(left) != 0 {
		t.Fatalf("failed copy left on the archive: %v", left)
	}
	if exists(blobPath(s.Root(), good)) {
		t.Fatal("the good blob was not moved")
	}
}

// With a blob on both tiers the live copy serves; when the live copy's
// index is destroyed, the archive copy still does. A damaged archive copy is
// replaced from live rather than trusted.
func TestBothTiers(t *testing.T) {
	s := twoTiers(t, 512)
	now := time.Now()
	data := randBytes(91, 4000)
	h := put(t, s, data)
	live, arc := blobPath(s.Root(), h), blobPath(s.ArchiveRoot(), h)
	raw, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(arc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arc, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, info := readAll(t, s, h); !bytes.Equal(got, data) || info.Tier != TierLive {
		t.Fatalf("both tiers: served from %s", info.Tier)
	}

	if err := os.Truncate(live, int64(len(raw)-5)); err != nil {
		t.Fatal(err)
	}
	if got, info := readAll(t, s, h); !bytes.Equal(got, data) || info.Tier != TierArchive {
		t.Fatalf("live copy truncated: served from %s", info.Tier)
	}
	if c, err := s.Check(ctx); err != nil || len(c.Corrupt) != 1 || c.Corrupt[0].Tier != TierLive || c.Archived != 1 {
		t.Fatalf("Check: %+v, %v", c, err)
	}

	// Now the other way round: good live copy, damaged archive copy.
	if err := os.WriteFile(live, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(arc, int64(len(raw)/2)); err != nil {
		t.Fatal(err)
	}
	age(t, s, h, now, 48*time.Hour)
	rep, err := s.ArchiveOlderThan(ctx, time.Hour, now)
	if err != nil || rep.Moved != 1 || rep.AlreadyArchived != 0 || len(rep.Failed) != 0 {
		t.Fatalf("archive over a damaged archive copy: %+v, %v", rep, err)
	}
	if exists(live) {
		t.Fatal("live copy still there")
	}
	if err := s.Verify(ctx, h); err != nil {
		t.Fatalf("archive copy after replacement: %v", err)
	}
}

func TestCheckFindsProblemsOnBothTiers(t *testing.T) {
	s := twoTiers(t, 512)
	now := time.Now()
	var hs []Hash
	for i := range 6 {
		hs = append(hs, put(t, s, randBytes(uint64(200+i), 3000+i)))
	}
	for _, h := range hs[:3] {
		age(t, s, h, now, 48*time.Hour)
	}
	if rep, err := s.ArchiveOlderThan(ctx, time.Hour, now); err != nil || rep.Moved != 3 {
		t.Fatalf("archive: %+v, %v", rep, err)
	}
	if c, err := s.Check(ctx); err != nil || !c.OK() || c.Live != 3 || c.Archived != 3 {
		t.Fatalf("clean Check: %+v, %v", c, err)
	}

	flip := func(path string) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		raw[len(raw)/4] ^= 0x40
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	flip(blobPath(s.ArchiveRoot(), hs[0]))
	flip(blobPath(s.Root(), hs[4]))
	// A blob under another blob's name: intact file, wrong content.
	wrong := blobPath(s.Root(), hs[5])
	raw, err := os.ReadFile(blobPath(s.Root(), hs[3]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrong, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	strayFile := filepath.Join(filepath.Dir(blobPath(s.Root(), hs[3])), "notes.txt")
	strayTmp := filepath.Join(s.Root(), tmpDir, "put-feedfacefeedface"+tmpExt)
	for _, p := range []string{strayFile, strayTmp} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c, err := s.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[Hash]Tier{}
	for _, f := range c.Corrupt {
		if !errors.Is(f.Err, ErrCorrupt) || f.Path == "" {
			t.Errorf("corrupt entry %+v", f)
		}
		got[f.Hash] = f.Tier
	}
	if len(got) != 3 || got[hs[0]] != TierArchive || got[hs[4]] != TierLive || got[hs[5]] != TierLive {
		t.Fatalf("corrupt blobs = %v", got)
	}
	if c.Live != 1 || c.Archived != 2 || c.OK() {
		t.Fatalf("Check counts: %+v", c)
	}
	if len(c.StrayFiles) != 1 || c.StrayFiles[0] != strayFile || len(c.StrayTemps) != 1 || c.StrayTemps[0] != strayTmp {
		t.Fatalf("strays: files %v, temporaries %v", c.StrayFiles, c.StrayTemps)
	}
	if err := s.Verify(ctx, hs[5]); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Verify of a blob under the wrong name: %v", err)
	}
	// Walk reads indexes only: it sees all six and stops when told to.
	n := 0
	if err := s.Walk(ctx, func(Hash, Info) error { n++; return nil }); err != nil || n != 6 {
		t.Fatalf("Walk: %d blobs, %v", n, err)
	}
	stop := errors.New("stop")
	n = 0
	if err := s.Walk(ctx, func(Hash, Info) error { n++; return stop }); !errors.Is(err, stop) || n != 1 {
		t.Fatalf("Walk with a failing callback: %d calls, %v", n, err)
	}
}
