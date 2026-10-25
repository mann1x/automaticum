// Package tbench2 imports tasks of Terminal-Bench 2.0 as the held-out suite
// tbench2 (spec 066). An imported task is an ordinary task: a seed, a
// prompt, a lane layer and a primary oracle, written once from a pinned
// commit of the source, so nothing of the source's own harness runs here.
//
// What the source gives, per task, and where it goes:
//
//   - instruction.md is the prompt, verbatim;
//   - environment/ built is the task's image: pinned by digest, it is the
//     lane layer tb2-<task> with Node and the runner added (064 §3.2), the
//     image the verifier runs in, and /app copied out of it is the seed;
//   - tests/ is the verifier: run by tests/test.sh with uvx and pytest, here
//     by the oracle with the same pytest and packages pinned and vendored,
//     with no network (the source fetches them when it verifies);
//   - solution/solve.sh, run once at import, gives the gold fixture.
//
// What the source does not say, the import config (tasks/tbench2/import.toml,
// Config) declares per task: that the agent or the tests run as root, and
// the paths besides /app the tests read or write (066 §3.1).
package tbench2

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/mann1x/automaticum/internal/task"
)

// Suite is the suite imported tasks go in.
const Suite = "tbench2"

// Layer is the lane layer of an imported task.
func Layer(name string) string { return "tb2-" + name }

// Source is one task of the source, as its files state it.
type Source struct {
	Name string // the directory, e.g. "schemelike-metacircular-eval"
	Dir  string

	Difficulty, Category string
	Tags                 []string
	ExpertMin, JuniorMin float64
	AgentTimeoutS        float64
	VerifierTimeoutS     float64
	Image                string // the environment image as the source names it (a tag)
	CPUs                 int
	MemoryMB             int
	AllowInternet        bool
	Canary               string // the source's canary GUID

	Instruction string
	// Pytest is the pytest version tests/test.sh runs, PytestArgs what it
	// runs pytest with (its report plugin's left out); Packages are the
	// other packages it adds, as given (name==version).
	Pytest     string
	PytestArgs []string
	Packages   []string

	// Declared is what the import config says of the task.
	Declared Declared
}

// Declared is what the import config states of one task, beyond the
// source's files: the source runs agent and tests as root in one container,
// and only what a task declares here is carried over (D110, D111).
type Declared struct {
	// RunAs and OracleRunAs are "root" for an agent or tests that need it;
	// left out, they run as the lane's user.
	RunAs       string `toml:"run_as"`
	OracleRunAs string `toml:"oracle_run_as"`
	// Writable are paths the tests write besides /app and /tests.
	Writable []string `toml:"writable"`
	// Collect are directories of the agent's container, besides /app,
	// that the tests read.
	Collect []string `toml:"collect"`
	// Why says why, for NOTES.md.
	Why string `toml:"why"`
}

// Config is the import config: the subset, and what each task declares.
type Config struct {
	Task map[string]Declared `toml:"task"`
}

// ConfigFile is where the import config is, in the repository.
var ConfigFile = filepath.Join("tasks", Suite, "import.toml")

