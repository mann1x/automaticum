package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/task"
)

// errFailed ends a command whose output already said what is wrong.
var errFailed = errors.New("failed")

// place is where a command finds the checkout and the configuration.
type place struct {
	repo, cfg *string
	json      *bool
}

func addPlace(fs *flag.FlagSet) place {
	return place{
		repo: fs.String("repo", os.Getenv("AUTOMATICUM_REPO"), "checkout with arms/ and tasks/"),
		cfg:  fs.String("config", os.Getenv("AUTOMATICUM_CONFIG"), "configuration directory"),
		json: fs.Bool("json", false, "print JSON"),
	}
}

// local loads the configuration directory; it is an error to have none.
func (p place) local() (*config.Local, error) {
	if *p.cfg == "" {
		return nil, errors.New("no configuration directory: give --config or set AUTOMATICUM_CONFIG")
	}
	return config.LoadLocal(*p.cfg)
}

// root is the checkout: the flag, else the configuration's, else here.
func (p place) root() string {
	if *p.repo != "" {
		return *p.repo
	}
	if *p.cfg != "" {
		if l, err := config.LoadLocal(*p.cfg); err == nil && l.Daemon.Repo != "" {
			return l.Daemon.Repo
		}
	}
	return "."
}

func (p place) arms() string  { return filepath.Join(p.root(), "arms") }
func (p place) tasks() string { return filepath.Join(p.root(), "tasks") }

