package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/docker"
)

const tasksRoot = "../../tasks"

func TestLoadAll(t *testing.T) {
	all, err := LoadAll(tasksRoot)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*Task{}
	for _, tk := range all {
		byName[tk.Name()] = tk
		if !tk.HasFixtures() {
			t.Errorf("%s has no pass and fail fixture", tk.Name())
		}
		if tk.PrimaryOracle() == nil || len(tk.Hash) != 64 || tk.Files["task.toml"] == "" {
			t.Errorf("%s: incomplete after load", tk.Name())
		}
	}
	mm := byName["custom/manic_miner"]
	if mm == nil || byName["demo/answer"] == nil {
		t.Fatalf("tasks found: %v", byName)
	}
	if mm.Version != 4 || mm.Limits.WallS != 7200 || len(mm.Artefacts) != 1 || len(mm.Fixtures) != 3 {
		t.Errorf("manic_miner: %+v", mm)
	}
	if strings.HasPrefix(mm.Prompt, "\n") || !strings.HasPrefix(mm.Prompt, "check manic_miner.html") || strings.HasSuffix(mm.Prompt, "\n") {
		t.Errorf("prompt has stray whitespace: %q", mm.Prompt)
	}
	// The seed is the old harness's file, byte for byte: CRLF and all.
	if got := mm.Files["seed/manic_miner.html"]; !strings.HasPrefix(got, "5b6559abbb62c4c4") {
		t.Errorf("seed file changed: %s", got)
	}
	if tk, err := Find(tasksRoot, "manic_miner"); err != nil || tk.Name() != "custom/manic_miner" {
		t.Errorf("Find: %v, %v", tk, err)
	}
	if _, err := Find(tasksRoot, "nope"); err == nil {
		t.Error("Find accepted a task that does not exist")
	}
}

