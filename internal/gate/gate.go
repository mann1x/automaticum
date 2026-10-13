// Package gate holds the gates: what must hold before a run starts (spec
// 050 §2 and §3). A gate that does not hold never lets a run through that
// then looks normal: the run is skipped with the gate's name and reason, or
// waits, and the refusal is notified.
package gate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/task"
)

// Status is what a gate says.
type Status string

const (
	Pass   Status = "pass"
	Refuse Status = "refuse" // the run must not start
	Wait   Status = "wait"   // the run stays queued until this holds
	// Unchecked means the gate could not look: it says why, and it does not
	// stop a run. It is never shown as a pass.
	Unchecked Status = "unchecked"
)

// Result is one gate's answer.
type Result struct {
	Gate    string `json:"gate"`
	Version string `json:"version"`
	Status  Status `json:"status"`
	Detail  string `json:"detail"`
}

// Input is what the gates look at: one arm on one task on one lane.
type Input struct {
	Local *config.Local
	Lane  string
	Arm   *config.Arm
	Task  *task.Task
	Image string // the lane image the run would use

	Engine interface {
		ImageInspect(ctx context.Context, ref string) (docker.Image, error)
	}
	HTTP    *http.Client
	Tasks   *task.Env
	SDK     run.Bundles
	RunsDir string
	// Free reports the free bytes of the disk a path is on, and Tmpfs
	// whether a path is on a RAM-backed file system.
	Free  func(path string) (uint64, error)
	Tmpfs func(path string) (bool, error)

	// Set by Run before the gates look.
	lane     *config.Lane
	endpoint *config.Endpoint
	identity *run.Identity
	probeErr error
}

// Gate is one check, with a name and a version so that a report can say
// which gate stopped what.
type Gate struct {
	Name, Version string
	Check         func(ctx context.Context, in *Input) (Status, string)
}

// Builtin lists the built-in gates in the order they are shown. The three
// that need an agent on the endpoint's host are listed too, and say so.
func Builtin() []Gate {
	agent := func(what string) func(context.Context, *Input) (Status, string) {
		return func(context.Context, *Input) (Status, string) {
			return Unchecked, what + ": needs an agent on the endpoint's host (milestone 7)"
		}
	}
	return []Gate{
		{"lane", "1", lane},
		{"free", "1", free},
		{"model_tag", "1", modelTag},
		{"template", "1", template},
		{"sampler", "1", sampler},
		{"thinking", "1", thinking},
		{"gpu_residency", "1", residency},
		{"harness_build", "1", harness},
		{"host", "1", host},
		{"oracle", "1", oracle},
		{"workspace", "1", workspace},
		{"bind_address", "1", bind},
		{"gpu_ownership", "1", agent("the lane's device binding and where the process landed")},
		{"server_log_level", "1", agent("the server's log level")},
		{"expert", "1", func(context.Context, *Input) (Status, string) {
			return Unchecked, "no expert model can be configured yet"
		}},
	}
}

// Run looks at everything once and returns every gate's answer. It asks the
// endpoint for its identity; it never loads or unloads a model.
func Run(ctx context.Context, in *Input) []Result {
	in.lane = in.Local.Lanes[in.Lane]
	in.endpoint = in.Local.Endpoints[in.Arm.Endpoint]
	if in.endpoint != nil {
		key, err := in.Local.APIKey(in.endpoint)
		if err != nil {
			in.probeErr = fmt.Errorf("the endpoint's credential cannot be read: %w", err)
		} else {
			hc := in.HTTP
			if hc == nil {
				hc = &http.Client{Timeout: 30 * time.Second}
			}
			id, err := run.Probe(ctx, hc, in.endpoint, key, in.Arm.Model.Model)
			in.identity, in.probeErr = &id, err
		}
	}
	var out []Result
	for _, g := range Builtin() {
		st, detail := g.Check(ctx, in)
		out = append(out, Result{Gate: g.Name, Version: g.Version, Status: st, Detail: detail})
	}
	return out
}

// Worst returns the answer that decides: the first refusal, else the first
// wait, else nil.
func Worst(results []Result) *Result {
	for _, want := range []Status{Refuse, Wait} {
		for i := range results {
			if results[i].Status == want {
				return &results[i]
			}
		}
	}
	return nil
}

