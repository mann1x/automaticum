package gate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/sdk"
	"github.com/mann1x/automaticum/internal/task"
)

// server is a model server that answers the four questions a probe asks.
type server struct {
	caps   []string
	digest string
	loaded []map[string]any // what /api/ps lists
	posts  int              // requests other than the probe's
}

func (s *server) start(t *testing.T) string {
	mux := http.NewServeMux()
	say := func(v any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(v) }
	}
	mux.HandleFunc("GET /api/version", say(map[string]string{"version": "1.0"}))
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		say(map[string]any{"models": []map[string]any{{"name": "m:1", "digest": s.digest}}})(w, r)
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, r *http.Request) {
		say(map[string]any{"template": "{{ .Prompt }}", "renderer": "r1", "parser": "p1", "capabilities": s.caps,
			"parameters": "think_budget \"medium\"\ntemperature 1\nnum_ctx 8192"})(w, r)
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, r *http.Request) { say(map[string]any{"models": s.loaded})(w, r) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.posts++; http.NotFound(w, r) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

type engine struct{ missing bool }

func (e engine) ImageInspect(context.Context, string) (docker.Image, error) {
	if e.missing {
		return docker.Image{}, errors.New("no such image")
	}
	return docker.Image{}, nil
}

// bundles is the provisioned SDK builds: one, unless err says otherwise.
type bundles struct{ err error }

func (b bundles) Check(name string, deep bool) (*sdk.Def, *sdk.Record, error) {
	if b.err != nil {
		return nil, nil, b.err
	}
	return &sdk.Def{Name: name, Version: name, Source: "registry"}, &sdk.Record{BundleSHA256: "0123456789abcdef"}, nil
}
func (b bundles) Path(name string) string { return "/bundles/" + name }

// rig is one arm on one task on one lane, with everything in order.
func rig(t *testing.T, s *server) *Input {
	t.Helper()
	arm, err := config.LoadArm("../../examples/config/arms", "stub")
	if err != nil {
		t.Fatal(err)
	}
	arm.Model.Model = "m:1"
	tk, err := task.Find(task.Roots{Public: "../../tasks"}, "demo/answer")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := &task.Env{Artefacts: filepath.Join(dir, "artefacts"), Scratch: filepath.Join(dir, "scratch")}
	if err := env.RecordProof(tk, 2, 3); err != nil {
		t.Fatal(err)
	}
	local := &config.Local{Dir: dir,
		Daemon:    config.Daemon{DataRoot: dir, FreeFloorGB: 10},
		Lanes:     map[string]*config.Lane{"lane-1": {Name: "lane-1", Host: "linux-gpu-1", Endpoints: []string{arm.Endpoint}}},
		Endpoints: map[string]*config.Endpoint{arm.Endpoint: {Name: arm.Endpoint, Kind: "ollama", BaseURL: s.start(t)}},
	}
	return &Input{Local: local, Lane: "lane-1", Arm: arm, Task: tk, Image: "automaticum-lane:base",
		Engine: engine{}, SDK: bundles{}, Tasks: env, RunsDir: dir,
		Free:  func(string) (uint64, error) { return 100 << 30, nil },
		Tmpfs: func(string) (bool, error) { return false, nil },
	}
}

func byGate(results []Result) map[string]Result {
	out := map[string]Result{}
	for _, r := range results {
		out[r.Gate] = r
	}
	return out
}

func TestAllInOrder(t *testing.T) {
	s := &server{caps: []string{"completion", "tools", "thinking"}, digest: "abc123"}
	in := rig(t, s)
	results := Run(context.Background(), in)
	if w := Worst(results); w != nil {
		t.Fatalf("a gate stood in the way: %+v", w)
	}
	got := byGate(results)
	for _, name := range []string{"lane", "free", "model_tag", "template", "sampler", "thinking", "harness_build", "host", "oracle", "workspace", "bind_address"} {
		if got[name].Status != Pass || got[name].Detail == "" || got[name].Version == "" {
			t.Errorf("%s: %+v", name, got[name])
		}
	}
	// What cannot be looked at says so and is not a pass.
	for _, name := range []string{"gpu_residency", "gpu_ownership", "server_log_level", "expert"} {
		if got[name].Status != Unchecked || got[name].Detail == "" {
			t.Errorf("%s: %+v", name, got[name])
		}
	}
	if !strings.Contains(got["template"].Detail, `renderer "r1"`) || !strings.Contains(got["thinking"].Detail, "medium") {
		t.Errorf("details: %q / %q", got["template"].Detail, got["thinking"].Detail)
	}
	if s.posts != 0 {
		t.Errorf("the gates sent %d requests beyond the probe: a gate never generates and never loads a model", s.posts)
	}
	if len(results) != len(Builtin()) {
		t.Errorf("%d answers for %d gates", len(results), len(Builtin()))
	}
}

// Each fault, the gate that must catch it, and what it must say.
func TestFaults(t *testing.T) {
	healthy := func() *server { return &server{caps: []string{"completion", "tools", "thinking"}, digest: "abc123"} }
	other := []map[string]any{{"name": "someone-elses:model", "size": 10, "size_vram": 10}}
	partly := []map[string]any{{"name": "m:1", "size": 10, "size_vram": 6}}
	fully := []map[string]any{{"name": "m:1", "size": 10, "size_vram": 10}}
	for _, c := range []struct {
		name   string
		server func(*server)
		input  func(*Input)
		gate   string
		status Status
		says   string
	}{
		{name: "another model is in memory", server: func(s *server) { s.loaded = other }, gate: "free", status: Wait, says: "someone-elses:model"},
		{name: "our model is in memory", server: func(s *server) { s.loaded = fully }, gate: "gpu_residency", status: Pass, says: "fully on the GPU"},
		{name: "partly on the CPU", server: func(s *server) { s.loaded = partly }, gate: "gpu_residency", status: Refuse, says: "6 of 10"},
		{name: "no tools", server: func(s *server) { s.caps = []string{"completion"} }, gate: "template", status: Refuse, says: "tool calls"},
		{name: "digest pinned and other", input: func(in *Input) { in.Arm.Model.Digest = "ffff" }, gate: "model_tag", status: Refuse, says: "pins"},
		{name: "digest pinned and right", input: func(in *Input) { in.Arm.Model.Digest = "abc123" }, gate: "model_tag", status: Pass, says: "as pinned"},
		{name: "model not on the endpoint", input: func(in *Input) { in.Arm.Model.Model = "absent:1" }, gate: "model_tag", status: Refuse, says: "does not have model"},
		{name: "lane may not use the endpoint", input: func(in *Input) { in.Local.Lanes["lane-1"].Endpoints = nil }, gate: "lane", status: Refuse, says: "may not use"},
		{name: "no such lane", input: func(in *Input) { in.Lane = "lane-9" }, gate: "lane", status: Refuse, says: "no lane"},
		{name: "SDK build not provisioned", input: func(in *Input) { in.SDK = bundles{sdk.ErrNotProvisioned} }, gate: "harness_build", status: Refuse, says: "cannot be established"},
		{name: "SDK bundle is not the pinned one", input: func(in *Input) { in.SDK = bundles{sdk.ErrChanged} }, gate: "harness_build", status: Refuse, says: "pinned hash"},
		{name: "no SDK builds on this installation", input: func(in *Input) { in.SDK = nil }, gate: "harness_build", status: Refuse, says: "provisions no SDK builds"},
		{name: "image missing", input: func(in *Input) { in.Engine = engine{missing: true} }, gate: "harness_build", status: Refuse, says: "is not there"},
		{name: "disk under the floor", input: func(in *Input) { in.Free = func(string) (uint64, error) { return 5 << 30, nil } }, gate: "host", status: Refuse, says: "under the floor"},
		{name: "oracle not proven", input: func(in *Input) { os.RemoveAll(filepath.Join(in.Tasks.Artefacts, "proven")) }, gate: "oracle", status: Refuse, says: "oracle verify"},
		{name: "task changed since the proof", input: func(in *Input) { in.Task.Hash = "another" }, gate: "oracle", status: Refuse, says: "was not proven"},
		{name: "runs on a RAM disk", input: func(in *Input) { in.Tmpfs = func(string) (bool, error) { return true, nil } }, gate: "workspace", status: Refuse, says: "RAM-backed"},
		{name: "runs directory gone", input: func(in *Input) { in.RunsDir = filepath.Join(in.RunsDir, "nowhere") }, gate: "workspace", status: Refuse, says: "not writable"},
		{name: "all-interfaces address", input: func(in *Input) { in.Local.Endpoints[in.Arm.Endpoint].BaseURL = "http://0.0.0.0:11434" }, gate: "bind_address", status: Refuse, says: "all-interfaces"},
		{name: "endpoint down", input: func(in *Input) { in.Local.Endpoints[in.Arm.Endpoint].BaseURL = "http://127.0.0.1:1" }, gate: "bind_address", status: Refuse, says: "not reachable"},
		{name: "incomplete arm", input: func(in *Input) { in.Arm.Sampler.Owner = "" }, gate: "sampler", status: Refuse, says: "not complete"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := healthy()
			if c.server != nil {
				c.server(s)
			}
			in := rig(t, s)
			if c.input != nil {
				c.input(in)
			}
			results := Run(context.Background(), in)
			got := byGate(results)[c.gate]
			if got.Status != c.status || !strings.Contains(got.Detail, c.says) {
				t.Errorf("%s: %s %q; want %s saying %q", c.gate, got.Status, got.Detail, c.status, c.says)
			}
			if w := Worst(results); c.status == Pass && w != nil {
				t.Errorf("stood in the way: %+v", w)
			} else if c.status != Pass && (w == nil || w.Status != c.status) {
				t.Errorf("the deciding answer is %+v, want a %s", w, c.status)
			}
			// The private address never appears in what a gate says.
			for _, r := range results {
				if strings.Contains(r.Detail, "127.0.0.1") {
					t.Errorf("%s names the endpoint's address: %q", r.Gate, r.Detail)
				}
			}
		})
	}
}

// A refusal outranks a wait.
func TestWorst(t *testing.T) {
	r := []Result{{Gate: "a", Status: Pass}, {Gate: "b", Status: Wait}, {Gate: "c", Status: Unchecked}, {Gate: "d", Status: Refuse}}
	if w := Worst(r); w == nil || w.Gate != "d" {
		t.Errorf("%+v", w)
	}
	if w := Worst(r[:3]); w == nil || w.Gate != "b" {
		t.Errorf("%+v", w)
	}
	if w := Worst([]Result{{Status: Pass}, {Status: Unchecked}}); w != nil {
		t.Errorf("%+v", w)
	}
}

// The static gates are the ones that need neither endpoint nor lane, and
// asking them asks nothing of the endpoint.
func TestStatic(t *testing.T) {
	s := &server{caps: []string{"completion", "tools", "thinking"}, digest: "abc123"}
	in := rig(t, s)
	results := Static(context.Background(), in)
	got := byGate(results)
	if len(results) != 2 || got["oracle"].Status != Pass || got["harness_build"].Status != Pass {
		t.Fatalf("static gates: %+v", results)
	}
	if s.posts != 0 {
		t.Errorf("the static gates sent the endpoint %d requests", s.posts)
	}
}
