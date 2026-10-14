package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/ingest"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// Basis is every recorded fact that must be equal for two groups of runs
// to be compared (020 §5), for one arm on one task. A fact whose value
// differs between the group's runs has every value, and is Mixed.
type Basis struct {
	Fields []BasisField `json:"fields"`
	// SHA256 is the hash of the canonical form: one "name=value" line per
	// field, in order, mixed values joined in sorted order.
	SHA256 string `json:"sha256"`
}

// BasisField is one fact of a basis.
type BasisField struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
	Mixed  bool     `json:"mixed,omitempty"`
}

// notRecorded marks a fact the run did not record, with why; it is never a
// default.
const notRecorded = "not recorded: "

// basisFields are the facts of a basis in order, each with the row of 020
// §5 it answers.
var basisFields = []string{
	"weights",              // weights: source, revision, sha256 per file
	"quantization",         // quantization and imatrix, from the weights' metadata
	"imatrix",              //
	"template",             // chat template as served
	"parser",               // reasoning and tool-call parser flags
	"sampler",              // sampler as sent, with its name
	"tag parameters",       // the tag's own values, stop sequences included
	"thinking",             // on/off, effort, budget, owner
	"output limit",         // output budget per request
	"context",              // context per slot as loaded
	"server",               // server kind, version, engine
	"concurrency",          // concurrency and batch composition
	"GPU",                  // GPU, residency
	"GPU driver and power", //
	"harness build",        // SDK release and package, runner, image
	"agent settings",       // the arm's agent, guards, compaction, limits, protocol
	"task",                 // task, version, hash of its files
	"scorer",               // the oracle
	"detectors",            // detector versions
	"seed",                 // seed set, when sampling
}

