package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
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
