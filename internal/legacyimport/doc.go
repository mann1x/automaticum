// Package legacyimport reads the run corpus of the old test harness and maps
// it onto the run record of automaticum (spec 100).
//
// The old harness is paused, not retired. This package only ever opens its
// files for reading: it never creates, renames, changes or removes anything
// under the legacy root or the state archive.
//
// The package is written against a small destination interface (Sink) so it
// does not depend on the store's engines. Streams are never held in memory:
// an event stream is scanned line by line while it is copied to the sink.
package legacyimport

// ImporterVersion is recorded on every imported run.
const ImporterVersion = "legacyimport/0.1.1"
