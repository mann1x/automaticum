package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/config"
)

// liveRig is a serving rig whose configuration is a directory of its own,
// so a test can change it under the daemon.
func liveRig(t *testing.T) (*rig, string) {
	t.Helper()
	dir := t.TempDir()
	repo, _ := filepath.Abs("../..")
	for _, sub := range []string{"arms", "models"} {
		src := filepath.Join(examplesConfig(t), sub)
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
		names, _ := filepath.Glob(filepath.Join(src, "*.toml"))
		for _, n := range names {
			b, _ := os.ReadFile(n)
			os.WriteFile(filepath.Join(dir, sub, filepath.Base(n)), b, 0o644)
		}
	}
	write(t, dir, map[string]string{
		"automaticum.toml":    "data_root = \"" + t.TempDir() + "\"\nrepo = \"" + repo + "\"\nfree_floor_gb = 1\nwatch_config = false\n",
		"token":               token,
		"lanes/lane-1.toml":   liveLane("stub"),
		"endpoints/stub.toml": liveEndpoint("7412"),
		// An arm on an endpoint the installation does not have yet.
		"arms/live.toml": "extends = \"stub\"\nendpoint = \"gpu0\"\n",
	})
	l, err := config.LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := newRig(t, false)
	g.d.Local = l
	go func() { g.d.Serve(g.life); close(g.served) }()
	return g, dir
}

func liveLane(endpoints ...string) string {
	return "host = \"linux-gpu-1\"\nendpoints = [\"" + strings.Join(endpoints, "\", \"") + "\"]\ncpus = 4\nmemory_gb = 8\npids = 1024\n"
}

func liveEndpoint(port string) string {
	return "kind = \"llamacpp\"\nbase_url = \"http://172.17.0.1:" + port + "\"\nhost = \"linux-gpu-1\"\ncapacity = 1\n"
}

// write puts files in dir; an empty text removes the file.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if text == "" {
			os.Remove(p)
			continue
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (g *rig) reload() (ReloadResult, int) {
	g.t.Helper()
	var res ReloadResult
	code := g.call("POST", "/v1/config/reload", nil, &res)
	return res, code
}

func (g *rig) lane(name string) *LaneStatus {
	for _, l := range g.status().Lanes {
		if l.Lane == name {
			return &l
		}
	}
	return nil
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("%s did not happen", what)
}

