package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/stubmodel"
	"github.com/mann1x/automaticum/internal/task"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		f    Facts
		want store.Verdict
	}{
		{"oracle passed", Facts{OracleRan: true, OraclePassed: true}, store.Pass},
		{"a pass outranks the clock", Facts{OracleRan: true, OraclePassed: true, Limit: "wall", LimitOwner: "arm", LimitBy: "runner"}, store.Pass},
		{"oracle said no", Facts{OracleRan: true}, store.Fail},
		{"limit and no pass", Facts{OracleRan: true, Limit: "idle", LimitOwner: "arm", LimitBy: "harness"}, store.Timeout},
		{"no verdict could be produced", Facts{Aborted: "crash", OracleRan: true, OraclePassed: true}, store.Aborted},
		{"the oracle never answered", Facts{}, store.Aborted},
		{"integrity outranks the oracle", Facts{Invalid: []string{"x"}, OracleRan: true, OraclePassed: true}, store.Invalid},
		{"integrity outranks an abort", Facts{Invalid: []string{"x"}, Aborted: "crash"}, store.Invalid},
		{"never started", Facts{Skipped: "no image", Invalid: []string{"x"}, Aborted: "y"}, store.Skipped},
	}
	for _, c := range cases {
		if got, reason := Decide(c.f); got != c.want || reason == "" {
			t.Errorf("%s: %s (%q), want %s", c.name, got, reason, c.want)
		}
	}
}

func TestStreamLines(t *testing.T) {
	st := newStream(time.Now())
	var seen []string
	st.onLine = func(l []byte) { seen = append(seen, string(l)) }
	// Lines arrive cut anywhere.
	for _, part := range []string{`{"k":"ev`, `ent","event":{}}` + "\n" + `not json` + "\n\n", `{"k":"end","outcome":"comp`, `leted","agent_end":"completed"}` + "\n"} {
		st.Write([]byte(part))
	}
	lines, events, bad, end := st.snapshot()
	if lines != 3 || events != 1 || bad != 1 || end == nil || end.Outcome != OutcomeCompleted || len(seen) != 2 {
		t.Fatalf("lines %d events %d bad %d end %+v seen %v", lines, events, bad, end, seen)
	}
}

// fakeState keeps runs in memory.
type fakeState struct {
	store.State
	mu   sync.Mutex
	runs map[string]store.Run
	refs []store.BlobRef
}

func (s *fakeState) UpsertRun(_ context.Context, r *store.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !r.Verdict.Valid() {
		return fmt.Errorf("not a verdict: %q", r.Verdict)
	}
	s.runs[r.ID] = *r
	return nil
}

func (s *fakeState) AddBlobRefs(_ context.Context, refs []store.BlobRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs = append(s.refs, refs...)
	return nil
}

// fakeBlobs keeps blobs in memory.
type fakeBlobs struct {
	mu   sync.Mutex
	data map[blob.Hash][]byte
}

func (b *fakeBlobs) Put(_ context.Context, r io.Reader) (blob.Hash, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	h := blob.Hash(hex.EncodeToString(sum[:]))
	b.mu.Lock()
	b.data[h] = data
	b.mu.Unlock()
	return h, int64(len(data)), nil
}

// fakeEngine stands in for Docker. The runner is a function; the demo
// task's oracle is done by hand.
type fakeEngine struct {
	label  string
	runner func(ctx context.Context, s docker.Spec, stdout io.Writer) (docker.Result, error)
	specs  []docker.Spec
}

func (e *fakeEngine) ImageInspect(context.Context, string) (docker.Image, error) {
	var im docker.Image
	im.ID = "sha256:image"
	if e.label != "" {
		im.Config.Labels = map[string]string{SDKReleaseLabel: e.label}
	}
	return im, nil
}

func mountOf(s docker.Spec, target string) string {
	for _, m := range s.Mounts {
		if m.Target == target {
			return m.Source
		}
	}
	return ""
}

