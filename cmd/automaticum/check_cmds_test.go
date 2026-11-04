package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
)

// exampleConfig is examples/config copied to a temporary directory, with
// the repo this checkout, a data root of its own and a token.
func exampleConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../examples/config")); err != nil {
		t.Fatal(err)
	}
	repo, _ := filepath.Abs("../..")
	path := filepath.Join(dir, "automaticum.toml")
	b, _ := os.ReadFile(path)
	s := regexp.MustCompile(`(?m)^repo = .*$`).ReplaceAllString(string(b), `repo = "`+repo+`"`)
	s = regexp.MustCompile(`(?m)^data_root = .*$`).ReplaceAllString(s, `data_root = "`+filepath.Join(dir, "data")+`"`)
	os.WriteFile(path, []byte(s), 0o644)
	os.WriteFile(filepath.Join(dir, "token"), []byte(strings.Repeat("x", 40)+"\n"), 0o600)
	return dir
}

// TestConfigCheck: the example configuration passes; a key under a table
// the file does not have (the daemon would refuse to start) and an arm
// naming an endpoint the installation lacks are each a problem.
func TestConfigCheck(t *testing.T) {
	ctx := context.Background()
	dir := exampleConfig(t)
	if err := configCheck(ctx, []string{"--config", dir}); err != nil {
		t.Fatalf("the example configuration: %v", err)
	}
	f, _ := os.OpenFile(filepath.Join(dir, "automaticum.toml"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("\n[daemon]\nprivate_tasks = \"x\"\n")
	f.Close()
	if err := configCheck(ctx, []string{"--config", dir}); err != errFailed {
		t.Errorf("an unknown table: %v", err)
	}

	dir = exampleConfig(t)
	os.WriteFile(filepath.Join(dir, "arms", "lost.toml"), []byte("extends = \"stub\"\nendpoint = \"nowhere\"\n"), 0o644)
	if err := configCheck(ctx, []string{"--config", dir}); err != errFailed {
		t.Errorf("an arm with no endpoint: %v", err)
	}
	if n, _, err := checkArms(mustLocal(t, dir)); err == nil || !strings.Contains(err.Error(), `"nowhere"`) || n == 0 {
		t.Errorf("checkArms: %d %v", n, err)
	}

	dir = exampleConfig(t)
	path := filepath.Join(dir, "automaticum.toml")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), `private_tasks = ""`, `private_tasks = "`+filepath.Join(dir, "missing")+`"`, 1)), 0o644)
	if err := configCheck(ctx, []string{"--config", dir}); err != errFailed {
		t.Errorf("a private root that is not there: %v", err)
	}
}

func mustLocal(t *testing.T, dir string) *config.Local {
	t.Helper()
	l, err := config.LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
