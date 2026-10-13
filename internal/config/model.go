package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModelIdentity says what a model tag is, in the owner's words: the facts no
// server can know. One file per tag in models/ (030 §4.1). A server calls a
// Qwen 3.6 tag "qwen35" and knows nothing of a merge's name; the run records
// this entry next to what the server and the weights file say.
type ModelIdentity struct {
	// Tag is the model tag as arms name it.
	Tag string `toml:"tag" json:"tag"`
	// Family is the model family, e.g. "Qwen 3.6"; Size its size, e.g. "27B".
	Family string `toml:"family" json:"family"`
	Size   string `toml:"size" json:"size"`
	// Variant says which model of the family this is: a base model, a fine
	// tune, a merge by its name and version.
	Variant string `toml:"variant" json:"variant"`
	// Lineage says what it was made from, when that is not the family's own
	// release: the merge's parents, the method.
	Lineage string `toml:"lineage" json:"lineage,omitempty"`
	// Digest pins the entry to one build of the tag; empty means any.
	Digest string `toml:"digest" json:"digest,omitempty"`
	Notes  string `toml:"notes" json:"notes,omitempty"`
	// Queried lists the facts above that the entry left empty and that
	// were read from the weights file through the server instead (D80).
	Queried []string `toml:"-" json:"queried,omitempty"`
	// File is the entry's file, for messages.
	File string `toml:"-" json:"-"`
}

// Missing lists what the entry leaves unstated among the facts every run
// must carry.
func (m *ModelIdentity) Missing() []string {
	if m == nil {
		return []string{"family", "size", "variant"}
	}
	var out []string
	for name, v := range map[string]string{"family": m.Family, "size": m.Size, "variant": m.Variant} {
		if strings.TrimSpace(v) == "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// LoadModels reads the catalogue: every *.toml in dir, by tag. A directory
// that is not there is an empty catalogue.
func LoadModels(dir string) (map[string]*ModelIdentity, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	out := map[string]*ModelIdentity{}
	for _, p := range paths {
		m := &ModelIdentity{File: filepath.Base(p)}
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
		if err := decodeStrict(p, m); err != nil {
			return nil, err
		}
		if m.Tag == "" {
			return nil, fmt.Errorf("config: %s names no tag", p)
		}
		if other := out[m.Tag]; other != nil {
			return nil, fmt.Errorf("config: tag %s is described twice, in %s and %s", m.Tag, other.File, m.File)
		}
		out[m.Tag] = m
	}
	return out, nil
}
