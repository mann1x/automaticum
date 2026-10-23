package run

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/sdk"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/task"
)

// Causes with which a run's context is cancelled from outside.
var (
	ErrOperatorAbort = errors.New("operator abort")
	ErrDaemonStop    = errors.New("daemon stop")
	errIdle          = errors.New("idle limit")
)

// Engine is what a run needs of the container engine. *docker.Client is one.
type Engine interface {
	task.Executor
	ImageInspect(ctx context.Context, ref string) (docker.Image, error)
}

// Blobs stores raw material. *blob.Store is one.
type Blobs interface {
	Put(ctx context.Context, r io.Reader) (blob.Hash, int64, error)
}

// Inference is the inference point of a run and the identity of its model,
// each fact with where it comes from: stated by the owner, declared in the
// configuration, said by the server, or read from the weights file.
type Inference struct {
	Endpoint      string `json:"endpoint"`
	Kind          string `json:"kind"`
	ServerVersion string `json:"server_version"`
	// Engine is the server's runner for the model; known once it is loaded.
	Engine string `json:"engine,omitempty"`
	// Host is the neutral name of the host the server runs on, and LaneHost
	// that of the host the agent ran on.
	Host     string `json:"host"`
	LaneHost string `json:"lane_host"`
	// AddedLater is set when these facts were not recorded by the run but
	// added to its record afterwards (D74): when, and from what.
	AddedLater *AddedLater `json:"added_later,omitempty"`
	GPU        struct {
		// Declared is what the endpoint's configuration states.
		Declared string  `json:"declared"`
		VRAMGB   float64 `json:"vram_gb,omitempty"`
		Notes    string  `json:"notes,omitempty"`
		// Measured stays empty until an agent on that host reads the
		// device (milestone 7); Share is the part of the model on a GPU
		// after the run, as the server says.
		Measured string   `json:"measured"`
		Share    *float64 `json:"share_on_gpu,omitempty"`
	} `json:"gpu"`
	Model struct {
		Tag    string `json:"tag"`
		Digest string `json:"digest"`
		// Stated is the catalogue's entry; nil when the tag has none.
		Stated *config.ModelIdentity `json:"stated"`
		// NotStated lists what the entry leaves out.
		NotStated []string `json:"not_stated,omitempty"`
		// Server is what the server says of the tag, and Weights what the
		// weights file says of itself.
		Server  json.RawMessage `json:"server,omitempty"`
		Weights map[string]any  `json:"weights,omitempty"`
	} `json:"model"`
}

// AddedLater marks facts of a record that the run itself did not write.
type AddedLater struct {
	At     time.Time `json:"at"`
	Source string    `json:"source"`
}

// Bundles gives the SDK build a run's arm names. *sdk.Env is one.
type Bundles interface {
	// Check says whether a build can be used: defined, installed, and the
	// bundle its definition pins; with deep, the files are hashed again.
	Check(name string, deep bool) (*sdk.Def, *sdk.Record, error)
	// Path is the bundle's directory, mounted into the run's container.
	Path(name string) string
}

// Executor runs runs. One Executor serves one daemon.
type Executor struct {
	Engine Engine
	Blobs  Blobs
	State  store.State
	Tasks  *task.Env
	// SDK is where the builds of the SDK under test are provisioned.
	SDK   Bundles
	Local *config.Local
	// RunsDir holds one directory per run while it is in flight.
	RunsDir string
	// Version is automaticum's own version, for the record.
	Version string
	HTTP    *http.Client
	// Grace is how long after the wall limit the container is killed if the
	// runner has not stopped by itself; zero means two minutes.
	Grace time.Duration
	// IdleTick is how often the idle limit is checked; zero means 5 s.
	IdleTick time.Duration
	// Free returns the free bytes of the filesystem holding a path.
	Free func(path string) (uint64, error)
}

