package config

import (
	"bytes"
	"os"
	"testing"
)

// The runner's tests read the resolved stub arm from a file. It must be
// what this package resolves, or those tests prove nothing.
func TestRunnerFixtureIsTheResolvedArm(t *testing.T) {
	a, err := LoadArm(armsDir, "stub")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../runner/test/arm-stub.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), a.Resolved()) {
		t.Fatal("runner/test/arm-stub.json is stale: write it again with `automaticum arm show stub --json`")
	}
}
