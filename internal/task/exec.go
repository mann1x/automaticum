package task

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mann1x/automaticum/internal/docker"
)

// Executor runs a container to its end. *docker.Client is one.
type Executor interface {
	Run(ctx context.Context, s docker.Spec, timeout time.Duration, stdout, stderr io.Writer, onStart func(id string)) (docker.Result, error)
}

// Env is where task work happens on this installation.
type Env struct {
	Exec Executor
	// Artefacts is the directory built artefacts are kept in, by hash.
	Artefacts string
	// Scratch is a directory for short-lived copies of workspaces. It must
	// be on a real disk, not a RAM-backed one.
	Scratch string
	// User is the "uid:gid" containers run as, and the owner of the
	// workspaces. Empty means "1000:1000".
	User string
	// BuildTimeout bounds an artefact build; zero means ten minutes.
	BuildTimeout time.Duration
}

func (e *Env) user() string {
	if e.User == "" {
		return "1000:1000"
	}
	return e.User
}

// ErrArtefactChanged means a build gave another result than the task pins.
var ErrArtefactChanged = errors.New("task: built artefact does not match its pinned sha256")

// ArtefactPath is where an artefact with this hash is kept.
func (e *Env) ArtefactPath(a *Artefact) string {
	return filepath.Join(e.Artefacts, a.SHA256, a.Output)
}

// Build makes sure the artefact exists under Env.Artefacts with the pinned
// hash, building it if it is not there, and returns its path. A build whose
// result has another hash is refused and nothing is kept.
func (e *Env) Build(ctx context.Context, t *Task, a *Artefact, log io.Writer) (string, error) {
	dst := e.ArtefactPath(a)
	if sum, err := HashFile(dst); err == nil {
		if sum == a.SHA256 {
			return dst, nil
		}
		// A damaged copy: rebuild it.
		os.RemoveAll(filepath.Dir(dst))
	}
	if err := os.MkdirAll(e.Artefacts, 0o755); err != nil {
		return "", err
	}
	out, err := os.MkdirTemp(e.Artefacts, ".build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(out)
	// The build runs as the lane user and must be able to write its output.
	if err := os.Chmod(out, 0o777); err != nil {
		return "", err
	}
	timeout := e.BuildTimeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	res, err := e.Exec.Run(ctx, docker.Spec{
		Image: a.Image, Cmd: a.Command, User: e.user(), Network: "none", WorkDir: "/out",
		Mounts: []docker.Mount{{Source: t.Dir, Target: "/task", ReadOnly: true}, {Source: out, Target: "/out"}},
		Tmpfs:  map[string]string{"/tmp": "exec"},
		Labels: map[string]string{"automaticum": "artefact-build"},
	}, timeout, log, log, nil)
	if err != nil {
		return "", fmt.Errorf("task: build %s: %w", a.Name, err)
	}
	if res.TimedOut || res.ExitCode != 0 {
		return "", fmt.Errorf("task: build %s: exit %d (timed out: %v)", a.Name, res.ExitCode, res.TimedOut)
	}
	sum, err := HashFile(filepath.Join(out, a.Output))
	if err != nil {
		return "", fmt.Errorf("task: build %s produced no %s: %w", a.Name, a.Output, err)
	}
	if sum != a.SHA256 {
		return "", fmt.Errorf("%w: %s is %s, pinned %s", ErrArtefactChanged, a.Name, sum, a.SHA256)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(out, a.Output), dst, 0o555); err != nil {
		return "", err
	}
	return dst, nil
}

// BuildAll builds every artefact of the task.
func (e *Env) BuildAll(ctx context.Context, t *Task, log io.Writer) error {
	for i := range t.Artefacts {
		if _, err := e.Build(ctx, t, &t.Artefacts[i], log); err != nil {
			return err
		}
	}
	return nil
}

// Manifest is the content of a workspace: path to sha256. Links are recorded
// as "link:<target>".
type Manifest map[string]string

// Hash is the hash of the whole manifest.
func (m Manifest) Hash() string { return hashMap(m) }

// PrepareWorkspace fills the empty directory dir with the task's seed and
// installed artefacts and returns what it placed. The directory must exist
// and be empty: a run never inherits what an earlier one left.
func (e *Env) PrepareWorkspace(t *Task, dir string) (Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("task: workspace %s is not empty", dir)
	}
	if t.Workspace.Seed != "" {
		if err := copyTree(filepath.Join(t.Dir, t.Workspace.Seed), dir); err != nil {
			return nil, err
		}
	}
	if err := e.install(t, dir, nil); err != nil {
		return nil, err
	}
	return ReadManifest(dir)
}

// install copies artefacts into dir. With only set, just that artefact's
// installs are placed.
func (e *Env) install(t *Task, dir string, only *Artefact) error {
	for _, in := range t.Workspace.Install {
		a := t.Artefact(in.Artefact)
		if only != nil && a != only {
			continue
		}
		src := e.ArtefactPath(a)
		if sum, err := HashFile(src); err != nil || sum != a.SHA256 {
			return fmt.Errorf("task: artefact %s is not built or is damaged; build the task first", a.Name)
		}
		mode, _ := in.FileMode()
		dst := filepath.Join(dir, in.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		// Whatever is there now, a link included, is replaced, not followed.
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := copyFile(src, dst, mode); err != nil {
			return err
		}
	}
	return nil
}

// ReadManifest hashes every file under dir.
func ReadManifest(dir string) (Manifest, error) {
	m := Manifest{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(dir, p)
		r = filepath.ToSlash(r)
		switch {
		case d.IsDir():
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			m[r] = "link:" + target
		case d.Type().IsRegular():
			sum, err := HashFile(p)
			if err != nil {
				return err
			}
			m[r] = sum
		default:
			m[r] = "special"
		}
		return nil
	})
	return m, err
}

