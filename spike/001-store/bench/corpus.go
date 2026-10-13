//go:build lance

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/store/duck"
	"github.com/mann1x/automaticum/internal/store/lance"
)

// corpus is an imported store opened for reading.
type corpus struct {
	blobs *blob.Store
	state *duck.State
}

type streamRef struct {
	RunID, Name string
	Hash        blob.Hash
	Size        int64
}

func openCorpus(ctx context.Context, data, archive string) (*corpus, error) {
	if data == "" {
		return nil, errors.New("-data is required")
	}
	b, err := blob.Open(blob.Options{Root: data, ArchiveRoot: archive})
	if err != nil {
		return nil, err
	}
	st, err := duck.Open(ctx, filepath.Join(data, "state.duckdb"))
	if err != nil {
		b.Close()
		return nil, err
	}
	return &corpus{b, st}, nil
}

func (c *corpus) Close() { c.state.Close(); c.blobs.Close() }

// streams lists the event streams of every run.
func (c *corpus) streams(ctx context.Context) ([]streamRef, error) {
	rows, err := c.state.Query(ctx, `SELECT run_id, name, hash, size FROM blob_refs WHERE kind = 'events' ORDER BY run_id, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []streamRef
	for rows.Next() {
		var r streamRef
		var h string
		if err := rows.Scan(&r.RunID, &r.Name, &h, &r.Size); err != nil {
			return nil, err
		}
		r.Hash = blob.Hash(h)
		out = append(out, r)
	}
	return out, rows.Err()
}

// The two delta markers: almost every line of a stream is one of these, and
// none carries anything a content_end line does not repeat in full.
var (
	deltaReasoning = []byte(`"type":"content_start","contentType":"reasoning"`)
	deltaText      = []byte(`"type":"content_start","contentType":"text"`)
)

func isDelta(line []byte) bool {
	head := line
	if len(head) > 200 {
		head = head[:200]
	}
	return bytes.Contains(head, deltaReasoning) || bytes.Contains(head, deltaText)
}

// streamEvent is the part of a stream line the chunker and the request
// simulation read.
type streamEvent struct {
	Type  string `json:"type"`
	Event *struct {
		Type        string          `json:"type"`
		ContentType string          `json:"contentType"`
		Iteration   int             `json:"iteration"`
		Reasoning   string          `json:"reasoning"`
		Text        string          `json:"text"`
		ToolName    string          `json:"toolName"`
		Input       json.RawMessage `json:"input"`
		Output      json.RawMessage `json:"output"`
		Reason      string          `json:"reason"`
		Metadata    *struct {
			Phase string `json:"phase"`
		} `json:"metadata"`
	} `json:"event"`
}

// part is one piece of content a stream yields, in order.
type part struct {
	Offset int64 // of the line in the uncompressed stream
	Turn   int
	Kind   string // reasoning, text, tool_args, tool_result, compaction
	Text   string
}

// walkStream calls fn for every part of one stream, and returns its
// uncompressed length.
func walkStream(r io.Reader, fn func(part) error) (int64, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var off int64
	turn := 0
	for {
		line, err := br.ReadBytes('\n')
		start := off
		off += int64(len(line))
		if len(line) > 0 && !isDelta(line) {
			var ev streamEvent
			if json.Unmarshal(line, &ev) == nil && ev.Type == "agent_event" && ev.Event != nil {
				e := ev.Event
				p := part{Offset: start, Turn: turn}
				switch {
				case e.Type == "iteration_start":
					turn = e.Iteration
				case e.Type == "content_end" && e.ContentType == "reasoning":
					p.Kind, p.Text = "reasoning", e.Reasoning
				case e.Type == "content_end" && e.ContentType == "text":
					p.Kind, p.Text = "text", e.Text
				case e.Type == "content_start" && e.ContentType == "tool":
					p.Kind, p.Text = "tool_args", e.ToolName+" "+string(e.Input)
				case e.Type == "content_end" && e.ContentType == "tool":
					p.Kind, p.Text = "tool_result", e.ToolName+" "+string(e.Output)
				case e.Type == "notice" && e.Reason == "auto_compaction" && e.Metadata != nil && e.Metadata.Phase == "started":
					p.Kind, p.Text = "compaction", "compaction"
				}
				if p.Kind != "" && p.Text != "" {
					if ferr := fn(p); ferr != nil {
						return off, ferr
					}
				}
			}
		}
		if err == io.EOF {
			return off, nil
		}
		if err != nil {
			return off, err
		}
	}
}

// split cuts text into pieces of at most max bytes, at a line break when one
// is near the end of the piece and never inside a character.
func split(text string, max int) []string {
	var out []string
	for len(text) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if i := strings.LastIndexByte(text[:cut], '\n'); i > max/2 {
			cut = i + 1
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, text)
	}
	return out
}

// perturb replaces about one word in twenty with a made-up one.
func perturb(rng *rand.Rand, text string) string {
	var b strings.Builder
	b.Grow(len(text) + 16)
	for len(text) > 0 {
		i := strings.IndexAny(text, " \n\t")
		word, sep := text, ""
		if i >= 0 {
			word, sep = text[:i], text[i:i+1]
		}
		used := len(word) + len(sep)
		if word != "" && rng.Intn(20) == 0 {
			word = "w" + strconv.FormatInt(rng.Int63n(1<<24), 36)
		}
		b.WriteString(word)
		b.WriteString(sep)
		text = text[used:]
		if i < 0 {
			break
		}
	}
	return b.String()
}

// chunkID locates a chunk in its blob: hash, offset of the stream line, piece.
func chunkID(h blob.Hash, off int64, piece int) string {
	return fmt.Sprintf("%s@%d#%d", h, off, piece)
}

func parseChunkID(id string) (blob.Hash, int64, bool) {
	h, rest, ok := strings.Cut(id, "@")
	if !ok {
		return "", 0, false
	}
	o, _, _ := strings.Cut(rest, "#")
	off, err := strconv.ParseInt(o, 10, 64)
	return blob.Hash(h), off, err == nil
}

// cmdChunks is the ingest half of T4 and T8: every stream is read back from
// the blob store, cut into chunks and written to Lance, and the full-text
// index is built. A uniform sample of the chunks is kept for embedding.
func cmdChunks(args []string) error {
	fs := flag.NewFlagSet("chunks", flag.ExitOnError)
	data := fs.String("data", "", "data root of an imported corpus")
	archive := fs.String("archive", "", "archive root")
	dir := fs.String("lance", "", "directory for the chunk dataset (must be empty)")
	max := fs.Int("max", 2000, "bytes per chunk")
	sampleN := fs.Int("sample", 100000, "chunks kept for embedding")
	sampleOut := fs.String("sample-out", "", "file for the sample, JSON lines")
	copies := fs.Int("copies", 1, "write each chunk this many times, as runs of their own; every copy after the first has about one word in twenty replaced, so the copies are not duplicates")
	fs.Parse(args)
	if *dir == "" {
		return errors.New("-lance is required")
	}
	if entries, _ := os.ReadDir(*dir); len(entries) > 0 {
		return fmt.Errorf("%s is not empty", *dir)
	}
	ctx := context.Background()
	c, err := openCorpus(ctx, *data, *archive)
	if err != nil {
		return err
	}
	defer c.Close()
	refs, err := c.streams(ctx)
	if err != nil {
		return err
	}
	s, err := lance.Open(lance.Options{Dir: *dir, Dim: 8})
	if err != nil {
		return err
	}
	defer s.Close()

	rng := rand.New(rand.NewSource(1))
	prng := rand.New(rand.NewSource(2))
	sample := make([]store.Chunk, 0, *sampleN)
	kinds := map[string]int{}
	var total, textBytes, streamBytes int64
	var batch []store.Chunk
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.Append(ctx, batch)
		batch = batch[:0]
		return err
	}
	start := time.Now()
	for _, ref := range refs {
		rc, _, err := c.blobs.Reader(ctx, ref.Hash)
		if err != nil {
			return fmt.Errorf("%s %s: %w", ref.RunID, ref.Name, err)
		}
		n, err := walkStream(rc, func(p part) error {
			if p.Kind == "compaction" {
				return nil
			}
			for i, piece := range split(p.Text, *max) {
				ch := store.Chunk{ID: chunkID(ref.Hash, p.Offset, i), RunID: ref.RunID, Turn: p.Turn, Kind: p.Kind, Text: piece}
				kinds[p.Kind]++
				total++
				textBytes += int64(len(piece))
				if len(sample) < *sampleN {
					sample = append(sample, ch)
				} else if j := rng.Int63n(total); j < int64(*sampleN) {
					sample[j] = ch
				}
				batch = append(batch, ch)
				for c := 1; c < *copies; c++ {
					cp := ch
					cp.ID, cp.RunID, cp.Text = fmt.Sprintf("%s~%d", ch.ID, c), fmt.Sprintf("%s-s%d", ch.RunID, c), perturb(prng, piece)
					batch = append(batch, cp)
					total++
					textBytes += int64(len(cp.Text))
				}
				if len(batch) >= 20000 {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			return nil
		})
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s %s: %w", ref.RunID, ref.Name, err)
		}
		streamBytes += n
	}
	if err := flush(); err != nil {
		return err
	}
	el := time.Since(start)
	out := map[string]any{"copies": *copies, "streams": len(refs), "stream_bytes": streamBytes, "chunks": total, "chunk_text_bytes": textBytes, "chunks_by_kind": kinds,
		"chunk_s": el.Seconds(), "chunk_mb_per_s_of_stream": float64(streamBytes) / 1e6 / el.Seconds(),
		"size_before_index_gb": float64(dirSize(*dir)) / (1 << 30)}
	start = time.Now()
	if err := s.BuildIndexes(ctx, store.IndexOptions{FullText: true}); err != nil {
		return err
	}
	out["index_build_s"] = time.Since(start).Seconds()
	out["size_after_index_gb"] = float64(dirSize(*dir)) / (1 << 30)
	if *sampleOut != "" {
		f, err := os.Create(*sampleOut)
		if err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		enc := json.NewEncoder(w)
		for i := range sample {
			if err := enc.Encode(&sample[i]); err != nil {
				return err
			}
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		out["sample"] = len(sample)
	}
	rss, peak := memory()
	out["rss_mib"], out["peak_rss_mib"] = rss, peak
	return emit(out)
}

// queries are things one would look for in this corpus.
var queries = []string{
	"oracle wrong", "giving up", "missing required argument", "syntax error", "collision detection",
	"the test is incorrect", "cannot be fixed", "timeout", "I will try a different approach", "the checker",
	"undefined is not a function", "off by one", "infinite loop", "permission denied", "no such file",
	"the patch failed to apply", "let me re-read the file", "all checks pass", "unexpected token", "the task is complete",
	"conveyor", "guardian", "willy", "portal", "air supply", "keyboard", "canvas", "requestAnimationFrame", "sprite", "jump",
}

// cmdText is T8, and the passage half of T15: full-text queries on the chunk
// dataset, then each hit's passage opened from its blob.
func cmdText(args []string) error {
	fs := flag.NewFlagSet("text", flag.ExitOnError)
	data := fs.String("data", "", "data root, to open passages (optional)")
	archive := fs.String("archive", "", "archive root")
	dir := fs.String("lance", "", "chunk dataset")
	passes := fs.Int("passes", 5, "warm passes over the query list")
	fs.Parse(args)
	ctx := context.Background()
	t0 := time.Now()
	s, err := lance.Open(lance.Options{Dir: *dir, Dim: 8})
	if err != nil {
		return err
	}
	defer s.Close()
	out := map[string]any{"open_ms": float64(time.Since(t0).Microseconds()) / 1000}
	rows, err := s.Count(ctx)
	if err != nil {
		return err
	}
	out["rows"] = rows
	rssRest, _ := memory()
	out["rss_at_rest_mib"] = rssRest

	// The first pass of a new process: indexes not yet loaded by it. The
	// operating system's file cache is not dropped; it is shared.
	var first, warm, filtered []time.Duration
	var hits []store.Hit
	empty := 0
	for _, q := range queries {
		t := time.Now()
		h, err := s.FullText(ctx, q, store.SearchOptions{Limit: 20})
		if err != nil {
			return fmt.Errorf("%q: %w", q, err)
		}
		first = append(first, time.Since(t))
		if len(h) == 0 {
			empty++
		}
		hits = append(hits, h...)
	}
	for p := 0; p < *passes; p++ {
		for _, q := range queries {
			t := time.Now()
			if _, err := s.FullText(ctx, q, store.SearchOptions{Limit: 20}); err != nil {
				return err
			}
			warm = append(warm, time.Since(t))
		}
	}
	for i, q := range queries {
		if len(hits) == 0 {
			break
		}
		run := hits[(i*37)%len(hits)].RunID
		t := time.Now()
		if _, err := s.FullText(ctx, q, store.SearchOptions{Limit: 20, RunIDs: []string{run}}); err != nil {
			return err
		}
		filtered = append(filtered, time.Since(t))
	}
	out["queries"], out["queries_without_hits"], out["hits"] = len(queries), empty, len(hits)
	out["first_pass_ms"], out["warm_ms"], out["filtered_by_run_ms"] = summarise(first), summarise(warm), summarise(filtered)

	if *data != "" && len(hits) > 0 {
		b, err := blob.Open(blob.Options{Root: *data, ArchiveRoot: *archive})
		if err != nil {
			return err
		}
		defer b.Close()
		byTier := map[string][]time.Duration{}
		rng := rand.New(rand.NewSource(1))
		for i := 0; i < 300; i++ {
			h, off, ok := parseChunkID(hits[rng.Intn(len(hits))].ID)
			if !ok {
				continue
			}
			t := time.Now()
			_, info, err := b.ReadRange(ctx, h, off, 64<<10)
			if err != nil {
				return fmt.Errorf("passage %s@%d: %w", h, off, err)
			}
			byTier[info.Tier.String()] = append(byTier[info.Tier.String()], time.Since(t))
		}
		passages := map[string]latency{}
		for tier, ds := range byTier {
			passages[tier] = summarise(ds)
		}
		out["open_passage_64kib_ms"] = passages
	}
	rss, peak := memory()
	out["rss_mib"], out["peak_rss_mib"] = rss, peak
	return emit(out)
}

// cmdRegex is T9: a regular expression over every line of every stream, read
// from the blobs, with no index.
func cmdRegex(args []string) error {
	fs := flag.NewFlagSet("regex", flag.ExitOnError)
	data := fs.String("data", "", "data root")
	archive := fs.String("archive", "", "archive root")
	pattern := fs.String("pattern", `(?i)(oracle|checker|test) (is|was|must be|seems) (wrong|broken|incorrect)`, "regular expression")
	workers := fs.Int("workers", 4, "streams scanned at once")
	skip := fs.Bool("skip-deltas", false, "do not match the streaming deltas, whose text the closing event repeats in full")
	fs.Parse(args)
	re, err := regexp.Compile(*pattern)
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, err := openCorpus(ctx, *data, *archive)
	if err != nil {
		return err
	}
	defer c.Close()
	refs, err := c.streams(ctx)
	if err != nil {
		return err
	}
	var bytesRead, lines, matches, compressed, matched int64
	var firstErr atomic.Value
	jobs := make(chan streamRef)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ref := range jobs {
				rc, info, err := c.blobs.Reader(ctx, ref.Hash)
				if err != nil {
					firstErr.CompareAndSwap(nil, err)
					continue
				}
				atomic.AddInt64(&compressed, info.CompressedSize)
				br := bufio.NewReaderSize(rc, 1<<20)
				for {
					line, err := br.ReadBytes('\n')
					if len(line) > 0 {
						atomic.AddInt64(&bytesRead, int64(len(line)))
						atomic.AddInt64(&lines, 1)
						if *skip && isDelta(line) {
							continue
						}
						atomic.AddInt64(&matched, int64(len(line)))
						if re.Match(line) {
							atomic.AddInt64(&matches, 1)
						}
					}
					if err != nil {
						if err != io.EOF {
							firstErr.CompareAndSwap(nil, err)
						}
						break
					}
				}
				rc.Close()
			}
		}()
	}
	for _, ref := range refs {
		jobs <- ref
	}
	close(jobs)
	wg.Wait()
	if err, _ := firstErr.Load().(error); err != nil {
		return err
	}
	el := time.Since(start)
	return emit(map[string]any{"pattern": *pattern, "workers": *workers, "streams": len(refs), "bytes": bytesRead, "compressed_bytes": compressed,
		"skip_deltas": *skip, "bytes_matched_against": matched,
		"lines": lines, "matching_lines": matches, "seconds": el.Seconds(), "mb_per_s": float64(bytesRead) / 1e6 / el.Seconds()})
}

// cmdArchiveHalf moves the older half of the live blobs to the archive tier,
// for T15.
func cmdArchiveHalf(args []string) error {
	fs := flag.NewFlagSet("archive-half", flag.ExitOnError)
	data := fs.String("data", "", "data root")
	archive := fs.String("archive", "", "archive root")
	fs.Parse(args)
	if *archive == "" {
		return errors.New("-archive is required")
	}
	ctx := context.Background()
	b, err := blob.Open(blob.Options{Root: *data, ArchiveRoot: *archive})
	if err != nil {
		return err
	}
	defer b.Close()
	var times []time.Time
	if err := b.Walk(ctx, func(_ blob.Hash, info blob.Info) error {
		if info.Tier == blob.TierLive {
			times = append(times, info.ModTime)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(times) < 2 {
		return errors.New("fewer than two live blobs")
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	cutoff := times[len(times)/2]
	start := time.Now()
	rep, err := b.ArchiveOlderThan(ctx, 0, cutoff)
	if err != nil {
		return err
	}
	el := time.Since(start)
	return emit(map[string]any{"live_blobs_before": len(times), "cutoff": cutoff, "report": rep, "seconds": el.Seconds(),
		"mb_per_s": float64(rep.BytesMoved) / 1e6 / el.Seconds()})
}
