package legacyimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
)

// Sink is where an import writes. A store adapter implements it with the
// blob store's Put and the State methods of the same names.
//
// PutBlob must consume r to its end or return an error. When
// Options.Workers is above one, PutBlob is called from several goroutines;
// the other three methods are always called one at a time.
type Sink interface {
	PutBlob(ctx context.Context, r io.Reader) (hash string, size int64, err error)
	UpsertRun(ctx context.Context, r *Run) error
	AddBlobRefs(ctx context.Context, refs []BlobRef) error
	ReplaceToolCalls(ctx context.Context, runID string, calls []ToolCall) error
}

// Resumer is an optional extension of Sink. When the sink implements it, a
// run whose stored fingerprint equals the fingerprint of its source is
// skipped. An adapter over the store can implement it with GetRun and
// Fingerprint.
type Resumer interface {
	ImportedFingerprint(ctx context.Context, runID string) (fingerprint string, found bool, err error)
}

// Fingerprint returns the source fingerprint recorded in a run's provenance,
// or "" when the run was not completely imported.
func Fingerprint(r *Run) string {
	if r == nil || len(r.Provenance) == 0 {
		return ""
	}
	var p struct {
		Fingerprint string `json:"source_fingerprint"`
		Complete    bool   `json:"import_complete"`
	}
	if json.Unmarshal(r.Provenance, &p) != nil || !p.Complete {
		return ""
	}
	return p.Fingerprint
}

// MemorySink keeps everything in memory. It is meant for tests: blobs are
// hashed and counted, and their bytes are kept only when KeepBlobs is set.
type MemorySink struct {
	KeepBlobs bool

	mu        sync.Mutex
	Runs      map[string]Run
	Refs      map[string][]BlobRef
	Calls     map[string][]ToolCall
	BlobSizes map[string]int64
	BlobData  map[string][]byte
	Puts      int
	Upserts   int
}

// NewMemorySink returns an empty MemorySink.
func NewMemorySink() *MemorySink {
	return &MemorySink{
		Runs:      map[string]Run{},
		Refs:      map[string][]BlobRef{},
		Calls:     map[string][]ToolCall{},
		BlobSizes: map[string]int64{},
		BlobData:  map[string][]byte{},
	}
}

// PutBlob hashes r with SHA-256.
func (m *MemorySink) PutBlob(ctx context.Context, r io.Reader) (string, int64, error) {
	h := sha256.New()
	var w io.Writer = h
	var keep *limitedBuffer
	if m.KeepBlobs {
		keep = &limitedBuffer{}
		w = io.MultiWriter(h, keep)
	}
	n, err := io.Copy(w, r)
	if err != nil {
		return "", n, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Puts++
	m.BlobSizes[sum] = n
	if keep != nil {
		m.BlobData[sum] = keep.b
	}
	return sum, n, nil
}

type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.b = append(l.b, p...)
	return len(p), nil
}

// UpsertRun stores a copy of r.
func (m *MemorySink) UpsertRun(ctx context.Context, r *Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Upserts++
	m.Runs[r.ID] = *r
	return nil
}

// AddBlobRefs is idempotent per (run, kind, name).
func (m *MemorySink) AddBlobRefs(ctx context.Context, refs []BlobRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ref := range refs {
		list := m.Refs[ref.RunID]
		replaced := false
		for i := range list {
			if list[i].Kind == ref.Kind && list[i].Name == ref.Name {
				list[i] = ref
				replaced = true
				break
			}
		}
		if !replaced {
			list = append(list, ref)
		}
		m.Refs[ref.RunID] = list
	}
	return nil
}

// ReplaceToolCalls replaces the tool calls of one run.
func (m *MemorySink) ReplaceToolCalls(ctx context.Context, runID string, calls []ToolCall) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls[runID] = append([]ToolCall(nil), calls...)
	return nil
}

// ImportedFingerprint implements Resumer.
func (m *MemorySink) ImportedFingerprint(ctx context.Context, runID string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Runs[runID]
	if !ok {
		return "", false, nil
	}
	fp := Fingerprint(&r)
	return fp, fp != "", nil
}
