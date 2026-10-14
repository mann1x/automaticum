package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The reference code's own fixtures (omnimergekit, eval/agentic/
// loop_detect.py, selftest, as of its commit 3b30097): synthetic texts and
// the detector each must or must not fire. Criterion 8 of 110 §1.
func referenceFixtures() []struct {
	name, text string
	fires      []string
} {
	var draftParts []string
	for j := 0; j < 3; j++ {
		var b strings.Builder
		for i := 0; i < 12; i++ {
			fmt.Fprintf(&b, "Distinct reasoning step %d follows here now. ", i+j*12)
		}
		draftParts = append(draftParts, b.String())
	}
	var healthy []string
	for i := 0; i < 200; i++ {
		healthy = append(healthy, fmt.Sprintf("Step %d: consider option %d carefully.", i, i))
	}
	rep := strings.Repeat
	return []struct {
		name, text string
		fires      []string
	}{
		{"char unit x12", rep("Let me check the file. ", 12), []string{ShortUnitRepeat}},
		{"single char x60", rep("a", 60), []string{CharRun}},
		{"sentence x6", rep("I should call get_stock_info to retrieve the data for NVDA now. ", 6), []string{SentenceRepeat}},
		{"paragraph(180) x4", rep("I have already retrieved the watchlist and the stock info. I will now verify the directory contents before proceeding with the next tool call in this sequence. ", 4), []string{SentenceRepeat}},
		{"real: search x402", rep(`I'll try to search for "revenue" in all files in the current directory. `, 402), []string{SentenceRepeat}},
		{"code indentation", "def f():\n" + rep("        x = 1\n", 12), nil},
		{"two-space run", rep("    ", 40), nil},
		{"numpy array print", "First row: [" + rep("0. ", 30) + "]", nil},
		{"ascii rule", rep("=", 60), nil},
		{"dash rule", rep("-", 60), nil},
		{"drafting 3x low-share", strings.Join(draftParts, "The function signature is sample_envelope with five arguments. "), nil},
		{"healthy long reasoning", strings.Join(healthy, " "), nil},
	}
}

// The three detectors of the requirements, by the reference's flag names.
var referenceFlags = map[string]string{ShortUnitRepeat: "rep", CharRun: "runchar", SentenceRepeat: "sentence"}

func flags(text string, o Options) map[string]bool {
	out := map[string]bool{"rep": false, "runchar": false, "sentence": false}
	for _, h := range Block(text, o) {
		if f, ok := referenceFlags[h.Detector]; ok {
			out[f] = true
		}
	}
	return out
}

// Each fixture must fire the detector the reference names, and the healthy
// ones none of the three, in both ways of matching.
func TestReferenceFixtures(t *testing.T) {
	for _, c := range referenceFixtures() {
		for _, o := range []Options{{}, {FirstMatch: true}} {
			got := flags(c.text, o)
			for _, d := range c.fires {
				if !got[referenceFlags[d]] {
					t.Errorf("%s (%s): %s did not fire", c.name, o.Matching(), d)
				}
			}
			if c.fires == nil && (got["rep"] || got["runchar"] || got["sentence"]) {
				t.Errorf("%s (%s): fired %v", c.name, o.Matching(), got)
			}
		}
	}
}

