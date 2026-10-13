//go:build lance

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/lance"
)

// embedBatch asks an Ollama-compatible server for the embeddings of texts.
func embedBatch(ctx context.Context, url, model string, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": model, "input": texts, "truncate": true})
	req, err := http.NewRequestWithContext(ctx, "POST", url+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, fmt.Errorf("embed: %s: %s", resp.Status, b)
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: %d vectors for %d texts", len(out.Embeddings), len(texts))
	}
	return out.Embeddings, nil
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

func meanStd(xs []float64) (mean, std float64) {
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	for _, x := range xs {
		std += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(std / float64(len(xs)))
}

// cmdEmbed embeds the sample of real chunks, stores them with their vectors,
// and measures vector and hybrid search on real embeddings. It also reports
// how the vectors are distributed, to judge the synthetic sets against.
func cmdEmbed(args []string) error {
	fs := flag.NewFlagSet("embed", flag.ExitOnError)
	sample := fs.String("sample", "", "sample file written by the chunks subcommand")
	dir := fs.String("lance", "", "directory for the dataset (must be empty)")
	url := fs.String("url", "http://127.0.0.1:11434", "Ollama-compatible server")
	model := fs.String("model", "", "embedding model")
	limit := fs.Int("limit", 0, "embed only this many chunks (0 = all)")
	batch := fs.Int("batch", 32, "texts per request")
	hold := fs.Int("queries", 200, "chunks held out as queries")
	k := fs.Int("k", 10, "neighbours")
	vecOut := fs.String("vectors-out", "", "write the vectors to this file, little-endian float32, row after row")
	vecIn := fs.String("vectors-in", "", "read the vectors from a file written by -vectors-out instead of embedding; needs -dim")
	dimIn := fs.Int("dim", 0, "dimension of the vectors in -vectors-in")
	fs.Parse(args)
	if *vecIn != "" && *dimIn <= 0 {
		return errors.New("-vectors-in needs -dim")
	}
	if *sample == "" || *dir == "" || *model == "" {
		return errors.New("-sample, -lance and -model are required")
	}
	if entries, _ := os.ReadDir(*dir); len(entries) > 0 {
		return fmt.Errorf("%s is not empty", *dir)
	}
	ctx := context.Background()
	f, err := os.Open(*sample)
	if err != nil {
		return err
	}
	defer f.Close()
	var chunks []store.Chunk
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var c store.Chunk
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return err
		}
		chunks = append(chunks, c)
		if *limit > 0 && len(chunks) >= *limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(chunks) <= *hold {
		return errors.New("sample smaller than the query set")
	}

	start := time.Now()
	var textBytes int64
	if *vecIn != "" {
		g, err := loadGenerator(*vecIn, *dimIn, 0)
		if err != nil {
			return err
		}
		if len(g.centres) < len(chunks) {
			return fmt.Errorf("%s holds %d vectors for %d chunks", *vecIn, len(g.centres), len(chunks))
		}
		for i := range chunks {
			chunks[i].Vector, chunks[i].Embedder = g.centres[i], *model
		}
	}
	for i := 0; *vecIn == "" && i < len(chunks); i += *batch {
		j := min(i+*batch, len(chunks))
		texts := make([]string, j-i)
		for n := range texts {
			texts[n] = chunks[i+n].Text
			textBytes += int64(len(texts[n]))
		}
		vecs, err := embedBatch(ctx, *url, *model, texts)
		if err != nil {
			return fmt.Errorf("after %d chunks: %w", i, err)
		}
		for n := range vecs {
			chunks[i+n].Vector, chunks[i+n].Embedder = vecs[n], *model
		}
	}
	el := time.Since(start)
	dim := len(chunks[0].Vector)
	out := map[string]any{"model": *model, "dim": dim, "chunks": len(chunks), "embed_s": el.Seconds(),
		"embed_chunks_per_s": float64(len(chunks)) / el.Seconds(), "embed_text_mb_per_s": float64(textBytes) / 1e6 / el.Seconds()}

	if *vecOut != "" {
		vf, err := os.Create(*vecOut)
		if err != nil {
			return err
		}
		w := bufio.NewWriterSize(vf, 1<<20)
		for i := range chunks {
			if err := binary.Write(w, binary.LittleEndian, chunks[i].Vector); err != nil {
				return err
			}
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if err := vf.Close(); err != nil {
			return err
		}
	}

	qs, base := chunks[:*hold], chunks[*hold:]

	// Distribution: cosine of random pairs, and of each query to its nearest
	// neighbour, by brute force.
	rng := rand.New(rand.NewSource(1))
	pairs := make([]float64, 20000)
	for i := range pairs {
		pairs[i] = cosine(base[rng.Intn(len(base))].Vector, base[rng.Intn(len(base))].Vector)
	}
	pm, ps := meanStd(pairs)
	exactIDs := make([]map[string]bool, len(qs))
	kth := make([]float64, len(qs)) // cosine of the k-th true neighbour
	vecOf := make(map[string][]float32, len(base))
	for j := range base {
		vecOf[base[j].ID] = base[j].Vector
	}
	nn := make([]float64, len(qs))
	for i, q := range qs {
		type sc struct {
			id string
			c  float64
		}
		all := make([]sc, len(base))
		for j := range base {
			all[j] = sc{base[j].ID, cosine(q.Vector, base[j].Vector)}
		}
		sort.Slice(all, func(a, b int) bool { return all[a].c > all[b].c })
		nn[i] = all[0].c
		exactIDs[i] = map[string]bool{}
		for _, a := range all[:*k] {
			exactIDs[i][a.id] = true
		}
		kth[i] = all[*k-1].c
	}
	nm, ns := meanStd(nn)
	out["cosine_random_pairs"] = map[string]float64{"mean": pm, "std": ps}
	out["cosine_nearest_neighbour"] = map[string]float64{"mean": nm, "std": ns}

	s, err := lance.Open(lance.Options{Dir: *dir, Dim: dim, Metric: "cosine"})
	if err != nil {
		return err
	}
	defer s.Close()
	for i := 0; i < len(base); i += 20000 {
		if err := s.Append(ctx, base[i:min(i+20000, len(base))]); err != nil {
			return err
		}
	}
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
	out["size_gb"] = float64(dirSize(*dir)) / (1 << 30)

	// Many chunks are near-copies of one another, so the ten nearest are
	// often tied and two correct answers can name different chunks. Recall
	// counts a hit as correct when it is at least as close as the true
	// tenth neighbour; RecallByID, which demands the same ids, is the
	// stricter figure and is kept to show the size of the effect.
	type setting struct {
		Name       string
		Recall     float64
		RecallByID float64
		Latency    latency
	}
	var settings []setting
	for _, o := range []struct {
		name string
		so   store.SearchOptions
	}{
		{"exact scan", store.SearchOptions{Limit: *k, Exact: true}},
		{"probes 10", store.SearchOptions{Limit: *k, Probes: 10}},
		{"probes 20, refine 10", store.SearchOptions{Limit: *k, Probes: 20, Refine: 10}},
		{"probes 50, refine 10", store.SearchOptions{Limit: *k, Probes: 50, Refine: 10}},
		{"probes 50, refine 30", store.SearchOptions{Limit: *k, Probes: 50, Refine: 30}},
	} {
		var ds []time.Duration
		var found, close, total int
		for i, q := range qs {
			t := time.Now()
			hits, err := s.Nearest(ctx, q.Vector, o.so)
			if err != nil {
				return fmt.Errorf("%s: %w", o.name, err)
			}
			ds = append(ds, time.Since(t))
			total += len(exactIDs[i])
			for _, h := range hits {
				if exactIDs[i][h.ID] {
					found++
				}
				if v := vecOf[h.ID]; v != nil && cosine(q.Vector, v) >= kth[i]-1e-6 {
					close++
				}
			}
		}
		settings = append(settings, setting{o.name, float64(close) / float64(total), float64(found) / float64(total), summarise(ds)})
	}
	out["nearest"] = settings

	// Hybrid: the text of a query list, with the vector of a held-out chunk.
	var hybrid []time.Duration
	for i, q := range qs {
		t := time.Now()
		if _, err := s.Hybrid(ctx, queries[i%len(queries)], q.Vector, store.SearchOptions{Limit: *k, Probes: 20, Refine: 10}); err != nil {
			return fmt.Errorf("hybrid: %w", err)
		}
		hybrid = append(hybrid, time.Since(t))
	}
	out["hybrid_ms"] = summarise(hybrid)
	rss, peak := memory()
	out["rss_mib"], out["peak_rss_mib"] = rss, peak
	return emit(out)
}
