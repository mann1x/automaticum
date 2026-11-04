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

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/task"
)

// taskPin builds each artefact of a task whatever it pins and prints the
// sha256 the build gave; --write puts them in the task's task.toml. The
// task may still have an empty sha256: that is what it is for, so a client
// can pin a task it wrote itself.
func taskPin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("task pin", flag.ExitOnError)
	p := addPlace(fs)
	write := fs.Bool("write", false, "write the sha256s into the task's task.toml, then load it to check")
	args = parse(fs, args)
	if len(args) != 1 {
		return errors.New("usage: automaticum task pin [--write] <suite/task | task directory>")
	}
	dir, err := task.Locate(p.tasks(), args[0])
	if err != nil {
		return err
	}
	t, err := task.LoadUnpinned(dir)
	if err != nil {
		return err
	}
	if len(t.Artefacts) == 0 {
		fmt.Printf("%s has no artefacts to pin\n", t.Name())
		return nil
	}
	env, err := taskEnv(p)
	if err != nil {
		return err
	}
	type pin struct {
		Name    string `json:"name"`
		SHA256  string `json:"sha256"`
		Was     string `json:"was"`
		Changed bool   `json:"changed"`
	}
	var pins []pin
	sums := map[string]string{}
	for i := range t.Artefacts {
		a := &t.Artefacts[i]
		sum, err := env.Pin(ctx, t, a, os.Stderr)
		if err != nil {
			return err
		}
		sums[a.Name] = sum
		pins = append(pins, pin{a.Name, sum, a.SHA256, sum != a.SHA256})
	}
	if *write {
		if err := task.WritePins(dir, sums); err != nil {
			return err
		}
		if t, err = task.Load(dir); err != nil {
			return fmt.Errorf("written, but the task does not load: %w", err)
		}
	}
	if *p.json {
		return printJSON(map[string]any{"task": t.Name(), "dir": dir, "written": *write, "artefacts": pins})
	}
	for _, x := range pins {
		state := "as pinned"
		switch {
		case x.Was == strings.Repeat("0", 64):
			state = "not pinned before"
		case x.Changed:
			state = "pinned " + x.Was
		}
		fmt.Printf("%s: sha256 %s (%s)\n", x.Name, x.SHA256, state)
	}
	if *write {
		fmt.Printf("written to %s; the task loads\n", filepath.Join(dir, "task.toml"))
	}
	return nil
}

// configCheck reads an installation's configuration as the daemon does at
// its start, and every arm, model and task it would use, and says what is
// wrong, without the daemon: a configuration edited is checked before a
// restart. It changes nothing.
func configCheck(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("config check", flag.ExitOnError)
	p := addPlace(fs)
	parse(fs, args)
	type check struct {
		What    string `json:"what"`
		OK      bool   `json:"ok"`
		Problem string `json:"problem,omitempty"`
		Note    string `json:"note,omitempty"`
	}
	var checks []check
	add := func(what string, err error, note string) {
		c := check{What: what, OK: err == nil, Note: note}
		if err != nil {
			c.Problem = err.Error()
			if strings.Contains(c.Problem, note) {
				c.Note = ""
			}
		}
		checks = append(checks, c)
	}
	local, err := p.local()
	add("automaticum.toml, lanes and endpoints", err, "")
	if local != nil {
		d := &local.Daemon
		_, err := local.TokenHash()
		add("the daemon's token", err, "")
		add("repo", dirWith(d.Repo, "tasks", "images"), d.Repo)
		add("data_root", dirOrParent(d.DataRoot), d.DataRoot)
		if d.ArchiveRoot != "" {
			add("archive_root", dirOrParent(d.ArchiveRoot), d.ArchiveRoot)
		}
		if d.PrivateTasks != "" {
			add("private_tasks", dirWith(d.PrivateTasks), d.PrivateTasks)
		}
		n, templates, err := checkArms(local)
		add("arms", err, fmt.Sprintf("%d arms, %d templates", n, templates))
		models, err := config.LoadModels(local.ModelsDir())
		add("models", err, fmt.Sprintf("%d models", len(models)))
		tasks, err := task.LoadAll(p.tasks())
		add("tasks", err, fmt.Sprintf("%d tasks", len(tasks)))
	}
	bad := 0
	for _, c := range checks {
		if !c.OK {
			bad++
		}
	}
	if *p.json {
		if err := printJSON(map[string]any{"ok": bad == 0, "checks": checks}); err != nil {
			return err
		}
	} else {
		w := table()
		fmt.Fprintln(w, "CHECK\tRESULT\t")
		for _, c := range checks {
			res := "ok"
			if !c.OK {
				res = "PROBLEM: " + c.Problem
			}
			if c.Note != "" {
				res += " (" + c.Note + ")"
			}
			fmt.Fprintf(w, "%s\t%s\t\n", c.What, res)
		}
		w.Flush()
	}
	if bad > 0 {
		if !*p.json {
			fmt.Printf("%d of %d checks found a problem: the daemon would not start, or would not run what they name\n", bad, len(checks))
		}
		return errFailed
	}
	return nil
}

// checkArms loads every arm, each on its own so that every problem is
// said, and checks that each arm but a template names an endpoint the
// installation has. It returns how many arms and templates it read.
func checkArms(l *config.Local) (arms, templates int, err error) {
	paths, err := filepath.Glob(filepath.Join(l.ArmsDir(), "*.toml"))
	if err != nil {
		return 0, 0, err
	}
	var problems []string
	for _, f := range paths {
		name := strings.TrimSuffix(filepath.Base(f), ".toml")
		a, err := config.LoadArm(l.ArmsDir(), name)
		switch {
		case err != nil:
			problems = append(problems, err.Error())
		case a.Template:
			templates++
		default:
			arms++
			if l.Endpoints[a.Endpoint] == nil {
				problems = append(problems, fmt.Sprintf("arm %s names endpoint %q, which this installation does not have", name, a.Endpoint))
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return arms, templates, errors.New(strings.Join(problems, "; "))
	}
	return arms, templates, nil
}

// dirWith says whether dir is a directory holding each of the subdirectories.
func dirWith(dir string, subs ...string) error {
	if dir == "" {
		return errors.New("not set")
	}
	for _, d := range append([]string{""}, subs...) {
		p := filepath.Join(dir, d)
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			return fmt.Errorf("%s is not a directory", p)
		}
	}
	return nil
}

// dirOrParent says whether dir is a directory, or one the daemon can make
// in a directory that exists.
func dirOrParent(dir string) error {
	if dir == "" {
		return errors.New("not set")
	}
	if st, err := os.Stat(dir); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	if st, err := os.Stat(filepath.Dir(dir)); err != nil || !st.IsDir() {
		return fmt.Errorf("neither %s nor its parent exists", dir)
	}
	return nil
}
