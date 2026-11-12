package detect

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// BlockRepeats counts as the client's loopscan score() does: 43 blocks
// (random sentences with repeats, every separator, accented letters, an
// empty block, one too short, line 90 written 25 times) were scored by
// that Python, verbatim, into testdata/block_repeats.json.
func TestBlockRepeatsAsLoopscan(t *testing.T) {
	b, err := os.ReadFile("testdata/block_repeats.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text   string `json:"text"`
		Units  int    `json:"units"`
		MaxRep int    `json:"maxrep"`
		Top    string `json:"top"`
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		got := BlockRepeats(c.Text, nil)
		if got.Units != c.Units || got.Max != c.MaxRep || got.Top != c.Top {
			t.Errorf("case %d: got %+v, loopscan units %d maxrep %d top %q", i, got, c.Units, c.MaxRep, c.Top)
		}
	}
	if got := BlockRepeats(cases[len(cases)-1].Text, Values{BlockKeyChars: 10}); got.Units != 0 {
		t.Errorf("a key cut below its minimum still counted: %+v", got)
	}
}

// Every threshold has a default, an origin and a source; overrides are
// checked by name, sign and kind, and resolved over the installation's
// with where each value came from.
func TestThresholds(t *testing.T) {
	for _, th := range Thresholds() {
		if th.Source == "" || th.Doc == "" || (th.Origin != "sdk" && th.Origin != "owner") {
			t.Errorf("%s: origin %q, source %q", th.Name, th.Origin, th.Source)
		}
	}
	if Values(nil).Int(BlockMaxRepeat) != 20 {
		t.Error("the owner's default of 20")
	}
	for _, bad := range [][]string{{"nope=1"}, {BlockMaxRepeat + "=2.5"}, {BlockMaxRepeat + "=-1"}, {BlockMaxRepeat}, {BlockMaxRepeat + "=x"}} {
		if _, err := ParseValues(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	cli, err := ParseValues([]string{BlockMaxRepeat + "=10"})
	if err != nil {
		t.Fatal(err)
	}
	merged, eff := Resolve(Values{BlockMaxRepeat: 15, BlockUnitChars: 30}, cli)
	if merged.Int(BlockMaxRepeat) != 10 || merged.Int(BlockUnitChars) != 30 || merged.Int(BlockKeyChars) != 80 {
		t.Errorf("merged %v", merged)
	}
	from := map[string]string{}
	for _, e := range eff {
		from[e.Name] = e.From
	}
	if from[BlockMaxRepeat] != "cli" || from[BlockUnitChars] != "config" || from[BlockKeyChars] != "owner default" {
		t.Errorf("from %v", from)
	}
	if err := (Values{"reasoning_block.nope": 1}).Check(); err == nil || !strings.Contains(err.Error(), BlockMaxRepeat) {
		t.Errorf("an unknown name, and the names listed: %v", err)
	}
}
