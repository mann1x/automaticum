package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var ctx = context.Background()

// randBytes returns n deterministic incompressible bytes.
func randBytes(seed uint64, n int) []byte {
	b := make([]byte, n)
	r := rand.NewChaCha8([32]byte{byte(seed), byte(seed >> 8), byte(seed >> 16), 1})
	_, _ = r.Read(b)
	return b
}

// textBytes returns n deterministic bytes that look like an event stream
// and compress about as well as one.
func textBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 7))
	words := []string{"tool_call", "read_file", "assistant", "content", "delta", "stream", "error", "result",
		"path", "internal/store/blob", "tokens", "usage", "timeout", "retry", "model", "finish_reason"}
	var b bytes.Buffer
	b.Grow(n + 256)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, `{"seq":%d,"ts":%d,"type":%q,"text":"`, i, 1760000000000+int64(i)*37, words[r.IntN(len(words))])
		for range 4 + r.IntN(12) {
			b.WriteString(words[r.IntN(len(words))])
			b.WriteByte(' ')
			fmt.Fprintf(&b, "%x ", r.Uint32())
		}
		b.WriteString("\"}\n")
	}
	return b.Bytes()[:n]
}

func hashOf(b []byte) Hash {
	s := sha256.Sum256(b)
	return Hash(hex.EncodeToString(s[:]))
}

