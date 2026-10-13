package sdk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/docker"
)

// pack writes a package as the packer does: a gzipped tar with everything
// under "package/".
func pack(t *testing.T, path, version string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"package/package.json": fmt.Sprintf(`{"name":%q,"version":%q,"main":"index.js"}`, Package, version),
		"package/index.js":     "module.exports = 'made up for a test';\n",
	} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(buf.Bytes())
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// define writes a local build's definition around a made-up package.
func define(t *testing.T, defs, packs, name, version string) {
	integrity := pack(t, filepath.Join(packs, "sdk.tgz"), version)
	dir := filepath.Join(defs, name)
	write(t, filepath.Join(dir, "sdk.toml"), fmt.Sprintf("version = %q\nsource = \"local\"\ncommit = \"0000000\"\n", version))
	write(t, filepath.Join(dir, "package.json"), fmt.Sprintf(`{"name":"bundle","private":true,"dependencies":{%q:"file:../packs/sdk.tgz"}}`, Package))
	write(t, filepath.Join(dir, "package-lock.json"), fmt.Sprintf(`{"name":"bundle","lockfileVersion":3,"requires":true,"packages":{
"":{"name":"bundle","dependencies":{%q:"file:../packs/sdk.tgz"}},
"node_modules/%s":{"version":%q,"resolved":"file:../packs/sdk.tgz","integrity":%q}}}`, Package, Package, version, integrity))
}

func TestDefinitions(t *testing.T) {
	defs := t.TempDir()
	e := &Env{Defs: defs, Root: t.TempDir(), Archive: t.TempDir()}
	define(t, defs, t.TempDir(), "1.2.3+abc", "1.2.3")
	if d, err := e.Def("1.2.3+abc"); err != nil || d.Version != "1.2.3" || d.Source != "local" || d.BundleSHA256 != "" {
		t.Fatalf("%+v %v", d, err)
	}
	if names, _ := e.List(); len(names) != 1 || names[0] != "1.2.3+abc" {
		t.Errorf("list: %v", names)
	}
	for name, text := range map[string]string{
		"no-version": "source = \"registry\"\n",
		"bad-source": "version = \"1.0.0\"\nsource = \"somewhere\"\n",
		"no-commit":  "version = \"1.0.0\"\nsource = \"local\"\n",
		"unknown":    "version = \"1.0.0\"\nsource = \"registry\"\nextra = 1\n",
	} {
		write(t, filepath.Join(defs, name, "sdk.toml"), text)
		if _, err := e.Def(name); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for _, name := range []string{"", "../x", "absent", ".hidden"} {
		if _, err := e.Def(name); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
	// Defined and not installed: a run cannot use it, and the error says how.
	if _, _, err := e.Check("1.2.3+abc", true); !errors.Is(err, ErrNotProvisioned) || !strings.Contains(err.Error(), "automaticum sdk add") {
		t.Errorf("not installed: %v", err)
	}
}

// The whole path, in real containers: install, pin, the same hash again,
// rebuild with no network from what was archived, and a changed bundle
// refused. It needs the container engine and the Node image.
func TestInstallRebuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs containers")
	}
	engine := docker.New("")
	ctx := context.Background()
	if _, err := engine.ImageInspect(ctx, NodeImage); err != nil {
		t.Skip("the Node image is not here: ", err)
	}
	base := os.Getenv("AUTOMATICUM_TEST_DIR") // a real disk the engine can mount
	if base == "" {
		t.Skip("AUTOMATICUM_TEST_DIR is not set")
	}
	root, err := os.MkdirTemp(base, "sdk-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer removeAll(root)
	defs, packs := filepath.Join(root, "defs"), filepath.Join(root, "packs")
	os.MkdirAll(packs, 0o755)
	e := &Env{Exec: engine, Defs: defs, Root: filepath.Join(root, "sdk"), Archive: filepath.Join(root, "archive")}
	define(t, defs, packs, "9.9.9+test", "9.9.9")

	var log bytes.Buffer
	rec, err := e.Add(ctx, "9.9.9+test", packs, &log)
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	d, _ := e.Def("9.9.9+test")
	if rec.BundleSHA256 == "" || d.BundleSHA256 != rec.BundleSHA256 || rec.PackagesSHA256 == "" || rec.Files < 2 {
		t.Fatalf("record %+v, pinned %q", rec, d.BundleSHA256)
	}
	if _, _, err := e.Check("9.9.9+test", true); err != nil {
		t.Fatalf("a fresh bundle: %v", err)
	}
	// Again from the same definition: the same bundle.
	again, err := e.Add(ctx, "9.9.9+test", packs, &log)
	if err != nil || again.BundleSHA256 != rec.BundleSHA256 {
		t.Fatalf("second install: %+v %v", again, err)
	}
	// With no network and without the packed packages' directory.
	os.RemoveAll(packs)
	sum, err := e.Rebuild(ctx, "9.9.9+test", &log)
	if err != nil || sum != rec.BundleSHA256 {
		t.Fatalf("rebuild from the archive: %q %v\n%s", sum, err, log.String())
	}
	// A file changed in the bundle: no run may use it.
	write(t, filepath.Join(e.Path("9.9.9+test"), Package, "index.js"), "module.exports = 'tampered';\n")
	if _, _, err := e.Check("9.9.9+test", true); !errors.Is(err, ErrChanged) {
		t.Errorf("a changed bundle: %v", err)
	}
	if _, _, err := e.Check("9.9.9+test", false); err != nil {
		t.Errorf("the shallow check trusts the record: %v", err)
	}
	// The installed package is another version than the definition says.
	os.MkdirAll(packs, 0o755)
	define(t, defs, packs, "9.9.8+test", "9.9.9")
	write(t, filepath.Join(defs, "9.9.8+test", "sdk.toml"), "version = \"9.9.8\"\nsource = \"local\"\ncommit = \"0000000\"\n")
	if _, err := e.Add(ctx, "9.9.8+test", packs, &log); err == nil {
		t.Error("a definition whose version is not the package's was installed")
	}
}
