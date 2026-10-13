// Package config loads automaticum's configuration: arms, lanes, endpoints
// and the daemon's own file (spec 030). Files are TOML. An unknown field is
// an error, never a silent default, and nothing is read from the environment.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Arm is one fully stated configuration of model, provider and agent
// (spec 030 section 5). A pointer field is one the arm must state: nil after
// resolution is a validation error, because the SDK would otherwise choose
// (most of its guards are on when a field is left out).
type Arm struct {
	// Name is the file's name without its extension.
	Name string `toml:"-" json:"name"`
	// Extends names a parent arm whose values this one overrides.
	Extends string `toml:"extends" json:"-"`
	// Template marks an arm that only exists to be extended. It may leave
	// values unstated, is never run, and the mark is not inherited.
	Template bool `toml:"template" json:"-"`
	// Endpoint names an endpoint file. An arm never holds an address.
	Endpoint string `toml:"endpoint" json:"endpoint"`

	Model      Model      `toml:"model" json:"model"`
	Thinking   Thinking   `toml:"thinking" json:"thinking"`
	Sampler    Sampler    `toml:"sampler" json:"sampler"`
	Agent      Agent      `toml:"agent" json:"agent"`
	Guards     Guards     `toml:"guards" json:"guards"`
	Compaction Compaction `toml:"compaction" json:"compaction"`
	Protocol   Protocol   `toml:"protocol" json:"protocol"`
	Limits     ArmLimits  `toml:"limits" json:"limits"`
	Harness    Harness    `toml:"harness" json:"harness"`

	// Origins says, for every stated value, which arm file it came from.
	Origins map[string]string `toml:"-" json:"-"`
	// ID is the sha256 of the resolved content: two files that resolve to
	// the same values are the same arm.
	ID string `toml:"-" json:"-"`
}

// Model says what is asked of the endpoint.
type Model struct {
	Provider      string `toml:"provider" json:"provider"`
	Model         string `toml:"model" json:"model"`
	Digest        string `toml:"digest" json:"digest,omitempty"` // optional pin
	ContextWindow *int   `toml:"context_window" json:"context_window"`
	// OutputTokens is the output budget per request; 0 leaves it to the
	// SDK's own rule (three quarters of the window, capped).
	OutputTokens *int `toml:"output_tokens" json:"output_tokens"`
}

// Thinking is who owns the thinking settings, and what they are.
type Thinking struct {
	// Owner is "tag" (send nothing; the model tag's own values apply) or
	// "arm" (send these and verify they were sent).
	Owner        string `toml:"owner" json:"owner"`
	Enabled      *bool  `toml:"enabled" json:"enabled,omitempty"`
	Effort       string `toml:"effort" json:"effort,omitempty"`
	BudgetTokens *int   `toml:"budget_tokens" json:"budget_tokens,omitempty"`
}