// Request names one run.
type Request struct {
	// ID is the run's id; empty lets Execute make one.
	ID   string
	Arm  *config.Arm
	Task *task.Task
	Lane string
	// Image is the lane image the run uses.
	Image string
	Batch string
	// Campaign is the campaign the batch belongs to, if any.
	Campaign string
	// OnStart is called once the container runs, with the record so far
	// and the container's id, so a restarted daemon can clean up.
	OnStart func(r *store.Run, containerID string)
	// OnLine is called with every line the runner prints.
	OnLine func(line []byte)
	// Gates is what the gates said before this run; it is recorded as is.
	Gates any
	// Identity is the catalogue's entry for the arm's model tag: what the
	// model is, in the owner's words. Nil when the tag has none.
	Identity *config.ModelIdentity
	// Unloaded lists the models the endpoint was asked to drop before the
	// run, when its configuration allows that (D79).
	Unloaded []string
	// Cell is what a mode C cell says of its endpoint (065 §3.1): the
	// caller's values and basis facts and what the server said of itself.
	// It is recorded as is.
	Cell any
}

// NewID returns a run id that sorts by time.
func NewID(now time.Time) string {
	var b [3]byte
	rand.Read(b[:])
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// limit is an effective limit and who set it.
type limit struct {
	Seconds int    `json:"seconds"`
	Owner   string `json:"owner"`
}

// lower takes the lower of the arm's and the task's limit; zero means none.
func lower(arm, tsk int) limit {
	switch {
	case arm > 0 && (tsk == 0 || arm <= tsk):
		return limit{arm, "arm"}
	case tsk > 0:
		return limit{tsk, "task"}
	}
	return limit{0, ""}
}

// provenance is everything recorded about how a run was made that is not a
// column of its own (spec 020 §1).
type provenance struct {
	Automaticum string `json:"automaticum"`
	Protocol    int    `json:"runner_protocol"`
	SDK         struct {
		Release       string `json:"release"`        // as the runner found it installed
		PackageSHA256 string `json:"package_sha256"` //
		Named         string `json:"named"`          // the build the arm names
		// What was provisioned for the run (040 §2): the version the
		// build's definition says, where it comes from, and the hash of
		// the bundle that was mounted, checked against its files.
		Version      string `json:"version"`
		Source       string `json:"source"`
		Commit       string `json:"commit,omitempty"`
		BundleSHA256 string `json:"bundle_sha256"`
		LockSHA256   string `json:"lock_sha256"`
	} `json:"sdk"`
	Image struct {
		Ref     string   `json:"ref"`
		ID      string   `json:"id"`
		Digests []string `json:"digests"`
	} `json:"image"`
	Task struct {
		Hash   string            `json:"hash"`
		Files  map[string]string `json:"files"`
		Prompt string            `json:"prompt"`
	} `json:"task"`
	// Inference says where and on what the run's model ran (020 §1.1).
	Inference *Inference `json:"inference,omitempty"`
	// Holds are the intervals in which the run was held between two model
	// requests, and HeldMS their sum, which the wall time leaves out.
	Holds  []Hold `json:"holds,omitempty"`
	HeldMS int64  `json:"held_ms,omitempty"`
	// Unloaded lists the models dropped from the endpoint's memory before
	// the run (D79).
	Unloaded []string `json:"unloaded_before_run,omitempty"`
	// Cell is a mode C cell's endpoint, values and facts (065 §3.1).
	Cell any `json:"cell,omitempty"`
	// Gates is every gate's answer before the run (050 §3).
	Gates          any       `json:"gates,omitempty"`
	EndpointBefore *Identity `json:"endpoint_before"`
	EndpointAfter  *Identity `json:"endpoint_after"`
	Limits         struct {
		Wall    limit  `json:"wall"`
		Idle    limit  `json:"idle"`
		Fired   string `json:"fired"`
		FiredBy string `json:"fired_by"`
	} `json:"limits"`
	Container struct {
		ID        string `json:"id"`
		ExitCode  int    `json:"exit_code"`
		OOMKilled bool   `json:"oom_killed"`
		TimedOut  bool   `json:"timed_out"`
		User      string `json:"user"`
	} `json:"container"`
	Stream struct {
		Lines    int `json:"lines"`
		Events   int `json:"events"`
		BadLines int `json:"bad_lines"`
	} `json:"stream"`
	Runner    *End `json:"runner"`
	Workspace struct {
		Before  string   `json:"before"` // hash of the manifest
		After   string   `json:"after"`
		Added   []string `json:"added"`
		Removed []string `json:"removed"`
		Changed []string `json:"changed"`
		// GitLeftOut says the workspace's .git directory is left out of
		// the lists above (and is in workspace_after).
		GitLeftOut bool `json:"git_left_out,omitempty"`
		// Git says the runner was asked to make the workspace a git
		// repository (D87: every workspace, unless the task says not).
		Git bool `json:"git,omitempty"`
	} `json:"workspace"`
	Oracle *task.OracleResult `json:"oracle"`
	// NotRecorded says what a full record holds that this one does not,
	// and why.
	NotRecorded map[string]string `json:"not_recorded"`
	Errors      []string          `json:"errors,omitempty"`
}

// Execute runs req to its end and returns the record it wrote. An error
// means the record could not be written; every other trouble is a verdict.
// Cancelling ctx with ErrOperatorAbort or ErrDaemonStop as its cause aborts
// the run.
func (x *Executor) Execute(ctx context.Context, req Request) (*store.Run, error) {
	now := time.Now()
	if req.ID == "" {
		req.ID = NewID(now)
	}
	a, t := req.Arm, req.Task
	r := &store.Run{
		ID: req.ID, Batch: req.Batch, Campaign: req.Campaign, Suite: t.Suite, TaskID: t.ID, TaskVersion: t.Version,
		ArmID: a.ID, Lane: req.Lane, StartedAt: now.UTC(),
		Provider: a.Model.Provider, Model: a.Model.Model, Endpoint: a.Endpoint,
		Thinking: thinkingWord(a), Arm: a.Resolved(),
	}
	// Harness is the SDK release once it is established, never the one the
	// arm merely names: first by the lane image's label, then by what the
	// runner finds installed.
	if l := x.Local.Lanes[req.Lane]; l != nil {
		r.Host = l.Host
	}
	var p provenance
	p.Automaticum, p.Protocol = x.Version, Protocol
	p.SDK.Named = a.Harness.SDKRelease
	p.Image.Ref = req.Image
	p.Gates = req.Gates
	p.Unloaded = req.Unloaded
	p.Cell = req.Cell
	p.Task.Hash, p.Task.Files, p.Task.Prompt = t.Hash, t.Files, t.Prompt
	p.NotRecorded = map[string]string{
		"server_log":     "needs agent mode on the endpoint's host",
		"weights":        "needs agent mode on the endpoint's host",
		"host_facts":     "free space and inotify limits are in the host gate's answer; clock sync and kernel settings are not read",
		"workspace_diff": "the workspaces before and after are stored whole; no textual diff is made",
	}
	var facts Facts
	var refs []store.BlobRef
	// The runner's two output streams are stored while it runs, side by
	// side, so what they append to is guarded.
	var mu sync.Mutex
	note := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		p.Errors = append(p.Errors, fmt.Sprintf(format, args...))
	}
	put := func(kind, name string, rd io.Reader) {
		// Storing must not be cut short by the run's own cancellation.
		h, n, err := x.Blobs.Put(context.WithoutCancel(ctx), rd)
		if err != nil {
			note("storing %s %s: %v", kind, name, err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		refs = append(refs, store.BlobRef{RunID: r.ID, Kind: kind, Name: name, Hash: h, Size: n})
	}
	putTree := func(kind, dir string) {
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(tarTree(dir, pw)) }()
		put(kind, "", pr)
		pr.Close()
	}

	dir := filepath.Join(x.RunsDir, r.ID)
	var held time.Duration
	finish := func() (*store.Run, error) {
		r.EndedAt = time.Now().UTC()
		// Time held is taken out of the wall time, and the run says that
		// it was held (050 §4).
		r.WallMS = r.EndedAt.Sub(r.StartedAt).Milliseconds() - held.Milliseconds()
		r.Paused = held > 0
		r.Verdict, r.VerdictReason = Decide(facts)
		p.Limits.Fired, p.Limits.FiredBy = facts.Limit, facts.LimitBy
		r.Provenance, _ = json.Marshal(p)
		sctx := context.WithoutCancel(ctx)
		if err := x.State.UpsertRun(sctx, r); err != nil {
			return r, fmt.Errorf("run %s: writing the record: %w", r.ID, err)
		}
		if err := x.State.AddBlobRefs(sctx, refs); err != nil {
			return r, fmt.Errorf("run %s: writing the blob references: %w", r.ID, err)
		}
		// The run's directory is only removed once the record is written.
		os.RemoveAll(dir)
		return r, nil
	}

	// What must hold before anything starts. Each failure is SKIPPED.
	lane, endpoint, skip := x.before(ctx, req, &p)
	if skip != "" {
		facts.Skipped = skip
		return finish()
	}
	r.Harness = a.Harness.SDKRelease
	p.Container.User = lane.User
	key, err := x.Local.APIKey(endpoint)
	if err != nil {
		facts.Skipped = "the endpoint's key cannot be read"
		return finish()
	}
	before, err := Probe(ctx, x.http(), endpoint, key, a.Model.Model)
	if err != nil {
		facts.Skipped = err.Error()
		return finish()
	}
	p.EndpointBefore = &before
	inf := &Inference{Endpoint: endpoint.Name, Kind: endpoint.Kind, ServerVersion: before.ServerVersion, Engine: before.Engine,
		Host: endpoint.Host, LaneHost: lane.Host}
	inf.GPU.Declared, inf.GPU.VRAMGB, inf.GPU.Notes = endpoint.Hardware.GPU, endpoint.Hardware.VRAMGB, endpoint.Hardware.Notes
	inf.Model.Tag, inf.Model.Digest = a.Model.Model, before.Digest
	stated := req.Identity
	if Ollama(endpoint) {
		stated = FillIdentity(stated, a.Model.Model, before.Weights)
	}
	inf.Model.Stated, inf.Model.NotStated = stated, stated.Missing()
	inf.Model.Server, inf.Model.Weights = before.Details, before.Weights
	p.Inference = inf

	wall := lower(*a.Limits.WallS, t.Limits.WallS)
	idle := lower(*a.Limits.IdleS, t.Limits.IdleS)
	p.Limits.Wall, p.Limits.Idle = wall, idle
	if wall.Seconds == 0 {
		facts.Skipped = "neither the arm nor the task sets a wall limit"
		return finish()
	}

	// The workspace, the job and the place for what the runner writes.
	ws, jobDir, out := filepath.Join(dir, "workspace"), filepath.Join(dir, "job"), filepath.Join(dir, "out")
	for _, d := range []string{ws, jobDir, out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			facts.Skipped = "the run's directory cannot be made: " + err.Error()
			return finish()
		}
	}
	manifest, err := x.Tasks.PrepareWorkspace(t, ws)
	if err != nil {
		facts.Skipped = "the workspace cannot be prepared: " + err.Error()
		return finish()
	}
	p.Workspace.Before = manifest.Hash()
	p.Workspace.Git = t.Workspace.GitRepo()
	job := Job{
		Protocol: Protocol, RunID: r.ID, SessionID: "run-" + r.ID, Arm: a.Resolved(),
		Endpoint: JobEndpoint{Name: endpoint.Name, Kind: endpoint.Kind, BaseURL: endpoint.BaseURL, APIKey: key},
		Prompt:   t.Prompt, DefaultAnswer: t.DefaultAnswer, User: t.User, WorkspaceGit: t.Workspace.GitRepo(),
		Workspace: containerWorkspace, Out: containerOut, WallS: wall.Seconds, SDKRelease: p.SDK.Version,
	}
	jb, _ := json.Marshal(job)
	if err := os.WriteFile(filepath.Join(jobDir, jobFile), jb, 0o644); err != nil {
		facts.Skipped = "the job cannot be written: " + err.Error()
		return finish()
	}
	if err := own(lane.User, ws, out); err != nil {
		facts.Skipped = "the workspace cannot be handed to the lane's user: " + err.Error()
		return finish()
	}
	putTree("workspace_before", ws)

	// The run itself.
	rctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	st := newStream(time.Now())
	st.onLine = req.OnLine
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	stored := make(chan struct{}, 2)
	go func() { put("events", "", stdoutR); stdoutR.Close(); stored <- struct{}{} }()
	go func() { put("runner_stderr", "", stderrR); stderrR.Close(); stored <- struct{}{} }()
	if idle.Seconds > 0 {
		go x.watchIdle(rctx, st, time.Duration(idle.Seconds)*time.Second, cancel)
	}
	grace := x.Grace
	if grace == 0 {
		grace = 2 * time.Minute
	}
	spec := docker.Spec{
		Name: "automaticum-" + r.ID, Image: req.Image, Cmd: RunnerCommand, WorkDir: containerWorkspace, User: lane.User,
		Mounts: []docker.Mount{
			{Source: ws, Target: containerWorkspace},
			{Source: jobDir, Target: containerJob, ReadOnly: true},
			{Source: out, Target: containerOut},
			// The SDK build the arm names; the image holds none.
			{Source: x.SDK.Path(a.Harness.SDKRelease), Target: sdk.Mount, ReadOnly: true},
		},
		Tmpfs:       map[string]string{"/tmp": ""},
		MemoryBytes: int64(lane.MemoryGB * (1 << 30)), NanoCPUs: int64(lane.CPUs * 1e9), PidsLimit: lane.Pids,
		Labels: map[string]string{"automaticum": "run", "automaticum.run": r.ID, "automaticum.lane": req.Lane},
		// Time held is not the run's time: the outer limit moves by it,
		// and does not fall while the run is being held.
		Extra: func() time.Duration {
			total, holding := st.heldFor(time.Now())
			if holding {
				total += time.Minute
			}
			return total
		},
	}
	res, runErr := x.Engine.Run(rctx, spec, time.Duration(wall.Seconds)*time.Second+grace,
		io.MultiWriter(st, stdoutW), stderrW, func(id string) {
			p.Container.ID = id
			if req.OnStart != nil {
				req.OnStart(r, id)
			}
		})
	stdoutW.Close()
	stderrW.Close()
	<-stored
	<-stored
	os.Remove(filepath.Join(jobDir, HoldFile))
	held, _ = st.heldFor(time.Now())
	p.Holds, p.HeldMS = st.heldIntervals(), held.Milliseconds()
	cause := context.Cause(rctx)
	cancel(nil)
	p.Container.ID, p.Container.ExitCode, p.Container.OOMKilled, p.Container.TimedOut = res.ID, res.ExitCode, res.OOMKilled, res.TimedOut
	p.Image.ID = res.ImageID
	lines, events, bad, end := st.snapshot()
	p.Stream.Lines, p.Stream.Events, p.Stream.BadLines = lines, events, bad
	p.Runner = end
	if end != nil {
		r.AgentEnd, r.ClaimedSuccess = end.AgentEnd, end.ClaimedSuccess
		p.SDK.Release, p.SDK.PackageSHA256 = end.SDK.Release, end.SDK.PackageSHA256
		if end.SDK.Release != "" {
			r.Harness = end.SDK.Release
		}
	}

	// Collect what the run left, whatever became of it.
	after, err := task.ReadManifest(ws)
	if err != nil {
		note("reading the workspace: %v", err)
	} else {
		p.Workspace.After = after.Hash()
		if t.Workspace.GitRepo() {
			// The repository the runner made is not the agent's work; it
			// is in workspace_after whole.
			after = withoutGit(after)
			p.Workspace.GitLeftOut = true
		}
		p.Workspace.Added, p.Workspace.Removed, p.Workspace.Changed = manifestDiff(manifest, after)
	}
	putTree("workspace_after", ws)
	x.collectOut(out, put, note)

	// What ended it.
	started := res.ID != "" && !res.Started.IsZero()
	switch {
	case errors.Is(cause, ErrOperatorAbort), errors.Is(cause, ErrDaemonStop):
		facts.Aborted = cause.Error()
	case errors.Is(cause, errIdle):
		facts.Limit, facts.LimitOwner, facts.LimitBy = "idle", idle.Owner, "harness"
	case ctx.Err() != nil:
		facts.Aborted = "the harness stopped: " + ctx.Err().Error()
	case runErr != nil && !started:
		facts.Skipped = "the lane's container could not be started: " + runErr.Error()
	case runErr != nil:
		facts.Aborted = "the container engine failed: " + runErr.Error()
	case res.OOMKilled:
		facts.Aborted = "the lane's container ran out of memory"
	case res.TimedOut:
		facts.Limit, facts.LimitOwner, facts.LimitBy = "wall", wall.Owner, "harness"
	case end == nil:
		facts.Aborted = fmt.Sprintf("the runner ended without its last line (exit code %d)", res.ExitCode)
	case end.Outcome == OutcomeRefused:
		facts.Skipped = "the runner refused to start: " + end.Reason
	case end.Outcome == OutcomeInvalid:
		facts.Invalid = append(facts.Invalid, end.Problems...)
		if len(end.Problems) == 0 {
			facts.Invalid = append(facts.Invalid, "the runner found the run invalid: "+end.Reason)
		}
	case end.Outcome == OutcomeWallLimit:
		facts.Limit, facts.LimitOwner, facts.LimitBy = "wall", wall.Owner, "runner"
	case end.Outcome == OutcomeGuardStop:
		facts.GuardStop = end.Reason
	case end.Outcome == OutcomeError:
		facts.Aborted = "the runner failed: " + end.Reason
	case end.Outcome != OutcomeCompleted:
		facts.Aborted = fmt.Sprintf("the runner reported an outcome this harness does not know: %q", end.Outcome)
	case end.AgentEnd == "error" && end.AgentRequestsAnswered != nil && *end.AgentRequestsAnswered == 0:
		// The SDK ended the session before the model answered anything:
		// the model was not tested, and the oracle would only judge the
		// seed (owner, 2026-10-16, D89).
		facts.Aborted = "the agent ended in error before the server answered any of its requests"
		if end.AgentError != "" {
			facts.Aborted += ": " + end.AgentError
		}
	}
	if facts.Skipped != "" {
		return finish()
	}
	if end != nil && end.SDK.Release != p.SDK.Version {
		// The runner should have refused; a run on another release is
		// not this arm.
		facts.Invalid = append(facts.Invalid, fmt.Sprintf("the run used SDK release %q, build %s of the arm is %s", end.SDK.Release, a.Harness.SDKRelease, p.SDK.Version))
	}
	if facts.Aborted == "" && len(facts.Invalid) == 0 && events == 0 {
		facts.Aborted = "the event stream is empty"
	}

	// The endpoint must still be what it was.
	octx := context.WithoutCancel(ctx)
	if afterID, err := Probe(octx, x.http(), endpoint, key, a.Model.Model); err != nil {
		note("probing the endpoint after the run: %v", err)
	} else {
		p.EndpointAfter = &afterID
		if inf.Engine == "" {
			inf.Engine = afterID.Engine
		}
		if l := afterID.Loaded; l != nil && l.Size > 0 {
			share := float64(l.SizeVRAM) / float64(l.Size)
			inf.GPU.Share = &share
		}
		facts.Invalid = append(facts.Invalid, before.Changed(afterID)...)
	}

	// The oracle, run by the harness on a copy, with its own artefact.
	// A daemon that is stopping does not wait for an oracle: the run is
	// ABORTED either way.
	if o := t.PrimaryOracle(); o != nil && !errors.Is(cause, ErrDaemonStop) {
		or, err := x.Tasks.RunOracle(octx, t, o, ws)
		// The output is stored as a blob, not twice.
		brief := or
		brief.Output = ""
		p.Oracle = &brief
		switch {
		case err != nil:
			note("running the oracle: %v", err)
			if facts.Aborted == "" {
				facts.Aborted = "the oracle could not be run"
			}
		case len(or.Tampered) > 0:
			if facts.Aborted == "" {
				facts.Aborted = "the oracle's copy in the workspace was changed: " + strings.Join(or.Tampered, ", ")
			}
		case or.Err != "":
			if facts.Aborted == "" {
				facts.Aborted = "the oracle gave no answer: " + or.Err
			}
		default:
			facts.OracleRan, facts.OraclePassed = true, or.Passed
		}
		put("oracle", o.Name, strings.NewReader(or.Output))
	}
	return finish()
}

