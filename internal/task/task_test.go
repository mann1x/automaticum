package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/docker"
)

const tasksRoot = "../../tasks"

func TestLoadAll(t *testing.T) {
	all, err := LoadAll(Roots{Public: tasksRoot})
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
	if tk, err := Find(Roots{Public: tasksRoot}, "manic_miner"); err != nil || tk.Name() != "custom/manic_miner" {
		t.Errorf("Find: %v, %v", tk, err)
	}
	if _, err := Find(Roots{Public: tasksRoot}, "nope"); err == nil {
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

// Every workspace is a git repository unless the task says git = false
// (D87), and a script that restores a checkpoint needs one.
func TestWorkspaceGit(t *testing.T) {
	tk, err := Load(write(t, minimal, map[string]string{}))
	if err != nil || !tk.Workspace.GitRepo() {
		t.Fatalf("by default: %v, %v", tk.Workspace.GitRepo(), err)
	}
	tk, err = Load(write(t, minimal+"[workspace]\ngit = false\n", map[string]string{}))
	if err != nil || tk.Workspace.GitRepo() {
		t.Fatalf("git = false: %v, %v", tk.Workspace.GitRepo(), err)
	}
	restore := "[user.default]\napproval = \"approve\"\nanswer = \"x\"\n[[user.rule]]\non = \"turn\"\nturn = 2\ndo = \"restore\"\nrestore = \"workspace\"\ncheckpoint = 1\n"
	if _, err := Load(write(t, minimal+restore, map[string]string{})); err != nil {
		t.Errorf("a restore in a git workspace: %v", err)
	}
	_, err = Load(write(t, minimal+"[workspace]\ngit = false\n"+restore, map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "git = false") {
		t.Errorf("a restore without git: %v", err)
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
	tk, err := Find(Roots{Public: tasksRoot}, "demo/answer")
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
	r, err := e.RunOracle(context.Background(), tk, tk.PrimaryOracle(), ws, nil)
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
	tk, err := Find(Roots{Public: tasksRoot}, "demo/answer")
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.PrimaryOracle().Image)
	os.MkdirAll(e.Scratch, 0o755)
	ws := filepath.Join(e.Scratch, "ws")
	os.MkdirAll(ws, 0o755)
	slow := *tk.PrimaryOracle()
	slow.Command, slow.TimeoutS = []string{"sh", "-c", "echo RESULT ok; sleep 60"}, 2
	r, err := e.RunOracle(context.Background(), tk, &slow, ws, nil)
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
	tk, err := Find(Roots{Public: tasksRoot}, "custom/manic_miner")
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
	r, err := e.RunOracle(ctx, tk, tk.PrimaryOracle(), ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || len(r.Tampered) != 1 || r.Tampered[0] != "run_game" {
		t.Errorf("a replaced oracle: %+v", r)
	}
}

// The private root (D105) is read beside the repository's tasks: its tasks
// must be private and carry a canary, and a task in both roots is refused.
func TestPrivateRoot(t *testing.T) {
	priv := t.TempDir()
	// Each test task is the demo task, renamed and moved.
	src, err := os.ReadFile(filepath.Join(tasksRoot, "demo", "answer", "task.toml"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(suite, id, extra string) {
		d := filepath.Join(priv, suite, id)
		if err := os.CopyFS(d, os.DirFS(filepath.Join(tasksRoot, "demo", "answer"))); err != nil {
			t.Fatal(err)
		}
		body := strings.Replace(strings.Replace(string(src), `id = "answer"`, `id = "`+id+`"`, 1), `suite = "demo"`, `suite = "`+suite+`"`, 1)
		body = strings.Replace(body, "version = 1\n", "version = 1\n"+extra, 1)
		if err := os.WriteFile(filepath.Join(d, "task.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	roots := Roots{Public: tasksRoot, Private: priv}
	write("ml", "py-fix", "private = true\ncanary = \"canary-0123456789abcdef\"\n")
	tk, err := Find(roots, "ml/py-fix")
	if err != nil || !tk.Private || tk.Canary == "" {
		t.Fatalf("a private task: %v %+v", err, tk)
	}
	if _, err := Find(roots, "custom/manic_miner"); err != nil {
		t.Errorf("the repository's tasks are still read: %v", err)
	}
	write("ml", "go-fix", "")
	if _, err := LoadAll(roots); err == nil || !strings.Contains(err.Error(), "private = true") {
		t.Errorf("a task in the private root that does not say private: %v", err)
	}
	os.RemoveAll(filepath.Join(priv, "ml", "go-fix"))
	write("ml", "rs-fix", "private = true\ncanary = \"short\"\n")
	if _, err := LoadAll(roots); err == nil || !strings.Contains(err.Error(), "canary") {
		t.Errorf("a private task with a short canary: %v", err)
	}
	os.RemoveAll(filepath.Join(priv, "ml", "rs-fix"))
	write("custom", "manic_miner", "private = true\ncanary = \"canary-0123456789abcdef\"\n")
	if _, err := LoadAll(roots); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("a task in both roots: %v", err)
	}
}

// A training task (D105) is a public task in a train-* suite that says
// training = true; the one without the other is refused.
func TestTrainingFlag(t *testing.T) {
	make := func(suite, extra string) string {
		dir := filepath.Join(t.TempDir(), suite, "answer")
		if err := os.CopyFS(dir, os.DirFS(filepath.Join(tasksRoot, "demo", "answer"))); err != nil {
			t.Fatal(err)
		}
		src, _ := os.ReadFile(filepath.Join(dir, "task.toml"))
		body := strings.Replace(string(src), `suite = "demo"`, `suite = "`+suite+`"`, 1)
		body = strings.Replace(body, "version = 1\n", "version = 1\n"+extra, 1)
		if err := os.WriteFile(filepath.Join(dir, "task.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	tk, err := Load(make("train-sh", "training = true\n"))
	if err != nil || !tk.Training || tk.Private {
		t.Fatalf("%v %+v", err, tk)
	}
	if _, err := Load(make("train-sh", "")); err == nil {
		t.Error("a task in a train-* suite that does not say training")
	}
	if _, err := Load(make("demo", "training = true\n")); err == nil {
		t.Error("a training task outside a train-* suite")
	}
}

func TestSeedFixture(t *testing.T) {
	make := func(expect string) string {
		dir := filepath.Join(t.TempDir(), "demo", "answer")
		if err := os.CopyFS(dir, os.DirFS(filepath.Join(tasksRoot, "demo", "answer"))); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(dir, "task.toml"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f, "\n[[fixture]]\nname = \"as-seeded\"\nexpect = %q\n", expect)
		f.Close()
		return dir
	}
	tk, err := Load(make("fail"))
	if err != nil {
		t.Fatal(err)
	}
	if f := tk.Fixtures[len(tk.Fixtures)-1]; f.Name != "as-seeded" || len(f.Files) != 0 {
		t.Errorf("%+v", f)
	}
	if _, err := Load(make("pass")); err == nil {
		t.Error("a fixture with no files, the seed itself, expected to pass")
	}
}

// ownOracle is demo/answer with an artefact only its oracle has, and a
// writable path for that oracle; extra is appended to its oracle.
func ownOracle(t *testing.T, extra string) string {
	dir := filepath.Join(t.TempDir(), "demo", "answer")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(tasksRoot, "demo", "answer"))); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "task.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(b), `command = ["sh", "-c", "grep -q 'is 42\\.' answer.txt && echo RESULT ok || echo RESULT wrong"]`,
		`artefact = "tests"
writable = ["/app", "/opt"]
command = ["sh", "-c", "test ! -e /w/tests.txt && touch /opt/x && cp -r /w/. /app && cat /oracle/tests.txt >> /app/answer.txt && grep -q withheld /app/answer.txt && grep -q 'is 42\\.' /app/answer.txt && echo RESULT ok || echo RESULT wrong"]`+extra, 1)
	s += `
[[artefact]]
name = "tests"
image = "debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587"
inputs = ["task.toml"]
command = ["sh", "-c", "printf 'withheld\\n' > /out/tests.txt"]
output = "tests.txt"
sha256 = "` + withheldSHA + `"
`
	if err := os.WriteFile(filepath.Join(dir, "task.toml"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const withheldSHA = "acbbc0faed6666d1ec9f09068b3fe08fc47ec2943839e4ca5aa22c39f33c65ba"

func TestOracleWritableRefused(t *testing.T) {
	if _, err := Load(ownOracle(t, "")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"app", "/app/", "/", "/w", "/tmp/x", "/oracle", "/app\", \"/app"} {
		if _, err := Load(ownOracle(t, "\nwritable = [\""+p+"\"]")); err == nil {
			t.Errorf("writable %q was accepted", p)
		}
	}
}

// An artefact the workspace does not install reaches the oracle at
// /oracle/<output>, read-only, and never the agent's workspace; the
// oracle may write the paths it names, empty, even one the image has as
// root's (/opt).
func TestOracleOwnArtefact(t *testing.T) {
	e := env(t)
	tk, err := Load(ownOracle(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.PrimaryOracle().Image)
	os.MkdirAll(e.Scratch, 0o755)
	if err := e.BuildAll(context.Background(), tk, io.Discard); err != nil {
		t.Fatal(err)
	}
	results, err := e.Verify(context.Background(), tk, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !r.OK {
			t.Errorf("fixture %s: %+v", r.Fixture, r.Runs)
		}
	}
	ws := filepath.Join(e.Scratch, "own")
	os.MkdirAll(ws, 0o755)
	m, err := e.PrepareWorkspace(tk, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["tests.txt"]; ok || len(m) != 1 {
		t.Errorf("the oracle's own artefact is in the workspace: %v", m)
	}
}

// run_as, the task's limits and its collect paths are validated (D110,
// D111, D112).
func TestRunAsCollectLimits(t *testing.T) {
	fixture := func(path string) string {
		return "[[fixture]]\nname = \"f\"\nexpect = \"pass\"\n[fixture.files]\n\"" + path + "\" = \"task.toml\"\n"
	}
	good := map[string]string{
		"root agent":        "run_as = \"root\"\n" + minimal,
		"root oracle":       minimal + "run_as = \"root\"\n",
		"limits":            minimal + "[limits]\ncpus = 1.5\nmemory_mb = 2048\n",
		"collect":           minimal + "[workspace]\ncollect = [\"/usr/local/bin\", \"/etc/tool\"]\n",
		"collected fixture": minimal + "[workspace]\ncollect = [\"/usr/local\"]\n" + fixture("/usr/local/bin/tool"),
	}
	for name, body := range good {
		if _, err := Load(write(t, body, map[string]string{})); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := map[string]string{
		"agent as someone":      "run_as = \"admin\"\n" + minimal,
		"oracle as someone":     minimal + "run_as = \"1000\"\n",
		"negative cpus":         minimal + "[limits]\ncpus = -1.0\n",
		"negative memory":       minimal + "[limits]\nmemory_mb = -1\n",
		"relative collect":      minimal + "[workspace]\ncollect = [\"usr\"]\n",
		"collect /":             minimal + "[workspace]\ncollect = [\"/\"]\n",
		"collect /tmp":          minimal + "[workspace]\ncollect = [\"/tmp/x\"]\n",
		"collect /proc":         minimal + "[workspace]\ncollect = [\"/proc\"]\n",
		"collect unclean":       minimal + "[workspace]\ncollect = [\"/usr/\"]\n",
		"collect twice":         minimal + "[workspace]\ncollect = [\"/usr\", \"/usr\"]\n",
		"collect overlaps":      minimal + "[workspace]\ncollect = [\"/usr\", \"/usr/lib\"]\n",
		"writable over collect": strings.Replace(minimal, "expect_exit = 0", "expect_exit = 0\nwritable = [\"/usr/lib\"]", 1) + "[workspace]\ncollect = [\"/usr\"]\n",
		"fixture outside":       minimal + "[workspace]\ncollect = [\"/usr/local\"]\n" + fixture("/etc/tool"),
		"fixture absolute":      minimal + fixture("/usr/local/bin/tool"),
	}
	for name, body := range bad {
		if _, err := Load(write(t, body, map[string]string{})); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A task's own limits hold where they are under the lane's (D112).
func TestResources(t *testing.T) {
	const gb = int64(1) << 30
	for _, c := range []struct {
		l         Limits
		cpus      float64
		mem       int64
		wantNano  int64
		wantBytes int64
	}{
		{Limits{}, 4, 8 * gb, 4e9, 8 * gb},
		{Limits{CPUs: 1, MemoryMB: 2048}, 4, 8 * gb, 1e9, 2 * gb},
		{Limits{CPUs: 8, MemoryMB: 16384}, 4, 8 * gb, 4e9, 8 * gb},
		{Limits{CPUs: 1, MemoryMB: 2048}, 0, 0, 1e9, 2 * gb},
		{Limits{}, 0, 0, 0, 0},
	} {
		n, b := Resources(c.l, c.cpus, c.mem)
		if n != c.wantNano || b != c.wantBytes {
			t.Errorf("%+v on %v CPUs and %d bytes: %d, %d", c.l, c.cpus, c.mem, n, b)
		}
	}
	if u, caps := As(Root, "1000:1000"); u != "0:0" || len(caps) == 0 {
		t.Errorf("root: %s %v", u, caps)
	}
	if u, caps := As("", "1000:1000"); u != "1000:1000" || caps != nil {
		t.Errorf("lane user: %s %v", u, caps)
	}
}

// An oracle that runs as root has the engine's capabilities, and gets a
// collected directory at its path: for a fixture, what the image has there
// with the fixture's files laid over it.
func TestOracleCollectAsRoot(t *testing.T) {
	e := env(t)
	body := `id = "collect"
suite = "demo"
version = 1
prompt = "Write 42 to /opt/tool/answer."
[limits]
oracle_s = 30
cpus = 1.0
memory_mb = 256
[workspace]
git = false
collect = ["/opt/tool", "/etc/apt"]
[[oracle]]
name = "check"
primary = true
image = "debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587"
run_as = "root"
command = ["sh", "-c", "test \"$(id -u)\" = 0 && chroot / true && test -d /etc/apt/apt.conf.d && grep -q 42 /opt/tool/answer && touch /opt/tool/written && echo RESULT ok || echo RESULT wrong"]
expect_pattern = 'RESULT ok'
[[fixture]]
name = "good"
expect = "pass"
[fixture.files]
"/opt/tool/answer" = "good.txt"
[[fixture]]
name = "none"
expect = "fail"
`
	tk, err := Load(write(t, body, map[string]string{"good.txt": "42\n"}))
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.PrimaryOracle().Image)
	os.MkdirAll(e.Scratch, 0o755)
	results, err := e.Verify(context.Background(), tk, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !r.OK {
			t.Errorf("fixture %s: %+v", r.Fixture, r.Runs)
		}
	}
	// As a run hands them over: what the agent's container had.
	ws, tool, apt := t.TempDir(), t.TempDir(), t.TempDir()
	os.Chmod(ws, 0o755)
	os.MkdirAll(filepath.Join(apt, "apt.conf.d"), 0o755)
	os.WriteFile(filepath.Join(tool, "answer"), []byte("42\n"), 0o644)
	r, err := e.RunOracle(context.Background(), tk, tk.PrimaryOracle(), ws, map[string]string{"/opt/tool": tool, "/etc/apt": apt})
	if err != nil || !r.Passed {
		t.Fatalf("%v: %+v", err, r)
	}
	if _, err := os.Stat(filepath.Join(tool, "written")); err == nil {
		t.Error("the oracle wrote the collected directory itself, not a copy")
	}
	if _, err := e.RunOracle(context.Background(), tk, tk.PrimaryOracle(), ws, map[string]string{"/opt/tool": tool}); err == nil {
		t.Error("an oracle ran without a collect path")
	}
}

// A file of the seed taken from an image is the agent's, as the rest of
// the workspace: the oracle sees what the agent made of it, and a change is
// no tampering (066 §3).
func TestSeedInstall(t *testing.T) {
	e := env(t)
	body := `id = "seedfile"
suite = "demo"
version = 1
prompt = "Delete v."
[workspace]
git = false
[[workspace.install]]
artefact = "v"
path = "v"
seed = true
[[artefact]]
name = "v"
image = "debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587"
command = ["cp", "/etc/debian_version", "/out/v"]
output = "v"
sha256 = "0e6ef511d8279cbe816b3596bdda9302016f5e29f6d16814b84ea7cbc12b3ffe"
[[oracle]]
name = "check"
primary = true
image = "debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587"
command = ["sh", "-c", "test ! -e /w/v && echo RESULT ok || echo RESULT wrong"]
expect_pattern = 'RESULT ok'
`
	tk, err := Load(write(t, body, map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	pull(t, e, tk.PrimaryOracle().Image)
	os.MkdirAll(e.Scratch, 0o755)
	if err := e.BuildAll(context.Background(), tk, io.Discard); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	os.Chmod(ws, 0o755)
	m, err := e.PrepareWorkspace(tk, ws)
	if err != nil || m["v"] != tk.Artefacts[0].SHA256 {
		t.Fatalf("the seed file: %v %v", m, err)
	}
	r, err := e.RunOracle(context.Background(), tk, tk.PrimaryOracle(), ws, nil)
	if err != nil || r.Passed {
		t.Fatalf("with the file: %v %+v", err, r)
	}
	os.Remove(filepath.Join(ws, "v"))
	r, err = e.RunOracle(context.Background(), tk, tk.PrimaryOracle(), ws, nil)
	if err != nil || !r.Passed || len(r.Tampered) > 0 {
		t.Fatalf("without it: %v %+v", err, r)
	}
	bad := strings.Replace(body, `command = ["sh"`, "artefact = \"v\"\ncommand = [\"sh\"", 1)
	if _, err := Load(write(t, bad, map[string]string{})); err == nil {
		t.Error("an oracle's artefact that is part of the seed was accepted")
	}
}