// basisOfRun reads one run's basis from its record, metrics and findings.
func basisOfRun(rec *store.Run, metrics map[string]float64) map[string]string {
	var p struct {
		Automaticum    string `json:"automaticum"`
		RunnerProtocol int    `json:"runner_protocol"`
		SDK            struct {
			Release       string `json:"release"`
			PackageSHA256 string `json:"package_sha256"`
		} `json:"sdk"`
		Image struct {
			ID string `json:"id"`
		} `json:"image"`
		Task struct {
			Hash string `json:"hash"`
		} `json:"task"`
		Inference *struct {
			Kind          string `json:"kind"`
			ServerVersion string `json:"server_version"`
			Engine        string `json:"engine"`
			GPU           struct {
				Declared string  `json:"declared"`
				VRAMGB   float64 `json:"vram_gb"`
			} `json:"gpu"`
			Model struct {
				Digest  string         `json:"digest"`
				Weights map[string]any `json:"weights"`
				Server  struct {
					Quantization string `json:"quantization_level"`
				} `json:"server"`
			} `json:"model"`
		} `json:"inference"`
		Before *run.Identity `json:"endpoint_before"`
		After  *run.Identity `json:"endpoint_after"`
		Oracle struct {
			Oracle  string `json:"Oracle"`
			ImageID string `json:"ImageID"`
		} `json:"oracle"`
	}
	json.Unmarshal(rec.Provenance, &p)
	var arm map[string]json.RawMessage
	json.Unmarshal(rec.Arm, &arm)
	canon := func(keys ...string) string {
		part := map[string]any{}
		for _, k := range keys {
			var v any
			if json.Unmarshal(arm[k], &v) == nil && v != nil {
				part[k] = v
			}
		}
		if len(part) == 0 {
			return ""
		}
		b, _ := json.Marshal(part) // a map's keys are written in order
		return string(b)
	}
	short := func(s string) string {
		s = strings.TrimPrefix(s, "sha256:")
		if len(s) > 12 {
			return s[:12]
		}
		return s
	}
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:6])
	}
	out := map[string]string{}
	set := func(name, value, why string) {
		if value == "" {
			value = notRecorded + why
		}
		out[name] = value
	}
	id := p.Before
	weights := map[string]any(nil)
	if p.Inference != nil {
		weights = p.Inference.Model.Weights
	} else if id != nil {
		weights = id.Weights
	}
	digest := ""
	if id != nil {
		digest = id.Digest
	}
	if p.Inference != nil && p.Inference.Model.Digest != "" {
		digest = p.Inference.Model.Digest
	}
	if digest != "" {
		digest = "digest " + short(digest)
		if ft, ok := weights["general.finetune"].(string); ok && ft != "" {
			digest += ", finetune " + ft
		}
	}
	set("weights", digest, "the run did not probe the model")
	quant := ""
	if p.Inference != nil {
		quant = p.Inference.Model.Server.Quantization
	}
	if ft, ok := weights["general.file_type"].(float64); ok {
		quant = strings.TrimSpace(fmt.Sprintf("%s, file type %g", quant, ft))
	}
	set("quantization", quant, "the server did not say")
	set("imatrix", "", "the weights' quantization metadata is not read through the server")
	if id != nil && id.TemplateSHA256 != "" {
		set("template", fmt.Sprintf("%s, %d bytes", short(id.TemplateSHA256), id.TemplateBytes), "")
		set("parser", fmt.Sprintf("renderer %q, parser %q", id.Renderer, id.Parser), "")
		if id.Parameters != "" {
			set("tag parameters", hash(run.ParametersKey(id.Parameters)), "")
		} else {
			set("tag parameters", "none", "")
		}
	} else {
		set("template", "", "this kind of endpoint does not say")
		set("parser", "", "this kind of endpoint does not say")
		set("tag parameters", "", "this kind of endpoint does not say")
	}
	set("sampler", canon("sampler"), "the arm has no sampler section")
	thinking := canon("thinking")
	if b, ok := metrics["thinking.budget_tokens.served"]; ok {
		thinking += fmt.Sprintf(", budget served %g tokens", b)
	}
	set("thinking", thinking, "the arm has no thinking section")
	if v, ok := metrics["cap.declared_tokens"]; ok {
		set("output limit", fmt.Sprintf("%g tokens", v), "")
	} else {
		set("output limit", "", "no request declared one, or the run was analysed before the cap audit")
	}
	ctx := ""
	if p.After != nil && p.After.Loaded != nil && p.After.Loaded.ContextLength > 0 {
		ctx = fmt.Sprintf("%d tokens as loaded", p.After.Loaded.ContextLength)
	}
	set("context", ctx, "the server did not say how the model was loaded")
	server := ""
	if p.Inference != nil {
		server = strings.TrimSpace(strings.Join([]string{p.Inference.Kind, p.Inference.ServerVersion, p.Inference.Engine}, " "))
	} else if id != nil {
		server = strings.TrimSpace(id.Kind + " " + id.ServerVersion + " " + id.Engine)
	}
	set("server", server, "the run did not probe the endpoint")
	// A lane runs one run at a time, and the free gate waits for an
	// endpoint holding no other model (050 §3.1).
	set("concurrency", "one run on the lane, no other model on the endpoint", "")
	gpu := ""
	if p.Inference != nil && p.Inference.GPU.Declared != "" {
		gpu = fmt.Sprintf("%s, %g GB, declared", p.Inference.GPU.Declared, p.Inference.GPU.VRAMGB)
	}
	if share, ok := metrics["endpoint.gpu_share"]; ok {
		gpu = strings.TrimPrefix(fmt.Sprintf("%s, %.0f%% on the GPU", gpu, 100*share), ", ")
	}
	set("GPU", gpu, "neither declared in the configuration nor reported by the server")
	set("GPU driver and power", "", "needs agent mode on the endpoint's host")
	if p.SDK.Release != "" {
		set("harness build", fmt.Sprintf("SDK %s (package %s), runner protocol %d, image %s, automaticum %s",
			p.SDK.Release, short(p.SDK.PackageSHA256), p.RunnerProtocol, short(p.Image.ID), p.Automaticum), "")
	} else {
		set("harness build", "", "the run did not record its SDK")
	}
	if a := canon("agent", "guards", "compaction", "limits", "protocol"); a != "" {
		set("agent settings", hash(a), "")
	} else {
		set("agent settings", "", "the arm has none")
	}
	set("task", fmt.Sprintf("%s/%s version %d, files %s", rec.Suite, rec.TaskID, rec.TaskVersion, short(p.Task.Hash)), "")
	if p.Oracle.Oracle != "" {
		set("scorer", fmt.Sprintf("%s, image %s", p.Oracle.Oracle, short(p.Oracle.ImageID)), "")
	} else {
		set("scorer", "", "no oracle ran")
	}
	// The detectors a run was analysed with, whether they found anything
	// or not: what analysis version it carries says.
	switch v, ok := metrics["analysis.version"]; {
	case !ok:
		set("detectors", "", "analysed before analyses recorded their version: `automaticum reanalyse` records it")
	case int(v) == ingest.AnalysisVersion:
		set("detectors", fmt.Sprintf("analysis %d: %s", ingest.AnalysisVersion, ingest.VersionsText()), "")
	default:
		set("detectors", fmt.Sprintf("analysis %g", v), "")
	}
	var sampler struct {
		Seed *int64 `json:"seed"`
	}
	json.Unmarshal(arm["sampler"], &sampler)
	if sampler.Seed != nil {
		set("seed", fmt.Sprint(*sampler.Seed), "")
	} else {
		set("seed", "none set", "")
	}
	return out
}

