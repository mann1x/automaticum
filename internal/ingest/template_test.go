package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

// TestPromptTemplate proves the prompt template's findings and metrics
// (D157): which template, on every run that recorded one; what its check
// and the requests' tools found missing; nothing at all from a runner
// before protocol 12, and only the account from protocol 12.
func TestPromptTemplate(t *testing.T) {
	read := func(lines ...string) (map[string]float64, map[string]store.Finding) {
		t.Helper()
		ev, err := ReadEvents(strings.NewReader(strings.Join(lines, "\n") + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		ms, fs := map[string]float64{}, map[string]store.Finding{}
		res := &Result{}
		promptTemplate("r", ev, func(n string, v float64) { ms[n] = v }, res)
		for _, f := range res.Findings {
			fs[f.Detector] = f
		}
		return ms, fs
	}
	tpl := `"prompt_template":{"use":"auto","name":"qwen","file_name":"qwen.md","source":"builtin","overlaid":true,"family":"qwen35moe","lookup":"asked"}`
	clean := `{"t":"2026-11-20T09:00:00Z","k":"runner","note":"prepared",` + tpl + `,"prompt_template_check":{"checked":true,"problems":[],"files":{"errors":[],"warnings":[]}}}`
	tools := func(un string) string {
		return `{"t":"2026-11-20T09:00:01Z","k":"runner","note":"prompt_template_tools","template":"qwen","tools":13,"uncovered":[` + un + `]}`
	}

	ms, fs := read(clean, tools(""))
	if f, ok := fs[PromptTemplateUsed]; !ok || f.Severity != "info" || !strings.Contains(string(f.Evidence), `"family":"qwen35moe"`) {
		t.Errorf("which template: %+v", fs)
	}
	if _, ok := fs[PromptTemplateIncomplete]; ok {
		t.Errorf("a clean template is not incomplete")
	}
	for n, want := range map[string]float64{"prompt_template.problems": 0, "prompt_template.problems.required": 0, "prompt_template.file_errors": 0, "prompt_template.uncovered_tools": 0} {
		if v, ok := ms[n]; !ok || v != want {
			t.Errorf("%s = %v (%v), want %v", n, v, ok, want)
		}
	}

	broken := `{"t":"2026-11-20T09:00:00Z","k":"runner","note":"prepared","prompt_template":{"use":"custom","name":"mine","sha256":"ab"},"prompt_template_check":{"checked":true,` +
		`"problems":[{"code":"missing-required-placeholder","section":"system","message":"{{CLINE_RULES}} is missing"},{"code":"unknown-placeholder","section":"system","message":"{{cwd}}"}],` +
		`"files":{"errors":[{"file_name":"b.md","source":"workspace","message":"no frontmatter"}],"warnings":[]}}}`
	ms, fs = read(broken, tools(`"browser"`), tools(`"ask_lsp","browser"`))
	f, ok := fs[PromptTemplateIncomplete]
	if !ok || f.Severity != "warning" || f.Count != 5 {
		t.Fatalf("incomplete: %+v", f)
	}
	var e struct {
		Template       string              `json:"template"`
		Use            string              `json:"use"`
		Problems       []TemplateProblem   `json:"problems"`
		UncoveredTools []string            `json:"uncovered_tools"`
		FileErrors     []TemplateFileError `json:"file_errors"`
	}
	json.Unmarshal(f.Evidence, &e)
	if e.Template != "mine" || e.Use != "custom" || len(e.Problems) != 2 || strings.Join(e.UncoveredTools, ",") != "ask_lsp,browser" || len(e.FileErrors) != 1 {
		t.Errorf("evidence %s", f.Evidence)
	}
	if ms["prompt_template.problems"] != 2 || ms["prompt_template.problems.required"] != 1 || ms["prompt_template.uncovered_tools"] != 2 || ms["prompt_template.file_errors"] != 1 {
		t.Errorf("metrics %v", ms)
	}

	// Only tools without a section: a note.
	_, fs = read(clean, tools(`"browser"`))
	if f := fs[PromptTemplateIncomplete]; f.Severity != "note" || f.Count != 1 {
		t.Errorf("uncovered only: %+v", f)
	}

	// Protocol 12: the account, no check, no coverage.
	ms, fs = read(`{"t":"2026-11-20T09:00:00Z","k":"runner","note":"prepared",` + tpl + `}`)
	if _, ok := fs[PromptTemplateUsed]; !ok || len(fs) != 1 || len(ms) != 0 {
		t.Errorf("protocol 12: %v %v", fs, ms)
	}
	// Before protocol 12: nothing.
	ms, fs = read(`{"t":"2026-11-20T09:00:00Z","k":"runner","note":"prepared","lsp":null}`)
	if len(fs) != 0 || len(ms) != 0 {
		t.Errorf("before protocol 12: %v %v", fs, ms)
	}
	// A bare Events, as the daemon's detect view starts from, reads them too.
	bare := &Events{}
	if err := bare.Read(strings.NewReader(tools(`"browser"`) + "\n")); err != nil || !bare.UncoveredSeen || len(bare.Uncovered) != 1 {
		t.Errorf("bare: %v %v", err, bare.Uncovered)
	}
}
