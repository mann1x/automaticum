package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const armsDir = "../../arms"

func TestRepositoryArms(t *testing.T) {
	// base is a template: it loads, and as an arm it would miss exactly
	// what each real arm must state for itself.
	base, err := LoadArm(armsDir, "base")
	if err != nil || !base.Template {
		t.Fatalf("base: %+v, %v", base, err)
	}
	ae := &ArmError{Arm: "base", Problems: base.Validate()}
	want := []string{"endpoint", "model.provider", "model.model", "model.context_window", "model.output_tokens", "thinking.owner", "sampler.owner"}
	for _, w := range want {
		found := false
		for _, p := range ae.Problems {
			found = found || strings.HasPrefix(p, w)
		}
		if !found {
			t.Errorf("base: no problem reported for %s; got %v", w, ae.Problems)
		}
	}
	if len(ae.Problems) != len(want) {
		t.Errorf("base: %d problems, want %d: %v", len(ae.Problems), len(want), ae.Problems)
	}

	stub, err := LoadArm(armsDir, "stub")
	if err != nil {
		t.Fatal(err)
	}
	if stub.Origins["agent.mistake_limit"] != "base" || stub.Origins["limits.wall_s"] != "stub" || stub.Origins["model.model"] != "stub" {
		t.Errorf("origins: %v", stub.Origins)
	}
	if *stub.Limits.WallS != 300 || *stub.Agent.MistakeLimit != 6 || len(stub.ID) != 64 {
		t.Errorf("stub resolved wrongly: %s", stub.Resolved())
	}
	if _, has := stub.Origins["extends"]; has {
		t.Error("extends is listed as a value")
	}
}

// arms writes arm files into a new directory.
func arms(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	base, err := os.ReadFile(filepath.Join(armsDir, "base.toml"))
	if err != nil {
		t.Fatal(err)
	}
	files["base"] = string(base)
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const good = `extends = "base"
endpoint = "e"
[model]
provider = "ollama"
model = "m:latest"
context_window = 96000
output_tokens = 0
[thinking]
owner = "arm"
enabled = true
effort = "xhigh"
[sampler]
owner = "arm"
name = "s1"
temperature = 0.8
repeat_last_n = 96
`

func TestArmValidation(t *testing.T) {
	if _, err := LoadArm(arms(t, map[string]string{"a": good}), "a"); err != nil {
		t.Fatalf("the good arm is refused: %v", err)
	}
	// Each case changes the good arm in one way and names a word the
	// refusal must contain.
	cases := []struct{ name, body, want string }{
		{"unknown field", good + "top_pp = 1.0\n", "unknown fields: sampler.top_pp (from a)"},
		{"unknown table", good + "[gaurds.tool_intent]\nenabled = true\n", "unknown fields"},
		{"sampler owner unstated", strings.Replace(good, "owner = \"arm\"\nname", "name", 1), "sampler.owner must be"},
		{"thinking owner unstated", strings.Replace(good, "owner = \"arm\"\nenabled", "enabled", 1), "thinking.owner must be"},
		{"tag owner with values", strings.Replace(good, "owner = \"arm\"\nname", "owner = \"tag\"\nname", 1), "must not state a sampler"},
		{"arm owner without values", strings.Replace(good, "temperature = 0.8\nrepeat_last_n = 96\n", "", 1), "no sampler value is stated"},
		{"sampler without a name", strings.Replace(good, "name = \"s1\"\n", "", 1), "sampler.name is not stated"},
		{"bad effort", strings.Replace(good, "xhigh", "extreme", 1), "thinking.effort"},
		{"thinking off with effort", strings.Replace(good, "enabled = true\neffort", "enabled = false\neffort", 1), "thinking is off"},
		{"guard unstated", good + "[guards.tool_intent]\n", ""}, // restating the table changes nothing
		{"escalation on", good + "[agent]\nescalation = true\n", "not supported yet"},
		{"approval other", good + "[agent]\napproval = \"ask\"\n", "agent.approval"},
		{"mistake limit zero", good + "[agent]\nmistake_limit = 0\n", "no way to switch it off"},
		{"bad extension kind", good + "[agent]\nconfig_extensions = [\"rulez\"]\n", "config_extensions"},
		{"loop detection off with numbers", good + "[guards.loop_detection]\nenabled = false\nsoft = 3\n", "soft and hard must not be stated"},
		{"compaction off with settings", good + "[compaction]\nenabled = false\ncouncil = true\n", "its other settings must not be stated"},
		{"task progress off with interval", good + "[agent.task_progress]\nenabled = false\ninterval = 2\n", "interval must not be stated"},
		{"protocol static without limits", good + "[protocol]\nmode = \"static\"\n", "protocol.max_changes"},
		{"bad release", good + "[harness]\nsdk_release = \"main\"\n", "harness.sdk_release"},
		{"no wall limit", good + "[limits]\nwall_s = 0\n", "limits.wall_s must be positive"},
		{"stop on loop signals", good + "[guards]\nstop_on = [\"loop_detection\", \"nudge.non_convergence\", \"reasoning_loop.removed\"]\n", ""},
		{"stop on an unknown guard", good + "[guards]\nstop_on = [\"loop_detect\"]\n", "names no guard that announces"},
		{"stop on an unknown phase", good + "[guards]\nstop_on = [\"nudge.no_tools\"]\n", "nudge announces no_tool_call"},
	}
	for _, c := range cases {
		_, err := LoadArm(arms(t, map[string]string{"a": c.body}), "a")
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: refused: %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want a refusal containing %q, got %v", c.name, c.want, err)
		}
	}
}

