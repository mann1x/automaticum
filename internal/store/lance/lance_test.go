//go:build lance

package lance

import (
	"context"
	"fmt"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

const testDim = 16

func vec(seed int) []float32 {
	v := make([]float32, testDim)
	for i := range v {
		v[i] = float32((seed*31+i*7)%17) / 17
	}
	return v
}

func chunks(run string, n int, text string) []store.Chunk {
	out := make([]store.Chunk, n)
	for i := range out {
		out[i] = store.Chunk{
			ID: fmt.Sprintf("%s-%04d", run, i), RunID: run, Turn: i, Kind: "reasoning",
			Text: fmt.Sprintf("%s number %d", text, i), Vector: vec(i), Embedder: "test/1",
		}
	}
	return out
}

func TestSearchRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Dim: testDim})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx); n != 0 {
		t.Fatalf("empty store counts %d", n)
	}
	if err := s.UpsertChunks(ctx, chunks("run-a", 300, "the oracle was wrong about the countdown")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, chunks("run-b", 300, "compaction ran before the window was full")); err != nil {
		t.Fatal(err)
	}
	// A chunk with no vector yet must be storable.
	if err := s.UpsertChunks(ctx, []store.Chunk{{ID: "run-c-0000", RunID: "run-c", Kind: "text", Text: "not embedded yet"}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx); n != 601 {
		t.Fatalf("count = %d, want 601", n)
	}
	// Upsert replaces by id.
	repl := chunks("run-a", 10, "replaced text about tampering")
	if err := s.UpsertChunks(ctx, repl); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx); n != 601 {
		t.Fatalf("count after upsert = %d, want 601", n)
	}
	if err := s.BuildIndexes(ctx, store.IndexOptions{FullText: true, Vector: true, Partitions: 4, SubVectors: 4}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.FullText(ctx, "tampering", store.SearchOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 10 {
		t.Fatalf("full-text hits = %d, want 10", len(hits))
	}
	for _, h := range hits {
		if h.RunID != "run-a" || h.Text == "" || h.Embedder != "test/1" {
			t.Fatalf("unexpected hit %+v", h)
		}
	}
	hits, err = s.FullText(ctx, "compaction", store.SearchOptions{Limit: 5, RunIDs: []string{"run-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("filtered full-text returned %d hits from the wrong run", len(hits))
	}

	exact, err := s.Nearest(ctx, vec(7), store.SearchOptions{Limit: 5, Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 5 {
		t.Fatalf("exact hits = %d, want 5", len(exact))
	}
	if exact[0].Turn != 7 && exact[0].Turn%17 != 7 {
		t.Fatalf("nearest neighbour of vec(7) is turn %d", exact[0].Turn)
	}
	indexed, err := s.Nearest(ctx, vec(7), store.SearchOptions{Limit: 5, Probes: 4, Refine: 4, Kinds: []string{"reasoning"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 5 {
		t.Fatalf("indexed hits = %d, want 5", len(indexed))
	}

	hyb, err := s.Hybrid(ctx, "window full", vec(3), store.SearchOptions{Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(hyb) != 8 {
		t.Fatalf("hybrid hits = %d, want 8", len(hyb))
	}
	for i := 1; i < len(hyb); i++ {
		if hyb[i].Score > hyb[i-1].Score {
			t.Fatal("hybrid hits are not sorted best first")
		}
	}

	if err := s.DeleteRun(ctx, "run-b"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx); n != 301 {
		t.Fatalf("count after delete = %d, want 301", n)
	}
	if err := s.Optimize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and read again.
	s, err = Open(Options{Dir: dir, Dim: testDim})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n, _ := s.Count(ctx); n != 301 {
		t.Fatalf("count after reopen = %d, want 301", n)
	}
	// Rows written after the indexes were built are still found.
	if err := s.UpsertChunks(ctx, []store.Chunk{{ID: "late-0", RunID: "late", Kind: "text", Text: "a zebra crossed late", Vector: vec(99), Embedder: "test/1"}}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.FullText(ctx, "zebra", store.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("unindexed row: full-text hits = %d, want 1", len(hits))
	}
}

func TestFuse(t *testing.T) {
	a := []store.Hit{{Chunk: store.Chunk{ID: "x"}}, {Chunk: store.Chunk{ID: "y"}}}
	b := []store.Hit{{Chunk: store.Chunk{ID: "y"}}, {Chunk: store.Chunk{ID: "z"}}}
	got := Fuse(2, a, b)
	if len(got) != 2 || got[0].ID != "y" {
		t.Fatalf("fuse = %+v, want y first and two results", got)
	}
}

func TestQuote(t *testing.T) {
	if got := quote("o'brien"); got != "'o''brien'" {
		t.Fatalf("quote = %s", got)
	}
}