// TestReloadLive: a lane and an endpoint added to the directory run work at
// once, with no restart; a configuration that does not load is refused and
// the one in force keeps serving; a lane taken out under a run finishes it
// and its queued runs wait until it is back.
func TestReloadLive(t *testing.T) {
	g, dir := liveRig(t)

	// Before: nothing may use gpu0.
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "live", Task: "answer"}, nil); code < 400 {
		t.Fatalf("a run on an unknown endpoint was taken: %d", code)
	}

	write(t, dir, map[string]string{"endpoints/gpu0.toml": liveEndpoint("8262"), "lanes/lane-2.toml": liveLane("gpu0")})
	var dry ReloadResult
	if code := g.call("POST", "/v1/config/reload?dry_run=1", nil, &dry); code != 200 || !dry.DryRun || !slices.Equal(dry.Change.Added, []string{"endpoint gpu0", "lane lane-2"}) {
		t.Fatalf("dry run: %d %+v", code, dry)
	}
	if g.d.Local.Lane("lane-2") != nil {
		t.Fatal("a dry run put the lane in force")
	}
	res, code := g.reload()
	if code != 200 || res.Error != "" || !slices.Equal(res.Change.Added, []string{"endpoint gpu0", "lane lane-2"}) {
		t.Fatalf("reload: %d %+v", code, res)
	}
	var started StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "live", Task: "answer"}, &started); code != 202 || started.Lane != "lane-2" {
		t.Fatalf("start on the new lane: %d %+v", code, started)
	}
	if v := g.waitFinished(started.RunIDs[0]); v.Verdict != "PASS" {
		t.Fatalf("run on the new lane: %+v", v)
	}

	// A broken file: refused, the configuration in force stays and serves.
	write(t, dir, map[string]string{"lanes/lane-3.toml": "host = \n"})
	res, code = g.reload()
	if code != http.StatusUnprocessableEntity || res.Error == "" {
		t.Fatalf("a broken directory: %d %+v", code, res)
	}
	st := g.status()
	if st.Config == nil || st.Config.Last == nil || st.Config.Last.Error == "" || g.lane("lane-2") == nil {
		t.Fatalf("status after a refused reload: %+v", st.Config)
	}
	write(t, dir, map[string]string{"lanes/lane-3.toml": ""})

	// A lane taken out under a run: the run finishes, the next one waits.
	g.runner.mu.Lock()
	g.runner.hold = make(chan struct{})
	g.runner.started = make(chan string, 4)
	g.runner.mu.Unlock()
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "live", Task: "answer", Count: 2}, &started); code != 202 {
		t.Fatalf("start: %d", code)
	}
	first := <-g.runner.started
	write(t, dir, map[string]string{"lanes/lane-2.toml": ""})
	res, code = g.reload()
	if code != 200 || !slices.Equal(res.Change.Removed, []string{"lane lane-2"}) || len(res.Retiring) != 1 || len(res.Waiting) != 1 {
		t.Fatalf("reload under a run: %d %+v", code, res)
	}
	if l := g.lane("lane-2"); l == nil || !l.Removed || l.State != "running" || l.RunID != first {
		t.Fatalf("the retiring lane: %+v", l)
	}
	close(g.runner.hold)
	if v := g.waitFinished(first); v.Verdict != "PASS" {
		t.Fatalf("the run on the retiring lane: %+v", v)
	}
	second := started.RunIDs[1]
	waitFor(t, "the second run waiting", func() bool {
		for _, q := range g.status().Queue {
			if q.RunID == second && strings.Contains(q.Stopped, "lane lane-2 is not in the configuration") {
				return true
			}
		}
		return false
	})
	if g.lane("lane-2") != nil {
		t.Fatal("the lane is still shown after its run")
	}
	// Back: the waiting run starts.
	write(t, dir, map[string]string{"lanes/lane-2.toml": liveLane("gpu0")})
	if _, code := g.reload(); code != 200 {
		t.Fatal("reload")
	}
	if v := g.waitFinished(second); v.Verdict != "PASS" {
		t.Fatalf("the run that waited: %+v", v)
	}

	// A key held until the next start is named while it differs, and no
	// longer once it is set back.
	settings := mustRead(t, filepath.Join(dir, "automaticum.toml"))
	write(t, dir, map[string]string{"automaticum.toml": strings.Replace(settings, "repo = ", "repo = \"/elsewhere\"\n# was: ", 1)})
	if res, code := g.reload(); code != 200 || !slices.Equal(res.Change.Restart, []string{"repo"}) || !slices.Equal(g.d.configState().Restart, []string{"repo"}) {
		t.Fatalf("restart key: %d %+v", code, res)
	}
	write(t, dir, map[string]string{"automaticum.toml": settings})
	if _, code := g.reload(); code != 200 || len(g.d.configState().Restart) != 0 {
		t.Fatalf("the restart key is still named after it was set back: %v", g.d.configState().Restart)
	}

	// The token is read again: the old one no longer writes.
	newToken := strings.Repeat("n", 40)
	write(t, dir, map[string]string{"token": newToken})
	if res, code := g.reload(); code != 200 || !slices.Contains(res.Change.Settings, "token") {
		t.Fatalf("token reload: %d %+v", code, res)
	}
	req, _ := http.NewRequest("POST", g.srv.URL+"/v1/config/reload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the old token still writes: %v", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", g.srv.URL+"/v1/config/reload", nil)
	req.Header.Set("Authorization", "Bearer "+newToken)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("the new token does not write: %v", resp.StatusCode)
	}
}

// TestWatchConfig: with watch_config on, a file written into the directory
// is in force without anyone asking.
func TestWatchConfig(t *testing.T) {
	g, dir := liveRig(t)
	write(t, dir, map[string]string{"automaticum.toml": strings.Replace(mustRead(t, filepath.Join(dir, "automaticum.toml")), "watch_config = false\n", "", 1)})
	if _, code := g.reload(); code != 200 {
		t.Fatal("reload")
	}
	g.d.WatchEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(g.life)
	defer cancel()
	go g.d.WatchConfig(ctx)
	waitFor(t, "the watcher starting", func() bool { return g.d.configState().Watching })
	write(t, dir, map[string]string{"endpoints/gpu0.toml": liveEndpoint("8262"), "lanes/lane-2.toml": liveLane("gpu0")})
	waitFor(t, "the new lane in force", func() bool { return g.d.Local.Lane("lane-2") != nil })
	if last := g.d.configState().Last; last == nil || last.Source != "watch" {
		t.Fatalf("last reload: %+v", last)
	}
	// Turned off by the directory itself: later changes wait for a reload.
	write(t, dir, map[string]string{"automaticum.toml": mustRead(t, filepath.Join(dir, "automaticum.toml")) + "watch_config = false\n"})
	waitFor(t, "watch_config = false in force", func() bool { return !g.d.Local.Settings().Watches() })
	write(t, dir, map[string]string{"lanes/lane-3.toml": liveLane("gpu0")})
	time.Sleep(200 * time.Millisecond)
	if g.d.Local.Lane("lane-3") != nil {
		t.Fatal("watch_config = false still reloads")
	}
}

// TestFaultedRun: a run whose harness panics is ABORTED and the daemon,
// and the lane, go on.
func TestFaultedRun(t *testing.T) {
	g := newRig(t, true)
	g.runner.mu.Lock()
	g.runner.fault = "boom"
	g.runner.mu.Unlock()
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	if v := g.waitFinished(resp.RunIDs[0]); v.Verdict != "ABORTED" || !strings.Contains(v.VerdictReason, "harness fault: boom") {
		t.Fatalf("the faulted run: %+v", v)
	}
	if v := g.waitFinished(resp.RunIDs[1]); v.Verdict != "PASS" {
		t.Fatalf("the run after the fault: %+v", v)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
