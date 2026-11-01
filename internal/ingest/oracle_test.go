package ingest

import (
	"encoding/json"
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

func TestPartialScore(t *testing.T) {
	for name, c := range map[string]struct {
		out           string
		passed, total int
		ok            bool
	}{
		"checker":  {"building\n" + `{"ok":false,"stage":"done","passed":7,"total":9,"failures":["a","b"],"output":""}` + "\n", 7, 9, true},
		"game":     {`{"ok":true,"frames_run":300,"reached_playing":true,"game_logic":{"ok":false,"passed":11,"total":12,"failed":["x"]}}`, 11, 12, true},
		"pytest":   {"PASSED test_a\nFAILED test_b\n=========== 3 passed, 1 failed, 2 skipped in 0.52s ===========\n", 3, 4, true},
		"pytest e": {"==== 1 failed, 2 errors, 1 warning in 12.30s (0:00:12) ====", 0, 3, true},
		"none":     {"RESULT ok\n", 0, 0, false},
		"build":    {`{"ok":false,"stage":"build","passed":0,"total":0}`, 0, 0, false},
	} {
		p, tt, ok := PartialScore(c.out)
		if p != c.passed || tt != c.total || ok != c.ok {
			t.Errorf("%s: %d/%d %v", name, p, tt, ok)
		}
	}
}

func TestOracleFindings(t *testing.T) {
	yes := true
	prov, _ := json.Marshal(map[string]any{
		"oracle":  map[string]any{"Oracle": "tests", "Passed": false},
		"oracles": []map[string]any{{"Oracle": "scope", "Passed": false, "ExitCode": 1}, {"Oracle": "lint", "Passed": true}},
	})
	r := &store.Run{ID: "r", Verdict: store.Fail, ClaimedSuccess: &yes, Provenance: prov}
	f, m := oracleFindings(r, map[string]string{"tests": `{"passed":2,"total":4}`, "scope": "touched ../x\n"})
	got := map[string]int{}
	for _, x := range f {
		got[x.Detector]++
	}
	if got[SecondaryOracle] != 1 || got[ClaimedNotPassed] != 1 || len(f) != 2 {
		t.Errorf("findings: %+v", f)
	}
	share := 0.0
	for _, x := range m {
		if x.Name == "oracle.partial_share" {
			share = x.Value
		}
	}
	if len(m) != 3 || share != 0.5 {
		t.Errorf("metrics: %+v", m)
	}
	r.Verdict = store.Pass
	if f, _ := oracleFindings(r, nil); len(f) != 1 || f[0].Detector != SecondaryOracle {
		t.Errorf("a pass that was claimed is no finding: %+v", f)
	}
}
