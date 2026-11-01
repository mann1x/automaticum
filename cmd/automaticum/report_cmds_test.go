package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mann1x/automaticum/internal/daemon"
)

func TestWriteRunsCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.csv")
	rows := []daemon.CellRow{
		{RunID: "r1", Task: "demo/answer", Verdict: "PASS", CountedAs: "pass", Metrics: map[string]float64{"wall_s": 12.5, "turns": 3}, Findings: map[string]int{"reasoning_loop": 1}},
		{RunID: "r2", Task: "demo/answer", Verdict: "FAIL", CountedAs: "fail", Metrics: map[string]float64{"turns": 4}},
	}
	if err := writeRunsCSV(path, rows); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	got, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"run_id", "task", "verdict", "verdict_reason", "counted_as", "retry_of", "superseded_by", "finding.reasoning_loop", "turns", "wall_s"},
		{"r1", "demo/answer", "PASS", "", "pass", "", "", "1", "3", "12.5"},
		{"r2", "demo/answer", "FAIL", "", "fail", "", "", "0", "4", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestPositionalAndFormat(t *testing.T) {
	if got := positional([]string{"--all", "a", "--server", "x", "b", "--json"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("positional: %q", got)
	}
	for f, ok := range map[string]bool{"jsonl": true, "csv": true, "parquet": false, "xml": false} {
		if err := checkFormat(f); (err == nil) != ok {
			t.Errorf("%s: %v", f, err)
		}
	}
}