func (e *fakeEngine) Run(ctx context.Context, s docker.Spec, _ time.Duration, stdout, _ io.Writer, onStart func(string)) (docker.Result, error) {
	e.specs = append(e.specs, s)
	if s.Labels["automaticum"] == "run" {
		if onStart != nil {
			onStart("container-1")
		}
		res, err := e.runner(ctx, s, stdout)
		res.ID, res.ImageID = "container-1", "sha256:image"
		if res.Started.IsZero() {
			res.Started = time.Now()
		}
		return res, err
	}
	// The oracle of the demo task.
	data, _ := os.ReadFile(filepath.Join(mountOf(s, "/w"), "answer.txt"))
	if strings.Contains(string(data), "is 42.") {
		fmt.Fprintln(stdout, "RESULT ok")
	} else {
		fmt.Fprintln(stdout, "RESULT wrong")
	}
	return docker.Result{ID: "oracle-1"}, nil
}

const release = "4.100.250"

func endLine(outcome string, more string) string {
	return fmt.Sprintf(`{"k":"end","outcome":%q,"agent_end":"completed","claimed_success":true,"sdk":{"release":%q,"package_sha256":"abc"},"read_back":"compared"%s}`+"\n", outcome, release, more)
}

// fix writes the right answer, as an agent would.
func fix(s docker.Spec) {
	os.WriteFile(filepath.Join(mountOf(s, containerWorkspace), "answer.txt"), []byte("The answer is 42.\n"), 0o644)
}

type world struct {
	x      *Executor
	engine *fakeEngine
	state  *fakeState
	blobs  *fakeBlobs
	req    Request
	stub   *stubmodel.Server
	model  *string // what the endpoint serves; a test may change it mid-run
}

func newWorld(t *testing.T) *world {
	t.Helper()
	arm, err := config.LoadArm("../../arms", "stub")
	if err != nil {
		t.Fatal(err)
	}
	tk, err := task.Find("../../tasks", "demo/answer")
	if err != nil {
		t.Fatal(err)
	}
	w := &world{engine: &fakeEngine{label: release}, state: &fakeState{runs: map[string]store.Run{}}, blobs: &fakeBlobs{data: map[blob.Hash][]byte{}}}
	// The endpoint: the stub, behind a switch that can change its identity.
	servers := map[string]*stubmodel.Server{
		"stub:latest": stubmodel.New(stubmodel.Script{}),
		"other":       stubmodel.New(stubmodel.Script{Model: "stub:latest", ContextWindow: 999}),
	}
	current := "stub:latest"
	w.model = &current
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { servers[*w.model].ServeHTTP(rw, r) }))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	local := &config.Local{
		Dir:       dir,
		Lanes:     map[string]*config.Lane{"lane-1": {Name: "lane-1", Host: "linux-gpu-1", Endpoints: []string{"stub"}, User: fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), CPUs: 2, MemoryGB: 1, Pids: 64}},
		Endpoints: map[string]*config.Endpoint{"stub": {Name: "stub", Kind: "ollama", BaseURL: srv.URL, Host: "linux-gpu-1", Capacity: 1}, "elsewhere": {Name: "elsewhere", Kind: "ollama", BaseURL: srv.URL}},
	}
	w.x = &Executor{
		Engine: w.engine, Blobs: w.blobs, State: w.state, Local: local, RunsDir: filepath.Join(dir, "runs"), Version: "test",
		Tasks:    &task.Env{Exec: w.engine, Artefacts: filepath.Join(dir, "artefacts"), Scratch: filepath.Join(dir, "scratch")},
		IdleTick: 20 * time.Millisecond, Grace: time.Second,
	}
	w.req = Request{Arm: arm, Task: tk, Lane: "lane-1", Image: "lane:test"}
	return w
}

