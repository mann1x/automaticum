package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Local is the configuration of one installation: the directory given to the
// daemon with --config. It holds addresses, paths and device bindings, so it
// is never tracked in the repository.
type Local struct {
	Dir       string
	Daemon    Daemon
	Lanes     map[string]*Lane
	Endpoints map[string]*Endpoint
}

// Daemon is automaticum.toml.
type Daemon struct {
	// DataRoot holds the store's live tier, workspaces and artefacts. It
	// must be on a real disk with room to spare.
	DataRoot string `toml:"data_root"`
	// ArchiveRoot is the store's archive tier; optional.
	ArchiveRoot string `toml:"archive_root"`
	// FreeFloorGB is the free space under which the daemon starts no run.
	FreeFloorGB int `toml:"free_floor_gb"`
	// Listen is the API's address. Anything but a loopback address needs
	// a token.
	Listen string `toml:"listen"`
	// TokenFile holds the API's write token; relative to the directory.
	TokenFile string `toml:"token_file"`
	// Repo is the checkout holding arms/, tasks/ and images/.
	Repo string `toml:"repo"`
	// DockerSocket is the engine's socket; empty means the default.
	DockerSocket string `toml:"docker_socket"`
	// Notify says where a refusing or waiting gate is told to (050 §2).
	Notify Notify `toml:"notify"`
}

// Notify is the [notify] table of automaticum.toml.
type Notify struct {
	// Webhook is a URL that gets one JSON object per notification, by
	// POST. Empty means the daemon's log only.
	Webhook string `toml:"webhook"`
}

// Lane is lanes/<name>.toml: an isolated place where one run executes.
type Lane struct {
	Name string `toml:"-"`
	// Host is the host's neutral name. Only the daemon's own host can run
	// a lane until agent mode exists.
	Host string `toml:"host"`
	// Endpoints lists the endpoints the lane may use.
	Endpoints []string `toml:"endpoints"`
	// User is the "uid:gid" the lane's containers run as.
	User string `toml:"user"`
	// Limits of the lane's containers.
	CPUs     float64 `toml:"cpus"`
	MemoryGB float64 `toml:"memory_gb"`
	Pids     int64   `toml:"pids"`
	// Labels are free-form, for placement later.
	Labels map[string]string `toml:"labels"`
}

// Endpoint is endpoints/<name>.toml: a model server.
type Endpoint struct {
	Name string `toml:"-"`
	// Kind is ollama, xollama or openai-compatible.
	Kind string `toml:"kind"`
	// BaseURL is the server's address as the lane's containers reach it.
	BaseURL string `toml:"base_url"`
	// Host is the neutral name of the host the server runs on.
	Host string `toml:"host"`
	// Capacity is how many runs it serves at once.
	Capacity int `toml:"capacity"`
	// APIKeyFile holds a credential; relative to the directory. Optional.
	APIKeyFile string `toml:"api_key_file"`
}

var endpointKinds = map[string]bool{"ollama": true, "xollama": true, "openai-compatible": true}

func decodeStrict(path string, v any) error {
	md, err := toml.DecodeFile(path, v)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return fmt.Errorf("config: %s: unknown fields: %s", path, strings.Join(keys, ", "))
	}
	return nil
}