// An empty stop_on is left out of the resolved arm, so an arm keeps the
// identity it had before the field existed (D100); a list is in it.
func TestStopOnIdentity(t *testing.T) {
	dir := arms(t, map[string]string{"a": good, "b": good + "[guards]\nstop_on = [\"loop_detection\"]\n"})
	a, err := LoadArm(dir, "a")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(a.Resolved()), "stop_on") {
		t.Errorf("an empty stop_on is in the resolved arm: %s", a.Resolved())
	}
	b, err := LoadArm(dir, "b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b.Resolved()), `"stop_on":["loop_detection"]`) || a.ID == b.ID {
		t.Errorf("stop_on is not part of the arm: %s", b.Resolved())
	}
}

// Without the parent, every guard and compaction setting is reported: none
// has a default.
func TestNothingIsDefaulted(t *testing.T) {
	bare := strings.Replace(good, "extends = \"base\"\n", "", 1)
	_, err := LoadArm(arms(t, map[string]string{"a": bare}), "a")
	var ae *ArmError
	if !errors.As(err, &ae) {
		t.Fatalf("got %v", err)
	}
	all := strings.Join(ae.Problems, "\n")
	for _, w := range []string{
		"guards.loop_detection.enabled", "guards.reasoning_loop.enabled", "guards.reasoning_repetition.enabled",
		"guards.strong_nudges.enabled", "guards.tool_intent.enabled", "guards.stale_read_rewrites.enabled",
		"guards.edit_verification", "compaction.enabled", "protocol.mode", "agent.checkpoints",
		"agent.task_progress.enabled", "agent.sub_agents.spawn", "agent.config_extensions", "agent.bundled_skills",
		"agent.escalation", "agent.mistake_limit", "agent.tool_result_cap", "agent.max_iterations", "limits.wall_s",
		"limits.idle_s", "harness.sdk_release", "agent.host_ide", "agent.mode",
	} {
		if !strings.Contains(all, w) {
			t.Errorf("an arm that does not state %s was not told so", w)
		}
	}
}