func (w *world) run(t *testing.T, ctx context.Context) (*store.Run, provenance) {
	t.Helper()
	r, err := w.x.Execute(ctx, w.req)
	if err != nil {
		t.Fatal(err)
	}
	var p provenance
	if err := json.Unmarshal(r.Provenance, &p); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.state.runs[r.ID]; !ok || got.Verdict != r.Verdict {
		t.Fatalf("the record was not written: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(w.x.RunsDir, r.ID)); err == nil {
		t.Errorf("the run's directory was left behind")
	}
	return r, p
}

func (w *world) kinds() map[string]int {
	out := map[string]int{}
	for _, ref := range w.state.refs {
		out[ref.Kind]++
	}
	return out
}

// One test per row of the verdict table, with the fault injected.
func TestVerdicts(t *testing.T) {
	event := `{"k":"event","event":{"type":"agent_event"}}` + "\n"
	type runner = func(ctx context.Context, s docker.Spec, stdout io.Writer) (docker.Result, error)
	cases := []struct {
		name   string
		setup  func(w *world)
		runner runner
		want   store.Verdict
		reason string
	}{
		{"pass", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			fix(s)
			io.WriteString(out, event+endLine(OutcomeCompleted, ""))
			return docker.Result{}, nil
		}, store.Pass, "oracle passed"},
		{"fail", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, event+endLine(OutcomeCompleted, ""))
			return docker.Result{}, nil
		}, store.Fail, "said no"},
		{"pass although the runner's clock fired", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			fix(s)
			io.WriteString(out, event+endLine(OutcomeWallLimit, ""))
			return docker.Result{}, nil
		}, store.Pass, "wall limit had fired"},
		{"timeout by the runner's limit", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, event+endLine(OutcomeWallLimit, ""))
			return docker.Result{}, nil
		}, store.Timeout, "enforced by the runner"},
		{"timeout by the outer kill", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, event)
			return docker.Result{TimedOut: true, ExitCode: 137}, nil
		}, store.Timeout, "enforced by the harness"},
		{"timeout by the idle limit", func(w *world) { one := 1; w.req.Arm.Limits.IdleS = &one }, func(ctx context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, event)
			<-ctx.Done()
			return docker.Result{ExitCode: 137}, ctx.Err()
		}, store.Timeout, "idle limit"},
		{"aborted: crash", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			fix(s)
			io.WriteString(out, event)
			return docker.Result{ExitCode: 1}, nil
		}, store.Aborted, "without its last line"},
		{"aborted: out of memory", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, event)
			return docker.Result{ExitCode: 137, OOMKilled: true}, nil
		}, store.Aborted, "out of memory"},
		{"aborted: empty stream", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			fix(s)
			io.WriteString(out, endLine(OutcomeCompleted, ""))
			return docker.Result{}, nil
		}, store.Aborted, "event stream is empty"},
		{"aborted: runner error", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, event+endLine(OutcomeError, `,"reason":"endpoint gave 503"`))
			return docker.Result{ExitCode: 1}, nil
		}, store.Aborted, "endpoint gave 503"},
		{"invalid: resolved value differs from the arm", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			fix(s)
			io.WriteString(out, endLine(OutcomeInvalid, `,"problems":["compaction.enabled: arm true, resolved false"]`))
			return docker.Result{ExitCode: 4}, nil
		}, store.Invalid, "compaction.enabled"},
		{"invalid: the run used another SDK release", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			fix(s)
			io.WriteString(out, event+strings.Replace(endLine(OutcomeCompleted, ""), release, "4.100.249", 1))
			return docker.Result{}, nil
		}, store.Invalid, "4.100.249"},
		{"skipped: the runner refused", nil, func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
			io.WriteString(out, endLine(OutcomeRefused, `,"reason":"installed SDK is 4.100.249"`))
			return docker.Result{ExitCode: 3}, nil
		}, store.Skipped, "refused"},
		{"skipped: image without a release label", func(w *world) { w.engine.label = "" }, nil, store.Skipped, "cannot be established"},
		{"skipped: image with another release", func(w *world) { w.engine.label = "4.100.249" }, nil, store.Skipped, "carries SDK release 4.100.249"},
		{"skipped: lane may not use the endpoint", func(w *world) { w.req.Arm.Endpoint = "elsewhere" }, nil, store.Skipped, "may not use"},
		{"skipped: endpoint lacks the model", func(w *world) { w.req.Arm.Model.Model = "absent:1" }, nil, store.Skipped, "does not have model"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			if c.setup != nil {
				c.setup(w)
			}
			w.engine.runner = c.runner
			r, p := w.run(t, context.Background())
			if r.Verdict != c.want || !strings.Contains(r.VerdictReason, c.reason) {
				t.Fatalf("verdict %s (%s), want %s (…%s…)", r.Verdict, r.VerdictReason, c.want, c.reason)
			}
			if c.runner == nil {
				if len(w.engine.specs) != 0 {
					t.Errorf("a skipped run started a container")
				}
				return
			}
			if p.Container.ID != "container-1" || p.EndpointBefore == nil || p.EndpointBefore.Digest == "" || p.Workspace.Before == "" {
				t.Errorf("provenance: %+v", p)
			}
			k := w.kinds()
			if k["workspace_before"] != 1 || k["workspace_after"] != 1 || k["events"] != 1 || k["runner_stderr"] != 1 {
				t.Errorf("blobs: %v", k)
			}
		})
	}
}