func lane(_ context.Context, in *Input) (Status, string) {
	switch {
	case in.lane == nil:
		return Refuse, fmt.Sprintf("this installation has no lane %q", in.Lane)
	case in.endpoint == nil:
		return Refuse, fmt.Sprintf("this installation has no endpoint %q", in.Arm.Endpoint)
	case !slices.Contains(in.lane.Endpoints, in.Arm.Endpoint):
		return Refuse, fmt.Sprintf("lane %s may not use endpoint %s", in.Lane, in.Arm.Endpoint)
	case in.Arm.Template || in.Arm.ID == "":
		return Refuse, in.Arm.Name + " is a template, not an arm"
	}
	return Pass, fmt.Sprintf("lane %s on %s may use endpoint %s", in.Lane, in.lane.Host, in.Arm.Endpoint)
}

// probed is the common start of the gates that read the endpoint's answer.
func probed(in *Input) (*run.Identity, Status, string) {
	switch {
	case in.endpoint == nil:
		return nil, Unchecked, "there is no endpoint to ask"
	case in.probeErr != nil:
		return nil, Unchecked, "the endpoint could not be asked: " + in.probeErr.Error()
	}
	return in.identity, Pass, ""
}

// free: a busy endpoint makes the run wait; nothing is unloaded (D64).
func free(_ context.Context, in *Input) (Status, string) {
	id, st, why := probed(in)
	if id == nil {
		return st, why
	}
	if slices.Contains(id.NotProbed, "residency") {
		return Unchecked, "this kind of endpoint does not say what it holds in memory"
	}
	if len(id.OtherModels) > 0 {
		return Wait, fmt.Sprintf("endpoint %s holds %s in memory; loading %s would push it out, and nothing is unloaded for a run",
			in.endpoint.Name, strings.Join(id.OtherModels, ", "), in.Arm.Model.Model)
	}
	if id.Loaded != nil {
		return Pass, "the endpoint holds this model and no other"
	}
	return Pass, "the endpoint holds no model in memory"
}

func modelTag(_ context.Context, in *Input) (Status, string) {
	if in.endpoint != nil && in.probeErr != nil {
		return Refuse, in.probeErr.Error()
	}
	id, st, why := probed(in)
	if id == nil {
		return st, why
	}
	if slices.Contains(id.NotProbed, "model digest") {
		if in.Arm.Model.Digest != "" {
			return Refuse, "the arm pins a digest and this kind of endpoint gives none"
		}
		return Pass, "the endpoint lists the model; this kind of endpoint gives no digest"
	}
	if pin := in.Arm.Model.Digest; pin != "" && pin != id.Digest {
		return Refuse, fmt.Sprintf("the tag's digest is %.12s, the arm pins %.12s", id.Digest, pin)
	} else if pin != "" {
		return Pass, fmt.Sprintf("digest %.12s, as pinned", id.Digest)
	}
	return Pass, fmt.Sprintf("digest %.12s; the arm pins none", id.Digest)
}

// template: a tag whose template lost its directives still answers, without
// tools. What the server says the tag can do is the evidence.
func template(_ context.Context, in *Input) (Status, string) {
	id, st, why := probed(in)
	if id == nil {
		return st, why
	}
	if slices.Contains(id.NotProbed, "template") {
		return Unchecked, "this kind of endpoint does not show the template"
	}
	how := fmt.Sprintf("template %d bytes (%.12s)", id.TemplateBytes, id.TemplateSHA256)
	if id.Renderer != "" || id.Parser != "" {
		how += fmt.Sprintf(", renderer %q, parser %q", id.Renderer, id.Parser)
	}
	if !slices.Contains(id.Capabilities, "tools") {
		return Refuse, "the tag does not offer tool calls, so its template or renderer is not intact: " + how
	}
	return Pass, "the tag offers " + strings.Join(id.Capabilities, ", ") + ": " + how
}

