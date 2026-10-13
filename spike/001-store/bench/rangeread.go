//go:build lance

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/mann1x/automaticum/internal/store/blob"
)

// textReader produces pseudo-JSON-lines text of a given length: compressible
// like an event stream, without being a copy of one.
type textReader struct {
	left int64
	rng  *rand.Rand
	buf  []byte
}

func (r *textReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && r.left > 0 {
		if len(r.buf) == 0 {
			r.buf = fmt.Appendf(r.buf[:0], `{"ts":%d,"type":"agent_event","event":{"type":"content_update","contentType":"reasoning","text":"%s %s %s"}}`+"\n",
				r.rng.Int63(), words[r.rng.Intn(len(words))], words[r.rng.Intn(len(words))], words[r.rng.Intn(len(words))])
		}
		c := copy(p[n:], r.buf)
		if int64(c) > r.left {
			c = int(r.left)
		}
		r.buf = r.buf[c:]
		n += c
		r.left -= int64(c)
	}
	return n, nil
}

func cmdRange(args []string) error {
	fs := flag.NewFlagSet("range", flag.ExitOnError)
	root := fs.String("root", "", "live blob root")
	archive := fs.String("archive", "", "archive blob root")
	gb := fs.Float64("gb", 4, "size of the blob, GiB uncompressed")
	reads := fs.Int("reads", 200, "range reads per tier")
	fs.Parse(args)
	if *root == "" || *archive == "" {
		return errors.New("-root and -archive are required")
	}
	ctx := context.Background()
	s, err := blob.Open(blob.Options{Root: *root, ArchiveRoot: *archive})
	if err != nil {
		return err
	}
	defer s.Close()
	size := int64(*gb * (1 << 30))
	out := map[string]any{"blob_gib": *gb}

	start := time.Now()
	hash, n, err := s.Put(ctx, &textReader{left: size, rng: rand.New(rand.NewSource(1))})
	if err != nil {
		return err
	}
	el := time.Since(start)
	info, err := s.Stat(hash)
	if err != nil {
		return err
	}
	out["put_s"] = el.Seconds()
	out["put_mb_per_s"] = float64(n) / 1e6 / el.Seconds()
	out["compressed_ratio"] = float64(info.Size) / float64(info.CompressedSize)

	const length = 1 << 20
	measure := func() (latency, blob.Tier, error) {
		rng := rand.New(rand.NewSource(2))
		var ds []time.Duration
		var tier blob.Tier
		for i := 0; i < *reads; i++ {
			off := rng.Int63n(size - length)
			t := time.Now()
			b, in, err := s.ReadRange(ctx, hash, off, length)
			if err != nil {
				return latency{}, tier, err
			}
			if len(b) != length {
				return latency{}, tier, fmt.Errorf("read %d bytes, want %d", len(b), length)
			}
			ds = append(ds, time.Since(t))
			tier = in.Tier
		}
		return summarise(ds), tier, nil
	}
	live, tier, err := measure()
	if err != nil {
		return err
	}
	out["live_1mib_ms"], out["live_tier"] = live, fmt.Sprint(tier)

	start = time.Now()
	rep, err := s.ArchiveOlderThan(ctx, 0, time.Now().Add(time.Second))
	if err != nil {
		return err
	}
	out["archive_s"] = time.Since(start).Seconds()
	out["archive_report"] = rep
	arch, tier, err := measure()
	if err != nil {
		return err
	}
	out["archive_1mib_ms"], out["archive_tier"] = arch, fmt.Sprint(tier)
	if err := s.Verify(ctx, hash); err != nil {
		return fmt.Errorf("verify after archive: %w", err)
	}
	out["verified_after_archive"] = true
	return emit(out)
}
