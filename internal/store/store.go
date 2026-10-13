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
	// PriorVerdict is what the verdict was before it was changed: a run
	// marked INVALID after it finished, or an imported run whose recorded
	// verdict was contradicted by how it ended.
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

// Metric is one number measured on a run at ingest (spec 070 §2). Metrics
// are derived: they are replaced per run and can be recomputed from blobs.
type Metric struct {
	RunID string
	Name  string // dotted, e.g. "tokens.completion", "tool_calls.editor"
	Value float64
}

// LaneState is the current state of one lane. It changes often.
type LaneState struct {
	Lane      string
	State     string // idle, running, paused, draining, offline, quarantined
	RunID     string
	UpdatedAt time.Time
	Detail    json.RawMessage
}

// QueueItem is a run that was asked for and has not finished. Its id is
// already the run's id. The arm and the task are named, not resolved: they
// are read when the run starts, and fixed from then on.
type QueueItem struct {
	RunID      string
	Batch      string
	Arm        string
	Task       string // "suite/task"
	Lane       string
	EnqueuedAt time.Time
}

// RunFilter selects runs. Zero fields do not filter.
type RunFilter struct {
	Suite    string
	TaskID   string
	ArmID    string
	Model    string
	Verdict  Verdict
	Batch    string
	Imported *bool
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
	// Newest lists the runs that started last first; otherwise by id.
	Newest bool
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
	ReplaceMetrics(ctx context.Context, runID string, metrics []Metric) error
	Metrics(ctx context.Context, runID string) ([]Metric, error)
	ToolCalls(ctx context.Context, runID string) ([]ToolCall, error)
	Findings(ctx context.Context, runID string) ([]Finding, error)

	SetLaneState(ctx context.Context, s LaneState) error
	LaneStates(ctx context.Context) ([]LaneState, error)

	// The queue survives a restart. Enqueue ignores an item whose run id
	// is already queued; Queue lists oldest first.
	Enqueue(ctx context.Context, items []QueueItem) error
	Queue(ctx context.Context) ([]QueueItem, error)
	Unqueue(ctx context.Context, runID string) error

	// Query runs read-only SQL for reports and statistics. It is the one
	// place the engine's dialect shows through the interface.
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// Checkpoint flushes the write-ahead log into the database file.
	Checkpoint(ctx context.Context) error
	Close() error
}

// Chunk is a piece of text from a run, with the vector that embeds it.
type Chunk struct {
	ID    string // stable: derived from run, kind, turn and position
	RunID string
	Turn  int
	Kind  string // reasoning, text, tool_args, tool_result, server_log, ...
	Text  string
	// Blob and Offset locate the chunk's source: the blob it was cut from and
	// the byte offset, in the uncompressed blob, of the line that holds it.
	// A hit opens its passage with a range read there.
	Blob     blob.Hash
	Offset   int64
	Vector   []float32 // nil when not embedded yet
	Embedder string    // identity and version of the embedder; empty when Vector is nil
}

// Hit is a chunk matched by a search, best first.
type Hit struct {
	Chunk
	Score float32 // higher is better, whatever the kind of search
}

// DefaultRefine is the refine factor of a vector search that does not set
// one. The spike measured recall between 0.05 and 0.6 without the step and
// above 0.9 with this factor (spec 080 section 7.4).
const DefaultRefine = 30

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
	// Probes and Refine trade recall for speed in vector search. Refine
	// re-ranks that many times Limit candidates by their full vectors; zero
	// means DefaultRefine and a negative value switches the step off.
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
