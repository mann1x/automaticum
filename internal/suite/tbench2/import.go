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
	"regexp"
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
	// Config is the import config: a task is imported only if it is there.
	Config *Config
	// Repeats is how often Verify judges each fixture; zero means 3.
	Repeats int
	Log     io.Writer
}

// laneUser is who an agent or test that is not declared root runs as.
const laneUser = "1000:1000"

// Limits on what an import puts in the repository, which is public.
const (
	maxSeed = 4 << 20
	// maxSeedFile is the largest file of the image's /app kept in seed/; a
	// larger one is taken from the image when the task is built (SeedFile).
	maxSeedFile = 1 << 20
	maxGold     = 4 << 20
	maxWheel    = 8 << 20
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
	d, ok := im.Config.Task[name]
	if !ok {
		return nil, fmt.Errorf("tbench2: %s is not in %s; a task is declared there before it is imported", name, ConfigFile)
	}
	s.Declared = d
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
	large := filepath.Join(stage, "large")
	if p.SeedFiles, err = im.seed(ctx, p.Image, filepath.Join(dir, "seed"), large); err != nil {
		return nil, fmt.Errorf("tbench2: %s: seed: %w", name, err)
	}
	if err := im.oracleFiles(s, dir); err != nil {
		return nil, fmt.Errorf("tbench2: %s: %w", name, err)
	}
	im.logf("%s: wheels for pytest %s %s\n", name, s.Pytest, strings.Join(s.Packages, " "))
	if p.PipImage, err = im.pipImage(ctx, p.Image); err != nil {
		return nil, fmt.Errorf("tbench2: %s: %w", name, err)
	}
	if p.Wheels, err = im.wheels(ctx, s, p.PipImage, dir); err != nil {
		return nil, fmt.Errorf("tbench2: %s: wheels: %w", name, err)
	}
	im.logf("%s: gold from solution/solve.sh\n", name)
	if p.Gold, err = im.gold(ctx, s, p.Image, dir, large); err != nil {
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
	if p.Verifier, err = im.Env.Pin(ctx, t, t.Artefact(verifierArtefact), im.Log); err != nil {
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

// run runs a container of the import to its end: as root with the
// engine's default capabilities, or as the lane's user, as the task
// declares (task.As). A command that fails is an error with its output.
func (im *Importer) run(ctx context.Context, sp docker.Spec, as string, timeout time.Duration) (docker.Result, error) {
	var out tail
	sp.User, sp.CapAdd = task.As(as, laneUser)
	if sp.WorkDir == "" {
		sp.WorkDir = "/app"
	}
	sp.Tmpfs = map[string]string{"/tmp": "exec"}
	sp.Labels = map[string]string{"automaticum": "tbench2-import"}
	r, err := im.Engine.Run(ctx, sp, timeout, &out, &out, nil)
	if err == nil {
		err = r.CollectErr
	}
	if err != nil {
		return r, err
	}
	if r.TimedOut || r.ExitCode != 0 {
		return r, fmt.Errorf("%s: exit %d (timed out: %v): %s", strings.Join(sp.Cmd, " "), r.ExitCode, r.TimedOut, out.String())
	}
	return r, nil
}

// seed copies the image's /app out. A file larger than maxSeedFile is
// moved to large, at the same path, and returned: the task takes it from the
// image (SeedFile) instead of the repository.
func (im *Importer) seed(ctx context.Context, image, dst, large string) ([]SeedFile, error) {
	if err := os.MkdirAll(dst, 0o777); err != nil {
		return nil, err
	}
	os.Chmod(dst, 0o777)
	if _, err := im.run(ctx, docker.Spec{Image: image, Cmd: []string{"sh", "-c", "cp -R /app/. /seed/"},
		Mounts: []docker.Mount{{Source: dst, Target: "/seed"}}, Network: "none"}, task.Root, 5*time.Minute); err != nil {
		return nil, err
	}
	os.Chmod(dst, 0o755)
	var files []SeedFile
	err := filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil || info.Size() <= maxSeedFile {
			return err
		}
		rel, _ := filepath.Rel(dst, p)
		sum, err := task.HashFile(p)
		if err != nil {
			return err
		}
		mode := "0644"
		if info.Mode()&0o111 != 0 {
			mode = "0755"
		}
		files = append(files, SeedFile{Path: filepath.ToSlash(rel), SHA256: sum, Mode: mode})
		to := filepath.Join(large, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		return os.Rename(p, to)
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, checkTree(dst, maxSeed)
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

// pipImage is the image pip runs in for a task's image: that image, if its
// Python has pip, so the packages are the ones its Python takes; otherwise
// the official image of the same Python version, whose choice of wheels
// that Python takes too.
func (im *Importer) pipImage(ctx context.Context, image string) (string, error) {
	if _, err := im.run(ctx, docker.Spec{Image: image, Cmd: []string{"python3", "-m", "pip", "--version"}, Network: "none"}, task.Root, 2*time.Minute); err == nil {
		return image, nil
	}
	var out tail
	r, err := im.Engine.Run(ctx, docker.Spec{Image: image, Network: "none", WorkDir: "/",
		Cmd: []string{"python3", "-c", "import sys; print('%d.%d' % sys.version_info[:2])"}}, 2*time.Minute, &out, io.Discard, nil)
	version := strings.TrimSpace(out.String())
	if err != nil || r.ExitCode != 0 || !versionRE.MatchString(version) {
		return "", fmt.Errorf("the image has no pip, and its Python version cannot be read (%v, %q)", err, version)
	}
	pi := "python:" + version + "-slim"
	if err := im.Engine.ImagePull(ctx, pi, im.Log); err != nil {
		return "", err
	}
	im.logf("the image's Python %s has no pip: packages are chosen by %s\n", version, pi)
	return pi, nil
}

var versionRE = regexp.MustCompile(`^3\.[0-9]+$`)

// wheels downloads pytest and the tests' packages, with pip in image (the
// verifier's own, or one of the same Python: pipImage), so they are the
// ones its Python takes: wheels where
// the package has them, and a source distribution where it has none, which
// is built into a wheel once (D114): the wheel is kept, and built.txt
// names the source distribution by hash. An earlier import's oracle/requirements.txt pins what was downloaded
// by hash and its oracle/built.txt what was built, so importing again
// gives the same files.
func (im *Importer) wheels(ctx context.Context, s *Source, image, dir string) ([]Wheel, error) {
	od := filepath.Join(dir, "oracle")
	dl, err := os.MkdirTemp(im.Env.Scratch, "tbench2-dl-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dl)
	os.Chmod(dl, 0o777)
	args := []string{"python3", "-m", "pip", "download", "--disable-pip-version-check", "--no-cache-dir", "--prefer-binary", "--dest", "/dl"}
	mounts := []docker.Mount{{Source: dl, Target: "/dl"}}
	earlier := filepath.Join(im.Repo, "tasks", Suite, s.Name, "oracle")
	if _, err := os.Stat(filepath.Join(earlier, "requirements.txt")); err == nil {
		args = append(args, "--no-deps", "--require-hashes", "-r", "/pins/requirements.txt")
		mounts = append(mounts, docker.Mount{Source: earlier, Target: "/pins", ReadOnly: true})
	} else {
		args = append(append(args, "pytest=="+s.Pytest), s.Packages...)
	}
	if _, err := im.run(ctx, docker.Spec{Image: image, Cmd: args, Mounts: mounts}, task.Root, 10*time.Minute); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dl)
	if err != nil {
		return nil, err
	}
	built := readBuilt(filepath.Join(earlier, "built.txt"))
	var ws []Wheel
	var req, record strings.Builder
	var total int64
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("pip left %s, which is no file", e.Name())
		}
		src := filepath.Join(dl, e.Name())
		sum, err := task.HashFile(src)
		if err != nil {
			return nil, err
		}
		name, version, kind := distribution(e.Name())
		if kind == "" {
			return nil, fmt.Errorf("pip left %s, which is neither a wheel nor a source distribution", e.Name())
		}
		fmt.Fprintf(&req, "%s==%s --hash=sha256:%s\n", name, version, sum)
		w := Wheel{File: e.Name(), SHA256: sum}
		if kind == "sdist" {
			if w, err = im.build(ctx, image, src, sum, built[sum], filepath.Join(earlier, "wheels"), filepath.Join(od, "wheels")); err != nil {
				return nil, fmt.Errorf("building %s: %w", e.Name(), err)
			}
			fmt.Fprintf(&record, "%s %s %s %s\n", w.File, w.SHA256, e.Name(), sum)
		} else if err := copyFile(src, filepath.Join(od, "wheels", e.Name())); err != nil {
			return nil, err
		}
		info, _ := e.Info()
		total += info.Size()
		ws = append(ws, w)
	}
	if total > maxWheel {
		return nil, fmt.Errorf("the packages are %d bytes, more than the %d a public repository should carry", total, maxWheel)
	}
	if len(ws) == 0 {
		return nil, errors.New("pip downloaded nothing")
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].File < ws[j].File })
	if record.Len() > 0 {
		if err := os.WriteFile(filepath.Join(od, "built.txt"), []byte(record.String()), 0o644); err != nil {
			return nil, err
		}
	}
	return ws, os.WriteFile(filepath.Join(od, "requirements.txt"), []byte(req.String()), 0o644)
}

// distribution reads a package file's name: its project, version and
// whether it is a wheel or a source distribution ("" if neither).
func distribution(file string) (name, version, kind string) {
	switch {
	case strings.HasSuffix(file, ".whl"):
		parts := strings.SplitN(file, "-", 3)
		if len(parts) == 3 {
			return parts[0], parts[1], "wheel"
		}
	case strings.HasSuffix(file, ".tar.gz"), strings.HasSuffix(file, ".zip"):
		base := strings.TrimSuffix(strings.TrimSuffix(file, ".tar.gz"), ".zip")
		if i := strings.LastIndex(base, "-"); i > 0 {
			return base[:i], base[i+1:], "sdist"
		}
	}
	return "", "", ""
}

// builtWheel is one line of oracle/built.txt.
type builtWheel struct{ file, sha256 string }

// readBuilt reads an earlier import's built.txt: by the source
// distribution's hash, the wheel built from it.
func readBuilt(p string) map[string]builtWheel {
	out := map[string]builtWheel{}
	b, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) == 4 {
			out[f[3]] = builtWheel{f[0], f[1]}
		}
	}
	return out
}

