package main

import (
	"encoding/json"
	"testing"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/ingest"
)

// TestTemplateText is run show's prompt template line and the two
// findings' lines (D155, D157).
func TestTemplateText(t *testing.T) {
	for raw, want := range map[string]string{
		`{"use":"auto","name":"qwen","source":"builtin","overlaid":true,"family":"qwen35moe","lookup":"asked"}`:                                      "qwen (builtin) over default; auto, family qwen35moe from the server",
		`{"use":"auto","name":"default","source":"builtin","overlaid":false,"family":null,"lookup":"asked","disabled":["qwen"],"disabled_by":"sdk"}`: "default (builtin); auto, the server gave no family; left out: qwen (by the sdk)",
		`{"use":"auto","name":"claude","source":"builtin","overlaid":true,"family":null,"lookup":"not_asked"}`:                                       "claude (builtin) over default; auto, by name and provider",
		`{"use":"auto","name":null,"lookup":"not_in_build"}`:                                                                                         "none: the SDK's built-in prompt; auto, by name and provider: this SDK build does not ask the family",
		`{"use":"custom","name":"mine","source":"global","overlaid":true,"file_name":"mine.md","sha256":"0123456789abcdef"}`:                         "mine (global) over default; custom mine.md, sha256 0123456789ab",
		`{"use":"gemma","name":"gemma","source":"builtin","overlaid":true}`:                                                                          "gemma (builtin) over default; forced: use = gemma",
	} {
		if got := templateText(json.RawMessage(raw)); got != want {
			t.Errorf("%s:\n got %q\nwant %q", raw, got, want)
		}
	}
	f := daemon.FindingView{Detector: ingest.PromptTemplateIncomplete, Severity: "warning", Count: 3,
		Evidence: json.RawMessage(`{"template":"mine","use":"custom","problems":[{"code":"missing-required-placeholder","section":"system","message":"{{CWD}} is missing"}],"uncovered_tools":["browser"],"file_errors":[{"file_name":"b.md","source":"workspace","message":"no frontmatter"}]}`)}
	want := "warning  prompt_template_incomplete  template mine is missing something: missing-required-placeholder (system): {{CWD}} is missing; no section, built-in text kept: browser; b.md (workspace) did not parse: no frontmatter"
	if got := findingLine(f); got != want {
		t.Errorf("finding:\n got %q\nwant %q", got, want)
	}
}
