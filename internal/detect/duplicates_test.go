package detect

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDefinitions(t *testing.T) {
	text := "function a() {}\nfunction a() {}\n  def b(x):\nfn c() {\nfunc d() {\nif (x) {\nfor (;;) {\n  update(dt) {\nclass X {\n"
	got := Definitions(text)
	want := map[string]int{"a": 2, "b": 1, "c": 1, "d": 1, "update": 1}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %d, want %d", k, got[k], v)
		}
	}
}

func TestDuplicateDefinitionCalls(t *testing.T) {
	at := func(n int) string {
		in, _ := json.Marshal(map[string]string{"file_path": "a", "new_text": strings.Repeat("function f() {}\n", n+1)})
		return string(in)
	}
	// At the measured most, nothing; one more, a firing. A call cut off
	// mid-string is read as far as it went.
	got := DuplicateDefinitionCalls([]Call{{Turn: 1, Tool: "editor", Input: at(DuplicateDefinitionsMax)}, {Turn: 2, Tool: "editor", Input: at(DuplicateDefinitionsMax + 1)},
		{Turn: 3, Tool: "editor", Input: `{"new_text":"` + strings.Repeat(`function g() {}\n`, 9)}})
	if len(got) != 2 || got[0].Turn != 2 || got[0].Repeated != DuplicateDefinitionsMax+1 || got[0].Cut || got[1].Turn != 3 || !got[1].Cut || got[1].Top[0].Name != "g" {
		t.Fatalf("%+v", got)
	}
}

// TestDuplicateDefinitionsCanary is a client's trained model's editor call
// (bm-889 sft5 step 200 gate, run 20261103-000031-39ec08-001, request 13),
// as the server streamed it until the output limit cut it at 96,000
// tokens: 215,471 characters (218,643 as the escaped JSON of the arguments) rewriting a file of about 130 lines.
func TestDuplicateDefinitionsCanary(t *testing.T) {
	f, err := os.Open("testdata/duplicate_definitions_call.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var c struct{ Tool, Arguments string }
	if err := json.NewDecoder(z).Decode(&c); err != nil {
		t.Fatal(err)
	}
	got := DuplicateDefinitionCalls([]Call{{Turn: 5, Tool: c.Tool, Input: c.Arguments}})
	if len(got) != 1 || got[0].In != "new_text" || !got[0].Cut || got[0].Definitions != 1541 || got[0].Names != 1049 || got[0].Repeated != 492 {
		t.Fatalf("%+v", got)
	}
}
