package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func frame(stream byte, s string) []byte {
	head := make([]byte, 8)
	head[0] = stream
	binary.BigEndian.PutUint32(head[4:], uint32(len(s)))
	return append(head, s...)
}

func TestDemux(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(1, "out one\n"))
	in.Write(frame(2, "err one\n"))
	in.Write(frame(1, ""))
	in.Write(frame(1, "out two\n"))
	var stdout, stderr bytes.Buffer
	if err := demux(&in, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out one\nout two\n" || stderr.String() != "err one\n" {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	// A stream cut inside a frame is an error, not a short read taken as whole.
	cut := frame(1, "whole frame")
	if err := demux(bytes.NewReader(cut[:len(cut)-3]), &stdout, nil); err == nil {
		t.Fatal("a truncated frame was accepted")
	}
}

// engine returns a client for the local engine, or skips the test.
func engine(t *testing.T) *Client {
	t.Helper()
	if testing.Short() {
		t.Skip("needs a Docker engine")
	}
	if _, err := os.Stat(DefaultSocket); err != nil {
		t.Skip("no Docker socket")
	}
	c := New("")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Version(ctx); err != nil {
		t.Skipf("Docker engine not reachable: %v", err)
	}
	return c
}

const testImage = "debian:bookworm-slim"

func TestRun(t *testing.T) {
	c := engine(t)
	ctx := context.Background()
	if _, err := c.ImageInspect(ctx, testImage); err != nil {
		if err := c.ImagePull(ctx, testImage, nil); err != nil {
			t.Skipf("cannot pull %s: %v", testImage, err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/in.txt", []byte("from the host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	var started string
	res, err := c.Run(ctx, Spec{
		Image: testImage, Cmd: []string{"sh", "-c", "cat /in/in.txt; echo to stderr >&2; id -u; exit 7"},
		User: "1000:1000", Network: "none", Mounts: []Mount{{Source: dir, Target: "/in", ReadOnly: true}},
		PidsLimit: 64, MemoryBytes: 256 << 20,
	}, time.Minute, &stdout, &stderr, func(id string) { started = id })
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 7 || res.TimedOut || res.ID == "" || started != res.ID || !strings.HasPrefix(res.ImageID, "sha256:") {
		t.Fatalf("result %+v, started %q", res, started)
	}
	if stdout.String() != "from the host\n1000\n" || stderr.String() != "to stderr\n" {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	// The container is gone afterwards.
	if _, _, err := c.Inspect(ctx, res.ID); !IsNotFound(err) {
		t.Fatalf("container still exists: %v", err)
	}
}

func TestRunTimeoutAndCancel(t *testing.T) {
	c := engine(t)
	if _, err := c.ImageInspect(context.Background(), testImage); err != nil {
		t.Skip("test image not present")
	}
	res, err := c.Run(context.Background(), Spec{Image: testImage, Cmd: []string{"sleep", "600"}, Network: "none"}, 2*time.Second, nil, nil, nil)
	if err != nil || !res.TimedOut {
		t.Fatalf("timeout: result %+v, err %v", res, err)
	}
	if _, _, err := c.Inspect(context.Background(), res.ID); !IsNotFound(err) {
		t.Fatalf("container survived its timeout: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var id string
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()
	res, err = c.Run(ctx, Spec{Image: testImage, Cmd: []string{"sleep", "600"}, Network: "none"}, 0, nil, nil, func(s string) { id = s })
	if err != context.Canceled || res.TimedOut || id == "" {
		t.Fatalf("cancel: result %+v, err %v", res, err)
	}
	if _, _, err := c.Inspect(context.Background(), id); !IsNotFound(err) {
		t.Fatalf("container survived cancellation: %v", err)
	}
}

func TestPickVersion(t *testing.T) {
	for _, c := range []struct{ min, want string }{
		{"", "v1.41"},
		{"1.24", "v1.41"},
		{"1.41", "v1.41"},
		{"1.44", "v1.44"},
		{"1.100", "v1.100"},
		{"x", "v1.41"},
	} {
		if got := pickVersion(c.min); got != c.want {
			t.Errorf("pickVersion(%q) = %s, want %s", c.min, got, c.want)
		}
	}
}

// untar writes what is under the archived directory into dst, links as
// links and hard links as links, and nothing outside dst.
func TestUntar(t *testing.T) {
	archive := func(entries ...tar.Header) *bytes.Buffer {
		var b bytes.Buffer
		tw := tar.NewWriter(&b)
		for _, h := range entries {
			if h.Typeflag == tar.TypeReg {
				h.Size = int64(len(h.Name))
			}
			tw.WriteHeader(&h)
			if h.Typeflag == tar.TypeReg {
				tw.Write([]byte(h.Name))
			}
		}
		tw.Close()
		return &b
	}
	dst := t.TempDir()
	err := untar(archive(
		tar.Header{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o755},
		tar.Header{Name: "bin/tool", Typeflag: tar.TypeReg, Mode: 0o755},
		tar.Header{Name: "bin/same", Typeflag: tar.TypeLink, Linkname: "bin/tool"},
		tar.Header{Name: "bin/sub/x", Typeflag: tar.TypeReg, Mode: 0o600},
		tar.Header{Name: "bin/up", Typeflag: tar.TypeSymlink, Linkname: "/etc"},
	), "bin", dst)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "same")); err != nil || string(b) != "bin/tool" {
		t.Errorf("the hard link: %q %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "tool")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("the tool: %v %v", fi, err)
	}
	if l, err := os.Readlink(filepath.Join(dst, "up")); err != nil || l != "/etc" {
		t.Errorf("the link: %q %v", l, err)
	}
	for name, a := range map[string]*bytes.Buffer{
		"through a link": archive(tar.Header{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o755},
			tar.Header{Name: "bin/up", Typeflag: tar.TypeSymlink, Linkname: "/tmp"},
			tar.Header{Name: "bin/up/escaped", Typeflag: tar.TypeReg, Mode: 0o644}),
		"up the tree":   archive(tar.Header{Name: "bin/../escaped", Typeflag: tar.TypeReg, Mode: 0o644}),
		"another top":   archive(tar.Header{Name: "etc/passwd", Typeflag: tar.TypeReg, Mode: 0o644}),
		"top is a link": archive(tar.Header{Name: "bin", Typeflag: tar.TypeSymlink, Linkname: "/etc"}),
		"hard link out": archive(tar.Header{Name: "bin/x", Typeflag: tar.TypeLink, Linkname: "etc/passwd"}),
	} {
		if err := untar(a, "bin", t.TempDir()); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if _, err := os.Stat("/tmp/escaped"); err == nil {
		t.Error("a file was written outside dst")
	}
}