func open(t testing.TB, o Options) *Store {
	t.Helper()
	if o.Root == "" {
		o.Root = filepath.Join(t.TempDir(), "live")
	}
	s, err := Open(o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func put(t testing.TB, s *Store, data []byte) Hash {
	t.Helper()
	h, n, err := s.Put(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("Put size = %d, want %d", n, len(data))
	}
	if want := hashOf(data); h != want {
		t.Fatalf("Put hash = %s, want %s", h, want)
	}
	return h
}

func readAll(t testing.TB, s *Store, h Hash) ([]byte, Info) {
	t.Helper()
	rc, info, err := s.Reader(ctx, h)
	if err != nil {
		t.Fatalf("Reader: %v", err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return b, info
}

func tmpEntries(t testing.TB, root string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(root, tmpDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestRoundTrip(t *testing.T) {
	const chunk = 1024
	s := open(t, Options{ChunkSize: chunk})
	cases := map[string]int{
		"empty": 0, "one byte": 1, "small": 300, "one short of a chunk": chunk - 1,
		"exactly one chunk": chunk, "one over a chunk": chunk + 1, "exactly three chunks": 3 * chunk,
		"multi-chunk odd": 7*chunk + 13, "prime": 10007,
	}
	for name, size := range cases {
		for _, kind := range []string{"random", "text"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				data := randBytes(uint64(size), size)
				if kind == "text" {
					data = textBytes(uint64(size), size)
				}
				h := put(t, s, data)
				got, info := readAll(t, s, h)
				if !bytes.Equal(got, data) {
					t.Fatalf("content differs: got %d bytes, want %d", len(got), len(data))
				}
				st, err := os.Stat(blobPath(s.Root(), h))
				if err != nil {
					t.Fatalf("blob not at its layout path: %v", err)
				}
				want := Info{Hash: h, Size: int64(size), CompressedSize: st.Size(), ChunkSize: chunk, Tier: TierLive, ModTime: info.ModTime}
				if info != want {
					t.Fatalf("Info = %+v, want %+v", info, want)
				}
				if si, err := s.Stat(h); err != nil || si != want {
					t.Fatalf("Stat = %+v, %v", si, err)
				}
				if ok, err := s.Has(h); err != nil || !ok {
					t.Fatalf("Has = %v, %v", ok, err)
				}
				if err := s.Verify(ctx, h); err != nil {
					t.Fatalf("Verify: %v", err)
				}
				all, _, err := s.ReadRange(ctx, h, 0, int64(size)+10)
				if err != nil || !bytes.Equal(all, data) {
					t.Fatalf("ReadRange of everything: %d bytes, %v", len(all), err)
				}
			})
		}
	}
	if left := tmpEntries(t, s.Root()); len(left) != 0 {
		t.Fatalf("temporaries left after Puts: %v", left)
	}
}

func TestDefaultChunkSize(t *testing.T) {
	s := open(t, Options{})
	data := textBytes(1, DefaultChunkSize+DefaultChunkSize/2)
	h := put(t, s, data)
	got, info := readAll(t, s, h)
	if !bytes.Equal(got, data) {
		t.Fatal("content differs")
	}
	if info.ChunkSize != DefaultChunkSize || info.CompressedSize >= info.Size {
		t.Fatalf("Info = %+v: want default chunk size and a smaller file than content", info)
	}
}

// A blob keeps the chunk size it was written with, whatever the store is
// reopened with.
func TestReopenWithOtherChunkSize(t *testing.T) {
	root := filepath.Join(t.TempDir(), "live")
	data := randBytes(3, 5000)
	s1 := open(t, Options{Root: root, ChunkSize: 512})
	h := put(t, s1, data)
	s2 := open(t, Options{Root: root, ChunkSize: 4096, Level: 19})
	got, info := readAll(t, s2, h)
	if !bytes.Equal(got, data) || info.ChunkSize != 512 {
		t.Fatalf("after reopen: equal=%v info=%+v", bytes.Equal(got, data), info)
	}
	b, _, err := s2.ReadRange(ctx, h, 500, 1000)
	if err != nil || !bytes.Equal(b, data[500:1500]) {
		t.Fatalf("ReadRange after reopen: %v", err)
	}
}

func TestReadRange(t *testing.T) {
	const chunk, size = 1000, 4500
	s := open(t, Options{ChunkSize: chunk})
	data := randBytes(9, size)
	h := put(t, s, data)

	check := func(off, n int64) {
		t.Helper()
		got, info, err := s.ReadRange(ctx, h, off, n)
		if err != nil {
			t.Fatalf("ReadRange(%d,%d): %v", off, n, err)
		}
		lo, hi := min(off, size), min(off+n, size)
		if !bytes.Equal(got, data[lo:hi]) {
			t.Fatalf("ReadRange(%d,%d): got %d bytes, want data[%d:%d]", off, n, len(got), lo, hi)
		}
		if info.Size != size || info.Tier != TierLive {
			t.Fatalf("ReadRange(%d,%d): Info = %+v", off, n, info)
		}
	}
	named := []struct {
		name   string
		off, n int64
	}{
		{"start of blob", 0, 10},
		{"whole first chunk", 0, chunk},
		{"ends at a boundary", 500, 500},
		{"starts at a boundary", chunk, 10},
		{"exactly the second chunk", chunk, chunk},
		{"last byte of a chunk", chunk - 1, 1},
		{"straddles one boundary", chunk - 1, 2},
		{"across three chunks", chunk - 5, chunk + 10},
		{"inside the short last chunk", 4100, 100},
		{"up to the end", 4400, 100},
		{"runs past the end", 4400, 1000},
		{"last byte", size - 1, 1},
		{"at the end", size, 10},
		{"past the end", size + 500, 10},
		{"zero length", 123, 0},
		{"everything", 0, size},
		{"more than everything", 0, 1 << 40},
	}
	for _, c := range named {
		t.Run(c.name, func(t *testing.T) { check(c.off, c.n) })
	}
	t.Run("sweep", func(t *testing.T) {
		for off := int64(0); off <= size+3; off += 97 {
			for _, n := range []int64{0, 1, 3, 999, 1000, 1001, 2500, size} {
				check(off, n)
			}
		}
	})
	t.Run("negative", func(t *testing.T) {
		if _, _, err := s.ReadRange(ctx, h, -1, 10); err == nil {
			t.Fatal("negative offset accepted")
		}
		if _, _, err := s.ReadRange(ctx, h, 0, -1); err == nil {
			t.Fatal("negative length accepted")
		}
	})
	t.Run("empty blob", func(t *testing.T) {
		e := put(t, s, nil)
		got, info, err := s.ReadRange(ctx, e, 0, 100)
		if err != nil || len(got) != 0 || info.Size != 0 {
			t.Fatalf("ReadRange on empty blob: %d bytes, %+v, %v", len(got), info, err)
		}
	})
}

func TestNotFoundAndBadHash(t *testing.T) {
	s := open(t, Options{})
	missing := hashOf([]byte("never stored"))
	if _, _, err := s.Reader(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reader of a missing blob: %v", err)
	}
	if _, _, err := s.ReadRange(ctx, missing, 0, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadRange of a missing blob: %v", err)
	}
	if _, err := s.Stat(missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat of a missing blob: %v", err)
	}
	if err := s.Verify(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Verify of a missing blob: %v", err)
	}
	if ok, err := s.Has(missing); ok || err != nil {
		t.Fatalf("Has of a missing blob: %v, %v", ok, err)
	}
	for _, bad := range []Hash{"", "abc", Hash("../../../../etc/passwd"), missing[:63] + "G", Hash(bytes.ToUpper([]byte(missing)))} {
		if _, err := s.Stat(bad); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Stat(%q): %v, want ErrInvalidHash", bad, err)
		}
		if _, err := s.Has(bad); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Has(%q): %v, want ErrInvalidHash", bad, err)
		}
		if _, _, err := s.ReadRange(ctx, bad, 0, 1); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("ReadRange(%q): %v, want ErrInvalidHash", bad, err)
		}
	}
}

type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestPutFailureLeavesNothing(t *testing.T) {
	s := open(t, Options{ChunkSize: 100})
	boom := errors.New("boom")
	// io.ErrUnexpectedEOF from the source is a failure, not the end.
	for _, e := range []error{boom, io.ErrUnexpectedEOF} {
		if _, _, err := s.Put(ctx, &failingReader{data: randBytes(1, 250), err: e}); !errors.Is(err, e) {
			t.Fatalf("Put with a source failing with %v: %v", e, err)
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.Put(cctx, bytes.NewReader(randBytes(2, 250))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put with a cancelled context: %v", err)
	}
	if left := tmpEntries(t, s.Root()); len(left) != 0 {
		t.Fatalf("temporaries left: %v", left)
	}
	n := 0
	if err := s.Walk(ctx, func(Hash, Info) error { n++; return nil }); err != nil || n != 0 {
		t.Fatalf("store not empty after failed Puts: %d blobs, %v", n, err)
	}
}

func TestDedup(t *testing.T) {
	s := open(t, Options{ChunkSize: 256})
	data := textBytes(4, 3000)
	h := put(t, s, data)
	path := blobPath(s.Root(), h)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if got := put(t, s, data); got != h {
			t.Fatalf("second Put hash = %s, want %s", got, h)
		}
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("the second Put replaced the file instead of discarding its copy")
	}
	if left := tmpEntries(t, s.Root()); len(left) != 0 {
		t.Fatalf("discarded copy left behind: %v", left)
	}
	n := 0
	if err := s.Walk(ctx, func(Hash, Info) error { n++; return nil }); err != nil || n != 1 {
		t.Fatalf("Walk saw %d blobs (%v), want 1", n, err)
	}
}

// A live copy with a destroyed index is not a duplicate: Put replaces it.
func TestPutHealsDamagedCopy(t *testing.T) {
	s := open(t, Options{ChunkSize: 256})
	data := randBytes(5, 1000)
	h := put(t, s, data)
	if err := os.Truncate(blobPath(s.Root(), h), 100); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx, h); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Verify of a truncated blob: %v", err)
	}
	put(t, s, data)
	if err := s.Verify(ctx, h); err != nil {
		t.Fatalf("Verify after storing again: %v", err)
	}
}

