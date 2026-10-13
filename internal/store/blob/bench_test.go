package blob

import (
	"bytes"
	"math/rand/v2"
	"os"
	"testing"
)

// BenchmarkPut measures Put of a 64 MiB event-stream-like blob (fsync and
// rename included), with the default level and chunk size.
func BenchmarkPut(b *testing.B) {
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"text-64MiB", textBytes(1, 64<<20)},
		{"random-64MiB", randBytes(1, 64<<20)},
	} {
		b.Run(c.name, func(b *testing.B) {
			s := open(b, Options{Root: b.TempDir()})
			b.SetBytes(int64(len(c.data)))
			var comp int64
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				// A different blob each time, or Put would only deduplicate.
				c.data[0], c.data[1], c.data[2] = byte(i), byte(i>>8), byte(i>>16)
				h, _, err := s.Put(ctx, bytes.NewReader(c.data))
				if err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				info, err := s.Stat(h)
				if err != nil {
					b.Fatal(err)
				}
				comp = info.CompressedSize
				// Keep the footprint to one blob, whatever b.N is.
				if err := os.Remove(blobPath(s.Root(), h)); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.ReportMetric(float64(len(c.data))/float64(comp), "ratio")
		})
	}
}

// BenchmarkReadRange measures reading 1 MiB at a random offset of a 256 MiB
// blob stored with the default 4 MiB chunks.
func BenchmarkReadRange(b *testing.B) {
	const size, n = 256 << 20, 1 << 20
	s := open(b, Options{Root: b.TempDir()})
	h, _, err := s.Put(ctx, bytes.NewReader(textBytes(2, size)))
	if err != nil {
		b.Fatal(err)
	}
	r := rand.New(rand.NewPCG(1, 2))
	b.Run("1MiB-of-256MiB", func(b *testing.B) {
		b.SetBytes(n)
		for b.Loop() {
			got, _, err := s.ReadRange(ctx, h, r.Int64N(size-n), n)
			if err != nil || len(got) != n {
				b.Fatal(len(got), err)
			}
		}
	})
	b.Run("1MiB-of-256MiB-parallel", func(b *testing.B) {
		b.SetBytes(n)
		b.RunParallel(func(pb *testing.PB) {
			r := rand.New(rand.NewPCG(rand.Uint64(), 3))
			for pb.Next() {
				got, _, err := s.ReadRange(ctx, h, r.Int64N(size-n), n)
				if err != nil || len(got) != n {
					b.Fatal(len(got), err)
				}
			}
		})
	})
}