// before checks what must hold before a run starts and returns why it does
// not, if so.
func (x *Executor) before(ctx context.Context, req Request, p *provenance) (*config.Lane, *config.Endpoint, string) {
	a, t := req.Arm, req.Task
	lane := x.Local.Lanes[req.Lane]
	if lane == nil {
		return nil, nil, fmt.Sprintf("this installation has no lane %q", req.Lane)
	}
	endpoint := x.Local.Endpoint(a.Endpoint)
	if endpoint == nil {
		return nil, nil, fmt.Sprintf("this installation has no endpoint %q", a.Endpoint)
	}
	if !x.Local.MayUse(req.Lane, a.Endpoint) {
		return nil, nil, fmt.Sprintf("lane %s may not use endpoint %s", req.Lane, a.Endpoint)
	}
	if a.Template || a.ID == "" {
		return nil, nil, fmt.Sprintf("%s is a template, not an arm", a.Name)
	}
	if t.PrimaryOracle() == nil {
		return nil, nil, "the task has no primary oracle"
	}
	// A scripted user that restores a checkpoint needs the arm's
	// checkpoints: without them the restore would fail inside the run.
	if t.User != nil && (a.Agent.Checkpoints == nil || !*a.Agent.Checkpoints) {
		for _, r := range t.User.Rules {
			if r.Do == "restore" {
				return nil, nil, fmt.Sprintf("the task's scripted user restores checkpoints, and arm %s has checkpoints off", a.Name)
			}
		}
	}
	for i := range t.Artefacts {
		art := &t.Artefacts[i]
		if sum, err := task.HashFile(x.Tasks.ArtefactPath(art)); err != nil || sum != art.SHA256 {
			return nil, nil, fmt.Sprintf("artefact %s of the task is not built; run `automaticum task build %s`", art.Name, t.Name())
		}
	}
	im, err := x.Engine.ImageInspect(ctx, req.Image)
	if err != nil {
		return nil, nil, fmt.Sprintf("the lane image %s is not there: %v", req.Image, err)
	}
	p.Image.ID, p.Image.Digests = im.ID, im.RepoDigests
	// Decision D54: no run without an established SDK release. The build
	// the arm names must be provisioned, and its files must hash to what
	// its definition pins.
	if x.SDK == nil {
		return nil, nil, "the SDK release cannot be established: this installation provisions no SDK builds"
	}
	def, rec, err := x.SDK.Check(a.Harness.SDKRelease, true)
	if err != nil {
		return nil, nil, "the SDK release cannot be established: " + err.Error()
	}
	p.SDK.Version, p.SDK.Source, p.SDK.Commit = def.Version, def.Source, def.Commit
	p.SDK.BundleSHA256, p.SDK.LockSHA256 = rec.BundleSHA256, rec.LockSHA256
	if x.Free != nil && x.Local.Daemon.FreeFloorGB > 0 {
		free, err := x.Free(x.RunsDir)
		if err != nil {
			return nil, nil, "free space cannot be read: " + err.Error()
		}
		if free < uint64(x.Local.Daemon.FreeFloorGB)<<30 {
			return nil, nil, fmt.Sprintf("free space is %d GB, under the floor of %d GB", free>>30, x.Local.Daemon.FreeFloorGB)
		}
	}
	return lane, endpoint, ""
}

