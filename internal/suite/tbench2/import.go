package tbench2

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/task"
)

//go:embed verifier/main.py verifier/build.py
var verifierFiles embed.FS

// Engine is what the import needs of Docker. *docker.Client is one.
type Engine interface {
	ImagePull(ctx context.Context, ref string, w io.Writer) error
	ImageInspect(ctx context.Context, ref string) (docker.Image, error)
	task.Executor
}

// Importer imports tasks of one checkout of the source into a repository.
type Importer struct {
	Engine Engine
	Env    *task.Env // builds the verifier and proves the oracle
	Source string    // the source's checkout, at Commit
	Commit string
	Repo   string // the repository the task and its layer are written to
	// Repeats is how often Verify judges each fixture; zero means 3.
	Repeats int
	Log     io.Writer
}

// Limits on what an import puts in the repository, which is public.
const (
	maxSeed  = 4 << 20
	maxGold  = 4 << 20
	maxWheel = 8 << 20
)

// Result is what became of one task.
type Result struct {
	Name string
	Dir  string // the task's directory in the repository
	Hash string
	// Verify is how the oracle judged each fixture.
	Verify []task.FixtureResult
}

// Import imports one task: it is written to the repository only if its
// oracle passes the gold fixture and fails the seed, every time.
func (im *Importer) Import(ctx context.Context, name string) (*Result, error) {
	s, err := Read(im.Source, name)
	if err != nil {
		return nil, err
	}
	p := &Pins{Commit: im.Commit}
	if p.Image, err = im.pin(ctx, s.Image); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(im.Env.Scratch, "tbench2-"+name+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o755); err != nil {
		return nil, err
	}
	dir := filepath.Join(stage, Suite, name)
	if err := os.MkdirAll(filepath.Join(dir, "oracle"), 0o755); err != nil {
		return nil, err
	}

	im.logf("%s: seed from %s\n", name, p.Image)
	if err := im.seed(ctx, p.Image, filepath.Join(dir, "seed")); err != nil {
		return nil, fmt.Errorf("tbench2: %s: seed: %w", name, err)
	}
	if err := im.oracleFiles(s, dir); err != nil {
		return nil, fmt.Errorf("tbench2: %s: %w", name, err)
	}
	im.logf("%s: wheels for pytest %s %s\n", name, s.Pytest, strings.Join(s.Packages, " "))
	if p.Wheels, err = im.wheels(ctx, s, p.Image, dir); err != nil {
		return nil, fmt.Errorf("tbench2: %s: wheels: %w", name, err)
	}
	im.logf("%s: gold from solution/solve.sh\n", name)
	if p.Gold, err = im.gold(ctx, s, p.Image, dir); err != nil {
		return nil, fmt.Errorf("tbench2: %s: gold: %w", name, err)
	}

	inputs, err := filesUnder(dir, "oracle")
	if err != nil {
		return nil, err
	}
	// The verifier is pinned by building it once.
	p.Verifier = strings.Repeat("0", 64)
	if err := im.writeTask(s, p, dir, inputs); err != nil {
		return nil, err
	}
	t, err := task.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("tbench2: %s: the written task: %w", name, err)
	}
	if p.Verifier, err = im.Env.Pin(ctx, t, &t.Artefacts[0], im.Log); err != nil {
		return nil, err
	}
	if err := im.writeTask(s, p, dir, inputs); err != nil {
		return nil, err
	}
	if t, err = task.Load(dir); err != nil {
		return nil, fmt.Errorf("tbench2: %s: the written task: %w", name, err)
	}
	if t.Prompt != s.Instruction {
		return nil, fmt.Errorf("tbench2: %s: the prompt did not survive task.toml", name)
	}

	// Gold validation (050 §3.2): the gold fixture passes, the seed fails.
	if err := im.Env.BuildAll(ctx, t, im.Log); err != nil {
		return nil, err
	}
	repeats := im.Repeats
	if repeats == 0 {
		repeats = 3
	}
	im.logf("%s: oracle against the fixtures, %d times each\n", name, repeats)
	res := &Result{Name: name, Hash: t.Hash}
	if res.Verify, err = im.Env.Verify(ctx, t, repeats); err != nil {
		return res, err
	}
	for _, r := range res.Verify {
		if !r.OK {
			return res, fmt.Errorf("tbench2: %s: the oracle does not hold on fixture %s; the task is not imported", name, r.Fixture)
		}
	}

	// Into the repository, replacing an earlier import of the task.
	res.Dir = filepath.Join(im.Repo, "tasks", Suite, name)
	if err := replaceDir(dir, res.Dir); err != nil {
		return res, err
	}
	// The proof is the task's as it now is in the repository.
	if t2, err := task.Load(res.Dir); err != nil || t2.Hash != t.Hash {
		return res, fmt.Errorf("tbench2: %s: the task in the repository is not the one proved (%v)", name, err)
	}
	if err := im.Env.RecordProof(t, len(res.Verify), repeats); err != nil {
		return res, err
	}
	layer := filepath.Join(im.Repo, "images", "layers", Layer(name))
	if err := os.MkdirAll(layer, 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(filepath.Join(layer, "Dockerfile"), []byte(LaneDockerfile(s, p)), 0o644); err != nil {
		return res, err
	}
	lic, err := os.ReadFile(filepath.Join(im.Source, "LICENSE"))
	if err != nil {
		return res, fmt.Errorf("tbench2: the source's LICENSE: %w", err)
	}
	return res, os.WriteFile(filepath.Join(im.Repo, "tasks", Suite, "LICENSE"), lic, 0o644)
}

