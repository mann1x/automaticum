// Package store defines the one interface through which automaticum reaches
// its data, so the engines behind it can be revisited (spec 080 §6).
//
// Three parts sit behind it:
//
//   - blobs: raw streams, wire captures and logs, content-addressed on disk
//     (package blob). They are the source of truth for raw material.
//   - State: runs and everything derived from them that is queried with SQL.
//   - Search: text chunks with their vectors and the full-text index.
//
// Search is derived data and can always be rebuilt from blobs. So can the
// derived tables of State (tool calls, findings). What cannot be rebuilt is
// written through State transactionally: verdicts, resolved arms, lane state.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mann1x/automaticum/internal/store/blob"
)

// ErrNotFound is returned when a run, blob or chunk does not exist.
var ErrNotFound = errors.New("store: not found")

// Verdict is the outcome of a run (spec 020 §3). No other value is valid.
type Verdict string

const (
	Pass    Verdict = "PASS"
	Fail    Verdict = "FAIL"
	Timeout Verdict = "TIMEOUT"
	Aborted Verdict = "ABORTED"
	Invalid Verdict = "INVALID"
	Skipped Verdict = "SKIPPED"
)

// Valid reports whether v is one of the six verdicts.
func (v Verdict) Valid() bool {
	switch v {
	case Pass, Fail, Timeout, Aborted, Invalid, Skipped:
		return true
	}
	return false
}

// Unknown marks a field whose value could not be recovered. It is never a
// default: a reader must be able to tell "unknown" from any real value.
const Unknown = "unknown"

// Run is one arm on one task, once.
type Run struct {
	ID          string // sortable by time; imported runs keep their original id
	Batch       string
	Cell        string
	Cohort      string
	Campaign    string
	Suite       string
	TaskID      string
	TaskVersion int
	ArmID       string
	Lane        string
	Host        string

	Verdict         Verdict
	OriginalVerdict string // the word an older harness used, kept verbatim
	VerdictReason   string // which limit, gate or check produced the verdict
	// PriorVerdict is set when a run was marked INVALID after it finished.
	PriorVerdict   Verdict
	AgentEnd       string // completed, aborted, max_iterations, stopped, mistake_limit
	ClaimedSuccess *bool  // nil when not known

	StartedAt time.Time
	EndedAt   time.Time
	WallMS    int64 // paused intervals excluded
	Paused    bool

	// Arm fields repeated as columns because reports group by them.
	Provider string
	Model    string
	Endpoint string
	Thinking string
	Harness  string // SDK build under test

	// Arm is the fully resolved arm; Provenance is everything else recorded
	// about how the run was made (spec 020 §1). Both are JSON objects.
	Arm        json.RawMessage
	Provenance json.RawMessage

	// Imported runs come from an older harness. Missing lists what the
	// source did not record, so reports can weigh "as configured" values
	// below "as sent" ones.
	Imported        bool
	ImporterVersion string
	Missing         []string
}

// BlobRef attaches a blob to a run under a kind and a name.
type BlobRef struct {
	RunID string
	Kind  string // events, wire, session, server_log, runtime_log, workspace_before, workspace_after, oracle, runner_stdout, ...
	Name  string // distinguishes several blobs of one kind; may be empty
	Hash  blob.Hash
	Size  int64 // uncompressed bytes
}

// ToolCall is one tool call of a run, as derived from its event stream.
type ToolCall struct {
	RunID      string
	Turn       int
	Seq        int // position within the run
	Tool       string
	OK         bool
	ErrorClass string // empty when OK
	ParsePath  string // native, repaired, from_text, from_reasoning
	DurationMS int64
}

// Finding is something a detector found in a run.
type Finding struct {
	RunID           string
	Detector        string
	DetectorVersion string
	Severity        string
	Turn            int
	Count           int
	Evidence        json.RawMessage
}

// LaneState is the current state of one lane. It changes often.
type LaneState struct {
	Lane      string
	State     string // idle, running, paused, draining, offline, quarantined
	RunID     string
	UpdatedAt time.Time
	Detail    json.RawMessage
}

// RunFilter selects runs. Zero fields do not filter.
type RunFilter struct {
	Suite    string
	TaskID   string
	ArmID    string
	Model    string
	Verdict  Verdict
	Imported *bool
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

// State holds runs and their derived tables. One process writes.
type State interface {
	UpsertRun(ctx context.Context, r *Run) error
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRuns(ctx context.Context, f RunFilter) ([]Run, error)
	CountRuns(ctx context.Context, f RunFilter) (int64, error)

	// AddBlobRefs, ReplaceToolCalls and ReplaceFindings are idempotent per
	// run, so an ingest that died can simply be run again.
	AddBlobRefs(ctx context.Context, refs []BlobRef) error
	BlobRefs(ctx context.Context, runID string) ([]BlobRef, error)
	ReplaceToolCalls(ctx context.Context, runID string, calls []ToolCall) error
	ReplaceFindings(ctx context.Context, runID string, findings []Finding) error

	SetLaneState(ctx context.Context, s LaneState) error
	LaneStates(ctx context.Context) ([]LaneState, error)

	// Query runs read-only SQL for reports and statistics. It is the one
	// place the engine's dialect shows through the interface.
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// Checkpoint flushes the write-ahead log into the database file.
	Checkpoint(ctx context.Context) error
	Close() error
}

// Chunk is a piece of text from a run, with the vector that embeds it.
type Chunk struct {
	ID       string // stable: derived from run, kind, turn and position
	RunID    string
	Turn     int
	Kind     string // reasoning, text, tool_args, tool_result, server_log, ...
	Text     string
	Vector   []float32 // nil when not embedded yet
	Embedder string    // identity and version of the embedder; empty when Vector is nil
}

// Hit is a chunk matched by a search, best first.
type Hit struct {
	Chunk
	Score float32 // higher is better, whatever the kind of search
}

// IndexOptions tunes Search.BuildIndexes. Zero values choose defaults.
type IndexOptions struct {
	FullText      bool
	Vector        bool
	Partitions    int // IVF partitions; 0 derives it from the row count
	SubVectors    int // product quantisation; 0 derives it from the dimension
	MaxIterations int
}

// SearchOptions narrows a search. Zero fields do not narrow.
type SearchOptions struct {
	Limit  int
	RunIDs []string
	Kinds  []string
	// Probes and Refine trade recall for speed in vector search.
	Probes int
	Refine int
	// Exact bypasses the vector index.
	Exact bool
}

// Search holds text chunks, their vectors and the full-text index.
type Search interface {
	// UpsertChunks inserts chunks, replacing any with the same ID.
	UpsertChunks(ctx context.Context, chunks []Chunk) error
	DeleteRun(ctx context.Context, runID string) error
	Count(ctx context.Context) (int64, error)

	BuildIndexes(ctx context.Context, o IndexOptions) error
	// Optimize compacts fragments and folds new rows into the indexes.
	Optimize(ctx context.Context) error

	FullText(ctx context.Context, query string, o SearchOptions) ([]Hit, error)
	Nearest(ctx context.Context, vector []float32, o SearchOptions) ([]Hit, error)
	// Hybrid fuses a full-text and a vector search by reciprocal rank.
	Hybrid(ctx context.Context, query string, vector []float32, o SearchOptions) ([]Hit, error)

	Close() error
}

// Store is the whole store.
type Store struct {
	Blobs  *blob.Store
	State  State
	Search Search // nil in builds without the search engine
}

// Close closes every part and returns the first error.
func (s *Store) Close() error {
	var first error
	if s.Search != nil {
		first = s.Search.Close()
	}
	if s.State != nil {
		if err := s.State.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