// OracleResult is what one run of an oracle gave.
type OracleResult struct {
	Oracle   string
	Command  []string
	Output   string // stdout and stderr together, as produced
	ExitCode int
	TimedOut bool
	Duration time.Duration
	// Passed is the oracle's answer. It is false when the oracle could not
	// answer; Err says why in that case.
	Passed bool
	// Tampered lists installed artefacts whose copy in the workspace no
	// longer had the pinned hash. The oracle ran from the harness's own
	// copy regardless.
	Tampered []string
	ImageID  string
	Err      string
}

// maxOracleOutput bounds what an oracle may print.
const maxOracleOutput = 4 << 20

// RunOracle runs an oracle on the files of workspace. It works on a copy:
// the workspace is not touched, and the oracle's artefact is installed from
// the harness's own copy over whatever the agent left. The container has no
// network and a read-only root.
func (e *Env) RunOracle(ctx context.Context, t *Task, o *Oracle, workspace string) (OracleResult, error) {
	res := OracleResult{Oracle: o.Name, Command: o.Command}
	if err := os.MkdirAll(e.Scratch, 0o755); err != nil {
		return res, err
	}
	tmp, err := os.MkdirTemp(e.Scratch, "oracle-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return res, err
	}
	if err := copyTree(workspace, tmp); err != nil {
		return res, err
	}
	// What did the agent do to its copies of the installed artefacts?
	for _, in := range t.Workspace.Install {
		a := t.Artefact(in.Artefact)
		if sum, err := HashFile(filepath.Join(tmp, in.Path)); err != nil || sum != a.SHA256 {
			res.Tampered = append(res.Tampered, in.Path)
		}
	}
	if o.Artefact != "" {
		if err := e.install(t, tmp, t.Artefact(o.Artefact)); err != nil {
			return res, err
		}
	}
	timeout := time.Duration(o.TimeoutS) * time.Second
	if timeout == 0 {
		timeout = time.Duration(t.Limits.OracleS) * time.Second
	}
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	var out limitedBuffer
	out.max = maxOracleOutput
	start := time.Now()
	r, err := e.Exec.Run(ctx, docker.Spec{
		Image: o.Image, Cmd: o.Command, User: e.user(), Network: "none", WorkDir: "/w", ReadOnlyRoot: true,
		Mounts: []docker.Mount{{Source: tmp, Target: "/w", ReadOnly: true}},
		Tmpfs:  map[string]string{"/tmp": ""},
		Labels: map[string]string{"automaticum": "oracle"},
	}, timeout, &out, &out, nil)
	res.Duration = time.Since(start)
	res.Output, res.ExitCode, res.TimedOut, res.ImageID = out.String(), r.ExitCode, r.TimedOut, r.ImageID
	if err != nil {
		return res, fmt.Errorf("task: oracle %s: %w", o.Name, err)
	}
	switch {
	case r.TimedOut:
		res.Err = "the oracle timed out"
	case out.truncated:
		res.Err = "the oracle printed more than the output limit"
	case o.pattern != nil:
		res.Passed = o.pattern.MatchString(res.Output)
	default:
		res.Passed = r.ExitCode == *o.ExpectExit
	}
	return res, nil
}

// FixtureResult is how the primary oracle judged one fixture.
type FixtureResult struct {
	Fixture string
	Expect  string
	Runs    []OracleResult
	// OK is true when every run gave the expected answer.
	OK bool
}

// Verify proves the primary oracle against the task's fixtures: each fixture
// is judged repeats times and must get its expected answer every time. It
// returns an error only when the check could not be made; a wrong or flaky
// oracle shows as a FixtureResult that is not OK.
func (e *Env) Verify(ctx context.Context, t *Task, repeats int) ([]FixtureResult, error) {
	if !t.HasFixtures() {
		return nil, fmt.Errorf("task: %s needs a fixture that passes and one that fails", t.Name())
	}
	if repeats < 1 {
		repeats = 1
	}
	o := t.PrimaryOracle()
	var out []FixtureResult
	for _, f := range t.Fixtures {
		ws, err := os.MkdirTemp(e.Scratch, "fixture-")
		if err != nil {
			return nil, err
		}
		fr := FixtureResult{Fixture: f.Name, Expect: f.Expect, OK: true}
		err = func() error {
			defer os.RemoveAll(ws)
			if _, err := e.PrepareWorkspace(t, ws); err != nil {
				return err
			}
			for dst, src := range f.Files {
				p := filepath.Join(ws, dst)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				os.Remove(p)
				if err := copyFile(filepath.Join(t.Dir, src), p, 0o644); err != nil {
					return err
				}
			}
			for i := 0; i < repeats; i++ {
				r, err := e.RunOracle(ctx, t, o, ws)
				if err != nil {
					return err
				}
				fr.Runs = append(fr.Runs, r)
				if r.Err != "" || r.Passed != (f.Expect == "pass") || len(r.Tampered) > 0 {
					fr.OK = false
				}
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, nil
}

// limitedBuffer keeps at most max bytes and remembers that more came.
type limitedBuffer struct {
	bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); len(p) > room {
		b.truncated = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// The mode asked for, whatever the process's umask.
	return os.Chmod(dst, mode)
}

// copyTree copies the files of src into the existing directory dst. Links
// are copied as links and never followed; anything that is neither a file,
// a directory nor a link is left out.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(src, p)
		if r == "." {
			return nil
		}
		target := filepath.Join(dst, r)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			return copyFile(p, target, info.Mode().Perm()|0o444)
		}
		return nil
	})
}
