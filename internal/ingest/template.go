package ingest

import (
	"encoding/json"

	"github.com/mann1x/automaticum/internal/store"
)

// PromptTemplateUsed is the finding that says which prompt template the
// session was given (040 §6.1.4, D155, D157): the runner's account from
// its prepared note, as it wrote it (use, name, file_name, source,
// overlaid, family, lookup, and sha256 for a custom one, disabled and
// disabled_by when the arm leaves templates out). On every run of
// protocol 12 on; it judges nothing.
const PromptTemplateUsed = "prompt_template"

// PromptTemplateIncomplete is the finding that the template the session
// was given is missing something (D157): the SDK's own check
// (validatePromptTemplate, run by the runner on the rendered template)
// reported a problem, such as {{CLINE_RULES}} (the plan-mode contract) or
// {{CWD}} missing, an unknown placeholder, an empty or repeated section, a
// compaction section without its token; or a tool the requests offered
// has no section, so it kept its built-in text; or a template file in the
// directories did not parse. A warning when the check reported a problem,
// a note otherwise.
const PromptTemplateIncomplete = "prompt_template_incomplete"

// TemplateCheck is the prepared note's prompt_template_check (protocol 13).
type TemplateCheck struct {
	Checked  bool              `json:"checked"`
	Problems []TemplateProblem `json:"problems"`
	Files    struct {
		Errors   []TemplateFileError `json:"errors"`
		Warnings []TemplateProblem   `json:"warnings"`
	} `json:"files"`
}

// TemplateProblem is one warning of the SDK's check: its code, the
// section ("system" or "tool: <name>") and its message; for a template
// file's warning, the file and where it was found.
type TemplateProblem struct {
	FileName string `json:"file_name,omitempty"`
	Source   string `json:"source,omitempty"`
	Code     string `json:"code"`
	Section  string `json:"section"`
	Message  string `json:"message"`
}

// TemplateFileError is a template file that did not parse.
type TemplateFileError struct {
	FileName string `json:"file_name"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// requiredPlaceholder is the SDK's code for a placeholder whose absence
// changes what the model does, not only what it is told.
const requiredPlaceholder = "missing-required-placeholder"

// promptTemplate writes the two findings and the prompt_template metrics:
// prompt_template.problems and .problems.required (the check's warnings,
// and those of a required placeholder), .file_errors (protocol 13 on), and
// .uncovered_tools (once a request was seen). A run of a runner before
// protocol 12 has none of them.
func promptTemplate(runID string, ev *Events, m func(string, float64), res *Result) {
	if ev.PromptTemplate == nil {
		return
	}
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: PromptTemplateUsed, DetectorVersion: Versions[PromptTemplateUsed],
		Severity: "info", Count: 1, Evidence: ev.PromptTemplate})
	c := ev.TemplateCheck
	var problems []TemplateProblem
	var fileErrors []TemplateFileError
	if c != nil {
		required := 0
		for _, p := range c.Problems {
			if p.Code == requiredPlaceholder {
				required++
			}
		}
		problems, fileErrors = c.Problems, c.Files.Errors
		if c.Checked {
			m("prompt_template.problems", float64(len(problems)))
			m("prompt_template.problems.required", float64(required))
		}
		m("prompt_template.file_errors", float64(len(fileErrors)))
	}
	if ev.UncoveredSeen {
		m("prompt_template.uncovered_tools", float64(len(ev.Uncovered)))
	}
	n := len(problems) + len(ev.Uncovered) + len(fileErrors)
	if n == 0 {
		return
	}
	severity := "note"
	if len(problems) > 0 {
		severity = "warning"
	}
	var name struct {
		Name string `json:"name"`
		Use  string `json:"use"`
	}
	json.Unmarshal(ev.PromptTemplate, &name)
	raw, _ := json.Marshal(struct {
		Template       string              `json:"template"`
		Use            string              `json:"use"`
		Problems       []TemplateProblem   `json:"problems,omitempty"`
		UncoveredTools []string            `json:"uncovered_tools,omitempty"`
		FileErrors     []TemplateFileError `json:"file_errors,omitempty"`
	}{name.Name, name.Use, problems, ev.Uncovered, fileErrors})
	res.Findings = append(res.Findings, store.Finding{RunID: runID, Detector: PromptTemplateIncomplete, DetectorVersion: Versions[PromptTemplateIncomplete],
		Severity: severity, Count: n, Evidence: raw})
}
