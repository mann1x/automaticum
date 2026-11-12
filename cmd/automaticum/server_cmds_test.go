package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/run"
)

const serverTag = "gemma-4-26b-a4b-it-bm889-sft11-sfinal-bf16-eos106"

// facts is a llama-server started with a path alias and a tag: its id is
// the path, the first alias in sorted order.
func facts(t *testing.T) *serverFacts {
	t.Helper()
	f := &serverFacts{Base: "http://127.0.0.1:8244", Props: &run.Props{NCtx: 8192, BuildInfo: "b1-x", ModelAlias: "/m/sft11.gguf," + serverTag}}
	if err := json.Unmarshal([]byte(`{"data":[{"id":"/m/sft11.gguf","aliases":["/m/sft11.gguf","`+serverTag+`"]}]}`), &f.Models); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPickTag(t *testing.T) {
	f := facts(t)
	if tag, err := pickTag(f, ""); err != nil || tag != serverTag {
		t.Errorf("the tag beside a path alias: %q %v", tag, err)
	}
	if _, err := pickTag(f, "other"); err == nil || !strings.Contains(err.Error(), "it answers to /m/sft11.gguf, "+serverTag) {
		t.Errorf("a name the server does not answer to: %v", err)
	}
	f.Models.Data[0].Aliases = append(f.Models.Data[0].Aliases, "second")
	if _, err := pickTag(f, ""); err == nil || !strings.Contains(err.Error(), "say which with --model") {
		t.Errorf("two tags: %v", err)
	}
	if tag, err := pickTag(f, "second"); err != nil || tag != "second" {
		t.Errorf("--model: %q %v", tag, err)
	}
	only := &serverFacts{}
	json.Unmarshal([]byte(`{"data":[{"id":"/m/x.gguf"}]}`), &only.Models)
	if _, err := pickTag(only, ""); err == nil || !strings.Contains(err.Error(), "--alias") {
		t.Errorf("only a path: %v", err)
	}
}

func TestContainerAddress(t *testing.T) {
	for _, tc := range []struct {
		url     string
		answers bool
		base    string
		forward string
	}{
		{"http://127.0.0.1:8244", false, "http://172.17.0.1:8244/v1", "127.0.0.1:8244"},
		{"http://localhost:8244/v1", false, "http://172.17.0.1:8244/v1", "127.0.0.1:8244"},
		{"http://127.0.0.1:8244/", true, "http://172.17.0.1:8244/v1", ""},
		{"http://10.0.0.5:8244", false, "http://10.0.0.5:8244/v1", ""},
	} {
		base, fwd, err := containerAddress(tc.url, "172.17.0.1", tc.answers)
		if err != nil || base != tc.base || fwd != tc.forward {
			t.Errorf("%s (answers on the gateway %v): %q %q %v", tc.url, tc.answers, base, fwd, err)
		}
	}
	if _, _, err := containerAddress("http://127.0.0.1", "172.17.0.1", false); err == nil {
		t.Error("a loopback address without a port")
	}
}

func TestAttachEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"host = \"h\"\nendpoints = [\"stub\"]\ncpus = 4\n": "host = \"h\"\nendpoints = [\"stub\", \"server-8244\"]\ncpus = 4\n",
		"endpoints = [\"a\", \"b\",]   # the GPU's\n":      "endpoints = [\"a\", \"b\", \"server-8244\"]   # the GPU's\n",
		"endpoints = []\n": "endpoints = [\"server-8244\"]\n",
		"# endpoints = [\"x\"]\nendpoints = [\"stub\"]\n": "# endpoints = [\"x\"]\nendpoints = [\"stub\", \"server-8244\"]\n",
		"endpoints = [\n  \"stub\",\n]\n":                 "",
	} {
		got, err := attachEndpoint(in, "server-8244")
		if want == "" {
			if err == nil {
				t.Errorf("a list over several lines was edited: %q", got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %q %v", in, got, err)
		}
	}
}

// serverConfig is an installation's directory with the stub endpoint, two
// lanes and the repository's arms, its daemon at listen.
func serverConfig(t *testing.T, listen string) *config.Local {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"automaticum.toml":    "data_root = \"/d\"\nrepo = \"/r\"\nfree_floor_gb = 1\nlisten = \"" + listen + "\"\n",
		"token":               strings.Repeat("t", 32),
		"endpoints/stub.toml": "kind = \"ollama\"\nbase_url = \"http://172.17.0.1:7412\"\nhost = \"linux-gpu-1\"\ncapacity = 1\n",
		"lanes/lane-1.toml":   "host = \"linux-gpu-1\"\nendpoints = [\"stub\"]\ncpus = 4\nmemory_gb = 8\npids = 1024\n",
		"lanes/lane-2.toml":   "# GPU 1\nhost = \"linux-gpu-1\"\nendpoints = [\"stub\"]\ncpus = 4\nmemory_gb = 8\npids = 1024\n",
	}
	for _, a := range []string{"base.toml", "stub.toml"} {
		b, err := os.ReadFile(filepath.Join("../../examples/config/arms", a))
		if err != nil {
			t.Fatal(err)
		}
		files["arms/"+a] = string(b)
	}
	for name, text := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l, err := config.LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The plan for a loopback-only llama-server: a new endpoint the daemon
// forwards, the idle lane attached, an arm on the server's own sampler
// whose window fits the server's slot; applied, it loads and validates;
// planned again, everything is reused; undone, nothing is left.
func TestPlanServer(t *testing.T) {
	l := serverConfig(t, "127.0.0.1:7411")
	st := &daemon.Status{Lanes: []daemon.LaneStatus{{Lane: "lane-1", State: "running"}, {Lane: "lane-2", State: "idle"}}}
	wall := 1800
	o := serverOpts{Extends: "stub", Sampler: "tag", Tasks: []string{"custom/manic_miner"}, Count: 10,
		Limits: &run.Override{WallS: &wall}, Today: time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)}
	p, err := planServer(l, facts(t), o, st, "172.17.0.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tag != serverTag || p.Kind != "llamacpp" || p.Endpoint != "server-8244" || !p.EndpointNew || p.ForwardTo != "127.0.0.1:8244" ||
		p.BaseURL != "http://172.17.0.1:8244/v1" || p.Lane != "lane-2" || !p.LaneAttach || !p.ArmNew {
		t.Fatalf("plan: %+v", p)
	}
	if p.Arm != "stub-server-8244-"+serverTag || p.Key != p.Arm+"-manic_miner-n10-w1800-20261112" {
		t.Errorf("arm %q key %q", p.Arm, p.Key)
	}
	for _, want := range []string{"extends = \"stub\"", "endpoint = \"server-8244\"", "model = \"" + serverTag + "\"", "provider = \"openai-compatible\"", "context_window = 8192", "[sampler]\nowner = \"tag\""} {
		if !strings.Contains(p.ArmText, want) {
			t.Errorf("arm text lacks %q:\n%s", want, p.ArmText)
		}
	}
	if len(p.ArmNotes) != 2 {
		t.Errorf("notes: %v", p.ArmNotes)
	}
	undo, err := p.apply(l)
	if err != nil {
		t.Fatal(err)
	}
	l2, err := config.LoadLocal(l.Dir)
	if err != nil {
		t.Fatalf("the written configuration does not load: %v", err)
	}
	if e := l2.Endpoint("server-8244"); e == nil || e.ForwardTo != "127.0.0.1:8244" || !l2.MayUse("lane-2", "server-8244") || l2.MayUse("lane-1", "server-8244") {
		t.Errorf("endpoint %+v, lane-2 may use it %v", e, l2.MayUse("lane-2", "server-8244"))
	}
	lane, _ := os.ReadFile(filepath.Join(l.Dir, "lanes/lane-2.toml"))
	if !strings.HasPrefix(string(lane), "# GPU 1\n") {
		t.Errorf("the lane's other lines were not kept:\n%s", lane)
	}
	again, err := planServer(l2, facts(t), o, st, "172.17.0.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.EndpointNew || again.ArmNew || again.LaneAttach || again.Endpoint != "server-8244" || again.Lane != "lane-2" {
		t.Errorf("planned again: %+v", again)
	}
	// An arm of that name with other values is not overwritten.
	o.Sampler = "arm"
	if _, err := planServer(l2, facts(t), o, st, "172.17.0.1", false); err == nil || !strings.Contains(err.Error(), "already exists with other values") {
		t.Errorf("an arm with other values: %v", err)
	}
	undo()
	if _, err := os.Stat(filepath.Join(l.Dir, "endpoints/server-8244.toml")); err == nil {
		t.Error("undo left the endpoint")
	}
	if b, _ := os.ReadFile(filepath.Join(l.Dir, "lanes/lane-2.toml")); string(b) != p.LaneOld {
		t.Errorf("undo did not put the lane back:\n%s", b)
	}
	// Nothing idle: the lanes are named.
	st.Lanes[1].State = "running"
	if _, err := planServer(l, facts(t), o, st, "172.17.0.1", false); err == nil || !strings.Contains(err.Error(), "lane-1 (running)") {
		t.Errorf("no idle lane: %v", err)
	}
}

// The whole command against a llama-server and a daemon: the files are
// written, the daemon reloads them, and the runs are started with the
// proposed key on the lane the plan chose.
func TestRunServer(t *testing.T) {
	llama := http.NewServeMux()
	llama.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":[{"id":"/m/sft11.gguf","aliases":["/m/sft11.gguf","`+serverTag+`"]}]}`)
	})
	llama.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"default_generation_settings":{"n_ctx":65536},"model_alias":"`+serverTag+`","build_info":"b1-x"}`)
	})
	srv := httptest.NewServer(llama)
	defer srv.Close()

	var l *config.Local
	var started daemon.StartRequest
	reloads := 0
	dm := http.NewServeMux()
	dm.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(daemon.Status{Lanes: []daemon.LaneStatus{{Lane: "lane-1", State: "idle"}, {Lane: "lane-2", State: "idle"}}})
	})
	dm.HandleFunc("POST /v1/config/reload", func(w http.ResponseWriter, _ *http.Request) {
		reloads++
		res := daemon.ReloadResult{}
		if _, err := config.LoadLocal(l.Dir); err != nil {
			res.Error = err.Error()
		}
		json.NewEncoder(w).Encode(res)
	})
	dm.HandleFunc("POST /v1/runs", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&started)
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(daemon.StartResponse{Batch: "k-" + started.Key, RunIDs: []string{"r1"}, Lane: started.Lane})
	})
	dsrv := httptest.NewServer(dm)
	defer dsrv.Close()
	l = serverConfig(t, strings.TrimPrefix(dsrv.URL, "http://"))
	t.Setenv("AUTOMATICUM_CONFIG", l.Dir)
	defer func(g func(context.Context, string) (string, error)) { bridgeGateway = g }(bridgeGateway)
	// The server listens on 127.0.0.1, which is also the "gateway" here:
	// it answers there, so nothing is forwarded.
	bridgeGateway = func(context.Context, string) (string, error) { return "127.0.0.1", nil }

	args := []string{"--config", l.Dir, "--url", srv.URL, "--task", "custom/manic_miner", "--extends", "stub", "-n", "10", "--wall-s", "1800", "--yes"}
	if err := runServer(context.Background(), append(args, "--dry-run")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.Dir, "endpoints")); err == nil {
		if m, _ := filepath.Glob(filepath.Join(l.Dir, "endpoints", "server-*")); len(m) != 0 || reloads != 0 {
			t.Fatalf("a dry run wrote %v, reloaded %d times", m, reloads)
		}
	}
	if err := runServer(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	arm := "stub-server-" + port + "-" + serverTag
	if started.Arm != arm || started.Count != 10 || started.Lane != "lane-1" || started.Key != arm+"-manic_miner-n10-w1800-"+time.Now().Format("20060102") ||
		started.Limits == nil || *started.Limits.WallS != 1800 || len(started.Tasks) != 1 {
		t.Errorf("started %+v", started)
	}
	e, err := os.ReadFile(filepath.Join(l.Dir, "endpoints", "server-"+port+".toml"))
	if err != nil || strings.Contains(string(e), "forward_to") || !strings.Contains(string(e), "base_url = \"http://127.0.0.1:"+port+"/v1\"") {
		t.Errorf("endpoint %s %v", e, err)
	}
	if reloads != 1 {
		t.Errorf("reloads %d", reloads)
	}
}
