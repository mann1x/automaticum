package blob

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	// DefaultChunkSize is the uncompressed size of one frame when
	// Options.ChunkSize is zero.
	DefaultChunkSize = 4 << 20
	// MaxChunkSize is the largest chunk size a blob may use.
	MaxChunkSize = 1 << 30
	// DefaultLevel is the zstd level used when Options.Level is zero.
	DefaultLevel = 3

	blobsDir = "blobs"
	tmpDir   = "tmp"
	blobExt  = ".zst"
	tmpExt   = ".tmp"
)

// Errors returned by the store. They are matched with errors.Is.
var (
	// ErrNotFound means no tier holds the blob.
	ErrNotFound = errors.New("blob: not found")
	// ErrCorrupt means a blob file is truncated, damaged, or does not hash
	// to its name.
	ErrCorrupt = errors.New("blob: corrupt")
	// ErrInvalidHash means a Hash is not 64 lowercase hex characters.
	ErrInvalidHash = errors.New("blob: invalid hash")
	// ErrNoArchive means the operation needs an archive root and none is
	// configured.
	ErrNoArchive = errors.New("blob: no archive root configured")
	// ErrArchiveUnavailable means the configured archive root cannot be
	// reached (missing, not a directory, or not writable when writing).
	ErrArchiveUnavailable = errors.New("blob: archive root unavailable")
)

// Tier says which root holds a blob.
type Tier uint8

// The tiers, in the order readers try them.
const (
	TierLive Tier = iota + 1
	TierArchive
)

// String returns "live" or "archive".
func (t Tier) String() string {
	switch t {
	case TierLive:
		return "live"
	case TierArchive:
		return "archive"
	}
	return fmt.Sprintf("tier(%d)", uint8(t))
}

// Options configures Open.
type Options struct {
	// Root is the live root. Required. It is created if missing.
	Root string
	// ArchiveRoot is the archive root. Optional. It is never created: a
	// missing directory means the archive is unreachable (for example an
	// unmounted disk), and creating it would write to the wrong place.
	ArchiveRoot string
	// Level is the zstd compression level, 1 to 22; 0 means DefaultLevel.
	// The encoder has four speed classes, so nearby levels may compress
	// alike.
	Level int
	// ChunkSize is the uncompressed size of one independently compressed
	// frame, and so the granularity of range reads; 0 means
	// DefaultChunkSize. Existing blobs keep the chunk size they were
	// written with.
	ChunkSize int
}

// Info describes a stored blob as found on one tier.
type Info struct {
	Hash           Hash
	Size           int64 // uncompressed bytes
	CompressedSize int64 // size of the file on disk, index included
	ChunkSize      int   // uncompressed bytes per frame
	Tier           Tier  // the tier that served this read
	ModTime        time.Time
}

// Failure is one blob or file an operation could not handle.
type Failure struct {
	Hash Hash
	Tier Tier
	Path string
	Err  error
}

// Store is a two-tier content-addressed blob store. Create one with Open.
type Store struct {
	live      string
	archive   string // empty when not configured
	chunkSize int

	enc *zstd.Encoder
	dec *zstd.Decoder

	locks     [256]sync.Mutex // serialise the commit of one hash
	archiveMu sync.Mutex      // one archive pass at a time

	tmpMu    sync.Mutex
	inflight map[string]struct{} // temporaries this process is writing

	bufs sync.Pool // *[]byte scratch buffers

	// hook, when set by a test, is called at named points of an archive
	// move; a returned error aborts the move right there.
	hook func(stage string) error
}