// write makes a task directory from a task.toml and files.
func write(t *testing.T, toml string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["task.toml"] = toml
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const img = `"x@sha256:` + "0000000000000000000000000000000000000000000000000000000000000000" + `"`

const minimal = `id = "t"
suite = "s"
version = 1
prompt = "do it"
[[oracle]]
name = "o"
primary = true
image = ` + img + `
command = ["true"]
expect_exit = 0
`

func TestLoadRejects(t *testing.T) {
	if _, err := Load(write(t, minimal, map[string]string{})); err != nil {
		t.Fatalf("the minimal task does not load: %v", err)
	}
	cases := map[string]string{
		"unknown field":         minimal + "\n[limits]\nwal_s = 3\n",
		"unknown top field":     "promt = \"x\"\n" + minimal,
		"no primary":            strings.Replace(minimal, "primary = true", "primary = false", 1),
		"two expectations":      minimal + "expect_pattern = \"ok\"\n",
		"no expectation":        strings.Replace(minimal, "expect_exit = 0\n", "", 1),
		"image not pinned":      strings.Replace(minimal, img, `"debian:latest"`, 1),
		"bad pattern":           strings.Replace(minimal, "expect_exit = 0", `expect_pattern = "("`, 1),
		"empty prompt":          strings.Replace(minimal, `prompt = "do it"`, `prompt = " "`, 1),
		"version zero":          strings.Replace(minimal, "version = 1", "version = 0", 1),
		"fixture file missing":  minimal + "[[fixture]]\nname = \"f\"\nexpect = \"pass\"\n[fixture.files]\n\"a\" = \"nope.txt\"\n",
		"fixture escapes":       minimal + "[[fixture]]\nname = \"f\"\nexpect = \"pass\"\n[fixture.files]\n\"../a\" = \"task.toml\"\n",
		"fixture bad expect":    minimal + "[[fixture]]\nname = \"f\"\nexpect = \"maybe\"\n[fixture.files]\n\"a\" = \"task.toml\"\n",
		"install no artefact":   minimal + "[[workspace.install]]\nartefact = \"a\"\npath = \"a\"\n",
		"artefact without pin":  minimal + "[[artefact]]\nname = \"a\"\nimage = " + img + "\ninputs = [\"task.toml\"]\ncommand = [\"x\"]\noutput = \"a\"\n",
		"seed is not a dir":     minimal + "[workspace]\nseed = \"task.toml\"\n",
		"oracle artefact wrong": strings.Replace(minimal, "expect_exit = 0", "expect_exit = 0\nartefact = \"a\"", 1),
	}
	for name, body := range cases {
		if _, err := Load(write(t, body, map[string]string{})); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHashChangesWithAnyFile(t *testing.T) {
	dir := write(t, minimal, map[string]string{"extra/note.txt": "one"})
	a, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra/note.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash == b.Hash {
		t.Error("the task hash ignores a changed file")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("a task holding a link was accepted")
	}
}

// env returns an Env on the local engine, or skips.
func env(t *testing.T) *Env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs a Docker engine")
	}
	if _, err := os.Stat(docker.DefaultSocket); err != nil {
		t.Skip("no Docker socket")
	}
	c := docker.New("")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Version(ctx); err != nil {
		t.Skipf("Docker engine not reachable: %v", err)
	}
	// The engine must be able to see these directories: a test directory
	// under the system's temporary directory works for a local engine.
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	artefacts := os.Getenv("AUTOMATICUM_TEST_ARTEFACTS") // reuse built artefacts between test runs
	if artefacts == "" {
		artefacts = filepath.Join(root, "artefacts")
	}
	return &Env{Exec: c, Artefacts: artefacts, Scratch: filepath.Join(root, "scratch")}
}

func pull(t *testing.T, e *Env, images ...string) {
	t.Helper()
	c := e.Exec.(*docker.Client)
	for _, im := range images {
		if _, err := c.ImageInspect(context.Background(), im); err != nil {
			if err := c.ImagePull(context.Background(), im, nil); err != nil {
				t.Skipf("cannot pull %s: %v", im, err)
			}
		}
	}
}

func TestVerifyDemo(t *testing.T) {
	e := env(t)
	tk, err := Find(tasksRoot, "demo/answer")
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.PrimaryOracle().Image)
	if err := os.MkdirAll(e.Scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	results, err := e.Verify(context.Background(), tk, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results: %+v", results)
	}
	for _, r := range results {
		if !r.OK || len(r.Runs) != 2 {
			t.Errorf("fixture %s: %+v", r.Fixture, r)
		}
	}

	// The oracle works on a copy: it cannot change the workspace, and a
	// workspace it judges wrong stays wrong.
	ws := filepath.Join(e.Scratch, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := e.PrepareWorkspace(tk, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["answer.txt"] == "" {
		t.Fatalf("manifest %v", m)
	}
	if _, err := e.PrepareWorkspace(tk, ws); err == nil {
		t.Error("a workspace that is not empty was accepted")
	}
	r, err := e.RunOracle(context.Background(), tk, tk.PrimaryOracle(), ws)
	if err != nil || r.Passed || r.Err != "" || !strings.Contains(r.Output, "RESULT wrong") {
		t.Fatalf("oracle on the seed: %+v, %v", r, err)
	}
	after, _ := ReadManifest(ws)
	if after.Hash() != m.Hash() {
		t.Error("the oracle changed the workspace")
	}
}

func TestOracleTimeout(t *testing.T) {
	e := env(t)
	tk, err := Find(tasksRoot, "demo/answer")
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.PrimaryOracle().Image)
	os.MkdirAll(e.Scratch, 0o755)
	ws := filepath.Join(e.Scratch, "ws")
	os.MkdirAll(ws, 0o755)
	slow := *tk.PrimaryOracle()
	slow.Command, slow.TimeoutS = []string{"sh", "-c", "echo RESULT ok; sleep 60"}, 2
	r, err := e.RunOracle(context.Background(), tk, &slow, ws)
	if err != nil {
		t.Fatal(err)
	}
	// It printed the passing words and then hung: that is not a pass.
	if r.Passed || !r.TimedOut || r.Err == "" {
		t.Fatalf("a timed-out oracle counted: %+v", r)
	}
}

func TestManicMiner(t *testing.T) {
	e := env(t)
	tk, err := Find(tasksRoot, "custom/manic_miner")
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.Artefacts[0].Image)
	os.MkdirAll(e.Scratch, 0o755)
	ctx := context.Background()
	if err := e.BuildAll(ctx, tk, nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	results, err := e.Verify(ctx, tk, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !r.OK {
			t.Errorf("fixture %s (expect %s): %+v", r.Fixture, r.Expect, r.Runs)
		}
	}

	// A pinned hash that the build does not give is refused.
	wrong := *tk
	wrong.Artefacts = []Artefact{tk.Artefacts[0]}
	wrong.Artefacts[0].SHA256 = strings.Repeat("0", 64)
	if _, err := e.Build(ctx, &wrong, &wrong.Artefacts[0], nil); !errors.Is(err, ErrArtefactChanged) {
		t.Errorf("a build with another hash: %v", err)
	}

	// An agent that replaces its copy of the oracle with one that always
	// says yes changes nothing: the harness's copy judges, and the swap is
	// reported.
	ws := filepath.Join(e.Scratch, "ws")
	os.MkdirAll(ws, 0o755)
	if _, err := e.PrepareWorkspace(tk, ws); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
echo '{"ok":true,"error":null,"frames_run":400,"reached_playing":true,"game_logic":{"ok":true,"passed":23,"total":23}}'
`
	os.Remove(filepath.Join(ws, "run_game"))
	if err := os.WriteFile(filepath.Join(ws, "run_game"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := e.RunOracle(ctx, tk, tk.PrimaryOracle(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || len(r.Tampered) != 1 || r.Tampered[0] != "run_game" {
		t.Errorf("a replaced oracle: %+v", r)
	}
}