func (im *Importer) logf(format string, a ...any) {
	if im.Log != nil {
		fmt.Fprintf(im.Log, format, a...)
	}
}

// pin pulls the source's image and returns it by digest.
func (im *Importer) pin(ctx context.Context, tag string) (string, error) {
	if err := im.Engine.ImagePull(ctx, tag, im.Log); err != nil {
		return "", fmt.Errorf("tbench2: pull %s: %w", tag, err)
	}
	img, err := im.Engine.ImageInspect(ctx, tag)
	if err != nil {
		return "", err
	}
	repo := tag
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	for _, d := range img.RepoDigests {
		if r, _, ok := strings.Cut(d, "@"); ok && (r == repo || r == "docker.io/"+repo) {
			return repo + d[len(r):], nil
		}
	}
	return "", fmt.Errorf("tbench2: %s has no digest from its registry (%v)", tag, img.RepoDigests)
}

// run runs a command in the image as root, with the task's files bound
// where the command expects them.
func (im *Importer) run(ctx context.Context, image string, cmd []string, mounts []docker.Mount, network string, timeout time.Duration) error {
	var out tail
	r, err := im.Engine.Run(ctx, docker.Spec{
		Image: image, Cmd: cmd, User: "0:0", Network: network, WorkDir: "/app", Mounts: mounts,
		Tmpfs: map[string]string{"/tmp": "exec"}, Labels: map[string]string{"automaticum": "tbench2-import"},
	}, timeout, &out, &out, nil)
	if err != nil {
		return err
	}
	if r.TimedOut || r.ExitCode != 0 {
		return fmt.Errorf("%s: exit %d (timed out: %v): %s", strings.Join(cmd, " "), r.ExitCode, r.TimedOut, out.String())
	}
	return nil
}

// seed copies the image's /app out.
func (im *Importer) seed(ctx context.Context, image, dst string) error {
	if err := os.MkdirAll(dst, 0o777); err != nil {
		return err
	}
	os.Chmod(dst, 0o777)
	if err := im.run(ctx, image, []string{"sh", "-c", "cp -R /app/. /seed/"}, []docker.Mount{{Source: dst, Target: "/seed"}}, "none", 5*time.Minute); err != nil {
		return err
	}
	os.Chmod(dst, 0o755)
	return checkTree(dst, maxSeed)
}