// Open opens the store, creating the live root if needed, and removes
// temporaries left behind by a process that died (on the live root, and on
// the archive root when it is reachable). An unreachable archive root is not
// an error here: reads of live blobs must keep working.
func Open(o Options) (*Store, error) {
	if o.Root == "" {
		return nil, errors.New("blob: open: no live root given")
	}
	if o.ChunkSize == 0 {
		o.ChunkSize = DefaultChunkSize
	}
	if o.ChunkSize < 1 || o.ChunkSize > MaxChunkSize {
		return nil, fmt.Errorf("blob: open: chunk size %d out of range 1..%d", o.ChunkSize, MaxChunkSize)
	}
	if o.Level == 0 {
		o.Level = DefaultLevel
	}
	if o.Level < 1 || o.Level > 22 {
		return nil, fmt.Errorf("blob: open: compression level %d out of range 1..22", o.Level)
	}
	live, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, fmt.Errorf("blob: open: live root: %w", err)
	}
	s := &Store{live: live, chunkSize: o.ChunkSize, inflight: make(map[string]struct{})}
	if o.ArchiveRoot != "" {
		if s.archive, err = filepath.Abs(o.ArchiveRoot); err != nil {
			return nil, fmt.Errorf("blob: open: archive root: %w", err)
		}
		if s.archive == s.live {
			return nil, errors.New("blob: open: archive root is the live root")
		}
	}
	for _, d := range []string{blobsDir, tmpDir} {
		if err := os.MkdirAll(filepath.Join(live, d), 0o755); err != nil {
			return nil, fmt.Errorf("blob: open: %w", err)
		}
	}
	if err := removeTemps(filepath.Join(live, tmpDir)); err != nil {
		return nil, fmt.Errorf("blob: open: remove stale temporaries: %w", err)
	}
	if s.archive != "" && s.archiveReachable() == nil {
		// Best effort: the archive may be read-only for this process.
		_ = removeTemps(filepath.Join(s.archive, tmpDir))
	}
	s.enc, err = zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(o.Level)),
		zstd.WithEncoderCRC(true),
		zstd.WithZeroFrames(true),
		zstd.WithEncoderConcurrency(runtime.GOMAXPROCS(0)))
	if err != nil {
		return nil, fmt.Errorf("blob: open: zstd encoder: %w", err)
	}
	s.dec, err = zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(0),
		zstd.WithDecodeAllCapLimit(true))
	if err != nil {
		return nil, fmt.Errorf("blob: open: zstd decoder: %w", err)
	}
	return s, nil
}

// Close releases the compressor. The store must not be used afterwards.
// Nothing on disk depends on Close being called.
func (s *Store) Close() error {
	s.dec.Close()
	return s.enc.Close()
}

// Root returns the live root.
func (s *Store) Root() string { return s.live }

// ArchiveRoot returns the archive root, or "" when none is configured.
func (s *Store) ArchiveRoot() string { return s.archive }

// path is where blob h lives under root. h must be valid.
func blobPath(root string, h Hash) string {
	hs := string(h)
	return filepath.Join(root, blobsDir, hs[:2], hs[2:4], hs+blobExt)
}

func (s *Store) lock(h Hash) *sync.Mutex {
	var b [1]byte
	_, _ = hex.Decode(b[:], []byte(h[:2]))
	return &s.locks[b[0]]
}

// archiveReachable reports whether the archive root is configured and is an
// existing directory.
func (s *Store) archiveReachable() error {
	if s.archive == "" {
		return ErrNoArchive
	}
	st, err := os.Stat(s.archive)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArchiveUnavailable, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrArchiveUnavailable, s.archive)
	}
	return nil
}

// createTemp creates a new temporary file in dir and records it as in
// flight, so Check does not report it as stray. The caller calls untrack
// when the file is renamed or removed.
func (s *Store) createTemp(dir, prefix string) (string, *os.File, error) {
	for range 16 {
		var r [8]byte
		if _, err := rand.Read(r[:]); err != nil {
			return "", nil, err
		}
		name := filepath.Join(dir, prefix+"-"+hex.EncodeToString(r[:])+tmpExt)
		s.tmpMu.Lock()
		s.inflight[name] = struct{}{}
		s.tmpMu.Unlock()
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return name, f, nil
		}
		s.untrack(name)
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("no free temporary name in %s", dir)
}

func (s *Store) untrack(name string) {
	s.tmpMu.Lock()
	delete(s.inflight, name)
	s.tmpMu.Unlock()
}

func (s *Store) isInflight(name string) bool {
	s.tmpMu.Lock()
	_, ok := s.inflight[name]
	s.tmpMu.Unlock()
	return ok
}

// getBuf returns a scratch buffer with capacity for at least n bytes.
func (s *Store) getBuf(n int) []byte {
	if p, ok := s.bufs.Get().(*[]byte); ok && cap(*p) >= n {
		return (*p)[:0]
	}
	return make([]byte, 0, n)
}

func (s *Store) putBuf(b []byte) {
	if cap(b) == 0 {
		return
	}
	s.bufs.Put(&b)
}

// removeTemps deletes everything in a temporary directory. A missing
// directory is fine.
func removeTemps(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range ents {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// syncDir flushes a directory so a rename or removal inside it survives a
// power loss. Windows cannot fsync a directory and does not need to; other
// failures are ignored too, because the data itself is already synced and
// some filesystems refuse to sync directories.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// mkdirBlobDir creates the fan-out directories for a blob path and makes
// newly created ones durable.
func mkdirBlobDir(final string) error {
	dir := filepath.Dir(final)
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	syncDir(filepath.Dir(dir))
	syncDir(filepath.Dir(filepath.Dir(dir)))
	return nil
}