// build makes a wheel of the source distribution sdist, in the image, or
// takes the one an earlier import built from the same file.
func (im *Importer) build(ctx context.Context, image, sdist, sum string, before builtWheel, earlierDir, dst string) (Wheel, error) {
	if before.file != "" {
		if got, err := task.HashFile(filepath.Join(earlierDir, before.file)); err == nil && got == before.sha256 {
			return Wheel{File: before.file, SHA256: got, Built: filepath.Base(sdist)}, copyFile(filepath.Join(earlierDir, before.file), filepath.Join(dst, before.file))
		}
	}
	out, err := os.MkdirTemp(im.Env.Scratch, "tbench2-wheel-")
	if err != nil {
		return Wheel{}, err
	}
	defer os.RemoveAll(out)
	os.Chmod(out, 0o777)
	_, err = im.run(ctx, docker.Spec{Image: image,
		Cmd: []string{"python3", "-m", "pip", "wheel", "--disable-pip-version-check", "--no-cache-dir", "--no-deps", "--wheel-dir", "/out", "/sdist/" + filepath.Base(sdist)},
		// The times in the wheel are fixed, so the same build gives the same file.
		Env:    []string{"SOURCE_DATE_EPOCH=315532800"},
		Mounts: []docker.Mount{{Source: filepath.Dir(sdist), Target: "/sdist", ReadOnly: true}, {Source: out, Target: "/out"}},
	}, task.Root, 10*time.Minute)
	if err != nil {
		return Wheel{}, err
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return Wheel{}, err
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".whl") {
		return Wheel{}, fmt.Errorf("pip wheel left %d files", len(entries))
	}
	f := entries[0].Name()
	got, err := task.HashFile(filepath.Join(out, f))
	if err != nil {
		return Wheel{}, err
	}
	return Wheel{File: f, SHA256: got, Built: filepath.Base(sdist)}, copyFile(filepath.Join(out, f), filepath.Join(dst, f))
}