func TestExtendsAndIdentity(t *testing.T) {
	child := "extends = \"a\"\n[sampler]\ntemperature = 0.2\n[guards.loop_detection]\nenabled = false\nsoft = 0\nhard = 0\n"
	dir := arms(t, map[string]string{"a": good, "b": child, "same": "extends = \"a\"\n", "loop1": "extends = \"loop2\"\n", "loop2": "extends = \"loop1\"\n"})
	a, err := LoadArm(dir, "a")
	if err != nil {
		t.Fatal(err)
	}
	same, err := LoadArm(dir, "same")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != same.ID {
		t.Error("two files that resolve to the same values are different arms")
	}
	if d := Diff(a, same); len(d) != 0 {
		t.Errorf("diff of equal arms: %v", d)
	}
	// A child that switches a guard off and states its numbers in the same
	// file has made a mistake.
	if _, err := LoadArm(dir, "b"); err == nil || !strings.Contains(err.Error(), "soft and hard must not be stated") {
		t.Errorf("b: %v", err)
	}
	// A child that only switches it off drops what the parent configured
	// under it, and the resolved arm no longer carries those values.
	off := "extends = \"a\"\n[guards.loop_detection]\nenabled = false\n[compaction]\nenabled = false\n[sampler]\nowner = \"tag\"\n[thinking]\nowner = \"tag\"\n[agent.task_progress]\nenabled = false\n"
	dir2 := arms(t, map[string]string{"a": good, "off": off})
	o, err := LoadArm(dir2, "off")
	if err != nil {
		t.Fatalf("switching features off in a child: %v", err)
	}
	if o.Guards.LoopDetection.Soft != nil || o.Compaction.Strategy != "" || o.Sampler.Temperature != nil || o.Sampler.Name != "" ||
		o.Thinking.Effort != "" || o.Thinking.Enabled != nil || o.Agent.TaskProgress.Interval != nil {
		t.Errorf("inherited settings survived their switch: %s", o.Resolved())
	}
	if _, has := o.Origins["sampler.temperature"]; has {
		t.Error("a dropped value still has an origin")
	}
	if _, err := LoadArm(dir, "loop1"); err == nil || !strings.Contains(err.Error(), "extends itself") {
		t.Errorf("a cycle: %v", err)
	}
	if _, err := LoadArm(dir, "missing"); err == nil {
		t.Error("an arm that does not exist loaded")
	}

	other := strings.Replace(good, "temperature = 0.8", "temperature = 0.2", 1)
	dir = arms(t, map[string]string{"a": good, "c": other})
	a, _ = LoadArm(dir, "a")
	c, err := LoadArm(dir, "c")
	if err != nil {
		t.Fatal(err)
	}
	d := Diff(a, c)
	if len(d) != 1 || d[0] != "sampler.temperature: 0.8 -> 0.2" {
		t.Errorf("diff: %v", d)
	}
	if a.ID == c.ID {
		t.Error("arms that differ have one identity")
	}
}

