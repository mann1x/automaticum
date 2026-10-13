// Package task loads tasks from the tasks directory, builds what they need,
// prepares workspaces and runs oracles (spec 060).
//
// A task is a directory tasks/<suite>/<task>/ with a task.toml. Everything a
// run depends on is hashed: a changed file is a changed task.
package task

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Task is one task, as its task.toml states it.
type Task struct {
	ID            string            `toml:"id"`
	Suite         string            `toml:"suite"`
	Version       int               `toml:"version"`
	Layers        []string          `toml:"layers"`
	Prompt        string            `toml:"prompt"`
	DefaultAnswer string            `toml:"default_answer"`
	Labels        map[string]string `toml:"labels"`
	Limits        Limits            `toml:"limits"`
	Workspace     Workspace         `toml:"workspace"`
	Artefacts     []Artefact        `toml:"artefact"`
	Oracles       []Oracle          `toml:"oracle"`
	Fixtures      []Fixture         `toml:"fixture"`

	// Dir is the task's directory. Files is every regular file under it
	// with its sha256, and Hash is the hash of that list: the task's
	// identity beyond its version number.
	Dir   string            `toml:"-"`
	Files map[string]string `toml:"-"`
	Hash  string            `toml:"-"`
}

// Limits are the task's own limits, in seconds. Zero means the task sets none.
type Limits struct {
	WallS   int `toml:"wall_s"`
	IdleS   int `toml:"idle_s"`
	OracleS int `toml:"oracle_s"`
}

// Workspace says what the agent starts from.
type Workspace struct {
	// Seed is a directory, relative to the task, copied into the workspace.
	Seed string `toml:"seed"`
	// Install places built artefacts in the workspace.
	Install []Install `toml:"install"`
}

// Install places one artefact at a path in the workspace.
type Install struct {
	Artefact string `toml:"artefact"`
	Path     string `toml:"path"`
	Mode     string `toml:"mode"` // octal, e.g. "0555"
}

// Artefact is something built from the task's files before the task can run.
type Artefact struct {
	Name    string   `toml:"name"`
	Image   string   `toml:"image"`   // pinned by digest
	Inputs  []string `toml:"inputs"`  // task files the build reads
	Command []string `toml:"command"` // the task is at /task, the output directory at /out
	Output  string   `toml:"output"`  // file name under /out
	SHA256  string   `toml:"sha256"`  // the pinned result
}

// Oracle decides, or helps decide, whether the task is done.
type Oracle struct {
	Name    string   `toml:"name"`
	Primary bool     `toml:"primary"`
	Image   string   `toml:"image"`
	Command []string `toml:"command"` // run in a copy of the workspace
	// Artefact, if set, is installed from the harness's own copy before
	// the command runs, whatever the agent left in its place.
	Artefact string `toml:"artefact"`
	// Exactly one expectation: a pattern the output must match, or an exit
	// code. A program that exits 0 either way is judged by its pattern.
	ExpectPattern string `toml:"expect_pattern"`
	ExpectExit    *int   `toml:"expect_exit"`
	TimeoutS      int    `toml:"timeout_s"`

	pattern *regexp.Regexp
}