// ReadConfig reads the import config of the repository.
func ReadConfig(repo string) (*Config, error) {
	var c Config
	md, err := toml.DecodeFile(filepath.Join(repo, ConfigFile), &c)
	if err != nil {
		return nil, fmt.Errorf("tbench2: %s: %w", ConfigFile, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("tbench2: %s: unknown keys %v", ConfigFile, u)
	}
	for name, d := range c.Task {
		if !nameRE.MatchString(name) {
			return nil, fmt.Errorf("tbench2: %s: %q is not a task name", ConfigFile, name)
		}
		for _, r := range []string{d.RunAs, d.OracleRunAs} {
			if r != "" && r != task.Root {
				return nil, fmt.Errorf("tbench2: %s: task %s: run_as must be %q or left out", ConfigFile, name, task.Root)
			}
		}
	}
	return &c, nil
}

// Names are the tasks of the config, in order.
func (c *Config) Names() []string {
	var out []string
	for n := range c.Task {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type sourceFile struct {
	Task struct {
		Name string `toml:"name"`
	} `toml:"task"`
	Metadata struct {
		Difficulty string   `toml:"difficulty"`
		Category   string   `toml:"category"`
		Tags       []string `toml:"tags"`
		Expert     float64  `toml:"expert_time_estimate_min"`
		Junior     float64  `toml:"junior_time_estimate_min"`
	} `toml:"metadata"`
	Verifier struct {
		TimeoutS float64 `toml:"timeout_sec"`
	} `toml:"verifier"`
	Agent struct {
		TimeoutS float64 `toml:"timeout_sec"`
	} `toml:"agent"`
	Environment struct {
		Image         string `toml:"docker_image"`
		CPUs          int    `toml:"cpus"`
		MemoryMB      int    `toml:"memory_mb"`
		GPUs          int    `toml:"gpus"`
		AllowInternet bool   `toml:"allow_internet"`
	} `toml:"environment"`
}

var (
	canaryRE  = regexp.MustCompile(`terminal-bench-canary GUID ([0-9a-f-]{36})`)
	withRE    = regexp.MustCompile(`(?m)^\s*-w\s+(\S+)\s*\\?$`)
	runRE     = regexp.MustCompile(`(?m)^\s*pytest --ctrf /logs/verifier/ctrf\.json (.+)$`)
	flagRE    = regexp.MustCompile(`^-[A-Za-z]+$`)
	pytestRE  = regexp.MustCompile(`^pytest==([0-9][0-9.]*)$`)
	packageRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*==[0-9][0-9A-Za-z.]*$`)
	nameRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
)

// reportPlugin writes the source's own report file; the oracle has the
// exit code instead, so it is not vendored.
const reportPlugin = "pytest-json-ctrf"

// Read reads one task of the source. It refuses a task this importer
// cannot carry over faithfully: one whose verifier is not the source's
// standard pytest run, or that needs a GPU.
func Read(root, name string) (*Source, error) {
	if !nameRE.MatchString(name) {
		return nil, fmt.Errorf("tbench2: %q is not a task name", name)
	}
	dir := filepath.Join(root, name)
	var f sourceFile
	if _, err := toml.DecodeFile(filepath.Join(dir, "task.toml"), &f); err != nil {
		return nil, fmt.Errorf("tbench2: %s: %w", name, err)
	}
	read := func(rel string) (string, error) {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", fmt.Errorf("tbench2: %s: %w", name, err)
		}
		return string(b), nil
	}
	s := &Source{
		Name: name, Dir: dir,
		Difficulty: f.Metadata.Difficulty, Category: f.Metadata.Category, Tags: f.Metadata.Tags,
		ExpertMin: f.Metadata.Expert, JuniorMin: f.Metadata.Junior,
		AgentTimeoutS: f.Agent.TimeoutS, VerifierTimeoutS: f.Verifier.TimeoutS,
		Image: f.Environment.Image, CPUs: f.Environment.CPUs, MemoryMB: f.Environment.MemoryMB,
		AllowInternet: f.Environment.AllowInternet,
	}
	var err error
	if s.Instruction, err = read("instruction.md"); err != nil {
		return nil, err
	}
	switch {
	case f.Task.Name != "terminal-bench/"+name:
		return nil, fmt.Errorf("tbench2: %s: task.toml names %q", name, f.Task.Name)
	case f.Environment.GPUs != 0:
		return nil, fmt.Errorf("tbench2: %s needs %d GPUs; a lane has none for the agent", name, f.Environment.GPUs)
	case s.Image == "" || strings.Contains(s.Image, "@"):
		return nil, fmt.Errorf("tbench2: %s: docker_image %q is not a tag", name, s.Image)
	case s.AgentTimeoutS <= 0 || s.VerifierTimeoutS <= 0:
		return nil, fmt.Errorf("tbench2: %s: the agent and verifier timeouts must be stated", name)
	case strings.TrimSpace(s.Instruction) == "":
		return nil, fmt.Errorf("tbench2: %s: instruction.md is empty", name)
	}
	df, err := read("environment/Dockerfile")
	if err != nil {
		return nil, err
	}
	if m := canaryRE.FindStringSubmatch(df); m != nil {
		s.Canary = m[1]
	}
	if _, err := read("tests/test_outputs.py"); err != nil {
		return nil, err
	}
	if _, err := read("solution/solve.sh"); err != nil {
		return nil, err
	}
	sh, err := read("tests/test.sh")
	if err != nil {
		return nil, err
	}
	// The source's standard verifier: uvx runs one pytest over
	// /tests/test_outputs.py, with packages added by -w.
	runs := runRE.FindAllStringSubmatch(sh, -1)
	if !strings.Contains(sh, "\nuvx \\\n") || len(runs) != 1 {
		return nil, fmt.Errorf("tbench2: %s: tests/test.sh is not the standard pytest run; it cannot be carried over as it is", name)
	}
	tests := false
	for _, a := range strings.Fields(runs[0][1]) {
		switch {
		case a == "/tests/test_outputs.py":
			tests = true
		case flagRE.MatchString(a):
		default:
			return nil, fmt.Errorf("tbench2: %s: tests/test.sh runs pytest with %q, which the import does not carry over", name, a)
		}
		s.PytestArgs = append(s.PytestArgs, a)
	}
	if !tests {
		return nil, fmt.Errorf("tbench2: %s: tests/test.sh does not run /tests/test_outputs.py", name)
	}
	for _, m := range withRE.FindAllStringSubmatch(sh, -1) {
		w := m[1]
		switch {
		case pytestRE.MatchString(w):
			s.Pytest = pytestRE.FindStringSubmatch(w)[1]
		case strings.HasPrefix(w, reportPlugin+"=="):
		case packageRE.MatchString(w):
			s.Packages = append(s.Packages, w)
		default:
			return nil, fmt.Errorf("tbench2: %s: tests/test.sh adds %q, which is not name==version", name, w)
		}
	}
	if s.Pytest == "" {
		return nil, fmt.Errorf("tbench2: %s: tests/test.sh pins no pytest version", name)
	}
	sort.Strings(s.Packages)
	return s, nil
}

// Wheel is one vendored package of the verifier.
type Wheel struct {
	File   string // the wheel's file name, under oracle/wheels/
	SHA256 string
	// Built names the source distribution the wheel was built from, for a
	// package with no wheel of its own (oracle/built.txt pins it).
	Built string
}

// Pins is what the import established outside the source's files.
type Pins struct {
	Commit string // the source's commit
	// Image is the environment image by digest: repo@sha256:...
	Image    string
	Wheels   []Wheel
	Verifier string // the sha256 of the built verifier
	// Gold maps the workspace paths solve.sh wrote to the files of the
	// gold fixture.
	Gold map[string]string
	// SeedFiles are the files of the image's /app too large for seed/.
	SeedFiles []SeedFile
	// PipImage is the image pip chose the packages in: Image, or the
	// official image of its Python when its own has no pip.
	PipImage string
}

// SeedFile is a file of the seed taken from the image when the task is
// built, not kept in the repository: an artefact that copies it out of the
// image, pinned by its hash and installed as part of the seed (066 §3).
type SeedFile struct {
	Path   string // in /app
	SHA256 string
	Mode   string
}

// verifierArtefact is the name of the verifier's artefact.
const verifierArtefact = "verifier"

// minutes is a source estimate as hours, minutes and seconds.
func minutes(m float64) string { return (time.Duration(m * float64(time.Minute))).String() }

// verifierFile is the verifier artefact's output.
const verifierFile = "verify.pyz"

// TaskTOML is the imported task's task.toml. inputs are the files of the
// task's oracle/ directory, relative to the task.
func TaskTOML(s *Source, p *Pins, inputs []string) (string, error) {
	if !strings.Contains(p.Image, "@sha256:") {
		return "", errors.New("tbench2: the image must be pinned by digest")
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("# %s: Terminal-Bench 2.0's task terminal-bench/%s, imported by\n", s.Name, s.Name)
	w("# `automaticum suite import tbench2` from commit %s (066).\n", p.Commit)
	w("# A held-out eval: never trained on and never exported. Written by the\n")
	w("# importer; import again instead of editing it.\n\n")
	w("id = %s\n", quote(s.Name))
	w("suite = %s\n", quote(Suite))
	w("version = 1\n")
	w("layers = [%s]\n", quote(Layer(s.Name)))
	w("default_answer = %s\n\n", quote("Use your own judgement and continue."))
	w("prompt = %s\n\n", promptString(s.Instruction))
	d := s.Declared
	if d.RunAs != "" {
		w("# Declared in %s: %s\n", filepath.ToSlash(ConfigFile), oneLine(d.Why))
		w("run_as = %s\n\n", quote(d.RunAs))
	}

	w("[labels]\n")
	labels := map[string]string{
		"source":           "terminal-bench-2",
		"source_commit":    p.Commit,
		"source_task":      "terminal-bench/" + s.Name,
		"difficulty":       s.Difficulty,
		"category":         s.Category,
		"tags":             strings.Join(s.Tags, ","),
		"expert_time":      minutes(s.ExpertMin),
		"junior_time":      minutes(s.JuniorMin),
		"source_image":     s.Image,
		"source_cpus":      fmt.Sprint(s.CPUs),
		"source_memory_mb": fmt.Sprint(s.MemoryMB),
		"source_internet":  fmt.Sprint(s.AllowInternet),
		"canary":           s.Canary,
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w("%s = %s\n", k, quote(labels[k]))
	}

	w("\n# The source's limits, for the agent's container and the oracle's.\n[limits]\n")
	w("wall_s = %d\n", int(s.AgentTimeoutS))
	w("oracle_s = %d\n", int(s.VerifierTimeoutS))
	if s.CPUs > 0 {
		w("cpus = %d.0\n", s.CPUs)
	}
	if s.MemoryMB > 0 {
		w("memory_mb = %d\n", s.MemoryMB)
	}

	w("\n[workspace]\nseed = \"seed\"\n")
	w("# The source's /app is no repository.\ngit = false\n")
	if len(d.Collect) > 0 {
		w("collect = [%s]\n", quoteAll(d.Collect))
	}
	for i, f := range p.SeedFiles {
		w("\n# /app/%s is too large for the repository: taken from the image.\n", f.Path)
		w("[[workspace.install]]\nartefact = \"seed-%d\"\npath = %s\nmode = %s\nseed = true\n", i+1, quote(f.Path), quote(f.Mode))
	}

	for i, f := range p.SeedFiles {
		w("\n[[artefact]]\nname = \"seed-%d\"\n", i+1)
		w("image = %s\n", quote(p.Image))
		w("command = [\"cp\", %s, %s]\n", quote("/app/"+f.Path), quote("/out/"+path.Base(f.Path)))
		w("output = %s\n", quote(path.Base(f.Path)))
		w("sha256 = %s\n", quote(f.SHA256))
	}

	w("\n[[artefact]]\nname = %s\n", quote(verifierArtefact))
	w("image = %s\n", quote(p.Image))
	w("inputs = [%s]\n", quoteAll(inputs))
	w("command = [\"python3\", \"/task/oracle/build.py\", \"/task/oracle\", \"/out/%s\"]\n", verifierFile)
	w("output = %s\n", quote(verifierFile))
	w("sha256 = %s\n", quote(p.Verifier))

	w("\n# The verifier is the oracle's alone: it is mounted at /oracle, never\n")
	w("# installed in the workspace. /app and /tests are where the tests look.\n")
	w("[[oracle]]\nname = \"tests\"\nprimary = true\nartefact = \"verifier\"\n")
	w("image = %s\n", quote(p.Image))
	w("# The source's own pytest arguments, after the verifier.\n")
	w("command = [%s]\n", quoteAll(append([]string{"python3", "/oracle/" + verifierFile}, s.PytestArgs...)))
	w("writable = [%s]\n", quoteAll(append([]string{"/app", "/tests"}, d.Writable...)))
	if d.OracleRunAs != "" {
		w("run_as = %s\n", quote(d.OracleRunAs))
	}
	w("expect_exit = 0\n")
	w("timeout_s = %d\n", int(s.VerifierTimeoutS))

	if len(p.Gold) > 0 {
		w("\n# What solution/solve.sh wrote, run once at import.\n")
		w("[[fixture]]\nname = \"gold\"\nexpect = \"pass\"\n[fixture.files]\n")
		paths := make([]string, 0, len(p.Gold))
		for k := range p.Gold {
			paths = append(paths, k)
		}
		sort.Strings(paths)
		for _, k := range paths {
			w("%s = %s\n", quote(k), quote(p.Gold[k]))
		}
	}
	w("\n[[fixture]]\nname = \"seed\"\nexpect = \"fail\"\n")
	return b.String(), nil
}

// LaneDockerfile is the imported task's lane layer: the task's image by
// digest with the runner added under its own prefix, off the agent's PATH
// (064 §3.2, D113), and /app made the workspace. Nothing else of the image
// changes: the agent finds the task's environment as the source made it.
func LaneDockerfile(s *Source, p *Pins) string {
	return fmt.Sprintf(`# Lane layer of tbench2/%[1]s, written by `+"`automaticum suite import tbench2`"+`
# from Terminal-Bench 2.0 commit %[2]s (066 §3): the task's own image
# (%[3]s, pinned by digest) with the runner added (064 §3.2) under its own
# prefix, /opt/automaticum, which is on no PATH (D113): Node and the runner,
# and a static ripgrep that only the SDK's own search finds. /app, where the
# task works, is made the workspace; nothing else of the image changes.
# Built by `+"`automaticum image build %[4]s`"+`; rebuild the base first.
FROM automaticum-lane:base AS lane
FROM %[5]s

COPY --from=lane /usr/local/bin/node /opt/automaticum/bin/node
COPY --from=lane /opt/automaticum/tools/rg /opt/automaticum/bin/rg
COPY --from=lane /opt/automaticum/runner /opt/automaticum/runner
RUN ["/opt/automaticum/bin/node", "--version"]
RUN rm -rf /app && ln -s /workspace /app
WORKDIR /workspace
`, s.Name, p.Commit, s.Image, Layer(s.Name), p.Image)
}

// Notes is the imported task's NOTES.md: where it comes from and what the
// import changed of the source's own way of running it.
func Notes(s *Source, p *Pins) string {
	var wheels []string
	for _, w := range p.Wheels {
		f := "`" + w.File + "`"
		if w.Built != "" {
			f += " (built from `" + w.Built + "`, which `oracle/built.txt` pins)"
		}
		wheels = append(wheels, f)
	}
	pk := "none"
	if len(s.Packages) > 0 {
		pk = "`" + strings.Join(s.Packages, "`, `") + "`"
	}
	d := s.Declared
	agent, tests := "the lane's user, uid 1000", "the lane's user"
	if d.RunAs == task.Root {
		agent = "root, with the engine's default capabilities"
	}
	if d.OracleRunAs == task.Root {
		tests = "root, with the engine's default capabilities"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `# tbench2/%[1]s

Terminal-Bench 2.0's task `+"`terminal-bench/%[1]s`"+` (Apache-2.0, see
`+"`../LICENSE`"+`), imported from commit `+"`%[2]s`"+` by
`+"`automaticum suite import tbench2`"+` (066). Expert estimate %[3]s, junior
%[4]s; agent timeout %[5]s.

- Image: `+"`%[6]s`"+`, pinned as `+"`%[7]s`"+`.
- Seed: the image's `+"`/app`"+`, copied out.
- Oracle: the task's `+"`tests/`"+` with pytest %[8]s and the tests' packages
  (%[9]s), run in the same image with no network; the source's
  `+"`tests/test.sh`"+` fetches uv, Python and these at verify time. Its report
  plugin (`+"`%[10]s`"+`) is left out: the exit code is the verdict. Vendored:
  %[11]s.
- Gold fixture: what `+"`solution/solve.sh`"+` wrote, run once in the image as the
  agent runs.
- Limits: the source's, %[12]d CPU and %[13]d MB, for the agent's container
  and the oracle's (never more than the lane has).
- The agent runs as %[14]s; the tests as %[15]s.
`, s.Name, p.Commit, minutes(s.ExpertMin), minutes(s.JuniorMin), (time.Duration(s.AgentTimeoutS) * time.Second).String(),
		s.Image, p.Image, s.Pytest, pk, reportPlugin, strings.Join(wheels, ", "), s.CPUs, s.MemoryMB, agent, tests)
	if p.PipImage != "" && p.PipImage != p.Image {
		fmt.Fprintf(&b, "- The image's Python has no pip: its packages were chosen by pip in `%s`, the same Python version.\n", p.PipImage)
	}
	for _, f := range p.SeedFiles {
		fmt.Fprintf(&b, "- `/app/%s` (%s) is too large for the repository: the task takes it from the image when it is built.\n", f.Path, f.SHA256[:12])
	}
	if len(d.Writable) > 0 {
		fmt.Fprintf(&b, "- The tests also write `%s`.\n", strings.Join(d.Writable, "`, `"))
	}
	if len(d.Collect) > 0 {
		fmt.Fprintf(&b, "- Besides `/app`, the tests get `%s` of the agent's container.\n", strings.Join(d.Collect, "`, `"))
	}
	if d.Why != "" {
		fmt.Fprintf(&b, "\nDeclared in `%s` (066 §3.1): %s\n", filepath.ToSlash(ConfigFile), oneLine(d.Why))
	}
	b.WriteString(`
Differences from the source's own harness (066 §4): only ` + "`/app`" + ` and what is
declared reach the verifier, not the rest of the container; the verifier is
offline, with its packages pinned.
`)
	return b.String()
}

// oneLine is s with its runs of white space made one space.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// quote is a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func quoteAll(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = quote(s)
	}
	return strings.Join(q, ", ")
}

// promptString is the instruction as a TOML string, readable where it can
// be: a multi-line literal string holds it byte for byte unless it has
// three quotes in a row, ends in a quote, or has a control character.
func promptString(s string) string {
	if strings.Contains(s, "'''") || strings.HasSuffix(s, "'") {
		return quote(s)
	}
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return quote(s)
		}
	}
	return "'''\n" + s + "'''"
}