// Sampler is who owns the sampler, its name and its values.
type Sampler struct {
	Owner            string   `toml:"owner" json:"owner"`
	Name             string   `toml:"name" json:"name,omitempty"`
	Temperature      *float64 `toml:"temperature" json:"temperature,omitempty"`
	TopP             *float64 `toml:"top_p" json:"top_p,omitempty"`
	TopK             *int     `toml:"top_k" json:"top_k,omitempty"`
	MinP             *float64 `toml:"min_p" json:"min_p,omitempty"`
	TypicalP         *float64 `toml:"typical_p" json:"typical_p,omitempty"`
	RepeatPenalty    *float64 `toml:"repeat_penalty" json:"repeat_penalty,omitempty"`
	RepeatLastN      *int     `toml:"repeat_last_n" json:"repeat_last_n,omitempty"`
	PresencePenalty  *float64 `toml:"presence_penalty" json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `toml:"frequency_penalty" json:"frequency_penalty,omitempty"`
	Seed             *int     `toml:"seed" json:"seed,omitempty"`
	Stop             []string `toml:"stop" json:"stop,omitempty"`
}

// Agent holds the agent settings that are not guards.
type Agent struct {
	Mode    string `toml:"mode" json:"mode"`         // act or plan
	HostIDE string `toml:"host_ide" json:"host_ide"` // the IDE named in the system prompt
	// MaxIterations caps the agent's turns; 0 means no cap.
	MaxIterations *int `toml:"max_iterations" json:"max_iterations"`
	// MistakeLimit is the number of consecutive mistakes that stops the run.
	MistakeLimit *int `toml:"mistake_limit" json:"mistake_limit"`
	// ToolResultCap is the most characters of a tool result kept.
	ToolResultCap *int `toml:"tool_result_cap" json:"tool_result_cap"`
	// Approval is the approval policy; only "auto" exists so far.
	Approval     string       `toml:"approval" json:"approval"`
	Checkpoints  *bool        `toml:"checkpoints" json:"checkpoints"`
	TaskProgress TaskProgress `toml:"task_progress" json:"task_progress"`
	SubAgents    SubAgents    `toml:"sub_agents" json:"sub_agents"`
	// ConfigExtensions lists what the agent may load from the workspace:
	// rules, skills, workflows, plugins, hooks. Stated, even when empty.
	ConfigExtensions *[]string `toml:"config_extensions" json:"config_extensions"`
	// BundledSkills lists the SDK's bundled skills that are on. Stated,
	// even when empty.
	BundledSkills *[]string `toml:"bundled_skills" json:"bundled_skills"`
	// DisabledTools removes tools from the list the model is offered.
	DisabledTools []string `toml:"disabled_tools" json:"disabled_tools,omitempty"`
	// Escalation to an expert model. Must be stated; only false is
	// supported so far.
	Escalation *bool `toml:"escalation" json:"escalation"`
}

// TaskProgress is the focus chain.
type TaskProgress struct {
	Enabled  *bool `toml:"enabled" json:"enabled"`
	Interval *int  `toml:"interval" json:"interval,omitempty"`
}

// SubAgents says whether the agent may delegate.
type SubAgents struct {
	Spawn *bool `toml:"spawn" json:"spawn"`
	Teams *bool `toml:"teams" json:"teams"`
}

// Guards are the interventions that change what a model's run looks like.
// Every one is stated (spec 030 section 5.1): there is no default.
type Guards struct {
	LoopDetection       LoopDetection `toml:"loop_detection" json:"loop_detection"`
	ReasoningLoop       Switch        `toml:"reasoning_loop" json:"reasoning_loop"`
	ReasoningRepetition Switch        `toml:"reasoning_repetition" json:"reasoning_repetition"`
	StrongNudges        Switch        `toml:"strong_nudges" json:"strong_nudges"`
	ToolIntent          Switch        `toml:"tool_intent" json:"tool_intent"`
	StaleReadRewrites   Switch        `toml:"stale_read_rewrites" json:"stale_read_rewrites"`
	// EditVerification is off, nudge or require.
	EditVerification string `toml:"edit_verification" json:"edit_verification"`
}

// Switch is a guard that is on, with the SDK's own parameters, or off.
type Switch struct {
	Enabled *bool `toml:"enabled" json:"enabled"`
}

// LoopDetection is the repeated-tool-call guard.
type LoopDetection struct {
	Enabled *bool `toml:"enabled" json:"enabled"`
	Soft    *int  `toml:"soft" json:"soft,omitempty"`
	Hard    *int  `toml:"hard" json:"hard,omitempty"`
}

// Compaction is the context compaction. Stated like a guard: a compaction
// that hides a loop makes a different arm.
type Compaction struct {
	Enabled            *bool  `toml:"enabled" json:"enabled"`
	Strategy           string `toml:"strategy" json:"strategy,omitempty"` // basic or agentic
	KeepRecentMessages *bool  `toml:"keep_recent_messages" json:"keep_recent_messages,omitempty"`
	Council            *bool  `toml:"council" json:"council,omitempty"`
	ThinkingSummary    *bool  `toml:"thinking_summary" json:"thinking_summary,omitempty"`
	CappedThinking     *bool  `toml:"capped_thinking" json:"capped_thinking,omitempty"`
	// ForceFullFrom forces a full compaction from the nth one on; 0 is off.
	ForceFullFrom *int `toml:"force_full_from" json:"force_full_from,omitempty"`
}

// Protocol is the change protocol.
type Protocol struct {
	Mode            string `toml:"mode" json:"mode"` // off or static
	MaxChanges      *int   `toml:"max_changes" json:"max_changes,omitempty"`
	MaxTransactions *int   `toml:"max_transactions" json:"max_transactions,omitempty"`
}

// ArmLimits are the arm's limits, in seconds. A task's own limit, when
// lower, wins; the run record says which fired and who owned it.
type ArmLimits struct {
	WallS *int `toml:"wall_s" json:"wall_s"`
	// IdleS is how long the event stream may be silent; 0 means no limit.
	IdleS *int `toml:"idle_s" json:"idle_s"`
}

// Harness pins what runs the agent.
type Harness struct {
	// SDKRelease is the SDK's release version. A run whose installed SDK
	// is another release does not start (decision D54).
	SDKRelease string `toml:"sdk_release" json:"sdk_release"`
}

var (
	armNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	// A release number, or one followed by "+" and a commit for a build
	// that was never released.
	releaseRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	efforts   = map[string]bool{"minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	extKinds  = map[string]bool{"rules": true, "skills": true, "workflows": true, "plugins": true, "hooks": true}
)

// LoadArm loads the arm called name from dir, resolving what it extends.
func LoadArm(dir, name string) (*Arm, error) {
	merged, origins, err := loadArmTree(dir, name, nil)
	if err != nil {
		return nil, err
	}
	delete(merged, "extends")
	delete(origins, "extends")
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(merged); err != nil {
		return nil, fmt.Errorf("arm %s: %w", name, err)
	}
	var a Arm
	md, err := toml.Decode(buf.String(), &a)
	if err != nil {
		return nil, fmt.Errorf("arm %s: %w", name, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = fmt.Sprintf("%s (from %s)", k, origins[k.String()])
		}
		return nil, fmt.Errorf("arm %s: unknown fields: %s", name, strings.Join(keys, ", "))
	}
	a.Name, a.Origins = name, origins
	a.dropInherited(armChain(dir, name))
	delete(a.Origins, "template")
	if a.Template {
		// A template is checked when something extends it.
		return &a, nil
	}
	if problems := a.Validate(); len(problems) > 0 {
		return nil, &ArmError{Arm: name, Problems: problems}
	}
	a.ID = a.hash()
	return &a, nil
}

// LoadArms loads every arm in dir, by name, templates left out.
func LoadArms(dir string) (map[string]*Arm, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	out := map[string]*Arm{}
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".toml")
		a, err := LoadArm(dir, name)
		if err != nil {
			return nil, err
		}
		if a.Template {
			continue
		}
		out[name] = a
	}
	return out, nil
}

// armChain lists an arm and its parents, the arm first. It is called after
// loadArmTree succeeded, so every file exists and there is no cycle.
func armChain(dir, name string) []string {
	var chain []string
	for name != "" {
		chain = append(chain, name)
		var m struct {
			Extends string `toml:"extends"`
		}
		toml.DecodeFile(filepath.Join(dir, name+".toml"), &m)
		name = m.Extends
	}
	return chain
}

// dropInherited clears the settings of a feature that a child switched off
// when those settings come from a parent further up. A child cannot unset a
// parent's value, so without this an arm could never switch off a guard its
// parent configures. A setting stated in the same file as the switch, or
// below it, is not cleared: Validate reports it, because there it is a
// mistake.
func (a *Arm) dropInherited(chain []string) {
	rank := func(key string) int {
		for i, f := range chain {
			if f == a.Origins[key] {
				return i
			}
		}
		return -1
	}
	// inherited reports whether key was stated further up than the switch.
	inherited := func(sw, key string) bool { return rank(key) > rank(sw) && rank(sw) >= 0 }
	drop := func(sw, key string, clear func()) {
		if inherited(sw, key) {
			clear()
			delete(a.Origins, key)
		}
	}
	if a.Guards.LoopDetection.Enabled != nil && !*a.Guards.LoopDetection.Enabled {
		drop("guards.loop_detection.enabled", "guards.loop_detection.soft", func() { a.Guards.LoopDetection.Soft = nil })
		drop("guards.loop_detection.enabled", "guards.loop_detection.hard", func() { a.Guards.LoopDetection.Hard = nil })
	}
	if c := &a.Compaction; c.Enabled != nil && !*c.Enabled {
		const sw = "compaction.enabled"
		drop(sw, "compaction.strategy", func() { c.Strategy = "" })
		drop(sw, "compaction.keep_recent_messages", func() { c.KeepRecentMessages = nil })
		drop(sw, "compaction.council", func() { c.Council = nil })
		drop(sw, "compaction.thinking_summary", func() { c.ThinkingSummary = nil })
		drop(sw, "compaction.capped_thinking", func() { c.CappedThinking = nil })
		drop(sw, "compaction.force_full_from", func() { c.ForceFullFrom = nil })
	}
	if a.Protocol.Mode == "off" {
		drop("protocol.mode", "protocol.max_changes", func() { a.Protocol.MaxChanges = nil })
		drop("protocol.mode", "protocol.max_transactions", func() { a.Protocol.MaxTransactions = nil })
	}
	if tp := &a.Agent.TaskProgress; tp.Enabled != nil && !*tp.Enabled {
		drop("agent.task_progress.enabled", "agent.task_progress.interval", func() { tp.Interval = nil })
	}
	if th := &a.Thinking; th.Owner == "tag" || (th.Enabled != nil && !*th.Enabled) {
		sw := "thinking.owner"
		if th.Owner != "tag" {
			sw = "thinking.enabled"
		}
		drop(sw, "thinking.effort", func() { th.Effort = "" })
		drop(sw, "thinking.budget_tokens", func() { th.BudgetTokens = nil })
		if th.Owner == "tag" {
			drop(sw, "thinking.enabled", func() { th.Enabled = nil })
		}
	}
	if sm := &a.Sampler; sm.Owner == "tag" {
		const sw = "sampler.owner"
		drop(sw, "sampler.name", func() { sm.Name = "" })
		drop(sw, "sampler.temperature", func() { sm.Temperature = nil })
		drop(sw, "sampler.top_p", func() { sm.TopP = nil })
		drop(sw, "sampler.top_k", func() { sm.TopK = nil })
		drop(sw, "sampler.min_p", func() { sm.MinP = nil })
		drop(sw, "sampler.typical_p", func() { sm.TypicalP = nil })
		drop(sw, "sampler.repeat_penalty", func() { sm.RepeatPenalty = nil })
		drop(sw, "sampler.repeat_last_n", func() { sm.RepeatLastN = nil })
		drop(sw, "sampler.presence_penalty", func() { sm.PresencePenalty = nil })
		drop(sw, "sampler.frequency_penalty", func() { sm.FrequencyPenalty = nil })
		drop(sw, "sampler.seed", func() { sm.Seed = nil })
		drop(sw, "sampler.stop", func() { sm.Stop = nil })
	}
}

// ArmError lists everything wrong with an arm.
type ArmError struct {
	Arm      string
	Problems []string
}

func (e *ArmError) Error() string {
	return fmt.Sprintf("arm %s is not valid:\n  - %s", e.Arm, strings.Join(e.Problems, "\n  - "))
}

// loadArmTree reads an arm file and its parents into one map, child over
// parent, and records which file each leaf value came from.
//
// A switch in a child also switches off what hangs under it in a parent: see
// dropInherited.
func loadArmTree(dir, name string, seen []string) (map[string]any, map[string]string, error) {
	if !armNameRE.MatchString(name) {
		return nil, nil, fmt.Errorf("arm: %q is not a valid arm name", name)
	}
	for _, s := range seen {
		if s == name {
			return nil, nil, fmt.Errorf("arm: %s extends itself through %s", name, strings.Join(seen, " -> "))
		}
	}
	path := filepath.Join(dir, name+".toml")
	var m map[string]any
	if _, err := toml.DecodeFile(path, &m); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("arm: no arm %q in %s", name, dir)
		}
		return nil, nil, fmt.Errorf("arm: %w", err)
	}
	origins := map[string]string{}
	markOrigins(m, "", name, origins)
	parent, _ := m["extends"].(string)
	if _, has := m["extends"]; has && parent == "" {
		return nil, nil, fmt.Errorf("arm %s: extends must name an arm", name)
	}
	if parent == "" {
		return m, origins, nil
	}
	pm, po, err := loadArmTree(dir, parent, append(seen, name))
	if err != nil {
		return nil, nil, err
	}
	delete(pm, "template")
	delete(po, "template")
	mergeMaps(pm, m)
	for k, v := range origins {
		// A child that restates an array or scalar replaces the parent's;
		// origins of the parent's leaves under it no longer apply.
		for pk := range po {
			if strings.HasPrefix(pk, k+".") {
				delete(po, pk)
			}
		}
		po[k] = v
	}
	return pm, po, nil
}

func markOrigins(m map[string]any, prefix, file string, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			markOrigins(sub, key, file, out)
			continue
		}
		out[key] = file
	}
}

// mergeMaps lays src over dst. Tables merge; anything else is replaced.
func mergeMaps(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeMaps(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

// Validate returns everything that is wrong or unstated in a resolved arm.
func (a *Arm) Validate() []string {
	var p []string
	bad := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }
	need := func(ok bool, field string) {
		if !ok {
			bad("%s is not stated", field)
		}
	}
	need(a.Endpoint != "", "endpoint")
	need(a.Model.Provider != "", "model.provider")
	need(a.Model.Model != "", "model.model")
	need(a.Model.ContextWindow != nil, "model.context_window")
	need(a.Model.OutputTokens != nil, "model.output_tokens (0 leaves it to the SDK's rule)")
	if a.Model.ContextWindow != nil && *a.Model.ContextWindow <= 0 {
		bad("model.context_window must be positive")
	}
	if a.Model.OutputTokens != nil && *a.Model.OutputTokens < 0 {
		bad("model.output_tokens must not be negative")
	}

	switch a.Thinking.Owner {
	case "tag":
		if a.Thinking.Enabled != nil || a.Thinking.Effort != "" || a.Thinking.BudgetTokens != nil {
			bad("thinking.owner is \"tag\": the arm must not state thinking values, since none is sent")
		}
	case "arm":
		need(a.Thinking.Enabled != nil, "thinking.enabled")
		if a.Thinking.Effort != "" && !efforts[a.Thinking.Effort] {
			bad("thinking.effort %q is not one of minimal, low, medium, high, xhigh, max", a.Thinking.Effort)
		}
		if a.Thinking.Enabled != nil && !*a.Thinking.Enabled && (a.Thinking.Effort != "" || a.Thinking.BudgetTokens != nil) {
			bad("thinking is off: effort and budget_tokens must not be stated")
		}
	default:
		bad("thinking.owner must be \"tag\" or \"arm\"")
	}

	s := &a.Sampler
	values := s.Temperature != nil || s.TopP != nil || s.TopK != nil || s.MinP != nil || s.TypicalP != nil ||
		s.RepeatPenalty != nil || s.RepeatLastN != nil || s.PresencePenalty != nil || s.FrequencyPenalty != nil ||
		s.Seed != nil || len(s.Stop) > 0
	switch s.Owner {
	case "tag":
		if values || s.Name != "" {
			bad("sampler.owner is \"tag\": the arm must not state a sampler, since none is sent")
		}
	case "arm":
		need(s.Name != "", "sampler.name")
		if !values {
			bad("sampler.owner is \"arm\" and no sampler value is stated")
		}
	default:
		bad("sampler.owner must be \"tag\" or \"arm\"")
	}

	g := &a.Agent
	if g.Mode != "act" && g.Mode != "plan" {
		bad("agent.mode must be \"act\" or \"plan\"")
	}
	need(g.HostIDE != "", "agent.host_ide")
	need(g.MaxIterations != nil, "agent.max_iterations (0 means no cap)")
	need(g.MistakeLimit != nil, "agent.mistake_limit")
	need(g.ToolResultCap != nil, "agent.tool_result_cap")
	if g.MistakeLimit != nil && *g.MistakeLimit < 1 {
		bad("agent.mistake_limit must be 1 or more; the SDK has no way to switch it off")
	}
	if g.ToolResultCap != nil && *g.ToolResultCap < 1 {
		bad("agent.tool_result_cap must be positive")
	}
	if g.MaxIterations != nil && *g.MaxIterations < 0 {
		bad("agent.max_iterations must not be negative")
	}
	if g.Approval != "auto" {
		bad("agent.approval must be \"auto\"; no other policy is supported yet")
	}
	need(g.Checkpoints != nil, "agent.checkpoints")
	need(g.TaskProgress.Enabled != nil, "agent.task_progress.enabled")
	if on(g.TaskProgress.Enabled) {
		need(g.TaskProgress.Interval != nil, "agent.task_progress.interval")
	} else if g.TaskProgress.Interval != nil {
		bad("agent.task_progress is off: interval must not be stated")
	}
	need(g.SubAgents.Spawn != nil, "agent.sub_agents.spawn")
	need(g.SubAgents.Teams != nil, "agent.sub_agents.teams")
	need(g.ConfigExtensions != nil, "agent.config_extensions (an empty list loads nothing from the workspace)")
	if g.ConfigExtensions != nil {
		for _, k := range *g.ConfigExtensions {
			if !extKinds[k] {
				bad("agent.config_extensions: %q is not one of rules, skills, workflows, plugins, hooks", k)
			}
		}
	}
	need(g.BundledSkills != nil, "agent.bundled_skills (an empty list switches all of them off)")
	need(g.Escalation != nil, "agent.escalation")
	if on(g.Escalation) {
		bad("agent.escalation: an expert model is not supported yet")
	}

	gd := &a.Guards
	need(gd.LoopDetection.Enabled != nil, "guards.loop_detection.enabled")
	if on(gd.LoopDetection.Enabled) {
		need(gd.LoopDetection.Soft != nil && gd.LoopDetection.Hard != nil, "guards.loop_detection.soft and .hard")
	} else if gd.LoopDetection.Soft != nil || gd.LoopDetection.Hard != nil {
		bad("guards.loop_detection is off: soft and hard must not be stated")
	}
	need(gd.ReasoningLoop.Enabled != nil, "guards.reasoning_loop.enabled")
	need(gd.ReasoningRepetition.Enabled != nil, "guards.reasoning_repetition.enabled")
	need(gd.StrongNudges.Enabled != nil, "guards.strong_nudges.enabled")
	need(gd.ToolIntent.Enabled != nil, "guards.tool_intent.enabled")
	need(gd.StaleReadRewrites.Enabled != nil, "guards.stale_read_rewrites.enabled")
	switch gd.EditVerification {
	case "off", "nudge", "require":
	case "":
		bad("guards.edit_verification is not stated")
	default:
		bad("guards.edit_verification must be off, nudge or require")
	}

	c := &a.Compaction
	need(c.Enabled != nil, "compaction.enabled")
	if on(c.Enabled) {
		if c.Strategy != "basic" && c.Strategy != "agentic" {
			bad("compaction.strategy must be \"basic\" or \"agentic\"")
		}
		need(c.KeepRecentMessages != nil, "compaction.keep_recent_messages")
		need(c.Council != nil, "compaction.council")
		need(c.ThinkingSummary != nil, "compaction.thinking_summary")
		need(c.CappedThinking != nil, "compaction.capped_thinking")
		need(c.ForceFullFrom != nil, "compaction.force_full_from (0 is off)")
	} else if c.Enabled != nil && (c.Strategy != "" || c.KeepRecentMessages != nil || c.Council != nil ||
		c.ThinkingSummary != nil || c.CappedThinking != nil || c.ForceFullFrom != nil) {
		bad("compaction is off: its other settings must not be stated")
	}

	switch a.Protocol.Mode {
	case "off":
		if a.Protocol.MaxChanges != nil || a.Protocol.MaxTransactions != nil {
			bad("protocol.mode is off: max_changes and max_transactions must not be stated")
		}
	case "static":
		need(a.Protocol.MaxChanges != nil, "protocol.max_changes")
		need(a.Protocol.MaxTransactions != nil, "protocol.max_transactions")
	case "":
		bad("protocol.mode is not stated")
	default:
		bad("protocol.mode must be \"off\" or \"static\"")
	}

	need(a.Limits.WallS != nil, "limits.wall_s")
	need(a.Limits.IdleS != nil, "limits.idle_s (0 means no limit)")
	if a.Limits.WallS != nil && *a.Limits.WallS <= 0 {
		bad("limits.wall_s must be positive")
	}
	if a.Limits.IdleS != nil && *a.Limits.IdleS < 0 {
		bad("limits.idle_s must not be negative")
	}
	if !releaseRE.MatchString(a.Harness.SDKRelease) {
		bad("harness.sdk_release must name an SDK build: a release such as 4.100.250, or 4.100.250+<commit> for one that was never released")
	}
	return p
}

func on(b *bool) bool { return b != nil && *b }

// Resolved returns the arm as canonical JSON: what a run stores and what the
// runner is given. Keys are in a fixed order.
func (a *Arm) Resolved() []byte {
	b, err := json.Marshal(a)
	if err != nil {
		panic(err) // the struct holds only plain values
	}
	var v any
	json.Unmarshal(b, &v)
	out, _ := json.Marshal(v) // maps marshal with sorted keys
	return out
}

// hash is the arm's identity: its resolved content without its name.
func (a *Arm) hash() string {
	var v map[string]any
	json.Unmarshal(a.Resolved(), &v)
	delete(v, "name")
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Diff returns the fields in which two resolved arms differ, as
// "field: a -> b" lines, sorted. The name is not compared.
func Diff(a, b *Arm) []string {
	fa, fb := map[string]string{}, map[string]string{}
	var va, vb map[string]any
	json.Unmarshal(a.Resolved(), &va)
	json.Unmarshal(b.Resolved(), &vb)
	delete(va, "name")
	delete(vb, "name")
	flatten(va, "", fa)
	flatten(vb, "", fb)
	keys := map[string]bool{}
	for k := range fa {
		keys[k] = true
	}
	for k := range fb {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		x, okx := fa[k]
		y, oky := fb[k]
		if !okx {
			x = "(not stated)"
		}
		if !oky {
			y = "(not stated)"
		}
		if x != y {
			out = append(out, fmt.Sprintf("%s: %s -> %s", k, x, y))
		}
	}
	sort.Strings(out)
	return out
}

func flatten(m map[string]any, prefix string, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flatten(sub, key, out)
			continue
		}
		b, _ := json.Marshal(v)
		out[key] = string(b)
	}
}

// Values returns every stated value of a resolved arm by its dotted field
// name, as text. The keys are those of Origins.
func Values(a *Arm) map[string]string {
	var v map[string]any
	json.Unmarshal(a.Resolved(), &v)
	delete(v, "name")
	out := map[string]string{}
	flatten(v, "", out)
	return out
}
