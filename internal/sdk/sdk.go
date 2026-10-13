// Package sdk provisions the SDK under test. A lane image holds no SDK: each
// build of the SDK is installed once, into a bundle of its own, and the
// bundle a run's arm names is mounted into the run's container. Several
// builds live side by side, and any of them can be made again, byte for
// byte, from what is kept (040 §2).
//
// A build is defined by a directory in the repository: the package manifest
// and lockfile it is installed from, and sdk.toml, which says what it is and
// pins the hash of the bundle the install must give. What the install
// downloads is archived, so a rebuild does not need the registry.
package sdk

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/task"
)

// NodeImage is the image a bundle is installed with: the same Node as the
// lane's base image, by digest.
const NodeImage = "node@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392"

// Mount is where a bundle sits in a run's container: the place the runner's
// imports are looked up.
const Mount = "/opt/automaticum/runner/node_modules"

// Package is the SDK's package name.
const Package = "@cerebriline/sdk"

// Def is one build's definition: sdk.toml in its directory.
type Def struct {
	// Name is the directory's name and what an arm's harness.sdk_release
	// says: a release number, or a release number, "+" and a commit for a
	// build that was never released.
	Name string `toml:"-"`
	Dir  string `toml:"-"`
	// Version is what the installed package's own manifest must say.
	Version string `toml:"version"`
	// Source is "registry" for a published release, or "local" for packages
	// packed from the SDK's repository; Commit names that repository's
	// commit for a local build.
	Source string `toml:"source"`
	Commit string `toml:"commit"`
	// BundleSHA256 pins the bundle: the hash every install of this
	// definition must give. Empty until the first install wrote it.
	BundleSHA256 string `toml:"bundle_sha256"`
}

// Record is what was written when a bundle was installed.
type Record struct {
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	Source         string    `json:"source"`
	Commit         string    `json:"commit,omitempty"`
	BundleSHA256   string    `json:"bundle_sha256"`
	LockSHA256     string    `json:"lock_sha256"`
	PackagesSHA256 string    `json:"packages_sha256"` // the archive of what was downloaded
	PackagesBytes  int64     `json:"packages_bytes"`
	NodeImage      string    `json:"node_image"`
	Files          int       `json:"files"`
	BuiltAt        time.Time `json:"built_at"`
}

// Runner runs a container to its end. *docker.Client is one.
type Runner interface {
	Run(ctx context.Context, s docker.Spec, timeout time.Duration, stdout, stderr io.Writer, onStart func(id string)) (docker.Result, error)
}

// Env is where bundles live on this installation.
type Env struct {
	Exec Runner
	// Defs is the directory of definitions in the checkout.
	Defs string
	// Root holds the installed bundles; it must be on a real disk.
	Root string
	// Archive holds what each install downloaded; it may be slower storage.
	Archive string
	// User is the "uid:gid" installs run as; empty means "1000:1000".
	User    string
	Timeout time.Duration
}

var (
	// ErrNotProvisioned means a build has no bundle on this installation.
	ErrNotProvisioned = errors.New("sdk: not provisioned")
	// ErrChanged means a bundle is not what its definition pins.
	ErrChanged = errors.New("sdk: bundle does not match its pinned hash")
)

// Def reads one build's definition.
func (e *Env) Def(name string) (*Def, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return nil, fmt.Errorf("sdk: %q is not a build's name", name)
	}
	d := &Def{Name: name, Dir: filepath.Join(e.Defs, name)}
	md, err := toml.DecodeFile(filepath.Join(d.Dir, "sdk.toml"), d)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("sdk: no build %s is defined (no %s/sdk.toml among the definitions)", name, name)
	}
	if err != nil {
		return nil, fmt.Errorf("sdk: %s: %w", name, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("sdk: %s/sdk.toml: unknown field %s", name, u[0])
	}
	switch {
	case d.Version == "":
		return nil, fmt.Errorf("sdk: %s/sdk.toml names no version", name)
	case d.Source != "registry" && d.Source != "local":
		return nil, fmt.Errorf("sdk: %s/sdk.toml: source must be registry or local", name)
	case d.Source == "local" && d.Commit == "":
		return nil, fmt.Errorf("sdk: %s/sdk.toml: a local build names its commit", name)
	}
	for _, f := range []string{"package.json", "package-lock.json"} {
		if _, err := os.Stat(filepath.Join(d.Dir, f)); err != nil {
			return nil, fmt.Errorf("sdk: %s has no %s", name, f)
		}
	}
	return d, nil
}

