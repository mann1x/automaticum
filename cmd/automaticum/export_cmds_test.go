package main

import (
	"os"
	"path/filepath"
	"testing"
)

// An anti-loop export and a training export never share a directory (D158).
func TestCheckExportDir(t *testing.T) {
	empty, training, antiLoop := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(training, "manifest.jsonl"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(antiLoop, "manifest.jsonl"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(antiLoop, antiLoopMarker), []byte(antiLoopNote), 0o644)
	for _, c := range []struct {
		dir      string
		antiLoop bool
		ok       bool
	}{
		{empty, false, true}, {empty, true, true},
		{training, false, true}, {training, true, false},
		{antiLoop, true, true}, {antiLoop, false, false},
		{filepath.Join(empty, "new"), true, true},
	} {
		if err := checkExportDir(c.dir, c.antiLoop); (err == nil) != c.ok {
			t.Errorf("%s, anti-loop %v: %v", c.dir, c.antiLoop, err)
		}
	}
}
