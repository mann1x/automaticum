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
	"runtime"
	"sync"
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
	// Taken before anything else, so that a signal sent while the daemon
	// starts is handled, never the default that would stop it.
	signal.Ignore(syscall.SIGPIPE)
	sig := make(chan os.Signal, 8)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	local, err := config.LoadLocal(cfgDir)
	if err != nil {
		return err
	}
	tokenHash, err := local.TokenHash()
	if err != nil {
		return err
	}
	d := &local.Daemon
	if d.Repo == "" {
		return errors.New("the configuration names no repo: the checkout with tasks/ and images/")
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
		Local: local, State: state, Blobs: blobs, Archiver: blobs, Containers: engine,
		Runner: &run.Executor{Engine: engine, Blobs: blobs, State: state, Tasks: tasks, SDK: builds, Local: local, RunsDir: runs, Version: version, Free: free},
		Repo:   d.Repo, RunsDir: runs, Version: version, TokenHash: tokenHash, Log: log, ControlFile: daemon.ControlFile(d.DataRoot),
	}
	dm.Gates = func(ctx context.Context, lane string, arm *config.Arm, tk *task.Task, image string) []gate.Result {
		return gate.Run(ctx, &gate.Input{Local: local, Lane: lane, Arm: arm, Task: tk, Image: image,
			Engine: engine, Tasks: tasks, SDK: builds, RunsDir: runs, Free: free, Tmpfs: ramBacked})
	}
	dm.Oracle, dm.Store = tasks.RunOracle, blobs.Put
	dm.Static = func(ctx context.Context, arm *config.Arm, tk *task.Task, image string) []gate.Result {
		return gate.Static(ctx, &gate.Input{Local: local, Arm: arm, Task: tk, Image: image, Engine: engine, Tasks: tasks, SDK: builds})
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
	api := &apiServer{handler: dm.Handler(), log: log}
	api.serve(ln)
	dm.Rebind = api.rebind
	log.Info("automaticumd is up", "version", version, "listen", ln.Addr().String(), "lanes", len(local.LaneNames()), "endpoints", len(local.EndpointNames()))

	sctx, abort := context.WithCancel(ctx)
	defer abort()
	go dm.WatchConfig(sctx)
	go handleSignals(sctx, sig, dm, log, abort)
	dm.Serve(sctx)
	api.shutdown(5 * time.Second)
	if err := state.Checkpoint(ctx); err != nil {
		log.Error("checkpoint", "error", err)
	}
	log.Info("automaticumd stopped")
	return nil
}

// handleSignals is what each signal asks of the daemon:
//
//   - SIGHUP reads the configuration again and puts it in force; a
//     configuration that does not load leaves the one in force.
//   - SIGUSR1 logs the state: the configuration, every lane, the queue.
//   - SIGUSR2 logs every goroutine's stack, for a daemon that seems stuck.
//   - SIGINT or SIGTERM drains: the runs in flight finish, nothing else
//     starts, then the daemon stops. A second one aborts the runs in flight.
//   - SIGPIPE is ignored: a log reader that goes away must not stop it.
func handleSignals(ctx context.Context, sig <-chan os.Signal, dm *daemon.Daemon, log *slog.Logger, abort context.CancelFunc) {
	stops := 0
	for {
		var s os.Signal
		select {
		case <-ctx.Done():
			return
		case s = <-sig:
		}
		switch s {
		case syscall.SIGHUP:
			log.Info("SIGHUP: reading the configuration again")
			go dm.Reload(context.WithoutCancel(ctx), "signal", false)
		case syscall.SIGUSR1:
			go dm.DumpState(context.WithoutCancel(ctx))
		case syscall.SIGUSR2:
			buf := make([]byte, 1<<20)
			for {
				n := runtime.Stack(buf, true)
				if n < len(buf) {
					buf = buf[:n]
					break
				}
				buf = make([]byte, 2*len(buf))
			}
			log.Info("SIGUSR2: every goroutine's stack", "stacks", string(buf))
		default:
			stops++
			if stops == 1 {
				log.Info("draining: the runs in flight finish first; signal again to abort them", "signal", s.String())
				dm.Drain()
				continue
			}
			log.Warn("aborting the runs in flight", "signal", s.String())
			abort()
			return
		}
	}
}

// apiServer is the API on its address. A reload that changes listen moves
// it: the new address is bound and served before the old one is let go,
// and requests in flight on the old one finish.
type apiServer struct {
	handler http.Handler
	log     *slog.Logger
	mu      sync.Mutex
	srv     *http.Server
}

func (a *apiServer) serve(ln net.Listener) {
	srv := &http.Server{Handler: a.handler, ReadHeaderTimeout: 10 * time.Second}
	a.mu.Lock()
	a.srv = srv
	a.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("the API stopped", "listen", ln.Addr().String(), "error", err)
		}
	}()
}

func (a *apiServer) rebind(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	a.mu.Lock()
	old := a.srv
	a.mu.Unlock()
	a.serve(ln)
	a.log.Info("the API moved", "listen", ln.Addr().String())
	if old != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			old.Shutdown(ctx)
		}()
	}
	return nil
}

func (a *apiServer) shutdown(grace time.Duration) {
	a.mu.Lock()
	srv := a.srv
	a.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	srv.Shutdown(ctx)
}
