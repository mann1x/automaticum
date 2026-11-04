package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unpinnedCopy is train-sh/sh_logtriage copied to a temporary root with its
// artefact's sha256 line replaced by line (removed when empty).
func unpinnedCopy(t *testing.T, line string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "train-sh", "sh_logtriage")
	if err := os.CopyFS(dir, os.DirFS("../../tasks/train-sh/sh_logtriage")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "task.toml"))
	pinned := `sha256 = "c645bdebac8183d89e0dc82890a5402109060c4969db340beb715b3afc117ea6"` + "\n"
	if !strings.Contains(string(b), pinned) {
		t.Fatal("the source task's pin moved; update the test")
	}
	if line != "" {
		line += "\n"
	}
	os.WriteFile(filepath.Join(dir, "task.toml"), []byte(strings.Replace(string(b), pinned, line, 1)), 0o644)
	return root, dir
}

// TestPin: a task with an empty sha256 is refused by Load and read by
// LoadUnpinned; WritePins writes the sum, replacing the value or adding the
// key after the name, and the task then loads.
func TestPin(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	for _, c := range []struct{ name, line string }{
		{"empty value", `sha256 = ""`},
		{"placeholder with a comment", `sha256 = "` + strings.Repeat("0", 64) + `" # pinned by task pin`},
		{"no key", ""},
	} {
		root, dir := unpinnedCopy(t, c.line)
		if c.line != `sha256 = "`+strings.Repeat("0", 64)+`" # pinned by task pin` {
			if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "sha256 must pin the result") {
				t.Errorf("%s: Load: %v", c.name, err)
			}
		}
		u, err := LoadUnpinned(dir)
		if err != nil {
			t.Fatalf("%s: LoadUnpinned: %v", c.name, err)
		}
		if a := u.Artefact("run_tests"); a == nil || !shaRE.MatchString(a.SHA256) {
			t.Fatalf("%s: artefact %+v", c.name, a)
		}
		if got, err := Locate(Roots{Public: root}, "train-sh/sh_logtriage"); err != nil || got != dir {
			t.Errorf("%s: Locate: %q %v", c.name, got, err)
		}
		if got, err := Locate(Roots{}, dir); err != nil || got != dir {
			t.Errorf("%s: Locate by directory: %q %v", c.name, got, err)
		}
		if err := WritePins(dir, map[string]string{"run_tests": sum}); err != nil {
			t.Fatalf("%s: WritePins: %v", c.name, err)
		}
		pinned, err := Load(dir)
		if err != nil {
			t.Fatalf("%s: Load after WritePins: %v", c.name, err)
		}
		if a := pinned.Artefact("run_tests"); a.SHA256 != sum {
			t.Errorf("%s: pinned %s", c.name, a.SHA256)
		}
		b, _ := os.ReadFile(filepath.Join(dir, "task.toml"))
		if n := strings.Count(string(b), "sha256 = "); n != 1 {
			t.Errorf("%s: %d sha256 keys:\n%s", c.name, n, b)
		}
		if strings.Contains(c.line, "#") && !strings.Contains(string(b), "# pinned by task pin") {
			t.Errorf("%s: the comment was lost", c.name)
		}
	}
	_, dir := unpinnedCopy(t, `sha256 = ""`)
	if err := WritePins(dir, map[string]string{"nope": sum}); err == nil {
		t.Error("an unknown artefact was written")
	}
	if _, err := Locate(Roots{Public: t.TempDir()}, "train-sh/missing"); err == nil {
		t.Error("a missing task was found")
	}
}