// LoadLocal reads and validates an installation's configuration directory.
func LoadLocal(dir string) (*Local, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	l := &Local{Dir: dir, Lanes: map[string]*Lane{}, Endpoints: map[string]*Endpoint{}}
	if err := decodeStrict(filepath.Join(dir, "automaticum.toml"), &l.Daemon); err != nil {
		return nil, err
	}
	d := &l.Daemon
	if d.Listen == "" {
		d.Listen = "127.0.0.1:7411"
	}
	if d.TokenFile == "" {
		d.TokenFile = "token"
	}
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	for name, p := range map[string]string{"data_root": d.DataRoot, "repo": d.Repo} {
		if !filepath.IsAbs(p) {
			bad("automaticum.toml: %s must be an absolute path", name)
		}
	}
	if d.ArchiveRoot != "" && !filepath.IsAbs(d.ArchiveRoot) {
		bad("automaticum.toml: archive_root must be an absolute path")
	}
	if d.FreeFloorGB <= 0 {
		bad("automaticum.toml: free_floor_gb must be stated and positive")
	}

	paths, _ := filepath.Glob(filepath.Join(dir, "endpoints", "*.toml"))
	for _, p := range paths {
		e := &Endpoint{Name: strings.TrimSuffix(filepath.Base(p), ".toml")}
		if err := decodeStrict(p, e); err != nil {
			return nil, err
		}
		if !armNameRE.MatchString(e.Name) {
			bad("endpoint %q: not a valid name", e.Name)
		}
		if !endpointKinds[e.Kind] {
			bad("endpoint %s: kind must be ollama, xollama or openai-compatible", e.Name)
		}
		if u, err := url.Parse(e.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("endpoint %s: base_url must be an http or https address", e.Name)
		} else if h := u.Hostname(); h == "0.0.0.0" || h == "::" {
			bad("endpoint %s: base_url names every interface; give the address the lane reaches", e.Name)
		}
		if e.Host == "" {
			bad("endpoint %s: host is not stated", e.Name)
		}
		if e.Capacity < 1 {
			bad("endpoint %s: capacity must be 1 or more", e.Name)
		}
		l.Endpoints[e.Name] = e
	}
	paths, _ = filepath.Glob(filepath.Join(dir, "lanes", "*.toml"))
	for _, p := range paths {
		ln := &Lane{Name: strings.TrimSuffix(filepath.Base(p), ".toml")}
		if err := decodeStrict(p, ln); err != nil {
			return nil, err
		}
		if !armNameRE.MatchString(ln.Name) {
			bad("lane %q: not a valid name", ln.Name)
		}
		if ln.Host == "" {
			bad("lane %s: host is not stated", ln.Name)
		}
		if ln.User == "" {
			ln.User = "1000:1000"
		}
		if strings.HasPrefix(ln.User, "0:") || ln.User == "0" || ln.User == "root" {
			bad("lane %s: a lane does not run as root", ln.Name)
		}
		if ln.CPUs <= 0 || ln.MemoryGB <= 0 || ln.Pids <= 0 {
			bad("lane %s: cpus, memory_gb and pids must be stated and positive", ln.Name)
		}
		if len(ln.Endpoints) == 0 {
			bad("lane %s: no endpoints", ln.Name)
		}
		for _, e := range ln.Endpoints {
			if l.Endpoints[e] == nil {
				bad("lane %s: no endpoint %q", ln.Name, e)
			}
		}
		l.Lanes[ln.Name] = ln
	}
	if len(l.Lanes) == 0 {
		bad("no lanes: lanes/*.toml is empty")
	}
	if len(problems) > 0 {
		return nil, errors.New("config: " + dir + ":\n  - " + strings.Join(problems, "\n  - "))
	}
	return l, nil
}

// Token returns the API's write token. The file must exist, be readable by
// its owner only, and hold at least 32 characters.
func (l *Local) Token() (string, error) {
	p := l.Daemon.TokenFile
	if !filepath.IsAbs(p) {
		p = filepath.Join(l.Dir, p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("config: token: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("config: %s is readable by others; make it mode 0600", p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if len(t) < 32 {
		return "", fmt.Errorf("config: the token in %s is shorter than 32 characters", p)
	}
	return t, nil
}

// APIKey returns the endpoint's credential, or "" when it has none.
func (l *Local) APIKey(e *Endpoint) (string, error) {
	if e.APIKeyFile == "" {
		return "", nil
	}
	p := e.APIKeyFile
	if !filepath.IsAbs(p) {
		p = filepath.Join(l.Dir, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("config: endpoint %s: %w", e.Name, err)
	}
	return strings.TrimSpace(string(b)), nil
}