// HoldFile is the file whose presence in a run's job directory holds the
// run: the runner looks for it before every model request and waits while
// it is there (runner/PROTOCOL.md).
const HoldFile = "hold"

// Hold asks a run in flight to stop before its next model request. The
// request being answered is finished first, and its tools run.
func (x *Executor) Hold(runID string) error {
	dir := filepath.Join(x.RunsDir, runID, "job")
	if _, err := os.Stat(filepath.Join(dir, jobFile)); err != nil {
		return fmt.Errorf("run %s has no job to hold", runID)
	}
	return os.WriteFile(filepath.Join(dir, HoldFile), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// Release lets a held run go on. It is not an error to release a run that
// is not held.
func (x *Executor) Release(runID string) error {
	err := os.Remove(filepath.Join(x.RunsDir, runID, "job", HoldFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (x *Executor) http() *http.Client {
	if x.HTTP != nil {
		return x.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (x *Executor) watchIdle(ctx context.Context, st *stream, max time.Duration, cancel context.CancelCauseFunc) {
	tick := x.IdleTick
	if tick == 0 {
		tick = 5 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if st.idle(now) > max {
				cancel(errIdle)
				return
			}
		}
	}
}

// collectOut stores what the runner wrote beside its output: the wire
// capture, and the session's data directory as one archive.
func (x *Executor) collectOut(out string, put func(kind, name string, r io.Reader), note func(string, ...any)) {
	entries, err := os.ReadDir(out)
	if err != nil {
		note("reading the runner's output directory: %v", err)
		return
	}
	for _, e := range entries {
		path := filepath.Join(out, e.Name())
		switch {
		case e.IsDir():
			pr, pw := io.Pipe()
			go func() { pw.CloseWithError(tarTree(path, pw)) }()
			put(kindOf(e.Name()), e.Name(), pr)
			pr.Close()
		case e.Type().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				note("opening %s: %v", e.Name(), err)
				continue
			}
			put(kindOf(e.Name()), e.Name(), f)
			f.Close()
		}
	}
}

// kindOf maps a name in the runner's output directory to a blob kind.
func kindOf(name string) string {
	switch {
	case strings.HasPrefix(name, "wire"):
		return "wire"
	case name == "data" || name == "home":
		return "session"
	case strings.HasPrefix(name, "runtime"):
		return "runtime_log"
	}
	return "runner_output"
}

func thinkingWord(a *config.Arm) string {
	if a.Thinking.Owner == "arm" && a.Thinking.Effort != "" {
		return a.Thinking.Effort
	}
	return a.Thinking.Owner
}

// withoutGit is a manifest without the files under .git/.
func withoutGit(m task.Manifest) task.Manifest {
	out := task.Manifest{}
	for name, sum := range m {
		if name != ".git" && !strings.HasPrefix(name, ".git/") {
			out[name] = sum
		}
	}
	return out
}

func manifestDiff(before, after task.Manifest) (added, removed, changed []string) {
	for name, sum := range after {
		switch old, ok := before[name]; {
		case !ok:
			added = append(added, name)
		case old != sum:
			changed = append(changed, name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return
}

// own gives directory trees to the lane's user, so the agent can write its
// workspace. When the harness is not privileged to do that, it opens the
// directories instead.
func own(user string, dirs ...string) error {
	uidS, gidS, _ := strings.Cut(user, ":")
	uid, err1 := strconv.Atoi(uidS)
	gid, err2 := strconv.Atoi(gidS)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("lane user %q is not uid:gid", user)
	}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if os.Getuid() == uid {
				return nil
			}
			if err := os.Lchown(path, uid, gid); err != nil {
				if !errors.Is(err, syscall.EPERM) {
					return err
				}
				if d.IsDir() {
					return os.Chmod(path, 0o777)
				}
				if d.Type().IsRegular() {
					return os.Chmod(path, 0o666)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// tarTree writes dir as a tar archive with a fixed order and no times or
// owners, so the same tree always gives the same bytes.
func tarTree(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Mode: int64(info.Mode().Perm())}
		switch {
		case d.IsDir():
			hdr.Typeflag, hdr.Name = tar.TypeDir, hdr.Name+"/"
		case info.Mode()&fs.ModeSymlink != 0:
			hdr.Typeflag = tar.TypeSymlink
			if hdr.Linkname, err = os.Readlink(path); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			hdr.Typeflag, hdr.Size = tar.TypeReg, info.Size()
		default:
			return nil // sockets, devices: not part of a workspace
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		// A file that grew since it was listed must not break the archive.
		_, err = io.CopyN(tw, f, hdr.Size)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}
