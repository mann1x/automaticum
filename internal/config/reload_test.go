package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// reloadDir writes an installation's directory: automaticum.toml, a token,
// and one file per lane and endpoint.
func reloadDir(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if text == "" {
			os.Remove(p)
			continue
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	reloadSettings = "data_root = \"/data/a\"\nrepo = \"/opt/r\"\nfree_floor_gb = 50\n"
	reloadLane     = "host = \"linux-gpu-1\"\nendpoints = [%s]\ncpus = 4\nmemory_gb = 8\npids = 1024\n"
	reloadEndpoint = "kind = \"llamacpp\"\nbase_url = \"http://172.17.0.1:%s\"\nhost = \"linux-gpu-1\"\ncapacity = 1\n"
)

func lane(endpoints ...string) string {
	return strings.Replace(reloadLane, "%s", `"`+strings.Join(endpoints, `", "`)+`"`, 1)
}

func endpoint(port string) string { return strings.Replace(reloadEndpoint, "%s", port, 1) }

// TestReload: a reload puts new lanes, endpoints and settings in force for
// every reader; one that does not load leaves the configuration in force
// whole; what only a start can take keeps its old value and is named.
func TestReload(t *testing.T) {
	dir := t.TempDir()
	reloadDir(t, dir, map[string]string{
		"automaticum.toml":     reloadSettings,
		"token":                strings.Repeat("t", 32),
		"lanes/lane-1.toml":    lane("gpu1"),
		"endpoints/gpu1.toml":  endpoint("8261"),
		"endpoints/spare.toml": endpoint("8263"),
	})
	l, err := LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	held := l.Endpoint("gpu1") // a run in flight holds its endpoint

	// A new endpoint, a new lane bound to it, one endpoint gone, one
	// changed, one setting changed.
	reloadDir(t, dir, map[string]string{
		"automaticum.toml":     reloadSettings + "[notify]\nevents = [\"run\"]\n",
		"lanes/lane-2.toml":    lane("gpu0"),
		"endpoints/gpu0.toml":  endpoint("8262"),
		"endpoints/gpu1.toml":  strings.Replace(endpoint("8261"), "capacity = 1", "capacity = 2", 1),
		"endpoints/spare.toml": "",
	})
	c, err := l.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Added, []string{"endpoint gpu0", "lane lane-2"}) || !slices.Equal(c.Removed, []string{"endpoint spare"}) ||
		!slices.Equal(c.Changed, []string{"endpoint gpu1"}) || !slices.Equal(c.Settings, []string{"notify"}) || len(c.Restart) != 0 {
		t.Fatalf("change: %+v", c)
	}
	if l.Lane("lane-2") == nil || !l.MayUse("lane-2", "gpu0") || l.Endpoint("spare") != nil || l.Endpoint("gpu1").Capacity != 2 {
		t.Fatalf("not in force: lanes %v endpoints %v", l.LaneNames(), l.EndpointNames())
	}
	if !l.Settings().Notify.Sends("run") {
		t.Error("the notify setting is not in force")
	}
	if held.Capacity != 1 {
		t.Error("the endpoint a run holds was changed under it")
	}

	// Nothing changed: an empty change.
	if c, err := l.Reload(); err != nil || !c.Empty() {
		t.Fatalf("again: %+v %v", c, err)
	}

	// A lane naming an endpoint that does not exist: refused, nothing of
	// it in force, not even the parts that were right.
	reloadDir(t, dir, map[string]string{
		"lanes/lane-3.toml":   lane("nowhere"),
		"endpoints/gpu9.toml": endpoint("8269"),
	})
	if _, err := l.Reload(); err == nil || !strings.Contains(err.Error(), `no endpoint "nowhere"`) {
		t.Fatalf("a bad directory reloaded: %v", err)
	}
	if l.Lane("lane-3") != nil || l.Endpoint("gpu9") != nil || l.Lane("lane-2") == nil {
		t.Fatalf("half applied: lanes %v endpoints %v", l.LaneNames(), l.EndpointNames())
	}
	reloadDir(t, dir, map[string]string{"lanes/lane-3.toml": "", "endpoints/gpu9.toml": ""})

	// A file that does not parse is refused the same way.
	reloadDir(t, dir, map[string]string{"lanes/lane-4.toml": "host = \n"})
	if _, err := l.Reload(); err == nil || l.Lane("lane-4") != nil {
		t.Fatalf("a broken file reloaded: %v", err)
	}
	// A client still finds the daemon, which serves on without the file.
	if c, err := LoadClient(dir); err != nil || c.Daemon.Listen != "127.0.0.1:7411" {
		t.Fatalf("a client is kept from the daemon by a lane file: %v", err)
	}
	reloadDir(t, dir, map[string]string{"lanes/lane-4.toml": ""})

	// The store and the checkout are held open: their keys wait for the
	// next start, the rest goes in force.
	reloadDir(t, dir, map[string]string{"automaticum.toml": strings.Replace(reloadSettings, "/opt/r", "/opt/r2", 1) + "free_floor_gb_x = 1\n"})
	if _, err := l.Reload(); err == nil {
		t.Fatal("an unknown key reloaded")
	}
	reloadDir(t, dir, map[string]string{"automaticum.toml": strings.Replace(strings.Replace(reloadSettings, "/opt/r", "/opt/r2", 1), "= 50", "= 60", 1)})
	c, err = l.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Restart, []string{"repo"}) || !slices.Contains(c.Settings, "free_floor_gb") {
		t.Fatalf("change: %+v", c)
	}
	if s := l.Settings(); s.Repo != "/opt/r" || s.FreeFloorGB != 60 {
		t.Fatalf("settings: repo %s floor %d", s.Repo, s.FreeFloorGB)
	}

	// A cell's endpoint keeps its name: a configured one of the same name
	// is refused while the cell runs, and the cell survives every reload.
	if err := l.AddCellEndpoint(&Endpoint{Name: "cell-x", Kind: "llamacpp"}, []string{"lane-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reload(); err != nil || l.Endpoint("cell-x") == nil || !l.MayUse("lane-1", "cell-x") {
		t.Fatalf("the cell did not survive a reload: %v", err)
	}
	reloadDir(t, dir, map[string]string{"endpoints/cell-x.toml": endpoint("8270")})
	if _, err := l.Reload(); err == nil || !strings.Contains(err.Error(), "running cell") {
		t.Fatalf("a configured endpoint took a cell's name: %v", err)
	}
}
