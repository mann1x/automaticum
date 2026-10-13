//go:build lance

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/lance"
)

// words gives synthetic chunks some text, so the dataset has the shape of the
// real one. Full-text numbers come from real text, not from this.
var words = strings.Fields(`the model agent oracle test file edit read tool call result error context window
compaction thinking budget sampler request response token loop repeat guard fired patch apply failed passed
timeout abort retry server log stream event turn reasoning text function variable return value check verify`)

// generator draws vectors from a mixture of Gaussians on the unit sphere:
// embeddings of real text cluster, and uniformly random vectors would make
// approximate search look worse than it is in use.
type generator struct {
	dim     int
	centres [][]float32
	noise   float64
}

func newGenerator(dim, clusters int, noise float64, seed int64) *generator {
	rng := rand.New(rand.NewSource(seed))
	g := &generator{dim: dim, noise: noise, centres: make([][]float32, clusters)}
	for i := range g.centres {
		g.centres[i] = unit(rng, dim, nil, 1)
	}
	return g
}

func unit(rng *rand.Rand, dim int, centre []float32, noise float64) []float32 {
	v := make([]float32, dim)
	var norm float64
	for i := range v {
		x := rng.NormFloat64() * noise
		if centre != nil {
			x += float64(centre[i])
		}
		v[i] = float32(x)
		norm += x * x
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

func (g *generator) vector(rng *rand.Rand) []float32 {
	// noise is per-vector spread relative to a unit centre, spread over dims.
	return unit(rng, g.dim, g.centres[rng.Intn(len(g.centres))], g.noise/math.Sqrt(float64(g.dim)))
}

func cmdVectors(args []string) error {
	fs := flag.NewFlagSet("vectors", flag.ExitOnError)
	dir := fs.String("dir", "", "directory for the Lance dataset (created; must be empty or absent unless -reuse)")
	n := fs.Int("n", 100000, "chunks")
	dim := fs.Int("dim", 1024, "vector dimension")
	batch := fs.Int("batch", 20000, "chunks per append")
	clusters := fs.Int("clusters", 2000, "mixture components")
	noise := fs.Float64("noise", 0.6, "spread of a component, relative to the centre's unit length")
	queries := fs.Int("queries", 100, "queries per setting")
	truth := fs.Int("truth", 50, "queries with an exact answer, for recall")
	k := fs.Int("k", 10, "neighbours")
	reuse := fs.Bool("reuse", false, "query an existing dataset instead of building one")
	floor := fs.Float64("free-floor-gb", 60, "stop if the filesystem has less free space than this")
	fs.Parse(args)
	if *dir == "" {
		return errors.New("-dir is required")
	}
	ctx := context.Background()
	gen := newGenerator(*dim, *clusters, *noise, 1)
	out := map[string]any{"n": *n, "dim": *dim, "clusters": *clusters, "noise": *noise, "k": *k}

	if !*reuse {
		if entries, _ := os.ReadDir(*dir); len(entries) > 0 {
			return fmt.Errorf("%s is not empty", *dir)
		}
	}
	s, err := lance.Open(lance.Options{Dir: *dir, Dim: *dim})
	if err != nil {
		return err
	}
	defer s.Close()

	if !*reuse {
		rng := rand.New(rand.NewSource(2))
		start := time.Now()
		for done := 0; done < *n; {
			if free := freeGB(*dir); free < *floor {
				return fmt.Errorf("free space %.0f GiB is under the floor of %.0f GiB after %d chunks", free, *floor, done)
			}
			m := min(*batch, *n-done)
			chunks := make([]store.Chunk, m)
			for i := range chunks {
				id := done + i
				var sb strings.Builder
				for w := 0; w < 40; w++ {
					sb.WriteString(words[rng.Intn(len(words))])
					sb.WriteByte(' ')
				}
				chunks[i] = store.Chunk{ID: fmt.Sprintf("c%09d", id), RunID: fmt.Sprintf("run-%05d", id/1000), Turn: id % 1000,
					Kind: "reasoning", Text: sb.String(), Vector: gen.vector(rng), Embedder: "synthetic/1"}
			}
			if err := s.Append(ctx, chunks); err != nil {
				return err
			}
			done += m
		}
		load := time.Since(start)
		out["load_s"] = load.Seconds()
		out["load_rows_per_s"] = float64(*n) / load.Seconds()
		out["size_after_load_gb"] = float64(dirSize(*dir)) / (1 << 30)

		start = time.Now()
		if err := s.BuildIndexes(ctx, store.IndexOptions{Vector: true}); err != nil {
			return err
		}
		out["vector_index_build_s"] = time.Since(start).Seconds()
		start = time.Now()
		if err := s.BuildIndexes(ctx, store.IndexOptions{FullText: true}); err != nil {
			return err
		}
		out["fulltext_index_build_s"] = time.Since(start).Seconds()
		out["size_after_index_gb"] = float64(dirSize(*dir)) / (1 << 30)
		_, peak := memory()
		out["peak_rss_after_build_mib"] = peak
	}
	count, err := s.Count(ctx)
	if err != nil {
		return err
	}
	out["rows"] = count

	// Queries are fresh draws from the same mixture.
	qrng := rand.New(rand.NewSource(3))
	qs := make([][]float32, *queries)
	for i := range qs {
		qs[i] = gen.vector(qrng)
	}
	// Exact answers for the first few.
	want := make([]map[string]bool, min(*truth, len(qs)))
	var exact []time.Duration
	for i := range want {
		t := time.Now()
		hits, err := s.Nearest(ctx, qs[i], store.SearchOptions{Limit: *k, Exact: true})
		if err != nil {
			return err
		}
		exact = append(exact, time.Since(t))
		want[i] = map[string]bool{}
		for _, h := range hits {
			want[i][h.ID] = true
		}
	}
	out["exact_ms"] = summarise(exact)

	type setting struct {
		Probes, Refine int
		Recall         float64
		Latency        latency
	}
	var settings []setting
	for _, p := range [][2]int{{10, 0}, {20, 0}, {20, 10}, {50, 10}, {100, 10}, {100, 30}} {
		o := store.SearchOptions{Limit: *k, Probes: p[0], Refine: p[1]}
		var ds []time.Duration
		var found, total int
		for i, q := range qs {
			t := time.Now()
			hits, err := s.Nearest(ctx, q, o)
			if err != nil {
				return err
			}
			ds = append(ds, time.Since(t))
			if i < len(want) {
				total += len(want[i])
				for _, h := range hits {
					if want[i][h.ID] {
						found++
					}
				}
			}
		}
		st := setting{Probes: p[0], Refine: p[1], Latency: summarise(ds)}
		if total > 0 {
			st.Recall = float64(found) / float64(total)
		}
		settings = append(settings, st)
	}
	out["indexed"] = settings

	// Filtered search and full-text on the synthetic text, for shape only.
	var filtered, text []time.Duration
	for i, q := range qs {
		t := time.Now()
		if _, err := s.Nearest(ctx, q, store.SearchOptions{Limit: *k, Probes: 20, Refine: 10, RunIDs: []string{fmt.Sprintf("run-%05d", i)}}); err != nil {
			return err
		}
		filtered = append(filtered, time.Since(t))
		t = time.Now()
		if _, err := s.FullText(ctx, words[i%len(words)]+" "+words[(i*7)%len(words)], store.SearchOptions{Limit: *k}); err != nil {
			return err
		}
		text = append(text, time.Since(t))
	}
	out["filtered_by_run_ms"] = summarise(filtered)
	out["fulltext_synthetic_ms"] = summarise(text)
	rss, peak := memory()
	out["rss_mib"], out["peak_rss_mib"] = rss, peak
	return emit(out)
}
