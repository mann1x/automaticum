package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
)

// ConfigState is the configuration in force and what became of the last
// attempt to read it again (GET /v1/config, `status`).
type ConfigState struct {
	Dir string `json:"dir"`
	// Loaded is when the configuration in force was read.
	Loaded    time.Time `json:"loaded"`
	Reloads   int       `json:"reloads"`
	Lanes     []string  `json:"lanes"`
	Endpoints []string  `json:"endpoints"`
	// Watching says the directory is read again on its own whenever a
	// file in it changes.
	Watching bool `json:"watching"`
	// Last is the last attempt, applied or refused.
	Last *ReloadResult `json:"last,omitempty"`
	// Restart are the settings whose value in the directory differs from
	// the one in force, held until the next start (config.Change.Restart).
	Restart []string `json:"restart,omitempty"`
}

// ReloadResult is one attempt to read the configuration again.
type ReloadResult struct {
	At     time.Time `json:"at"`
	Source string    `json:"source"` // signal, watch, api
	// Error is why nothing changed; the configuration in force stays.
	Error  string        `json:"error,omitempty"`
	Change config.Change `json:"change"`
	// Retiring are the lanes taken out of the configuration while a run
	// was in flight on them: each finishes its run and starts no other.
	Retiring []string `json:"retiring,omitempty"`
	// Waiting are queued runs whose lane or endpoint the configuration no
	// longer has; they wait, and start once it has them again.
	Waiting []string `json:"waiting,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

// watchEvery is how often the watcher reads the configuration directory:
// the queue's own pace (Serve), so a change is in force before the next
// start would see it.
const watchEvery = 2 * time.Second

// tokenHash is the write token's hash in force.
func (d *Daemon) tokenHash() string {
	d.cfgMu.Lock()
	defer d.cfgMu.Unlock()
	if d.liveToken != "" {
		return d.liveToken
	}
	return d.TokenHash
}

// Reload reads the configuration directory again and puts it in force
// without stopping anything (SIGHUP, the watcher, POST /v1/config/reload).
// A directory that does not load, a token that cannot be read or a listen
// address that cannot be bound changes nothing: the configuration in force
// stays, whole, and the result says why. Runs in flight keep the lane and
// endpoint they started with; a lane taken out finishes its run first.
func (d *Daemon) Reload(ctx context.Context, source string, dryRun bool) ReloadResult {
	d.init()
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	res := ReloadResult{At: time.Now().UTC(), Source: source, DryRun: dryRun}
	defer func() {
		if !dryRun {
			d.recordReload(res)
		}
	}()
	next, err := config.LoadLocal(d.Local.Dir)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	token, err := next.TokenHash()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	old := d.Local.Settings()
	if dryRun {
		// Applied to a copy: the same checks, nothing put in force.
		probe := &config.Local{Dir: d.Local.Dir, Daemon: old, Lanes: map[string]*config.Lane{}, Endpoints: map[string]*config.Endpoint{}}
		for _, n := range d.Local.LaneNames() {
			probe.Lanes[n] = d.Local.Lane(n)
		}
		for _, n := range d.Local.EndpointNames() {
			probe.Endpoints[n] = d.Local.Endpoint(n)
		}
		for _, n := range next.EndpointNames() {
			if e := d.Local.Endpoint(n); e != nil && e.Cell {
				res.Error = fmt.Sprintf("config: endpoint %s is the name of a running cell's endpoint; rename it or stop the cell", n)
				return res
			}
		}
		res.Change, err = probe.Apply(next)
		if err != nil {
			res.Error = err.Error()
		}
		d.consequences(ctx, next, &res)
		return res
	}
	// The address first: a new one that cannot be bound refuses the
	// reload before anything changes.
	listen := next.Settings().Listen
	if listen != old.Listen && d.Rebind != nil {
		if err := d.Rebind(listen); err != nil {
			res.Error = fmt.Sprintf("listen %s: %v; the API stays on %s", listen, err, old.Listen)
			return res
		}
	}
	res.Change, err = d.Local.Apply(next)
	if err != nil {
		res.Error = err.Error()
		if listen != old.Listen && d.Rebind != nil {
			if err := d.Rebind(old.Listen); err != nil {
				d.Log.Error("the API cannot go back to its address", "listen", old.Listen, "error", err)
			}
		}
		return res
	}
	if token != d.tokenHash() {
		res.Change.Settings = append(res.Change.Settings, "token")
		d.cfgMu.Lock()
		d.liveToken = token
		d.cfgMu.Unlock()
	}
	d.consequences(ctx, d.Local, &res)
	d.poke()
	return res
}

// consequences names what the change means for runs: lanes retiring under
// a run in flight and queued runs left without their lane or endpoint.
func (d *Daemon) consequences(ctx context.Context, l *config.Local, res *ReloadResult) {
	flying := map[string]bool{}
	d.mu.Lock()
	for lane, f := range d.flights {
		flying[f.runID] = true
		if l.Lane(lane) == nil {
			res.Retiring = append(res.Retiring, fmt.Sprintf("%s (run %s)", lane, f.runID))
		}
	}
	d.mu.Unlock()
	slices.Sort(res.Retiring)
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return
	}
	for _, it := range queue {
		if flying[it.RunID] {
			continue
		}
		if l.Lane(it.Lane) == nil {
			res.Waiting = append(res.Waiting, fmt.Sprintf("%s: lane %s is not in the configuration", it.RunID, it.Lane))
			continue
		}
		arm, cell, err := d.armFor(it.Batch, it.Arm)
		if err == nil && cell == nil && l.Endpoint(arm.Endpoint) == nil {
			res.Waiting = append(res.Waiting, fmt.Sprintf("%s: endpoint %s is not in the configuration", it.RunID, arm.Endpoint))
		}
	}
}

func (d *Daemon) recordReload(res ReloadResult) {
	d.cfgMu.Lock()
	d.lastReload = &res
	if res.Error == "" {
		d.reloads++
		d.loaded = res.At
		// The values in force keep the start's for these keys, so each
		// reload names every one still waiting: a key set back drops out.
		d.restart = slices.Clone(res.Change.Restart)
	}
	d.cfgMu.Unlock()
	switch {
	case res.Error != "":
		d.Log.Error("the configuration was not reloaded; the one in force stays", "source", res.Source, "error", res.Error)
	case res.Change.Empty():
		d.Log.Info("configuration reloaded: nothing changed", "source", res.Source)
	default:
		c := res.Change
		d.Log.Info("configuration reloaded", "source", res.Source, "added", c.Added, "removed", c.Removed, "changed", c.Changed,
			"settings", c.Settings, "restart", c.Restart, "retiring", res.Retiring, "waiting", len(res.Waiting))
		if len(c.Restart) > 0 {
			d.Log.Warn("these settings hold only from the next start; their old values stay in force", "keys", c.Restart)
		}
	}
	d.bump()
}

// configState is the configuration in force and the last reload.
func (d *Daemon) configState() ConfigState {
	d.init()
	d.cfgMu.Lock()
	defer d.cfgMu.Unlock()
	st := ConfigState{Dir: d.Local.Dir, Loaded: d.loaded, Reloads: d.reloads, Last: d.lastReload,
		Lanes: d.Local.LaneNames(), Endpoints: d.Local.EndpointNames(), Watching: d.watching,
		Restart: slices.Clone(d.restart)}
	if st.Loaded.IsZero() {
		st.Loaded = d.started.UTC()
	}
	return st
}

func (d *Daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, d.configState())
}

func (d *Daemon) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	res := d.Reload(r.Context(), "api", dryRun(r))
	status := http.StatusOK
	if res.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	reply(w, status, res)
}

// WatchConfig reads the configuration directory every two seconds and,
// unless watch_config is false, reloads it when anything in it that the
// daemon reads has changed: automaticum.toml, lanes/, endpoints/ and the
// token files. A change that
// does not load is logged and left; the next change is tried again. It
// returns when ctx ends.
func (d *Daemon) WatchConfig(ctx context.Context) {
	d.init()
	d.cfgMu.Lock()
	d.watching = true
	d.cfgMu.Unlock()
	defer func() {
		d.cfgMu.Lock()
		d.watching = false
		d.cfgMu.Unlock()
	}()
	every := d.WatchEvery
	if every == 0 {
		every = watchEvery
	}
	seen := configSignature(d.Local)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sig := configSignature(d.Local)
		if sig == seen {
			continue
		}
		if !d.Local.Settings().Watches() {
			// watch_config = false: SIGHUP and `config reload` only.
			seen = sig
			continue
		}
		// A file being written: wait until two reads agree.
		select {
		case <-ctx.Done():
			return
		case <-time.After(every / 4):
		}
		if again := configSignature(d.Local); again != sig {
			continue
		}
		seen = sig
		d.guard("the configuration watcher", func() { d.Reload(ctx, "watch", false) })
	}
}

// configSignature is a hash of every file a reload reads, by name and
// content, so that any edit, addition or removal changes it.
func configSignature(l *config.Local) string {
	h := sha256.New()
	add := func(p string) {
		f, err := os.Open(p)
		if err != nil {
			fmt.Fprintf(h, "%s\x00missing\x00", p)
			return
		}
		defer f.Close()
		fmt.Fprintf(h, "%s\x00", p)
		io.Copy(h, f)
		h.Write([]byte{0})
	}
	add(filepath.Join(l.Dir, "automaticum.toml"))
	for _, sub := range []string{"lanes", "endpoints"} {
		paths, _ := filepath.Glob(filepath.Join(l.Dir, sub, "*.toml"))
		slices.Sort(paths)
		for _, p := range paths {
			add(p)
		}
	}
	s := l.Settings()
	for _, p := range []string{s.TokenFile, s.ClientTokenFile} {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(l.Dir, p)
		}
		add(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DumpState logs what the daemon is doing (SIGUSR1): the configuration in
// force, every lane and the queue.
func (d *Daemon) DumpState(ctx context.Context) {
	cs := d.configState()
	d.Log.Info("state: configuration", "dir", cs.Dir, "loaded", cs.Loaded, "reloads", cs.Reloads,
		"lanes", strings.Join(cs.Lanes, " "), "endpoints", strings.Join(cs.Endpoints, " "), "watching", cs.Watching, "restart", cs.Restart)
	st, err := d.status(ctx)
	if err != nil {
		d.Log.Error("state", "error", err)
		return
	}
	d.Log.Info("state: daemon", "version", st.Version, "started", st.Started, "draining", st.Draining, "queued", len(st.Queue))
	for _, l := range st.Lanes {
		d.Log.Info("state: lane", "lane", l.Lane, "state", l.State, "run", l.RunID, "arm", l.Arm, "task", l.Task,
			"waiting", l.Waiting, "drained", l.Drained, "quarantined", l.Quarantined, "removed", l.Removed)
	}
	for _, q := range st.Queue {
		if q.Stopped != "" {
			d.Log.Info("state: queued", "run", q.RunID, "lane", q.Lane, "arm", q.Arm, "why", q.Stopped)
		}
	}
}
