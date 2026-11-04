package task

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// unpinnedSHA stands for an artefact's sha256 not written yet, while
// `task pin` builds it.
var unpinnedSHA = strings.Repeat("0", 64)

// LoadUnpinned reads the task in dir as Load does, but lets through an
// artefact whose sha256 is empty, so that it can be built and pinned
// (`automaticum task pin`).
func LoadUnpinned(dir string) (*Task, error) { return load(dir, true) }

// Locate finds a task's directory, given as suite/task under one of the
// roots or as a directory holding a task.toml, without loading the rest of
// the roots: one task still being written does not hide another.
func Locate(r Roots, name string) (string, error) {
	if st, err := os.Stat(filepath.Join(name, "task.toml")); err == nil && !st.IsDir() {
		return filepath.Abs(name)
	}
	suite, id, ok := strings.Cut(name, "/")
	if !ok || suite == "" || id == "" || strings.Contains(id, "/") {
		return "", fmt.Errorf("task: say suite/task or a task's directory, not %q", name)
	}
	var found []string
	for _, root := range []string{r.Public, r.Private} {
		if root == "" {
			continue
		}
		dir := filepath.Join(root, suite, id)
		if _, err := os.Stat(filepath.Join(dir, "task.toml")); err == nil {
			found = append(found, dir)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("task: no task %q under %s", name, r)
	case 1:
		return filepath.Abs(found[0])
	}
	return "", fmt.Errorf("task: %s is in both the repository and the private root", name)
}

var (
	tableRE   = regexp.MustCompile(`^\s*\[`)
	artRE     = regexp.MustCompile(`^\s*\[\[\s*artefact\s*\]\]\s*(#.*)?$`)
	nameKeyRE = regexp.MustCompile(`^(\s*)name\s*=\s*("[^"]*"|'[^']*')`)
	shaKeyRE  = regexp.MustCompile(`^(\s*)sha256\s*=\s*("[^"]*"|'[^']*')(.*)$`)
)

// WritePins writes each artefact's sha256 into the task.toml in dir, by
// artefact name, keeping every other line as it was: the value of an
// existing sha256 key is replaced, and one is added after the artefact's
// name when it has none.
func WritePins(dir string, sums map[string]string) error {
	path := filepath.Join(dir, "task.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	var out []string
	done := map[string]bool{}
	// The artefact table being read: where its name line went in out, its
	// name, and whether its sha256 was written.
	in, nameAt, name, wrote := false, -1, "", false
	finish := func() {
		if !in || wrote || name == "" || nameAt < 0 {
			return
		}
		if sum, ok := sums[name]; ok {
			indent := nameKeyRE.FindStringSubmatch(strings.TrimRight(out[nameAt], "\n"))[1]
			line := indent + "sha256 = " + strconv.Quote(sum) + "\n"
			out = append(out[:nameAt+1], append([]string{line}, out[nameAt+1:]...)...)
			done[name] = true
		}
	}
	for _, l := range lines {
		bare := strings.TrimRight(l, "\r\n")
		if tableRE.MatchString(bare) {
			finish()
			in, nameAt, name, wrote = artRE.MatchString(bare), -1, "", false
			out = append(out, l)
			continue
		}
		if in {
			if m := nameKeyRE.FindStringSubmatch(bare); m != nil && name == "" {
				name = unquote(m[2])
				nameAt = len(out)
			} else if m := shaKeyRE.FindStringSubmatch(bare); m != nil {
				if sum, ok := sums[name]; ok && name != "" {
					l = m[1] + "sha256 = " + strconv.Quote(sum) + m[3] + l[len(bare):]
					done[name], wrote = true, true
				} else if name == "" {
					return fmt.Errorf("task: %s: an artefact's sha256 stands before its name; write name first", path)
				}
			}
		}
		out = append(out, l)
	}
	finish()
	var missing []string
	for n := range sums {
		if !done[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("task: %s: no artefact named %s", path, strings.Join(missing, ", "))
	}
	return os.WriteFile(path, []byte(strings.Join(out, "")), 0o644)
}

func unquote(s string) string {
	if strings.HasPrefix(s, "'") {
		return strings.Trim(s, "'")
	}
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}