// Fixture is a workspace state with a known answer.
type Fixture struct {
	Name   string            `toml:"name"`
	Expect string            `toml:"expect"` // "pass" or "fail"
	Files  map[string]string `toml:"files"`  // workspace path -> task file
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Load reads and validates the task in dir.
func Load(dir string) (*Task, error) {
	// Containers mount the task's directory, which takes an absolute path.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "task.toml")
	var t Task
	md, err := toml.DecodeFile(path, &t)
	if err != nil {
		return nil, fmt.Errorf("task: %w", err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("task: %s: unknown fields: %s", path, strings.Join(keys, ", "))
	}
	t.Dir = dir
	if err := t.validate(); err != nil {
		return nil, fmt.Errorf("task: %s: %w", path, err)
	}
	if err := t.hashFiles(); err != nil {
		return nil, fmt.Errorf("task: %s: %w", dir, err)
	}
	return &t, nil
}

// LoadAll loads every task under root (tasks/<suite>/<task>/task.toml),
// sorted by suite and id.
func LoadAll(root string) ([]*Task, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "task.toml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []*Task
	for _, p := range paths {
		t, err := Load(filepath.Dir(p))
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		if want := filepath.Join(t.Suite, t.ID); rel != want {
			return nil, fmt.Errorf("task: %s declares %s; the directory must be %s", p, want, want)
		}
		out = append(out, t)
	}
	return out, nil
}

// Find loads the task with this id under root. The id may be "suite/task" or,
// when it is unambiguous, just "task".
func Find(root, id string) (*Task, error) {
	all, err := LoadAll(root)
	if err != nil {
		return nil, err
	}
	var found []*Task
	for _, t := range all {
		if id == t.ID || id == t.Suite+"/"+t.ID {
			found = append(found, t)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("task: no task %q under %s", id, root)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("task: %q names %d tasks; say suite/task", id, len(found))
}

// rel checks that p is a clean relative path inside the task or workspace.
func rel(p string) error {
	if p == "" || filepath.IsAbs(p) || filepath.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("%q is not a relative path inside the directory", p)
	}
	return nil
}

func (t *Task) file(p string) error {
	if err := rel(p); err != nil {
		return err
	}
	fi, err := os.Lstat(filepath.Join(t.Dir, p))
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", p)
	}
	return nil
}

func (t *Task) validate() error {
	if !nameRE.MatchString(t.ID) || !nameRE.MatchString(t.Suite) {
		return errors.New("id and suite must be lower-case names")
	}
	if t.Version < 1 {
		return errors.New("version must be 1 or more")
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return errors.New("prompt is empty")
	}
	if t.Limits.WallS < 0 || t.Limits.IdleS < 0 || t.Limits.OracleS < 0 {
		return errors.New("limits must not be negative")
	}
	if t.Workspace.Seed != "" {
		if err := rel(t.Workspace.Seed); err != nil {
			return fmt.Errorf("workspace.seed: %w", err)
		}
		if fi, err := os.Stat(filepath.Join(t.Dir, t.Workspace.Seed)); err != nil || !fi.IsDir() {
			return fmt.Errorf("workspace.seed: %s is not a directory", t.Workspace.Seed)
		}
	}
	arts := map[string]bool{}
	for i := range t.Artefacts {
		a := &t.Artefacts[i]
		switch {
		case !nameRE.MatchString(a.Name) || arts[a.Name]:
			return fmt.Errorf("artefact %q: name missing, malformed or repeated", a.Name)
		case !strings.Contains(a.Image, "@sha256:"):
			return fmt.Errorf("artefact %s: image must be pinned by digest", a.Name)
		case len(a.Command) == 0 || len(a.Inputs) == 0:
			return fmt.Errorf("artefact %s: command and inputs are required", a.Name)
		case rel(a.Output) != nil || strings.Contains(a.Output, "/"):
			return fmt.Errorf("artefact %s: output must be a file name", a.Name)
		case !shaRE.MatchString(a.SHA256):
			return fmt.Errorf("artefact %s: sha256 must pin the result", a.Name)
		}
		for _, in := range a.Inputs {
			if err := t.file(in); err != nil {
				return fmt.Errorf("artefact %s: input: %w", a.Name, err)
			}
		}
		arts[a.Name] = true
	}
	for _, in := range t.Workspace.Install {
		if !arts[in.Artefact] {
			return fmt.Errorf("workspace.install: no artefact %q", in.Artefact)
		}
		if err := rel(in.Path); err != nil {
			return fmt.Errorf("workspace.install: %w", err)
		}
		if _, err := in.FileMode(); err != nil {
			return fmt.Errorf("workspace.install %s: %w", in.Path, err)
		}
	}
	names, primaries := map[string]bool{}, 0
	for i := range t.Oracles {
		o := &t.Oracles[i]
		switch {
		case !nameRE.MatchString(o.Name) || names[o.Name]:
			return fmt.Errorf("oracle %q: name missing, malformed or repeated", o.Name)
		case !strings.Contains(o.Image, "@sha256:"):
			return fmt.Errorf("oracle %s: image must be pinned by digest", o.Name)
		case len(o.Command) == 0:
			return fmt.Errorf("oracle %s: command is required", o.Name)
		case (o.ExpectPattern == "") == (o.ExpectExit == nil):
			return fmt.Errorf("oracle %s: give exactly one of expect_pattern and expect_exit", o.Name)
		case o.Artefact != "" && !arts[o.Artefact]:
			return fmt.Errorf("oracle %s: no artefact %q", o.Name, o.Artefact)
		case o.TimeoutS < 0:
			return fmt.Errorf("oracle %s: timeout must not be negative", o.Name)
		}
		if o.ExpectPattern != "" {
			re, err := regexp.Compile(o.ExpectPattern)
			if err != nil {
				return fmt.Errorf("oracle %s: expect_pattern: %w", o.Name, err)
			}
			o.pattern = re
		}
		names[o.Name] = true
		if o.Primary {
			primaries++
		}
	}
	if primaries != 1 {
		return fmt.Errorf("exactly one oracle must be primary; %d are", primaries)
	}
	fx := map[string]bool{}
	for _, f := range t.Fixtures {
		if !nameRE.MatchString(f.Name) || fx[f.Name] {
			return fmt.Errorf("fixture %q: name missing, malformed or repeated", f.Name)
		}
		if f.Expect != "pass" && f.Expect != "fail" {
			return fmt.Errorf("fixture %s: expect must be pass or fail", f.Name)
		}
		if len(f.Files) == 0 {
			return fmt.Errorf("fixture %s: no files", f.Name)
		}
		for dst, src := range f.Files {
			if err := rel(dst); err != nil {
				return fmt.Errorf("fixture %s: %w", f.Name, err)
			}
			if err := t.file(src); err != nil {
				return fmt.Errorf("fixture %s: %w", f.Name, err)
			}
		}
		fx[f.Name] = true
	}
	return nil
}

// FileMode parses the install mode; an empty mode is 0644.
func (in Install) FileMode() (fs.FileMode, error) {
	if in.Mode == "" {
		return 0o644, nil
	}
	m, err := strconv.ParseUint(in.Mode, 8, 32)
	if err != nil || m > 0o777 {
		return 0, fmt.Errorf("mode %q is not an octal file mode", in.Mode)
	}
	return fs.FileMode(m), nil
}

// HasFixtures reports whether the task has a fixture that must pass and one
// that must fail. A task without both cannot be used (spec 060 section 3).
func (t *Task) HasFixtures() bool {
	var pass, fail bool
	for _, f := range t.Fixtures {
		pass = pass || f.Expect == "pass"
		fail = fail || f.Expect == "fail"
	}
	return pass && fail
}

// PrimaryOracle returns the oracle that gives the verdict.
func (t *Task) PrimaryOracle() *Oracle {
	for i := range t.Oracles {
		if t.Oracles[i].Primary {
			return &t.Oracles[i]
		}
	}
	return nil
}

// Artefact returns the artefact with this name, or nil.
func (t *Task) Artefact(name string) *Artefact {
	for i := range t.Artefacts {
		if t.Artefacts[i].Name == name {
			return &t.Artefacts[i]
		}
	}
	return nil
}

// Name is "suite/id".
func (t *Task) Name() string { return t.Suite + "/" + t.ID }

// hashFiles hashes every regular file of the task. Anything else under the
// task directory (a link, a device) is an error: a task is plain files.
func (t *Task) hashFiles() error {
	t.Files = map[string]string{}
	err := filepath.WalkDir(t.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		r, _ := filepath.Rel(t.Dir, p)
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", r)
		}
		sum, err := HashFile(p)
		if err != nil {
			return err
		}
		t.Files[filepath.ToSlash(r)] = sum
		return nil
	})
	if err != nil {
		return err
	}
	t.Hash = hashMap(t.Files)
	return nil
}

// hashMap hashes a path-to-hash map in a fixed order.
func hashMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s\x00%s\n", k, m[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// HashFile returns the sha256 of a file, in hex.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