func TestRecord(t *testing.T) {
	w := newWorld(t)
	var started *store.Run
	var lines int
	w.req.OnStart = func(r *store.Run, id string) { started = r }
	w.req.OnLine = func([]byte) { lines++ }
	w.engine.runner = func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
		fix(s)
		outDir := mountOf(s, containerOut)
		os.WriteFile(filepath.Join(outDir, "wire.jsonl"), []byte(`{"seq":1}`+"\n"), 0o644)
		os.MkdirAll(filepath.Join(outDir, "data", "sessions"), 0o755)
		os.WriteFile(filepath.Join(outDir, "data", "sessions", "s.json"), []byte("{}"), 0o644)
		os.WriteFile(filepath.Join(mountOf(s, containerWorkspace), "notes.txt"), []byte("x"), 0o644)
		// The job is what the runner was given.
		var job Job
		data, err := os.ReadFile(filepath.Join(mountOf(s, containerJob), jobFile))
		if err != nil || json.Unmarshal(data, &job) != nil {
			return docker.Result{}, fmt.Errorf("job: %v", err)
		}
		if job.Protocol != Protocol || job.SDKRelease != release || job.WallS != 300 || job.Endpoint.Name != "stub" || job.Prompt == "" || job.DefaultAnswer != "Continue." {
			return docker.Result{}, fmt.Errorf("job: %+v", job)
		}
		io.WriteString(out, `{"k":"event","event":{}}`+"\n"+endLine(OutcomeCompleted, `,"requests":2`))
		return docker.Result{}, nil
	}
	r, p := w.run(t, context.Background())
	if r.Verdict != store.Pass {
		t.Fatalf("%s: %s", r.Verdict, r.VerdictReason)
	}
	if started == nil || lines != 2 {
		t.Errorf("OnStart %v, lines %d", started, lines)
	}
	if r.Harness != release || p.SDK.Release != release || p.SDK.ImageLabel != release || p.SDK.PackageSHA256 != "abc" {
		t.Errorf("SDK release not recorded: %q %+v", r.Harness, p.SDK)
	}
	if r.Host != "linux-gpu-1" || r.Model != "stub:latest" || r.Provider != "ollama" || r.AgentEnd != "completed" || r.ClaimedSuccess == nil || !*r.ClaimedSuccess || r.ArmID == "" || r.TaskVersion != 1 {
		t.Errorf("record: %+v", r)
	}
	if p.Limits.Wall != (limit{300, "arm"}) || p.Limits.Idle != (limit{120, "arm"}) {
		t.Errorf("limits: %+v", p.Limits)
	}
	if fmt.Sprint(p.Workspace.Added, p.Workspace.Changed, p.Workspace.Removed) != "[notes.txt] [answer.txt] []" {
		t.Errorf("workspace: %+v", p.Workspace)
	}
	if p.Oracle == nil || !p.Oracle.Passed || p.Oracle.Output != "" || p.EndpointAfter == nil || p.Runner.Requests != 2 {
		t.Errorf("provenance: %+v", p)
	}
	k := w.kinds()
	if k["wire"] != 1 || k["session"] != 1 || k["oracle"] != 1 {
		t.Errorf("blobs: %v", k)
	}
	// The lane's container: limits, no privilege asked for, three mounts.
	s := w.engine.specs[0]
	if s.MemoryBytes != 1<<30 || s.NanoCPUs != 2e9 || s.PidsLimit != 64 || len(s.Mounts) != 3 || s.Network != "" || s.User == "" {
		t.Errorf("spec: %+v", s)
	}
	// The oracle ran on a copy with no network.
	if o := w.engine.specs[1]; o.Network != "none" || mountOf(o, "/w") == mountOf(s, containerWorkspace) {
		t.Errorf("oracle spec: %+v", o)
	}
}