func sampler(_ context.Context, in *Input) (Status, string) {
	if problems := in.Arm.Validate(); len(problems) > 0 {
		return Refuse, "the arm is not complete: " + strings.Join(problems, "; ")
	}
	id, _, _ := probed(in)
	tag := ""
	if id != nil {
		var have []string
		for _, line := range strings.Split(id.Parameters, "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && slices.Contains(samplerNames, f[0]) {
				have = append(have, f[0]+" "+strings.Join(f[1:], " "))
			}
		}
		tag = "; the tag sets none of its own"
		if len(have) > 0 {
			tag = "; the tag's own: " + strings.Join(have, ", ")
		}
	}
	if in.Arm.Sampler.Owner == "tag" {
		return Pass, "the sampler is left to the tag" + tag
	}
	return Pass, fmt.Sprintf("the arm states its sampler (%s); whether it was sent is read from the wire after the run%s", in.Arm.Sampler.Name, tag)
}

var samplerNames = []string{"temperature", "top_p", "top_k", "min_p", "typical_p", "repeat_penalty", "repeat_last_n",
	"presence_penalty", "frequency_penalty", "seed", "stop"}

// thinking never refuses: a value the provider does not send becomes a
// finding with the value in effect (D63).
func thinking(_ context.Context, in *Input) (Status, string) {
	id, _, _ := probed(in)
	tag := "not read"
	can := true
	if id != nil {
		tag = "none"
		for _, line := range strings.Split(id.Parameters, "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "think_budget" {
				tag = strings.Trim(f[1], `"`)
			}
		}
		can = slices.Contains(id.Capabilities, "thinking") || slices.Contains(id.NotProbed, "template")
	}
	t := in.Arm.Thinking
	if t.Owner == "tag" {
		return Pass, "thinking is left to the tag; the tag's budget: " + tag
	}
	stated := "on"
	switch {
	case t.Enabled != nil && !*t.Enabled:
		stated = "off"
	case t.Effort != "":
		stated = "effort " + t.Effort
	case t.BudgetTokens != nil:
		stated = fmt.Sprintf("budget %d tokens", *t.BudgetTokens)
	}
	note := ""
	if !can && stated != "off" {
		note = "; the tag does not list thinking among what it can do"
	}
	return Pass, fmt.Sprintf("the arm states thinking %s and wins over the tag's budget (%s); what was sent is read from the wire after the run%s", stated, tag, note)
}

func residency(_ context.Context, in *Input) (Status, string) {
	id, st, why := probed(in)
	if id == nil {
		return st, why
	}
	if slices.Contains(id.NotProbed, "residency") {
		return Unchecked, "this kind of endpoint does not say where the model sits"
	}
	l := id.Loaded
	switch {
	case l == nil:
		return Unchecked, "the model is not loaded; the run loads it and the probe after the run records where it sits"
	case l.SizeVRAM < l.Size:
		return Refuse, fmt.Sprintf("the model is only partly on the GPU: %d of %d bytes", l.SizeVRAM, l.Size)
	}
	return Pass, fmt.Sprintf("the model is fully on the GPU (%d bytes, context %d)", l.Size, l.ContextLength)
}

// harness: the lane image is there, and the SDK build the arm names is
// provisioned as its definition pins it (D54, 040 §2). The run hashes the
// bundle's files again before it starts, and the runner reads the installed
// package's own version.
func harness(ctx context.Context, in *Input) (Status, string) {
	if in.Engine == nil {
		return Unchecked, "no container engine to ask"
	}
	if _, err := in.Engine.ImageInspect(ctx, in.Image); err != nil {
		return Refuse, fmt.Sprintf("the lane image %s is not there: %v", in.Image, err)
	}
	if in.SDK == nil {
		return Refuse, "the SDK release cannot be established: this installation provisions no SDK builds"
	}
	name := in.Arm.Harness.SDKRelease
	def, rec, err := in.SDK.Check(name, false)
	if err != nil {
		return Refuse, "the SDK release cannot be established: " + err.Error()
	}
	from := "the registry"
	if def.Source == "local" {
		from = "commit " + def.Commit + " of the SDK's repository"
	}
	return Pass, fmt.Sprintf("SDK build %s (package version %s, from %s) is provisioned, bundle %.12s as pinned; lane image %s is there",
		name, def.Version, from, rec.BundleSHA256, in.Image)
}

