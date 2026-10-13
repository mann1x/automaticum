//go:build lance

package storetest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/duck"
	"github.com/mann1x/automaticum/internal/store/lance"
)

const (
	crashEnv   = "AUTOMATICUM_CRASH_CHILD"
	crashDim   = 32
	crashKills = 20
	chunksPer  = 40
)

func crashVec(seed int) []float32 {
	v := make([]float32, crashDim)
	for i := range v {
		v[i] = float32((seed*13+i*5)%23) / 23
	}
	return v
}

func runID(gen, i int) string { return fmt.Sprintf("g%02d-run-%06d", gen, i) }

// crashChild writes runs and their chunks until it is killed. It prints the
// index of a run only after UpsertRun has returned, so the parent knows which
// runs the store acknowledged.
func crashChild(dir string, gen int) {
	ctx := context.Background()
	st, err := duck.Open(ctx, filepath.Join(dir, "state.duckdb"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "child: duck:", err)
		os.Exit(3)
	}
	se, err := lance.Open(lance.Options{Dir: filepath.Join(dir, "lance"), Dim: crashDim})
	if err != nil {
		fmt.Fprintln(os.Stderr, "child: lance:", err)
		os.Exit(3)
	}
	out := bufio.NewWriter(os.Stdout)
	fmt.Fprintln(out, "ready")
	out.Flush()
	for i := 0; ; i++ {
		id := runID(gen, i)
		if err := st.UpsertRun(ctx, &store.Run{ID: id, Verdict: store.Pass, Suite: "crash", WallMS: int64(i)}); err != nil {
			fmt.Fprintln(os.Stderr, "child: upsert run:", err)
			os.Exit(3)
		}
		fmt.Fprintln(out, i)
		out.Flush()
		chunks := make([]store.Chunk, chunksPer)
		for j := range chunks {
			chunks[j] = store.Chunk{ID: fmt.Sprintf("%s-%03d", id, j), RunID: id, Turn: j, Kind: "reasoning",
				Text: "the agent wrote something about the oracle", Vector: crashVec(i + j), Embedder: "test/1"}
		}
		if err := se.Append(ctx, chunks); err != nil {
			fmt.Fprintln(os.Stderr, "child: append chunks:", err)
			os.Exit(3)
		}
		if err := st.SetLaneState(ctx, store.LaneState{Lane: "lane-1", State: "running", RunID: id}); err != nil {
			fmt.Fprintln(os.Stderr, "child: lane state:", err)
			os.Exit(3)
		}
	}
}

func TestMain(m *testing.M) {
	if spec := os.Getenv(crashEnv); spec != "" {
		dir, genStr, _ := strings.Cut(spec, "|")
		gen, _ := strconv.Atoi(genStr)
		crashChild(dir, gen)
		return
	}
	os.Exit(m.Run())
}

// TestKillDuringWrites is threshold T2 of the store spike: SIGKILL a writer
// twenty times; every time the store must reopen, no run the store
// acknowledged may be lost, and the search engine must reopen and accept
// writes (it holds derived data, so losing unacknowledged chunks is allowed).
func TestKillDuringWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("kills a writer twenty times")
	}
	ctx := context.Background()
	dir := t.TempDir()
	rng := rand.New(rand.NewSource(1))
	acked := map[int]int{} // generation -> highest acknowledged index
	var totalRuns, lanceRows int64

	for gen := 0; gen < crashKills; gen++ {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s|%d", crashEnv, dir, gen))
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		var last atomic.Int64
		last.Store(-1)
		ready := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			sc := bufio.NewScanner(stdout)
			for sc.Scan() {
				line := sc.Text()
				if line == "ready" {
					close(ready)
					continue
				}
				if n, err := strconv.Atoi(line); err == nil {
					last.Store(int64(n))
				}
			}
		}()
		select {
		case <-ready:
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			t.Fatalf("generation %d: child did not open the store: %s", gen, stderr.String())
		}
		time.Sleep(time.Duration(50+rng.Intn(450)) * time.Millisecond)
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		<-done
		err = cmd.Wait()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 3 {
			t.Fatalf("generation %d: child failed before the kill: %s", gen, stderr.String())
		}
		acked[gen] = int(last.Load())

		// Reopen both engines after the kill.
		st, err := duck.Open(ctx, filepath.Join(dir, "state.duckdb"))
		if err != nil {
			t.Fatalf("generation %d: DuckDB did not reopen: %v", gen, err)
		}
		for g, hi := range acked {
			for i := 0; i <= hi; i++ {
				if _, err := st.GetRun(ctx, runID(g, i)); err != nil {
					t.Fatalf("generation %d: acknowledged run %s is gone: %v", gen, runID(g, i), err)
				}
			}
		}
		totalRuns, err = st.CountRuns(ctx, store.RunFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		se, err := lance.Open(lance.Options{Dir: filepath.Join(dir, "lance"), Dim: crashDim})
		if err != nil {
			t.Fatalf("generation %d: Lance did not reopen: %v", gen, err)
		}
		lanceRows, err = se.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if lanceRows%chunksPer != 0 {
			t.Fatalf("generation %d: Lance holds %d rows, not a whole number of appends of %d", gen, lanceRows, chunksPer)
		}
		if lanceRows > 0 {
			if _, err := se.Nearest(ctx, crashVec(1), store.SearchOptions{Limit: 3, Exact: true}); err != nil {
				t.Fatalf("generation %d: Lance cannot be read after the kill: %v", gen, err)
			}
		}
		if err := se.Close(); err != nil {
			t.Fatal(err)
		}
	}

	ackedTotal := 0
	for _, hi := range acked {
		ackedTotal += hi + 1
	}
	t.Logf("T2: %d kills, %d runs acknowledged, %d runs in DuckDB, %d rows in Lance (%d whole appends)",
		crashKills, ackedTotal, totalRuns, lanceRows, lanceRows/chunksPer)
	if ackedTotal == 0 {
		t.Fatal("no run was ever acknowledged; the test proved nothing")
	}
}