// oracleFiles writes the verifier's sources: the source's tests without
// the script that fetches and runs them, and the build and entry point.
func (im *Importer) oracleFiles(s *Source, dir string) error {
	tests := filepath.Join(dir, "oracle", "tests")
	err := filepath.WalkDir(filepath.Join(s.Dir, "tests"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(s.Dir, "tests"), p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(tests, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("tests/%s is not a regular file", rel)
		}
		if rel == "test.sh" {
			return nil
		}
		return copyFile(p, filepath.Join(tests, rel))
	})
	if err != nil {
		return err
	}
	for _, n := range []string{"main.py", "build.py"} {
		b, _ := verifierFiles.ReadFile("verifier/" + n)
		if err := os.WriteFile(filepath.Join(dir, "oracle", n), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// wheels downloads pytest and the tests' packages as wheels, in the image
// the verifier runs in, so they are the ones its Python takes. An earlier
// import's oracle/requirements.txt pins them by hash, so importing again
// gives the same files.
func (im *Importer) wheels(ctx context.Context, s *Source, image, dir string) ([]Wheel, error) {
	wd := filepath.Join(dir, "oracle", "wheels")
	if err := os.MkdirAll(wd, 0o777); err != nil {
		return nil, err
	}
	os.Chmod(wd, 0o777)
	args := []string{"pip", "download", "--disable-pip-version-check", "--no-cache-dir", "--only-binary=:all:", "--dest", "/wheels"}
	mounts := []docker.Mount{{Source: wd, Target: "/wheels"}}
	earlier := filepath.Join(im.Repo, "tasks", Suite, s.Name, "oracle", "requirements.txt")
	if _, err := os.Stat(earlier); err == nil {
		args = append(args, "--no-deps", "--require-hashes", "-r", "/pins/requirements.txt")
		mounts = append(mounts, docker.Mount{Source: filepath.Dir(earlier), Target: "/pins", ReadOnly: true})
	} else {
		args = append(append(args, "pytest=="+s.Pytest), s.Packages...)
	}
	if err := im.run(ctx, image, args, mounts, "", 10*time.Minute); err != nil {
		return nil, err
	}
	os.Chmod(wd, 0o755)
	entries, err := os.ReadDir(wd)
	if err != nil {
		return nil, err
	}
	var ws []Wheel
	var req strings.Builder
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".whl") {
			return nil, fmt.Errorf("pip left %s, which is no wheel", e.Name())
		}
		total += info.Size()
		sum, err := task.HashFile(filepath.Join(wd, e.Name()))
		if err != nil {
			return nil, err
		}
		ws = append(ws, Wheel{File: e.Name(), SHA256: sum})
		parts := strings.SplitN(e.Name(), "-", 3)
		fmt.Fprintf(&req, "%s==%s --hash=sha256:%s\n", parts[0], parts[1], sum)
	}
	if total > maxWheel {
		return nil, fmt.Errorf("the wheels are %d bytes, more than the %d a public repository should carry", total, maxWheel)
	}
	if len(ws) == 0 {
		return nil, errors.New("pip downloaded nothing")
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].File < ws[j].File })
	return ws, os.WriteFile(filepath.Join(dir, "oracle", "requirements.txt"), []byte(req.String()), 0o644)
}

// gold runs solve.sh on a copy of the seed, as root in the image as the
// source does, and keeps what it wrote as the gold fixture's files.
func (im *Importer) gold(ctx context.Context, s *Source, image, dir string) (map[string]string, error) {
	seed := filepath.Join(dir, "seed")
	work, err := os.MkdirTemp(im.Env.Scratch, "tbench2-gold-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	if err := os.CopyFS(filepath.Join(work, "app"), os.DirFS(seed)); err != nil {
		return nil, err
	}
	err = im.run(ctx, image, []string{"bash", "/solution/solve.sh"}, []docker.Mount{
		{Source: filepath.Join(work, "app"), Target: "/app"},
		{Source: filepath.Join(s.Dir, "solution"), Target: "/solution", ReadOnly: true},
	}, "none", time.Duration(s.AgentTimeoutS)*time.Second)
	if err != nil {
		return nil, err
	}
	before, err := task.ReadManifest(seed)
	if err != nil {
		return nil, err
	}
	after, err := task.ReadManifest(filepath.Join(work, "app"))
	if err != nil {
		return nil, err
	}
	gold := map[string]string{}
	var total int64
	for p, sum := range after {
		if before[p] == sum {
			continue
		}
		if strings.HasPrefix(sum, "link:") {
			return nil, fmt.Errorf("solve.sh made %s a link", p)
		}
		src := filepath.Join(work, "app", filepath.FromSlash(p))
		info, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		total += info.Size()
		rel := path.Join("fixtures", "gold", p)
		if err := copyFile(src, filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return nil, err
		}
		gold[p] = rel
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			return nil, fmt.Errorf("solve.sh removed %s, which a fixture cannot say", p)
		}
	}
	if len(gold) == 0 {
		return nil, errors.New("solve.sh changed nothing")
	}
	if total > maxGold {
		return nil, fmt.Errorf("the gold fixture is %d bytes, more than %d", total, maxGold)
	}
	return gold, nil
}

func (im *Importer) writeTask(s *Source, p *Pins, dir string, inputs []string) error {
	text, err := TaskTOML(s, p, inputs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "task.toml"), []byte(text), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte(Notes(s, p)), 0o644)
}

// filesUnder lists the regular files under dir/sub, relative to dir.
func filesUnder(dir, sub string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(dir, sub), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// checkTree refuses what a task directory may not hold, and a tree larger
// than max bytes.
func checkTree(dir string, max int64) error {
	var total int64
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if total += info.Size(); total > max {
			return fmt.Errorf("more than %d bytes", max)
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info.Mode()&0o111 != 0 {
		mode = 0o755
	}
	return os.WriteFile(dst, b, mode)
}

// replaceDir puts the staged directory where dst is, replacing it.
func replaceDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.CopyFS(dst, os.DirFS(src))
}

// tail keeps the end of a command's output, for its error.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = t.b[len(t.b)-4096:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(t.b)))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}
