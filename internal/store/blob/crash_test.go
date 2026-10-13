package blob

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	killChildEnv   = "AUTOMATICUM_BLOB_KILL_CHILD_ROOT"
	killChunk      = 16 << 10
	killBlobChunks = 24
)

// TestMain lets the test binary double as the writer that TestKillDuringPut
// kills.
func TestMain(m *testing.M) {
	if root := os.Getenv(killChildEnv); root != "" {
		killChild(root)
		return
	}
	os.Exit(m.Run())
}

// slowReader hands out random bytes one chunk per Read with a pause, so
// that a Put takes tens of milliseconds and a kill lands inside one.
type slowReader struct {
	r    *rand.ChaCha8
	left int // bytes
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(time.Millisecond)
	n := min(len(p), killChunk, s.left)
	_, _ = s.r.Read(p[:n])
	s.left -= n
	return n, nil
}

// killChild stores blobs from four goroutines until it is killed.
func killChild(root string) {
	s, err := Open(Options{Root: root, ChunkSize: killChunk})
	if err != nil {
		fmt.Println("open failed:", err)
		os.Exit(3)
	}
	fmt.Println("ready")
	for g := range 4 {
		go func() {
			for i := 0; ; i++ {
				var seed [32]byte
				copy(seed[:], fmt.Sprintf("%d-%d-%d", os.Getpid(), g, i))
				src := &slowReader{r: rand.NewChaCha8(seed), left: killBlobChunks * killChunk}
				if _, _, err := s.Put(ctx, src); err != nil {
					fmt.Println("put failed:", err)
					os.Exit(4)
				}
			}
		}()
	}
	select {}
}

func TestKillDuringPut(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns and kills 20 processes")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "live")
	var leftovers, blobs int
	for i := range 20 {
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), killChildEnv+"="+root)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil || line != "ready\n" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("run %d: child said %q, %v", i, line, err)
		}
		// Long enough for some Puts to finish and for others to be under way.
		time.Sleep(time.Duration(40+rand.IntN(80)) * time.Millisecond)
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("run %d: kill: %v", i, err)
		}
		_ = cmd.Wait()

		// What the kill left behind, before anything cleans up. Every
		// file under a valid name must be a complete blob; the partial
		// ones must all be temporaries.
		left := tmpEntries(t, root)
		leftovers += len(left)
		for _, name := range left {
			if filepath.Ext(name) != tmpExt {
				t.Fatalf("run %d: unexpected file in tmp: %s", i, name)
			}
		}
		verifyAllFiles(t, root, i)

		// The store reopens, drops the temporaries, and works.
		s, err := Open(Options{Root: root, ChunkSize: killChunk})
		if err != nil {
			t.Fatalf("run %d: reopen: %v", i, err)
		}
		if left := tmpEntries(t, root); len(left) != 0 {
			t.Fatalf("run %d: Open left temporaries: %v", i, left)
		}
		rep, err := s.Check(ctx)
		if err != nil || !rep.OK() {
			t.Fatalf("run %d: Check after reopen: %+v, %v", i, rep, err)
		}
		blobs = rep.Live
		data := randBytes(uint64(1000+i), 50_000)
		h := put(t, s, data)
		if got, _ := readAll(t, s, h); !bytes.Equal(got, data) {
			t.Fatalf("run %d: store does not round-trip after reopen", i)
		}
		s.Close()
	}
	t.Logf("20 kills: %d interrupted Puts left a temporary, %d complete blobs in the store", leftovers, blobs)
	if leftovers == 0 {
		t.Fatal("no kill landed inside a Put; the test proved nothing")
	}
	if blobs <= 20 {
		t.Fatal("the children completed no Put; the test covers only half the window")
	}
}

// verifyAllFiles checks, without the cleanup Open does, that everything
// under blobs/ is a complete, correctly named blob.
func verifyAllFiles(t *testing.T, root string, run int) {
	t.Helper()
	s := &Store{live: root, chunkSize: killChunk, inflight: map[string]struct{}{}}
	probe := open(t, Options{}) // only to borrow a decoder
	s.dec = probe.dec
	err := walkTier(ctx, root, TierLive, func(e entry) error {
		if e.stray {
			return fmt.Errorf("stray file under blobs/: %s", e.path)
		}
		info, err := s.verifyPath(ctx, e.path, e.hash, TierLive)
		if err != nil {
			return fmt.Errorf("partial or damaged file under a valid name: %w", err)
		}
		if info.Size != killBlobChunks*killChunk && info.Size != 50_000 {
			return fmt.Errorf("blob %s has %d bytes, not a size any Put wrote", e.hash, info.Size)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run %d: %v", run, err)
	}
}