// basisOf gathers the bases of a group's runs.
func basisOf(runs []map[string]string) Basis {
	var b Basis
	var canon strings.Builder
	for _, name := range basisFields {
		f := BasisField{Name: name}
		for _, r := range runs {
			if v := r[name]; !slices.Contains(f.Values, v) {
				f.Values = append(f.Values, v)
			}
		}
		sort.Strings(f.Values)
		f.Mixed = len(f.Values) > 1
		b.Fields = append(b.Fields, f)
		fmt.Fprintf(&canon, "%s=%s\n", name, strings.Join(f.Values, " | "))
	}
	sum := sha256.Sum256([]byte(canon.String()))
	b.SHA256 = hex.EncodeToString(sum[:])
	return b
}

// BasisDiff is one fact in which two bases differ.
type BasisDiff struct {
	Name string   `json:"name"`
	A    []string `json:"a"`
	B    []string `json:"b"`
}

// DiffBasis lists exactly the facts in which two bases differ.
func DiffBasis(a, b Basis) []BasisDiff {
	out := []BasisDiff{}
	for i := range a.Fields {
		if i < len(b.Fields) && !slices.Equal(a.Fields[i].Values, b.Fields[i].Values) {
			out = append(out, BasisDiff{Name: a.Fields[i].Name, A: a.Fields[i].Values, B: b.Fields[i].Values})
		}
	}
	return out
}

// BasisGroup is the basis of one arm on one task over some batches.
type BasisGroup struct {
	Arm   string   `json:"arm"`
	ArmID string   `json:"arm_id"`
	Task  string   `json:"task"`
	Runs  []string `json:"runs"`
	Basis Basis    `json:"basis"`
}

// handleBasis gives the basis of every arm on every task of some batches,
// over the runs that `compare` counts.
func (d *Daemon) handleBasis(w http.ResponseWriter, r *http.Request) {
	v, code, err := d.compareView(r.Context(), r.URL.Query()["batch"])
	if err != nil {
		fail(w, code, "%v", err)
		return
	}
	out := []BasisGroup{}
	for _, g := range v.Groups {
		out = append(out, BasisGroup{Arm: g.Arm, ArmID: g.ArmID, Task: g.Task, Runs: g.Runs, Basis: g.Basis})
	}
	reply(w, http.StatusOK, out)
}
