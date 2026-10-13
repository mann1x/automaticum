//go:build lance

package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// The old corpus has no wire capture, so the request bodies are simulated
// from the event streams: request N of a run is a fixed preamble (standing in
// for the system prompt and tool definitions) followed by every message up to
// turn N, and a compaction replaces the oldest messages with a summary. This
// reproduces the property that matters for deduplication (each request
// repeats almost all of the one before) and nothing else about a real body.

// dedupStore counts what a content-addressed store would hold.
type dedupStore struct {
	seen       map[[32]byte]struct{}
	enc        *zstd.Encoder
	pieces     int64 // references written
	unique     int64 // distinct pieces
	rawUnique  int64 // bytes of distinct pieces
	compressed int64 // those bytes, each piece compressed alone
	buf        []byte
}

func newDedupStore() *dedupStore {
	enc, _ := zstd.NewWriter(nil)
	return &dedupStore{seen: map[[32]byte]struct{}{}, enc: enc}
}

func (d *dedupStore) add(p []byte) {
	d.pieces++
	h := sha256.Sum256(p)
	if _, ok := d.seen[h]; ok {
		return
	}
	d.seen[h] = struct{}{}
	d.unique++
	d.rawUnique += int64(len(p))
	d.buf = d.enc.EncodeAll(p, d.buf[:0])
	d.compressed += int64(len(d.buf))
}

// total is the bytes on disk: the pieces, and a 32-byte reference per use.
func (d *dedupStore) total() int64 { return d.compressed + 32*d.pieces }

var gear = func() (t [256]uint64) {
	rng := rand.New(rand.NewSource(7))
	for i := range t {
		t[i] = rng.Uint64()
	}
	return
}()

// cdc cuts b at content-defined boundaries and calls fn for each piece.
func cdc(b []byte, minSize, maxSize int, mask uint64, fn func([]byte)) {
	for len(b) > 0 {
		n := len(b)
		if n > minSize {
			var h uint64
			end := min(n, maxSize)
			n = end
			for i := minSize; i < end; i++ {
				h = h<<1 + gear[b[i]]
				if h&mask == 0 {
					n = i + 1
					break
				}
			}
		}
		fn(b[:n])
		b = b[n:]
	}
}

func cmdDedup(args []string) error {
	fs := flag.NewFlagSet("dedup", flag.ExitOnError)
	data := fs.String("data", "", "data root")
	archive := fs.String("archive", "", "archive root")
	every := fs.Int("every", 4, "use one run in this many")
	preambleKiB := fs.Int("preamble-kib", 60, "size of the fixed preamble")
	fs.Parse(args)
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

	rng := rand.New(rand.NewSource(11))
	var pre strings.Builder
	for pre.Len() < *preambleKiB<<10 {
		pre.WriteString(words[rng.Intn(len(words))])
		pre.WriteByte(' ')
	}
	preamble := []byte(`{"role":"system","content":` + strconv.Quote(pre.String()) + `}`)

	byPart, small, large := newDedupStore(), newDedupStore(), newDedupStore()
	whole, _ := zstd.NewWriter(nil)
	var raw, wholeBytes, requests, streamBytes, streamCompressed int64
	var runs, compactions int
	var body, zbuf []byte
	seenRun := map[string]bool{}
	n := 0
	for _, ref := range refs {
		if !seenRun[ref.RunID] {
			seenRun[ref.RunID] = true
			n++
		}
		if (n-1)%*every != 0 {
			continue
		}
		rc, info, err := c.blobs.Reader(ctx, ref.Hash)
		if err != nil {
			return err
		}
		runs++
		streamCompressed += info.CompressedSize
		msgs := [][]byte{preamble}
		lastTurn := 0
		send := func() {
			requests++
			body = body[:0]
			for i, m := range msgs {
				if i > 0 {
					body = append(body, ',')
				}
				body = append(body, m...)
				byPart.add(m)
			}
			raw += int64(len(body))
			zbuf = whole.EncodeAll(body, zbuf[:0])
			wholeBytes += int64(len(zbuf))
			cdc(body, 2<<10, 64<<10, 1<<13-1, small.add)
			cdc(body, 16<<10, 512<<10, 1<<16-1, large.add)
		}
		size, err := walkStream(rc, func(p part) error {
			if p.Turn != lastTurn {
				if lastTurn > 0 {
					send()
				}
				lastTurn = p.Turn
			}
			switch p.Kind {
			case "compaction":
				compactions++
				keep := len(msgs) - (len(msgs)-1)*6/10
				summary := []byte(fmt.Sprintf(`{"role":"user","content":"summary of %s up to turn %d: %s"}`, ref.RunID, p.Turn, pre.String()[:4<<10]))
				msgs = append([][]byte{preamble, summary}, msgs[len(msgs)-keep+1:]...)
			case "reasoning", "text":
				msgs = append(msgs, []byte(`{"role":"assistant","`+p.Kind+`":`+strconv.Quote(p.Text)+`}`))
			case "tool_args":
				msgs = append(msgs, []byte(`{"role":"assistant","tool_call":`+strconv.Quote(p.Text)+`}`))
			case "tool_result":
				msgs = append(msgs, []byte(`{"role":"tool","content":`+strconv.Quote(p.Text)+`}`))
			}
			return nil
		})
		rc.Close()
		if err != nil {
			return err
		}
		streamBytes += size
	}
	gb := func(b int64) float64 { return float64(b) / (1 << 30) }
	scheme := func(d *dedupStore) map[string]any {
		return map[string]any{"gib": gb(d.total()), "percent_of_raw": 100 * float64(d.total()) / float64(raw),
			"references": d.pieces, "distinct_pieces": d.unique, "distinct_raw_gib": gb(d.rawUnique), "reference_gib": gb(32 * d.pieces)}
	}
	return emit(map[string]any{
		"simulated": true, "runs": runs, "requests": requests, "compactions": compactions,
		"request_bodies_raw_gib":                gb(raw),
		"event_streams_raw_gib":                 gb(streamBytes),
		"event_streams_stored_gib":              gb(streamCompressed),
		"whole_body_compressed":                 map[string]any{"gib": gb(wholeBytes), "percent_of_raw": 100 * float64(wholeBytes) / float64(raw)},
		"by_message_part":                       scheme(byPart),
		"content_defined_avg_8kib":              scheme(small),
		"content_defined_avg_64kib":             scheme(large),
		"wire_capture_to_event_stream_stored_x": float64(byPart.total()) / float64(streamCompressed),
	})
}
