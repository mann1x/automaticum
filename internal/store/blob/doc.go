// Package blob is the content-addressed store for raw material: event
// streams, wire captures, logs, snapshots (spec 080 §4 and §5).
//
// A blob is addressed by the sha256 of its uncompressed content and stored
// zstd-compressed in a seekable framing (see FORMAT.md) so that a range can
// be read without decompressing the whole blob. Blobs live on two tiers: a
// live root and an optional archive root. A blob keeps its hash as its
// address on both; every reader resolves the tier on open, live first, then
// archive, so no caller needs to know where a blob is (spec 080 §5.1).
//
// A Store is safe for concurrent use by many goroutines. One process owns a
// root at a time: Open removes leftover temporaries, which would be the
// in-flight writes of another process using the same root.
package blob

// Hash is the lowercase hex sha256 of a blob's uncompressed content. It is
// the blob's address on every tier.
type Hash string

// Valid reports whether h is 64 lowercase hexadecimal characters.
func (h Hash) Valid() bool {
	if len(h) != 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
