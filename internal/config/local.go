package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

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

	// cells are the endpoints mode C cells were given (065 §3.1), each
	// with the lanes that may use it. They are not in the directory: the
	// daemon adds them while it runs, so they are read through Endpoint
	// and MayUse, under the lock.
	mu    sync.RWMutex
	cells map[string]cellEndpoint
}

type cellEndpoint struct {
	endpoint *Endpoint
	lanes    []string
}

// Endpoint returns the endpoint called name, a configured one or a cell's;
// nil when there is none.
func (l *Local) Endpoint(name string) *Endpoint {
	if e := l.Endpoints[name]; e != nil {
		return e
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cells[name].endpoint
}

// MayUse says whether lane may use the endpoint called name.
func (l *Local) MayUse(lane, name string) bool {
	if ln := l.Lanes[lane]; ln != nil && slices.Contains(ln.Endpoints, name) {
		return true
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	c, ok := l.cells[name]
	return ok && slices.Contains(c.lanes, lane)
}

// AddCellEndpoint makes a cell's endpoint usable from lanes. Its name must
// not be one of the configured endpoints'; a cell's endpoint of the same
// name is replaced.
func (l *Local) AddCellEndpoint(e *Endpoint, lanes []string) error {
	if l.Endpoints[e.Name] != nil {
		return fmt.Errorf("config: endpoint %s is configured; a cell cannot replace it", e.Name)
	}
	if !endpointKinds[e.Kind] {
		return fmt.Errorf("config: endpoint %s: kind must be %s", e.Name, kindList)
	}
	e.Cell = true
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cells == nil {
		l.cells = map[string]cellEndpoint{}
	}
	l.cells[e.Name] = cellEndpoint{endpoint: e, lanes: slices.Clone(lanes)}
	return nil
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
	// Kind is ollama, xollama, openai-compatible or llamacpp (an
	// OpenAI-compatible llama-server).
	Kind string `toml:"kind"`
	// BaseURL is the server's address as the lane's containers reach it.
	BaseURL string `toml:"base_url"`
	// Host is the neutral name of the host the server runs on.
	Host string `toml:"host"`
	// Capacity is how many runs it serves at once.
	Capacity int `toml:"capacity"`
	// APIKeyFile holds a credential; relative to the directory. Optional.
	APIKeyFile string `toml:"api_key_file"`
	// UnloadOthers lets the daemon ask an Ollama or xOllama server to drop
	// whatever other model it holds before a run starts, so that arms on
	// different models can follow each other on one device (D79). Off
	// unless stated: on a device shared with other work it pushes that
	// work's model out.
	UnloadOthers bool `toml:"unload_others"`
	// Hardware is what the server runs on, as the installation's owner
	// declares it. It is recorded in every run as declared, not measured.
	Hardware Hardware `toml:"hardware"`
	// Cell marks the endpoint of a mode C cell: the caller serves the
	// model there and automaticum never loads or unloads anything on it
	// (065 §2).
	Cell bool `toml:"-"`
}

// Hardware is the [hardware] table of an endpoint.
type Hardware struct {
	// GPU names the device the endpoint's models run on.
	GPU string `toml:"gpu"`
	// VRAMGB is that device's memory.
	VRAMGB float64 `toml:"vram_gb"`
	// Notes is free text: other devices, a driver, a power limit.
	Notes string `toml:"notes"`
}

var endpointKinds = map[string]bool{"ollama": true, "xollama": true, "openai-compatible": true, "llamacpp": true}

const kindList = "ollama, xollama, openai-compatible or llamacpp"

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
			bad("endpoint %s: kind must be %s", e.Name, kindList)
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