func TestConcurrent(t *testing.T) {
	s := open(t, Options{ChunkSize: 4096})
	same := textBytes(11, 100_000)
	sameHash := hashOf(same)
	const writers = 16
	var wg sync.WaitGroup
	errs := make(chan error, 4*writers)
	for i := range writers {
		wg.Add(2)
		go func() { // identical content
			defer wg.Done()
			h, n, err := s.Put(ctx, bytes.NewReader(same))
			if err != nil || h != sameHash || n != int64(len(same)) {
				errs <- fmt.Errorf("Put same: %s %d %v", h, n, err)
				return
			}
			// Readable the moment Put returns, also while others still write it.
			got, _, err := s.ReadRange(ctx, h, 50_000, 20_000)
			if err != nil || !bytes.Equal(got, same[50_000:70_000]) {
				errs <- fmt.Errorf("ReadRange same: %v", err)
			}
			if err := s.Verify(ctx, h); err != nil {
				errs <- fmt.Errorf("Verify same: %w", err)
			}
		}()
		go func() { // different content
			defer wg.Done()
			data := randBytes(uint64(100+i), 30_000+i*1111)
			h, _, err := s.Put(ctx, bytes.NewReader(data))
			if err != nil || h != hashOf(data) {
				errs <- fmt.Errorf("Put different: %v", err)
				return
			}
			rc, _, err := s.Reader(ctx, h)
			if err != nil {
				errs <- err
				return
			}
			got, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || !bytes.Equal(got, data) {
				errs <- fmt.Errorf("read different: %v", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	rep, err := s.Check(ctx)
	if err != nil || !rep.OK() || rep.Live != writers+1 {
		t.Fatalf("Check after concurrent Puts: %+v, %v", rep, err)
	}
	if left := tmpEntries(t, s.Root()); len(left) != 0 {
		t.Fatalf("temporaries left: %v", left)
	}
}

// fileIndex reads the index of a stored blob straight from its file.
func fileIndex(t testing.TB, path string) *index {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	ix, err := readIndex(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestTruncated(t *testing.T) {
	s := open(t, Options{ChunkSize: 64})
	data := randBytes(21, 300) // five frames
	h := put(t, s, data)
	path := blobPath(s.Root(), h)
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ix := fileIndex(t, path)
	t.Logf("file is %d bytes, data ends at %d, frames start at %v", len(orig), ix.offsets[ix.frames()], ix.offsets[:ix.frames()])

	// Every possible truncation point, which covers: empty file, inside
	// each frame, exactly between frames, inside the index, inside the
	// footer; then the same file with bytes appended.
	damaged := make(map[string][]byte)
	for n := 0; n < len(orig); n++ {
		damaged[fmt.Sprintf("truncated to %d", n)] = orig[:n]
	}
	damaged["one byte appended"] = append(bytes.Clone(orig), 0)
	damaged["itself appended"] = append(bytes.Clone(orig), orig...)
	for name, content := range damaged {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := s.Verify(ctx, h); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%s: Verify = %v, want ErrCorrupt", name, err)
		}
		if b, _, err := s.ReadRange(ctx, h, 0, int64(len(data))); !errors.Is(err, ErrCorrupt) || b != nil {
			t.Fatalf("%s: ReadRange = %d bytes, %v, want ErrCorrupt", name, len(b), err)
		}
		if b, _, err := s.ReadRange(ctx, h, 290, 5); !errors.Is(err, ErrCorrupt) || b != nil {
			t.Fatalf("%s: ReadRange of the tail = %d bytes, %v, want ErrCorrupt", name, len(b), err)
		}
		// Appended bytes can leave a readable index in front of a damaged
		// last frame; then the damage shows when the reader gets there.
		rc, _, err := s.Reader(ctx, h)
		if err == nil {
			_, err = io.ReadAll(rc)
			rc.Close()
		}
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%s: Reader = %v, want ErrCorrupt", name, err)
		}
	}
	rep, err := s.Check(ctx)
	if err != nil || len(rep.Corrupt) != 1 || rep.Corrupt[0].Hash != h {
		t.Fatalf("Check on a damaged blob: %+v, %v", rep, err)
	}
}

func TestFlippedBytes(t *testing.T) {
	const chunk = 64
	s := open(t, Options{ChunkSize: chunk})
	data := randBytes(22, 300) // five frames
	h := put(t, s, data)
	path := blobPath(s.Root(), h)
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ix := fileIndex(t, path)
	dataEnd := ix.offsets[ix.frames()]
	frameOf := func(pos int) int {
		for i := 0; i < ix.frames(); i++ {
			if int64(pos) < ix.offsets[i+1] {
				return i
			}
		}
		return -1
	}

	var harmless, detected int
	for pos := 0; pos < len(orig); pos++ {
		for _, bit := range []byte{0x01, 0x10, 0x80, 0xff} {
			mut := bytes.Clone(orig)
			mut[pos] ^= bit
			if err := os.WriteFile(path, mut, 0o644); err != nil {
				t.Fatal(err)
			}
			where := fmt.Sprintf("byte %d ^ %#02x", pos, bit)
			verr := s.Verify(ctx, h)
			if verr != nil && !errors.Is(verr, ErrCorrupt) {
				t.Fatalf("%s: Verify = %v, want ErrCorrupt", where, verr)
			}

			if int64(pos) >= dataEnd {
				// The index: every flip must be noticed by every read.
				if verr == nil {
					t.Fatalf("%s (index): Verify passed", where)
				}
				if b, _, err := s.ReadRange(ctx, h, 0, 10); !errors.Is(err, ErrCorrupt) || b != nil {
					t.Fatalf("%s (index): ReadRange = %d bytes, %v", where, len(b), err)
				}
				detected++
				continue
			}

			// A data frame: a read touching it fails, or the flip did
			// not change what the frame decodes to; wrong bytes never
			// come back. Reads elsewhere are unaffected.
			k := frameOf(pos)
			lo, hi := k*chunk, min((k+1)*chunk, len(data))
			b, _, rerr := s.ReadRange(ctx, h, int64(lo), int64(hi-lo))
			switch {
			case rerr != nil && !errors.Is(rerr, ErrCorrupt):
				t.Fatalf("%s (frame %d): ReadRange = %v, want ErrCorrupt", where, k, rerr)
			case rerr == nil && !bytes.Equal(b, data[lo:hi]):
				t.Fatalf("%s (frame %d): ReadRange returned wrong bytes silently", where, k)
			case (rerr == nil) != (verr == nil):
				t.Fatalf("%s (frame %d): ReadRange says %v but Verify says %v", where, k, rerr, verr)
			case rerr == nil:
				harmless++
			default:
				detected++
			}
			for other := 0; other < ix.frames(); other++ {
				if other == k {
					continue
				}
				lo, hi := other*chunk, min((other+1)*chunk, len(data))
				b, _, err := s.ReadRange(ctx, h, int64(lo), int64(hi-lo))
				if err != nil || !bytes.Equal(b, data[lo:hi]) {
					t.Fatalf("%s (frame %d): reading intact frame %d failed: %v", where, k, other, err)
				}
			}
			// The streaming reader delivers the frames before the damage and then fails.
			if rerr != nil {
				rc, _, err := s.Reader(ctx, h)
				if err != nil {
					t.Fatalf("%s: Reader: %v", where, err)
				}
				got, err := io.ReadAll(rc)
				rc.Close()
				if !errors.Is(err, ErrCorrupt) || !bytes.Equal(got, data[:k*chunk]) {
					t.Fatalf("%s (frame %d): Reader gave %d bytes and %v", where, k, len(got), err)
				}
			}
		}
	}
	t.Logf("%d flips detected, %d flips harmless (frame still decodes to the same content)", detected, harmless)
	if harmless*20 > detected {
		t.Fatalf("too many undetected flips: %d of %d", harmless, harmless+detected)
	}
}

func TestStaleTempRemovedOnOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "live")
	s := open(t, Options{Root: root, ChunkSize: 128})
	data := randBytes(31, 1000)
	h := put(t, s, data)

	// What a killed Put leaves: a partial copy under a temporary name.
	full, err := os.ReadFile(blobPath(root, h))
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, tmpDir, "put-0123456789abcdef"+tmpExt)
	if err := os.WriteFile(stale, full[:len(full)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Check(ctx)
	if err != nil || len(rep.StrayTemps) != 1 || rep.StrayTemps[0] != stale || rep.OK() {
		t.Fatalf("Check with a stray temporary: %+v, %v", rep, err)
	}
	n := 0
	if err := s.Walk(ctx, func(Hash, Info) error { n++; return nil }); err != nil || n != 1 {
		t.Fatalf("Walk counted the temporary: %d blobs, %v", n, err)
	}

	s2 := open(t, Options{Root: root, ChunkSize: 128})
	if left := tmpEntries(t, root); len(left) != 0 {
		t.Fatalf("Open left temporaries: %v", left)
	}
	if got, _ := readAll(t, s2, h); !bytes.Equal(got, data) {
		t.Fatal("blob changed by reopening")
	}
	if rep, err := s2.Check(ctx); err != nil || !rep.OK() || rep.Live != 1 {
		t.Fatalf("Check after reopen: %+v, %v", rep, err)
	}
}

func TestOpenRejectsBadOptions(t *testing.T) {
	dir := t.TempDir()
	for name, o := range map[string]Options{
		"no root":             {},
		"negative chunk":      {Root: dir, ChunkSize: -1},
		"huge chunk":          {Root: dir, ChunkSize: MaxChunkSize + 1},
		"level out of range":  {Root: dir, Level: 23},
		"archive equals live": {Root: dir, ArchiveRoot: dir},
	} {
		if s, err := Open(o); err == nil {
			s.Close()
			t.Errorf("%s: Open succeeded", name)
		}
	}
}

func TestZstdCLICompatibility(t *testing.T) {
	zstdBin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("zstd binary not on PATH")
	}
	s := open(t, Options{ChunkSize: 1 << 16})
	for name, data := range map[string][]byte{
		"empty":       nil,
		"small":       []byte("hello, blob\n"),
		"one chunk":   textBytes(41, 1<<16),
		"multi-chunk": textBytes(42, 5<<16+123),
		"random":      randBytes(43, 3<<16+1),
	} {
		t.Run(name, func(t *testing.T) {
			h := put(t, s, data)
			var stderr bytes.Buffer
			cmd := exec.Command(zstdBin, "-d", "-c", blobPath(s.Root(), h))
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("zstd -d: %v: %s", err, stderr.String())
			}
			if !bytes.Equal(out, data) {
				t.Fatalf("zstd -d gave %d bytes, want %d", len(out), len(data))
			}
			if out, err := exec.Command(zstdBin, "-t", blobPath(s.Root(), h)).CombinedOutput(); err != nil {
				t.Fatalf("zstd -t: %v: %s", err, out)
			}
		})
	}
}