func local(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLocal(t *testing.T) {
	if _, err := LoadLocal("../../examples/config"); err != nil {
		t.Fatalf("the example configuration does not load: %v", err)
	}
	daemon := "data_root = \"/data\"\nfree_floor_gb = 10\nrepo = \"/repo\"\n"
	lane := "host = \"h\"\nendpoints = [\"e\"]\ncpus = 2\nmemory_gb = 4\npids = 256\n"
	ep := "kind = \"ollama\"\nbase_url = \"http://10.0.0.1:11434\"\nhost = \"h\"\ncapacity = 1\n"
	ok := map[string]string{"automaticum.toml": daemon, "lanes/l.toml": lane, "endpoints/e.toml": ep, "token": strings.Repeat("x", 40) + "\n"}
	l, err := LoadLocal(local(t, ok))
	if err != nil {
		t.Fatal(err)
	}
	if l.Lanes["l"].User != "1000:1000" || l.Daemon.Listen != "127.0.0.1:7411" {
		t.Errorf("defaults: %+v %+v", l.Lanes["l"], l.Daemon)
	}
	if tok, err := l.Token(); err != nil || len(tok) != 40 {
		t.Errorf("token: %q, %v", tok, err)
	}
	change := func(name, body string) map[string]string {
		m := map[string]string{}
		for k, v := range ok {
			m[k] = v
		}
		m[name] = body
		return m
	}
	cases := map[string]map[string]string{
		"unknown field":       change("lanes/l.toml", lane+"gpus = 1\n"),
		"lane as root":        change("lanes/l.toml", lane+"user = \"0:0\"\n"),
		"lane without limits": change("lanes/l.toml", "host = \"h\"\nendpoints = [\"e\"]\n"),
		"unknown endpoint":    change("lanes/l.toml", strings.Replace(lane, "\"e\"", "\"nope\"", 1)),
		"all interfaces":      change("endpoints/e.toml", strings.Replace(ep, "10.0.0.1", "0.0.0.0", 1)),
		"bad kind":            change("endpoints/e.toml", strings.Replace(ep, "ollama", "magic", 1)),
		"relative data root":  change("automaticum.toml", strings.Replace(daemon, "/data", "data", 1)),
		"no free floor":       change("automaticum.toml", strings.Replace(daemon, "free_floor_gb = 10\n", "", 1)),
	}
	for name, files := range cases {
		if _, err := LoadLocal(local(t, files)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A token others can read, or a short one, is refused.
	dir := local(t, ok)
	os.Chmod(filepath.Join(dir, "token"), 0o644)
	l, _ = LoadLocal(dir)
	if _, err := l.Token(); err == nil {
		t.Error("a world-readable token was accepted")
	}
	dir = local(t, change("token", "short\n"))
	l, _ = LoadLocal(dir)
	if _, err := l.Token(); err == nil {
		t.Error("a short token was accepted")
	}
}

func TestTemplate(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("parent", "template = true\n[agent]\nmode = \"act\"\n")
	write("child", "extends = \"parent\"\n")
	p, err := LoadArm(dir, "parent")
	if err != nil || !p.Template || p.ID != "" {
		t.Fatalf("template: %+v, %v", p, err)
	}
	// The mark is not inherited: the child is a real arm and must be whole.
	c, err := LoadArm(dir, "child")
	var ae *ArmError
	if !errors.As(err, &ae) {
		t.Fatalf("child of a template loaded without its own values: %+v, %v", c, err)
	}
	if _, err := LoadArms(dir); err == nil {
		t.Fatal("LoadArms accepted an incomplete arm")
	}
	os.Remove(filepath.Join(dir, "child.toml"))
	all, err := LoadArms(dir)
	if err != nil || len(all) != 0 {
		t.Fatalf("LoadArms returned a template: %v, %v", all, err)
	}
}

// An arm that accepts a model reload or a model partly on the GPU is
// another arm; one that states nothing keeps its identity (D73).
func TestAccepts(t *testing.T) {
	dir := arms(t, map[string]string{"a": good, "reload": "extends = \"a\"\n[accepts]\nmodel_reload = true\n", "none": "extends = \"a\"\n[accepts]\nmodel_reload = false\n"})
	a, err := LoadArm(dir, "a")
	if err != nil {
		t.Fatal(err)
	}
	reload, err := LoadArm(dir, "reload")
	if err != nil {
		t.Fatal(err)
	}
	none, err := LoadArm(dir, "none")
	if err != nil {
		t.Fatal(err)
	}
	if !reload.Accepts.ModelReload || reload.Accepts.PartialGPU || reload.ID == a.ID {
		t.Errorf("an arm that accepts a reload: %+v, same identity %v", reload.Accepts, reload.ID == a.ID)
	}
	if none.ID != a.ID || strings.Contains(string(a.Resolved()), "accepts") {
		t.Errorf("an arm that accepts nothing changed identity, or says so: %s", a.Resolved())
	}
}