// parse parses flags wherever they stand among the arguments and returns the
// arguments that are not flags.
func parse(fs *flag.FlagSet, args []string) []string {
	var rest []string
	for {
		fs.Parse(args)
		args = fs.Args()
		if len(args) == 0 {
			return rest
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

func table() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0) }

func armValidate(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("arm validate", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	names := args
	if len(names) == 0 {
		paths, err := filepath.Glob(filepath.Join(p.arms(), "*.toml"))
		if err != nil {
			return err
		}
		for _, f := range paths {
			names = append(names, strings.TrimSuffix(filepath.Base(f), ".toml"))
		}
		if len(names) == 0 {
			return fmt.Errorf("no arms in %s", p.arms())
		}
	}
	// An endpoint can only be checked where a configuration exists.
	var local *config.Local
	if *p.cfg != "" {
		var err error
		if local, err = p.local(); err != nil {
			return err
		}
	}
	bad := 0
	for _, name := range names {
		a, err := config.LoadArm(p.arms(), name)
		if err == nil && !a.Template && local != nil && local.Endpoints[a.Endpoint] == nil {
			err = fmt.Errorf("arm %s names endpoint %q, which this installation does not have", name, a.Endpoint)
		}
		if err != nil {
			bad++
			fmt.Println(err)
			continue
		}
		if a.Template {
			fmt.Printf("%s: template, checked through the arms that extend it\n", name)
			continue
		}
		fmt.Printf("%s: valid, %s\n", name, a.ID[:12])
	}
	if bad > 0 {
		return errFailed
	}
	return nil
}

func armShow(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("arm show", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	if len(args) != 1 {
		return errors.New("usage: automaticum arm show <arm>")
	}
	a, err := config.LoadArm(p.arms(), args[0])
	if err != nil {
		return err
	}
	if *p.json {
		fmt.Printf("%s\n", a.Resolved())
		return nil
	}
	if a.Template {
		fmt.Printf("arm  %s\nid   none: a template is never run\n\n", a.Name)
	} else {
		fmt.Printf("arm  %s\nid   %s\n\n", a.Name, a.ID)
	}
	values := config.Values(a)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := table()
	for _, k := range keys {
		fmt.Fprintf(w, "%s\t%s\t%s\n", k, values[k], a.Origins[k])
	}
	return w.Flush()
}

func armDiff(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("arm diff", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	if len(args) != 2 {
		return errors.New("usage: automaticum arm diff <arm> <arm>")
	}
	a, err := config.LoadArm(p.arms(), args[0])
	if err != nil {
		return err
	}
	b, err := config.LoadArm(p.arms(), args[1])
	if err != nil {
		return err
	}
	lines := config.Diff(a, b)
	if len(lines) == 0 && !a.Template && !b.Template {
		fmt.Printf("%s and %s resolve to the same arm, %s\n", a.Name, b.Name, a.ID[:12])
		return nil
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return nil
}

func taskLs(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("task ls", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	all, err := task.LoadAll(p.tasks())
	if err != nil {
		return err
	}
	w := table()
	fmt.Fprintln(w, "TASK\tVERSION\tLAYERS\tORACLES\tFIXTURES\tHASH")
	for _, t := range all {
		fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%s\n", t.Name(), t.Version, strings.Join(t.Layers, ","), len(t.Oracles), len(t.Fixtures), t.Hash[:12])
	}
	return w.Flush()
}

func taskShow(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("task show", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	if len(args) != 1 {
		return errors.New("usage: automaticum task show <task>")
	}
	t, err := task.Find(p.tasks(), args[0])
	if err != nil {
		return err
	}
	fmt.Printf("task     %s\nversion  %d\nhash     %s\nlayers   %s\n", t.Name(), t.Version, t.Hash, strings.Join(t.Layers, ", "))
	fmt.Printf("limits   wall %s, idle %s, oracle %s\n", human.Seconds(float64(t.Limits.WallS)), human.Seconds(float64(t.Limits.IdleS)), human.Seconds(float64(t.Limits.OracleS)))
	for _, a := range t.Artefacts {
		fmt.Printf("artefact %s -> %s, sha256 %s\n", a.Name, a.Output, a.SHA256)
	}
	for _, o := range t.Oracles {
		kind := "pattern"
		if o.ExpectExit != nil {
			kind = "exit code"
		}
		primary := ""
		if o.Primary {
			primary = ", primary"
		}
		fmt.Printf("oracle   %s: %s, judged by %s%s\n", o.Name, strings.Join(o.Command, " "), kind, primary)
	}
	for _, f := range t.Fixtures {
		fmt.Printf("fixture  %s: expect %s\n", f.Name, f.Expect)
	}
	fmt.Printf("\nprompt:\n%s\n\nfiles:\n", strings.TrimRight(t.Prompt, "\n"))
	names := make([]string, 0, len(t.Files))
	for n := range t.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("  %s  %s\n", t.Files[n][:12], n)
	}
	return nil
}

// taskEnv is where artefacts are built and oracles run on this installation.
func taskEnv(p place) (*task.Env, error) {
	l, err := p.local()
	if err != nil {
		return nil, err
	}
	env := &task.Env{
		Exec:      docker.New(l.Daemon.DockerSocket),
		Artefacts: filepath.Join(l.Daemon.DataRoot, "artefacts"),
		Scratch:   filepath.Join(l.Daemon.DataRoot, "scratch"),
	}
	for _, dir := range []string{env.Artefacts, env.Scratch} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return env, nil
}

func taskBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("task build", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	if len(args) != 1 {
		return errors.New("usage: automaticum task build <task>")
	}
	t, err := task.Find(p.tasks(), args[0])
	if err != nil {
		return err
	}
	env, err := taskEnv(p)
	if err != nil {
		return err
	}
	if len(t.Artefacts) == 0 {
		fmt.Printf("%s has no artefacts to build\n", t.Name())
		return nil
	}
	if err := env.BuildAll(ctx, t, os.Stderr); err != nil {
		return err
	}
	for i := range t.Artefacts {
		a := &t.Artefacts[i]
		fmt.Printf("%s: %s, sha256 %s\n", a.Name, env.ArtefactPath(a), a.SHA256)
	}
	return nil
}

func oracleVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("oracle verify", flag.ExitOnError)
	p := addPlace(fs)
	repeats := fs.Int("repeats", 3, "how many times each fixture is judged")
	args = parse(fs, args)
	if len(args) != 1 {
		return errors.New("usage: automaticum oracle verify <task>")
	}
	t, err := task.Find(p.tasks(), args[0])
	if err != nil {
		return err
	}
	env, err := taskEnv(p)
	if err != nil {
		return err
	}
	if err := env.BuildAll(ctx, t, os.Stderr); err != nil {
		return err
	}
	results, err := env.Verify(ctx, t, *repeats)
	if err != nil {
		return err
	}
	bad := 0
	w := table()
	fmt.Fprintln(w, "FIXTURE\tEXPECT\tRUNS\tRESULT")
	for _, r := range results {
		var got []string
		for _, run := range r.Runs {
			switch {
			case run.Err != "":
				got = append(got, "error")
			case run.Passed:
				got = append(got, "pass")
			default:
				got = append(got, "fail")
			}
		}
		verdict := "ok"
		if !r.OK {
			verdict = "WRONG"
			bad++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Fixture, r.Expect, strings.Join(got, " "), verdict)
	}
	w.Flush()
	if bad > 0 {
		fmt.Printf("%s: the oracle is not proven: %d of %d fixtures judged wrongly or unevenly\n", t.Name(), bad, len(results))
		return errFailed
	}
	if err := env.RecordProof(t, len(results), *repeats); err != nil {
		return fmt.Errorf("the oracle is proven, but the record of it could not be written: %w", err)
	}
	fmt.Printf("%s: the oracle is proven on %d fixtures, %d runs each\n", t.Name(), len(results), *repeats)
	return nil
}

func laneLs(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("lane ls", flag.ExitOnError)
	p := addPlace(fs)
	args = parse(fs, args)
	l, err := p.local()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(l.Lanes))
	for n := range l.Lanes {
		names = append(names, n)
	}
	sort.Strings(names)
	w := table()
	fmt.Fprintln(w, "LANE\tHOST\tENDPOINTS\tUSER\tCPUS\tMEMORY GB\tPIDS")
	for _, n := range names {
		ln := l.Lanes[n]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%g\t%g\t%d\n", n, ln.Host, strings.Join(ln.Endpoints, ","), ln.User, ln.CPUs, ln.MemoryGB, ln.Pids)
	}
	return w.Flush()
}

// endpointLs prints names, kinds and hosts. It never prints an address: the
// output of a command ends up in reports and issues.
func endpointLs(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("endpoint ls", flag.ExitOnError)
	p := addPlace(fs)
	address := fs.Bool("address", false, "also print each endpoint's address")
	args = parse(fs, args)
	l, err := p.local()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(l.Endpoints))
	for n := range l.Endpoints {
		names = append(names, n)
	}
	sort.Strings(names)
	w := table()
	head := "ENDPOINT\tKIND\tHOST\tCAPACITY\tKEY"
	if *address {
		head += "\tADDRESS"
	}
	fmt.Fprintln(w, head)
	for _, n := range names {
		e := l.Endpoints[n]
		key := "no"
		if e.APIKeyFile != "" {
			key = "yes"
		}
		line := fmt.Sprintf("%s\t%s\t%s\t%d\t%s", n, e.Kind, e.Host, e.Capacity, key)
		if *address {
			line += "\t" + e.BaseURL
		}
		fmt.Fprintln(w, line)
	}
	return w.Flush()
}