// gold runs solve.sh on a copy of the seed, in the image as the agent
// will run (as the lane's user, or as root if the task declares it), and
// keeps what it wrote as the gold fixture's files: in /app, and in the
// task's collect paths, against what the image has there.
func (im *Importer) gold(ctx context.Context, s *Source, image, dir, large string) (map[string]string, error) {
	work, err := os.MkdirTemp(im.Env.Scratch, "tbench2-gold-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	// The workspace as the agent gets it: the seed and its large files.
	app, start := filepath.Join(work, "app"), filepath.Join(work, "start")
	for _, d := range []string{app, start} {
		if err := os.CopyFS(d, os.DirFS(filepath.Join(dir, "seed"))); err != nil {
			return nil, err
		}
		if _, err := os.Stat(large); err == nil {
			if err := os.CopyFS(d, os.DirFS(large)); err != nil {
				return nil, err
			}
		}
	}
	if s.Declared.RunAs != task.Root {
		if err := ownTree(app, 1000, 1000); err != nil {
			return nil, err
		}
	}
	collect := s.Declared.Collect
	before, after := map[string]string{}, map[string]string{}
	for i, c := range collect {
		before[c], after[c] = filepath.Join(work, fmt.Sprintf("before-%d", i)), filepath.Join(work, fmt.Sprintf("after-%d", i))
		for _, d := range []string{before[c], after[c]} {
			if err := os.Mkdir(d, 0o755); err != nil {
				return nil, err
			}
		}
	}
	if len(collect) > 0 {
		if _, err := im.run(ctx, docker.Spec{Image: image, Cmd: []string{"/bin/sh", "-c", ":"}, Network: "none", Collect: before}, task.Root, 5*time.Minute); err != nil {
			return nil, err
		}
	}
	_, err = im.run(ctx, docker.Spec{Image: image, Cmd: []string{"bash", "/solution/solve.sh"}, Network: "none", Collect: after,
		Mounts: []docker.Mount{
			{Source: app, Target: "/app"},
			{Source: filepath.Join(s.Dir, "solution"), Target: "/solution", ReadOnly: true},
		}}, s.Declared.RunAs, time.Duration(s.AgentTimeoutS)*time.Second)
	if err != nil {
		return nil, err
	}
	gold := map[string]string{}
	var total int64
	keep := func(beforeDir, afterDir, at, fixture string) error {
		b, err := task.ReadManifest(beforeDir)
		if err != nil {
			return err
		}
		a, err := task.ReadManifest(afterDir)
		if err != nil {
			return err
		}
		for p, sum := range a {
			if b[p] == sum {
				continue
			}
			key := path.Join(at, p)
			if strings.HasPrefix(sum, "link:") || sum == "special" {
				return fmt.Errorf("solve.sh made %s a link or a special file", key)
			}
			src := filepath.Join(afterDir, filepath.FromSlash(p))
			info, err := os.Stat(src)
			if err != nil {
				return err
			}
			total += info.Size()
			rel := path.Join("fixtures", fixture, p)
			if err := copyFile(src, filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				return err
			}
			gold[key] = rel
		}
		for p := range b {
			if _, ok := a[p]; !ok {
				return fmt.Errorf("solve.sh removed %s, which a fixture cannot say", path.Join(at, p))
			}
		}
		return nil
	}
	if err := keep(start, app, "", "gold"); err != nil {
		return nil, err
	}
	for _, c := range collect {
		if err := keep(before[c], after[c], c, path.Join("gold-root", c)); err != nil {
			return nil, err
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

// ownTree gives dir and what is under it to uid:gid, so the lane's user
// can write it; a harness that may not chown opens it to everyone instead.
func ownTree(dir string, uid, gid int) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if os.Getuid() == 0 {
			return os.Lchown(p, uid, gid)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return os.Chmod(p, 0o777)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(p, info.Mode().Perm()|0o666)
	})
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
