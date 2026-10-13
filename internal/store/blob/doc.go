// Package blob is the content-addressed store for raw material: event
// streams, wire captures, logs, snapshots (spec 080 §4 and §5).
package blob

// Hash is the lowercase hex sha256 of a blob's uncompressed content. It is
// the blob's address on every tier.
type Hash string
