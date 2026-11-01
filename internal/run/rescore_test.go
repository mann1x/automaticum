package run

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// TestRescoreVerdict proves that a rescore decides the verdict from the
// run's recorded facts with the oracle's new answer (D121).
func TestRescoreVerdict(t *testing.T) {
	prov := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	wall := prov(map[string]any{"limits": map[string]any{"wall": map[string]any{"seconds": 60, "owner": "task"}, "fired": "wall", "fired_by": "runner"}})
	guard := prov(map[string]any{"runner": map[string]any{"outcome": OutcomeGuardStop, "reason": "the reasoning loop guard"}})
	infra := prov(map[string]any{"runner": map[string]any{"outcome": OutcomeCompleted, "agent_end": "error", "agent_error": "socket hang up"}})
	for _, c := range []struct {
		name   string
		run    store.Run
		passed bool
		want   store.Verdict
		reason string
	}{
		{"fail now passes", store.Run{Verdict: store.Fail}, true, store.Pass, "oracle passed"},
		{"pass now fails", store.Run{Verdict: store.Pass}, false, store.Fail, "oracle ran and said no"},
		{"timeout now passes", store.Run{Verdict: store.Timeout, Provenance: wall}, true, store.Pass, "the wall limit had fired"},
		{"pass after the limit now fails", store.Run{Verdict: store.Pass, Provenance: wall}, false, store.Timeout, "wall limit (set by the task, enforced by the runner)"},
		{"a guard stop stays failed", store.Run{Verdict: store.Fail, Provenance: guard}, true, store.Fail, "stopped at the reasoning loop guard; the oracle said pass"},
		{"a server fault is never a failure", store.Run{Verdict: store.Pass, Provenance: infra}, false, store.Aborted, "the server failed"},
	} {
		v, reason, ok := Rescore(&c.run, c.passed)
		if !ok || v != c.want || !strings.Contains(reason, c.reason) {
			t.Errorf("%s: %s %q %v", c.name, v, reason, ok)
		}
	}
	for _, v := range []store.Verdict{store.Aborted, store.Skipped, store.Invalid} {
		if _, _, ok := Rescore(&store.Run{Verdict: v}, true); ok {
			t.Errorf("%s was rescored", v)
		}
	}
}

// TestUntar proves that a tree stored by tarTree comes back whole, and
// that a name outside the directory is refused.
func TestUntar(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(src, "a", "b", "f.txt"), []byte("hello"), 0o640)
	os.Symlink("b/f.txt", filepath.Join(src, "a", "link"))
	var buf bytes.Buffer
	if err := tarTree(src, &buf); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Untar(bytes.NewReader(buf.Bytes()), dst); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a", "link")); err != nil || string(b) != "hello" {
		t.Errorf("restored: %q %v", b, err)
	}
	if st, _ := os.Stat(filepath.Join(dst, "a", "b", "f.txt")); st.Mode().Perm() != 0o640 {
		t.Errorf("mode: %v", st.Mode())
	}
	var bad bytes.Buffer
	tw := newTarWriter(&bad, "../escape")
	tw.Close()
	if err := Untar(bytes.NewReader(bad.Bytes()), t.TempDir()); err == nil {
		t.Error("a name outside the directory was written")
	}
}

func newTarWriter(w *bytes.Buffer, name string) *tar.Writer {
	tw := tar.NewWriter(w)
	tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644})
	return tw
}
