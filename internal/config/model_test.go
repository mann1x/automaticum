package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModels(t *testing.T) {
	// The repository's own catalogue reads, and names the tags its arms use.
	models, err := LoadModels("../../models")
	if err != nil {
		t.Fatal(err)
	}
	arms, err := LoadArms("../../arms")
	if err != nil {
		t.Fatal(err)
	}
	for name, a := range arms {
		if models[a.Model.Model] == nil {
			t.Errorf("arm %s uses tag %s, which the catalogue does not describe", name, a.Model.Model)
		}
	}
	if m := models["stub:latest"]; m == nil || len(m.Missing()) != 0 {
		t.Errorf("stub: %+v", m)
	}
	var none *ModelIdentity
	if got := strings.Join(none.Missing(), ","); got != "family,size,variant" {
		t.Errorf("no entry: %s", got)
	}
	if got := strings.Join((&ModelIdentity{Family: "F", Size: " "}).Missing(), ","); got != "size,variant" {
		t.Errorf("half an entry: %s", got)
	}

	dir := t.TempDir()
	write := func(name, text string) { os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644) }
	write("a.toml", "tag = \"m:1\"\nfamily = \"F\"\nsize = \"1B\"\nvariant = \"merge v4\"\nlineage = \"A + B\"\n")
	if m, err := LoadModels(dir); err != nil || m["m:1"].Variant != "merge v4" || m["m:1"].File != "a.toml" {
		t.Fatalf("%+v %v", m, err)
	}
	write("b.toml", "tag = \"m:1\"\nfamily = \"G\"\n")
	if _, err := LoadModels(dir); err == nil || !strings.Contains(err.Error(), "described twice") {
		t.Errorf("one tag in two files: %v", err)
	}
	write("b.toml", "family = \"G\"\n")
	if _, err := LoadModels(dir); err == nil {
		t.Error("an entry without a tag was accepted")
	}
	write("b.toml", "tag = \"m:2\"\nfamilly = \"G\"\n")
	if _, err := LoadModels(dir); err == nil {
		t.Error("an unknown field was accepted")
	}
	if m, err := LoadModels(filepath.Join(dir, "absent")); err != nil || len(m) != 0 {
		t.Errorf("no catalogue directory: %v %v", m, err)
	}
}