// Defs lists the names of the defined builds.
func (e *Env) List() ([]string, error) {
	entries, err := os.ReadDir(e.Defs)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, en := range entries {
		if en.IsDir() {
			if _, err := os.Stat(filepath.Join(e.Defs, en.Name(), "sdk.toml")); err == nil {
				out = append(out, en.Name())
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// Path is the directory of a build's bundle: what is mounted at Mount.
func (e *Env) Path(name string) string { return filepath.Join(e.Root, name, "node_modules") }

func (e *Env) recordPath(name string) string { return filepath.Join(e.Root, name, "bundle.json") }
func (e *Env) packagesPath(name string) string {
	return filepath.Join(e.Archive, name, "packages.tar")
}

// Record reads what was written when the bundle was installed.
func (e *Env) Record(name string) (*Record, error) {
	b, err := os.ReadFile(e.recordPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: build %s; run `automaticum sdk add %s`", ErrNotProvisioned, name, name)
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("sdk: the record of %s is not readable: %w", name, err)
	}
	return &r, nil
}

// Check says whether a build can be used for a run: it is defined, its
// bundle is installed, and the bundle is the one the definition pins. With
// deep, the bundle's files are hashed again; without, the record is trusted.
func (e *Env) Check(name string, deep bool) (*Def, *Record, error) {
	d, err := e.Def(name)
	if err != nil {
		return nil, nil, err
	}
	r, err := e.Record(name)
	if err != nil {
		return d, nil, err
	}
	if d.BundleSHA256 == "" {
		return d, r, fmt.Errorf("sdk: build %s is not pinned: its sdk.toml has no bundle_sha256", name)
	}
	if r.BundleSHA256 != d.BundleSHA256 {
		return d, r, fmt.Errorf("%w: build %s was installed as %.12s, pinned %.12s", ErrChanged, name, r.BundleSHA256, d.BundleSHA256)
	}
	if deep {
		sum, _, err := HashTree(e.Path(name))
		if err != nil {
			return d, r, fmt.Errorf("sdk: the bundle of %s cannot be read: %w", name, err)
		}
		if sum != d.BundleSHA256 {
			return d, r, fmt.Errorf("%w: the files of build %s hash to %.12s, pinned %.12s", ErrChanged, name, sum, d.BundleSHA256)
		}
	}
	return d, r, nil
}

// HashTree is the hash of a bundle: of every file's path and content, and
// every link's target. Times and owners do not enter it.
func HashTree(dir string) (sum string, files int, err error) {
	if _, err := os.Stat(dir); err != nil {
		return "", 0, err
	}
	m, err := task.ReadManifest(dir)
	if err != nil {
		return "", 0, err
	}
	return m.Hash(), len(m), nil
}

// Add installs a build's bundle from the registry (or, for a local build,
// from the packed packages in packs), archives what was downloaded, and
// writes the record. The first install of a definition pins it: the hash is
// written into sdk.toml, to be committed. Later installs must give that hash.
func (e *Env) Add(ctx context.Context, name, packs string, log io.Writer) (*Record, error) {
	d, err := e.Def(name)
	if err != nil {
		return nil, err
	}
	if d.Source == "local" && packs == "" {
		return nil, fmt.Errorf("sdk: %s is a local build: give the directory of its packed packages", name)
	}
	if d.Source == "registry" && packs != "" {
		return nil, fmt.Errorf("sdk: %s comes from the registry and takes no packed packages", name)
	}
	work, err := e.workDir(name)
	if err != nil {
		return nil, err
	}
	defer removeAll(work)
	cache := filepath.Join(work, "cache")
	if packs != "" {
		// The packed packages are part of what must be kept.
		if err := copyDir(packs, filepath.Join(cache, "packs")); err != nil {
			return nil, err
		}
	}
	if err := open(cache); err != nil {
		return nil, err
	}
	sum, files, err := e.install(ctx, d, work, false, log)
	if err != nil {
		return nil, err
	}
	if d.BundleSHA256 != "" && sum != d.BundleSHA256 {
		return nil, fmt.Errorf("%w: build %s installs as %s, pinned %s", ErrChanged, name, sum, d.BundleSHA256)
	}
	rec := &Record{Name: name, Version: d.Version, Source: d.Source, Commit: d.Commit, BundleSHA256: sum,
		NodeImage: NodeImage, Files: files, BuiltAt: time.Now().UTC()}
	if rec.LockSHA256, err = task.HashFile(filepath.Join(d.Dir, "package-lock.json")); err != nil {
		return nil, err
	}
	// Keep what was downloaded, then put the bundle in its place.
	arch := e.packagesPath(name)
	if err := os.MkdirAll(filepath.Dir(arch), 0o755); err != nil {
		return nil, err
	}
	if rec.PackagesSHA256, rec.PackagesBytes, err = tarDir(cache, arch+".tmp"); err != nil {
		return nil, fmt.Errorf("sdk: archiving the packages of %s: %w", name, err)
	}
	if err := os.Rename(arch+".tmp", arch); err != nil {
		return nil, err
	}
	removeAll(filepath.Join(e.Root, name))
	if err := os.MkdirAll(filepath.Join(e.Root, name), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(filepath.Join(work, "out", "node_modules"), e.Path(name)); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(e.recordPath(name), append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	if d.BundleSHA256 == "" {
		if err := pin(d, sum); err != nil {
			return rec, fmt.Errorf("sdk: the bundle is installed, but its hash could not be written into %s/sdk.toml: %w", name, err)
		}
	}
	return rec, nil
}

// Rebuild installs a build again from the archived packages alone, with no
// network, into a scratch directory, and returns the hash it gives. It
// changes nothing: it is the proof that the build can be reproduced.
func (e *Env) Rebuild(ctx context.Context, name string, log io.Writer) (string, error) {
	d, err := e.Def(name)
	if err != nil {
		return "", err
	}
	rec, err := e.Record(name)
	if err != nil {
		return "", err
	}
	arch := e.packagesPath(name)
	if sum, err := task.HashFile(arch); err != nil {
		return "", fmt.Errorf("sdk: the archived packages of %s are not there: %w", name, err)
	} else if sum != rec.PackagesSHA256 {
		return "", fmt.Errorf("sdk: the archived packages of %s hash to %.12s, recorded %.12s", name, sum, rec.PackagesSHA256)
	}
	work, err := e.workDir(name)
	if err != nil {
		return "", err
	}
	defer removeAll(work)
	if err := untar(arch, filepath.Join(work, "cache")); err != nil {
		return "", err
	}
	if err := open(filepath.Join(work, "cache")); err != nil {
		return "", err
	}
	sum, _, err := e.install(ctx, d, work, true, log)
	return sum, err
}

func (e *Env) workDir(name string) (string, error) {
	if err := os.MkdirAll(e.Root, 0o755); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(e.Root, ".install-"+name+"-")
	if err != nil {
		return "", err
	}
	for _, sub := range []string{"out", "cache"} {
		if err := os.Mkdir(filepath.Join(work, sub), 0o777); err != nil {
			return "", err
		}
		// The install runs as the lane user and must be able to write.
		if err := os.Chmod(filepath.Join(work, sub), 0o777); err != nil {
			return "", err
		}
	}
	return work, os.Chmod(work, 0o755)
}

// install runs the package install in a container. Install scripts of
// dependencies are never run. Offline, the container has no network and
// everything comes from the cache.
func (e *Env) install(ctx context.Context, d *Def, work string, offline bool, log io.Writer) (string, int, error) {
	user := e.User
	if user == "" {
		user = "1000:1000"
	}
	timeout := e.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	flags, network := "", ""
	if offline {
		flags, network = " --offline", "none"
	}
	script := "set -e; cp /def/package.json /def/package-lock.json /out/; cd /out; " +
		"npm ci --omit=dev --ignore-scripts --no-audit --no-fund --cache /cache/npm" + flags + "; " +
		"rm -f package.json package-lock.json; " +
		`test "$(node -p "require('/out/node_modules/` + Package + `/package.json').version")" = "$SDK_VERSION"`
	spec := docker.Spec{
		Image: NodeImage, Cmd: []string{"sh", "-c", script}, User: user, Network: network, WorkDir: "/out",
		Env: []string{"HOME=/tmp", "SDK_VERSION=" + d.Version, "npm_config_update_notifier=false"},
		Mounts: []docker.Mount{
			{Source: d.Dir, Target: "/def", ReadOnly: true},
			{Source: filepath.Join(work, "out"), Target: "/out"},
			{Source: filepath.Join(work, "cache"), Target: "/cache"},
		},
		Tmpfs:  map[string]string{"/tmp": "exec"},
		Labels: map[string]string{"automaticum": "sdk-install"},
	}
	// A local build's manifest names its packed packages as ../packs/<file>.
	if packs := filepath.Join(work, "cache", "packs"); d.Source == "local" {
		spec.Mounts = append(spec.Mounts, docker.Mount{Source: packs, Target: "/packs", ReadOnly: true})
	}
	res, err := e.Exec.Run(ctx, spec, timeout, log, log, nil)
	if err != nil {
		return "", 0, fmt.Errorf("sdk: installing %s: %w", d.Name, err)
	}
	if res.TimedOut || res.ExitCode != 0 {
		return "", 0, fmt.Errorf("sdk: installing %s: exit %d (timed out: %v); the installed package must be version %s", d.Name, res.ExitCode, res.TimedOut, d.Version)
	}
	return HashTree(filepath.Join(work, "out", "node_modules"))
}

// pin writes the bundle's hash into the definition.
func pin(d *Def, sum string) error {
	path := filepath.Join(d.Dir, "sdk.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.TrimRight(string(b), "\n") + "\nbundle_sha256 = \"" + sum + "\"\n"
	d.BundleSHA256 = sum
	return os.WriteFile(path, []byte(text), 0o644)
}

// open lets the install's user read and write a tree this process made.
func open(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o777)
		}
		return os.Chmod(p, 0o666)
	})
}

// removeAll removes a tree whose directories an install may have left
// without write permission.
func removeAll(dir string) {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(p, 0o755)
		}
		return nil
	})
	os.RemoveAll(dir)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o755)
		case d.Type().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return fmt.Errorf("sdk: %s is neither a file nor a directory", p)
	})
}

// tarDir writes a directory's regular files into a tar file, in name order
// and without times or owners, and returns the file's hash and size.
func tarDir(dir, dst string) (string, int64, error) {
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		// The installer's own logs are not packages.
		if d.IsDir() && (rel == "npm/_logs" || rel == "npm/_update-notifier-last-checked") {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			names = append(names, rel)
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Strings(names)
	f, err := os.Create(dst)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, h))
	for _, name := range names {
		in, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return "", 0, err
		}
		st, err := in.Stat()
		if err == nil {
			err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: st.Size(), Typeflag: tar.TypeReg})
		}
		if err == nil {
			_, err = io.Copy(tw, in)
		}
		in.Close()
		if err != nil {
			return "", 0, err
		}
	}
	if err := tw.Close(); err != nil {
		return "", 0, err
	}
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), st.Size(), f.Sync()
}

func untar(src, dir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(h.Name)
		if !filepath.IsLocal(name) || h.Typeflag != tar.TypeReg {
			return fmt.Errorf("sdk: unexpected entry %q in the archived packages", h.Name)
		}
		to := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(to), 0o777); err != nil {
			return err
		}
		out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
}
