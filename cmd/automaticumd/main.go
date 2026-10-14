// Command automaticumd is the daemon: it owns the store, works the queue on
// the lanes of this host and serves the API every interface is a client of.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/gate"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/sdk"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/store/duck"
	"github.com/mann1x/automaticum/internal/task"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg := flag.String("config", os.Getenv("AUTOMATICUM_CONFIG"), "configuration directory")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := serve(*cfg, log); err != nil {
		log.Error("automaticumd stops", "error", err)
		os.Exit(1)
	}
}

func free(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// ramBacked reports whether a path is on a file system held in memory.
func ramBacked(path string) (bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, err
	}
	const tmpfs, ramfs = 0x01021994, 0x858458f6
	return int64(st.Type) == tmpfs || int64(st.Type) == ramfs, nil
}

func serve(cfgDir string, log *slog.Logger) error {
	if cfgDir == "" {
		return errors.New("no configuration directory: give --config or set AUTOMATICUM_CONFIG")
	}
	local, err := config.LoadLocal(cfgDir)
	if err != nil {
		return err
	}
	token, err := local.Token()
	if err != nil {
		return err
	}
	d := &local.Daemon
	if d.Repo == "" {
		return errors.New("the configuration names no repo: the checkout with arms/ and tasks/")
	}
	runs := filepath.Join(d.DataRoot, "runs")
	for _, dir := range []string{d.DataRoot, runs, filepath.Join(d.DataRoot, "artefacts"), filepath.Join(d.DataRoot, "scratch")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	blobs, err := blob.Open(blob.Options{Root: d.DataRoot, ArchiveRoot: d.ArchiveRoot})
	if err != nil {
		return err
	}
	defer blobs.Close()
	ctx := context.Background()
	state, err := duck.Open(ctx, filepath.Join(d.DataRoot, "state.duckdb"))
	if err != nil {
		return err
	}
	defer state.Close()

	engine := docker.New(d.DockerSocket)
	if _, err := engine.Version(ctx); err != nil {
		return fmt.Errorf("the container engine does not answer: %w", err)
	}
	tasks := &task.Env{Exec: engine, Artefacts: filepath.Join(d.DataRoot, "artefacts"), Scratch: filepath.Join(d.DataRoot, "scratch")}
	archive := filepath.Join(d.DataRoot, "sdk-packages")
	if d.ArchiveRoot != "" {
		archive = filepath.Join(d.ArchiveRoot, "sdk-packages")
	}
	builds := &sdk.Env{Exec: engine, Defs: filepath.Join(d.Repo, "runner", "sdk"), Root: filepath.Join(d.DataRoot, "sdk"), Archive: archive}
	dm := &daemon.Daemon{
		Local: local, State: state, Blobs: blobs, Containers: engine,
		Runner: &run.Executor{Engine: engine, Blobs: blobs, State: state, Tasks: tasks, SDK: builds, Local: local, RunsDir: runs, Version: version, Free: free},
		Repo:   d.Repo, RunsDir: runs, Version: version, Token: token, Log: log, ControlFile: daemon.ControlFile(d.DataRoot),
	}
	dm.Gates = func(ctx context.Context, lane string, arm *config.Arm, tk *task.Task, image string) []gate.Result {
		return gate.Run(ctx, &gate.Input{Local: local, Lane: lane, Arm: arm, Task: tk, Image: image,
			Engine: engine, Tasks: tasks, SDK: builds, RunsDir: runs, Free: free, Tmpfs: ramBacked})
	}
	if err := dm.Recover(ctx); err != nil {
		return fmt.Errorf("recovering after the last stop: %w", err)
	}

	ln, err := net.Listen("tcp", d.Listen)
	if err != nil {
		return err
	}
	// The detectors' controls (070 §3). One that fails its controls is
	// unavailable: ingest reports nothing for it, which is not a zero.
	if results, ok := detect.SelfTest(); ok {
		log.Info("detector self-test passed", "version", detect.Version, "controls", len(results))
	} else {
		for _, r := range results {
			if !r.OK {
				log.Error("detector self-test failed", "control", r.Control, "domain", r.Domain, "want", r.Want, "got", r.Got)
			}
		}
	}
	srv := &http.Server{Handler: dm.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("the API stopped", "error", err)
		}
	}()
	log.Info("automaticumd is up", "version", version, "listen", ln.Addr().String(), "lanes", len(local.Lanes), "endpoints", len(local.Endpoints))

	// The first signal drains: the run in flight finishes, nothing else
	// starts. A second one aborts it.
	sctx, abort := context.WithCancel(ctx)
	defer abort()
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Info("draining: the run in flight finishes first; signal again to abort it")
		dm.Drain()
		<-sig
		log.Warn("aborting the run in flight")
		abort()
	}()
	dm.Serve(sctx)
	shut, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	srv.Shutdown(shut)
	if err := state.Checkpoint(ctx); err != nil {
		log.Error("checkpoint", "error", err)
	}
	log.Info("automaticumd stopped")
	return nil
}