// TestAgainstReference runs the reference code itself and compares, block
// by block. It needs AUTOMATICUM_REFERENCE (the path of loop_detect.py) and
// python3; AUTOMATICUM_BLOCKS may name a JSON file holding a list of texts
// to add to the fixtures. The reference code says which way it matches
// (its version 2 matches every repeat, as this library does; version 1,
// with no such field, the first only): this library, matching the same
// way, must give the reference's three flags on every block.
func TestAgainstReference(t *testing.T) {
	ref := os.Getenv("AUTOMATICUM_REFERENCE")
	if ref == "" {
		t.Skip("AUTOMATICUM_REFERENCE not set")
	}
	var texts []string
	for _, c := range referenceFixtures() {
		texts = append(texts, c.text)
	}
	for _, c := range Controls() {
		texts = append(texts, c.Text)
	}
	texts = append(texts, strings.Repeat("    ", 12)+strings.Repeat("hello wor ", 10), strings.Repeat("=", 41)+strings.Repeat("a", 41),
		strings.Repeat("é", 41), strings.Repeat("12 ", 10), "one\ntwo\n"+strings.Repeat("ab\ncd\n", 9))
	if p := os.Getenv("AUTOMATICUM_BLOCKS"); p != "" {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var more []string
		if err := json.Unmarshal(raw, &more); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, more...)
	}
	in, _ := json.Marshal(texts)
	cmd := exec.Command("python3", "-I", "-c", `
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("ref", sys.argv[1]); ref = importlib.util.module_from_spec(spec); spec.loader.exec_module(ref)
out = []
for t in json.load(sys.stdin):
    d = ref.detect(t, runaway_chars=10**12)
    out.append({"rep": d["rep"], "runchar": d["runchar"], "sentence": d["sentence"], "every": d.get("matching") == "every_match"})
json.dump(out, sys.stdout)`, ref)
	cmd.Stdin = strings.NewReader(string(in))
	cmd.Stderr = os.Stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var want []map[string]bool
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	differ, fired := 0, map[string]int{}
	o := Options{FirstMatch: len(want) > 0 && !want[0]["every"]}
	for i, text := range texts {
		got := flags(text, o)
		for _, f := range []string{"rep", "runchar", "sentence"} {
			if want[i][f] {
				fired[f]++
			}
			if got[f] != want[i][f] {
				differ++
				t.Errorf("block %d (%d characters, %.40q): %s is %v here (%s), %v by the reference code", i, len([]rune(text)), text, f, got[f], o.Matching(), want[i][f])
			}
		}
	}
	t.Logf("%d blocks, matching %s on both sides; the reference code fired %v; %d differences", len(texts), o.Matching(), fired, differ)
}

// TestCohorts reads real cohorts packaged by the reference's owners: one
// text block per line with the reference's own expected output, for both
// of its versions ("expected_v2", every match; "expected_v1", first match).
// AUTOMATICUM_COHORTS lists the files, separated by ":". They are model
// transcripts on benchmark tasks and stay out of this repository. The
// runaway size of the cohorts is 10,000 characters, given here explicitly.
func TestCohorts(t *testing.T) {
	files := os.Getenv("AUTOMATICUM_COHORTS")
	if files == "" {
		t.Skip("AUTOMATICUM_COHORTS not set")
	}
	type expected struct {
		Rep, Runchar, Sentence, Runaway bool
		SentenceReps                    int `json:"sentence_reps"`
		Chars                           int
	}
	for _, path := range strings.Split(files, ":") {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		blocks, differ, fired := 0, map[string]int{}, map[string]int{}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row struct {
				Text string
				V2   expected `json:"expected_v2"`
				V1   expected `json:"expected_v1"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			blocks++
			for _, c := range []struct {
				name string
				o    Options
				want expected
			}{{"v2", Options{RunawayChars: 10000}, row.V2}, {"v1", Options{RunawayChars: 10000, FirstMatch: true}, row.V1}} {
				got := map[string]bool{}
				for _, h := range Block(row.Text, c.o) {
					got[h.Detector] = true
				}
				for d, w := range map[string]bool{ShortUnitRepeat: c.want.Rep, CharRun: c.want.Runchar, SentenceRepeat: c.want.Sentence, Runaway: c.want.Runaway} {
					if w {
						fired[c.name+" "+d]++
					}
					if got[d] != w {
						differ[c.name+" "+d]++
						if differ[c.name+" "+d] <= 3 {
							t.Errorf("%s, %s: %s is %v here, %v by the reference (%d characters, %.60q)", path[strings.LastIndex(path, "/")+1:], c.name, d, got[d], w, c.want.Chars, row.Text)
						}
					}
				}
				if n := len([]rune(row.Text)); n != c.want.Chars {
					differ[c.name+" length"]++
				}
			}
		}
		t.Logf("%s: %d blocks; the reference fired %v; differences %v", path[strings.LastIndex(path, "/")+1:], blocks, fired, differ)
	}
}