func TestOperatorAbortAndEndpointChange(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	w.engine.runner = func(rctx context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
		fix(s)
		io.WriteString(out, `{"k":"event","event":{}}`+"\n")
		cancel(ErrOperatorAbort)
		<-rctx.Done()
		return docker.Result{ExitCode: 137}, rctx.Err()
	}
	r, p := w.run(t, ctx)
	if r.Verdict != store.Aborted || r.VerdictReason != "operator abort" {
		t.Fatalf("%s: %s", r.Verdict, r.VerdictReason)
	}
	// An aborted run is still collected and judged, for the record.
	if p.Oracle == nil || w.kinds()["workspace_after"] != 1 {
		t.Errorf("an aborted run was not collected: %+v", p)
	}

	w = newWorld(t)
	w.engine.runner = func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
		fix(s)
		*w.model = "other" // same name, other parameters
		io.WriteString(out, `{"k":"event","event":{}}`+"\n"+endLine(OutcomeCompleted, ""))
		return docker.Result{}, nil
	}
	r, _ = w.run(t, context.Background())
	if r.Verdict != store.Invalid || !strings.Contains(r.VerdictReason, "parameters changed") {
		t.Fatalf("%s: %s", r.Verdict, r.VerdictReason)
	}
}

// A workspace copy of the oracle's artefact that the agent changed.
func TestTamperedOracle(t *testing.T) {
	w := newWorld(t)
	dir := t.TempDir()
	body := []byte("#!/bin/sh\necho RESULT ok\n")
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	td := filepath.Join(dir, "demo", "tool")
	os.MkdirAll(filepath.Join(td, "seed"), 0o755)
	os.WriteFile(filepath.Join(td, "seed", "answer.txt"), []byte("The answer is 42.\n"), 0o644)
	os.WriteFile(filepath.Join(td, "build.sh"), []byte("true\n"), 0o644)
	os.WriteFile(filepath.Join(td, "task.toml"), []byte(`id = "tool"
suite = "demo"
version = 1
prompt = "p"
[limits]
wall_s = 60
[workspace]
seed = "seed"
[[workspace.install]]
artefact = "check"
path = "check"
mode = "0555"
[[artefact]]
name = "check"
image = "debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587"
inputs = ["build.sh"]
command = ["sh", "/task/build.sh"]
output = "check"
sha256 = "`+sha+`"
[[oracle]]
name = "check"
primary = true
image = "debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587"
command = ["./check"]
artefact = "check"
expect_pattern = "RESULT ok"
`), 0o644)
	tk, err := task.Load(td)
	if err != nil {
		t.Fatal(err)
	}
	w.req.Task = tk
	w.engine.runner = func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
		io.WriteString(out, `{"k":"event","event":{}}`+"\n"+endLine(OutcomeCompleted, ""))
		return docker.Result{}, nil
	}
	// Not built: the run does not start.
	if r, _ := w.run(t, context.Background()); r.Verdict != store.Skipped || !strings.Contains(r.VerdictReason, "not built") {
		t.Fatalf("%s: %s", r.Verdict, r.VerdictReason)
	}
	art := w.x.Tasks.ArtefactPath(tk.Artefact("check"))
	os.MkdirAll(filepath.Dir(art), 0o755)
	os.WriteFile(art, body, 0o755)
	// Untouched: the (fake) oracle answers; the seed already holds 42.
	if r, _ := w.run(t, context.Background()); r.Verdict != store.Pass {
		t.Fatalf("%s: %s", r.Verdict, r.VerdictReason)
	}
	w.engine.runner = func(_ context.Context, s docker.Spec, out io.Writer) (docker.Result, error) {
		p := filepath.Join(mountOf(s, containerWorkspace), "check")
		os.Chmod(p, 0o755)
		os.WriteFile(p, []byte("#!/bin/sh\necho RESULT ok # always\n"), 0o755)
		io.WriteString(out, `{"k":"event","event":{}}`+"\n"+endLine(OutcomeCompleted, ""))
		return docker.Result{}, nil
	}
	r, _ := w.run(t, context.Background())
	if r.Verdict != store.Aborted || !strings.Contains(r.VerdictReason, "was changed: check") {
		t.Fatalf("%s: %s", r.Verdict, r.VerdictReason)
	}
}