func host(_ context.Context, in *Input) (Status, string) {
	var notes []string
	floor := uint64(in.Local.Daemon.FreeFloorGB) << 30
	if in.Free == nil {
		return Unchecked, "free space cannot be read on this platform"
	}
	for _, dir := range []string{in.RunsDir, in.Local.Daemon.DataRoot} {
		if dir == "" {
			continue
		}
		n, err := in.Free(dir)
		if err != nil {
			return Refuse, "free space cannot be read: " + err.Error()
		}
		if floor > 0 && n < floor {
			return Refuse, fmt.Sprintf("free space is %d GB, under the floor of %d GB", n>>30, in.Local.Daemon.FreeFloorGB)
		}
		notes = append(notes, fmt.Sprintf("%d GB free", n>>30))
	}
	if floor == 0 {
		notes = append(notes, "no floor is configured")
	} else {
		notes = append(notes, fmt.Sprintf("floor %d GB", in.Local.Daemon.FreeFloorGB))
	}
	if b, err := os.ReadFile("/proc/sys/fs/inotify/max_user_watches"); err == nil {
		notes = append(notes, "inotify watches "+strings.TrimSpace(string(b)))
	}
	return Pass, strings.Join(notes, ", ") + "; clock sync and the user-namespace setting are not read"
}

func oracle(_ context.Context, in *Input) (Status, string) {
	t := in.Task
	if t.PrimaryOracle() == nil {
		return Refuse, "the task has no primary oracle"
	}
	if in.Tasks == nil {
		return Unchecked, "no task environment to ask"
	}
	for i := range t.Artefacts {
		a := &t.Artefacts[i]
		if sum, err := task.HashFile(in.Tasks.ArtefactPath(a)); err != nil || sum != a.SHA256 {
			return Refuse, fmt.Sprintf("artefact %s of the task is not built; run `automaticum task build %s`", a.Name, t.Name())
		}
	}
	p := in.Tasks.Proven(t)
	if p == nil {
		return Refuse, fmt.Sprintf("the oracle of %s, as the task is now, was not proven against its fixtures; run `automaticum oracle verify %s`", t.Name(), t.Name())
	}
	return Pass, fmt.Sprintf("proven on %d fixtures, %d times each, on %s, for the task as it is now", p.Fixtures, p.Repeats, p.At.Format("2006-01-02"))
}

// workspace: every run starts from the task's own files in a fresh
// directory on a real disk. The copy and its hash check are made by the run.
func workspace(_ context.Context, in *Input) (Status, string) {
	if in.RunsDir == "" {
		return Unchecked, "no runs directory is configured"
	}
	if in.Tmpfs != nil {
		ram, err := in.Tmpfs(in.RunsDir)
		if err != nil {
			return Refuse, "the runs directory cannot be read: " + err.Error()
		}
		if ram {
			return Refuse, "the runs directory is on a RAM-backed file system"
		}
	}
	probe, err := os.MkdirTemp(in.RunsDir, ".gate-")
	if err != nil {
		return Refuse, "the runs directory is not writable: " + err.Error()
	}
	os.Remove(probe)
	if in.Task.Workspace.Seed != "" {
		if st, err := os.Stat(in.Task.Dir + "/" + in.Task.Workspace.Seed); err != nil || !st.IsDir() {
			return Refuse, "the task's seed directory is not there"
		}
	}
	return Pass, "a fresh directory on a real disk for each run; the run copies the task's files and checks their hashes"
}

// bind: what can be seen from here. Whether the server listens on every
// interface of its host is only visible on that host.
func bind(_ context.Context, in *Input) (Status, string) {
	if in.endpoint == nil {
		return Unchecked, "there is no endpoint to ask"
	}
	u, err := url.Parse(in.endpoint.BaseURL)
	if err != nil || u.Hostname() == "" {
		return Refuse, "the endpoint's address is not readable"
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
		return Refuse, "the endpoint's address is the all-interfaces address, which names no server"
	}
	if in.probeErr != nil {
		return Refuse, "the endpoint is not reachable: " + in.probeErr.Error()
	}
	return Pass, "the endpoint answers from this host; whether it listens on every interface of its own host needs an agent there (milestone 7), and reachability from inside the lane's container is proven by the run's first request"
}
