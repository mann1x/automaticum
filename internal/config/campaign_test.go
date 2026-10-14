package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCampaign(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		os.WriteFile(filepath.Join(dir, name+".toml"), []byte(text), 0o644)
	}
	write("good", `
description = "A against B overnight"
[stop]
at = 2026-10-15T08:00:00+02:00
[[batch]]
name = "ab"
arms = ["a", "b"]
tasks = ["custom/manic_miner"]
runs = 10
[batch.stop]
after_consecutive_pass = 5
on_invalid = true
`)
	c, err := LoadCampaign(dir, "good")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "good" || len(c.Batches) != 1 || c.Batches[0].Stop.AfterConsecutivePass != 5 || !c.Batches[0].Stop.OnInvalid || c.Stop.At.IsZero() || len(c.SHA256) != 64 {
		t.Errorf("%+v", c)
	}
	write("unknown", `
[[batch]]
name = "x"
arms = ["a"]
tasks = ["t"]
runs = 1
retries = 3
`)
	if _, err := LoadCampaign(dir, "unknown"); err == nil || !strings.Contains(err.Error(), "retries") {
		t.Errorf("an unknown field: %v", err)
	}
	write("bad", `
[[batch]]
name = "x"
runs = 0
order = "random"
[[batch]]
name = "x"
arms = ["a"]
tasks = ["t"]
runs = 1
`)
	_, err = LoadCampaign(dir, "bad")
	for _, want := range []string{"no arms", "no tasks", "runs must be 1", "order is", "named twice"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
	if _, err := LoadCampaign(dir, "../good"); err == nil {
		t.Error("a path was taken for a name")
	}
}
