package legacyimport

import "github.com/mann1x/automaticum/internal/store"

// The run record is the store's. The names are aliased here so the rest of
// the package, and its callers, read the same either way.
type (
	// Verdict is the outcome of a run (spec 020 section 3).
	Verdict = store.Verdict
	// Run is one arm on one task, once.
	Run = store.Run
	// BlobRef attaches a blob to a run under a kind and a name.
	BlobRef = store.BlobRef
	// ToolCall is one tool call of a run, as derived from its event stream.
	ToolCall = store.ToolCall
	// Finding is something a detector, or here the import, noticed in a run.
	Finding = store.Finding
)

// The six verdicts.
const (
	Pass    = store.Pass
	Fail    = store.Fail
	Timeout = store.Timeout
	Aborted = store.Aborted
	Invalid = store.Invalid
	Skipped = store.Skipped
)

// Unknown marks a field whose value could not be recovered. It is never a
// default.
const Unknown = store.Unknown
