package duck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/store"
)

func open(t *testing.T, path string) *State {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.duckdb")
	s := open(t, path)

	yes := true
	want := store.Run{
		ID: "20261011-144820-0432", Suite: "custom", TaskID: "manic_miner", TaskVersion: 3, ArmID: "arm-1",
		Lane: "lane-1", Host: "linux-gpu-1",
		Verdict: store.Invalid, OriginalVerdict: "FIXED", VerdictReason: "model unloaded mid-run", PriorVerdict: store.Pass,
		AgentEnd: "completed", ClaimedSuccess: &yes,
		StartedAt: time.Date(2026, 10, 11, 14, 48, 20, 0, time.UTC), EndedAt: time.Date(2026, 10, 11, 15, 2, 0, 0, time.UTC),
		WallMS: 820000, Provider: "ollama", Model: "some-model:tag", Thinking: store.Unknown,
		Arm: json.RawMessage(`{"sampler":{"temperature":0.2}}`), Provenance: json.RawMessage(`{"oracle_sha":"abc"}`),
		Imported: true, ImporterVersion: "1", Missing: []string{"wire", "thinking"},
	}
	if err := s.UpsertRun(ctx, &want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("round trip differs:\n got %+v\nwant %+v", *got, want)
	}

	// Upsert replaces.
	want.Verdict, want.PriorVerdict, want.ClaimedSuccess = store.Pass, "", nil
	if err := s.UpsertRun(ctx, &want); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountRuns(ctx, store.RunFilter{}); n != 1 {
		t.Fatalf("count after upsert = %d, want 1", n)
	}
	got, _ = s.GetRun(ctx, want.ID)
	if got.Verdict != store.Pass || got.ClaimedSuccess != nil {
		t.Fatalf("upsert did not replace: %+v", got)
	}

	if _, err := s.GetRun(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing run: err = %v, want ErrNotFound", err)
	}
	if err := s.UpsertRun(ctx, &store.Run{ID: "x", Verdict: "broken"}); err == nil {
		t.Fatal("a verdict outside the enum was accepted")
	}
	if err := s.UpsertRun(ctx, &store.Run{Verdict: store.Pass}); err == nil {
		t.Fatal("a run with no id was accepted")
	}

	// Survives a close and reopen, including an unclean one's worth of WAL.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, path)
	defer s.Close()
	if n, _ := s.CountRuns(ctx, store.RunFilter{}); n != 1 {
		t.Fatalf("count after reopen = %d, want 1", n)
	}
}

func TestFiltersAndDerivedTables(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.duckdb"))
	defer s.Close()

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		v := store.Pass
		if i%3 == 0 {
			v = store.Fail
		}
		r := store.Run{ID: fmt.Sprintf("run-%03d", i), Suite: "custom", TaskID: "t", ArmID: fmt.Sprintf("arm-%d", i%2),
			Model: fmt.Sprintf("m%d", i%2), Verdict: v, StartedAt: base.Add(time.Duration(i) * time.Hour), WallMS: int64(i * 1000)}
		if err := s.UpsertRun(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.CountRuns(ctx, store.RunFilter{Verdict: store.Fail}); n != 10 {
		t.Fatalf("FAIL count = %d, want 10", n)
	}
	if n, _ := s.CountRuns(ctx, store.RunFilter{Model: "m1", Since: base.Add(10 * time.Hour)}); n != 10 {
		t.Fatalf("filtered count = %d, want 10", n)
	}
	page, err := s.ListRuns(ctx, store.RunFilter{Limit: 5, Offset: 5})
	if err != nil || len(page) != 5 || page[0].ID != "run-005" {
		t.Fatalf("page = %v (err %v)", page, err)
	}

	refs := []store.BlobRef{{RunID: "run-001", Kind: "events", Hash: "aa", Size: 10}, {RunID: "run-001", Kind: "wire", Name: "0001", Hash: "bb", Size: 20}}
	for i := 0; i < 2; i++ { // idempotent
		if err := s.AddBlobRefs(ctx, refs); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.BlobRefs(ctx, "run-001")
	if err != nil || !reflect.DeepEqual(got, refs) {
		t.Fatalf("blob refs = %+v (err %v)", got, err)
	}

	calls := make([]store.ToolCall, 500)
	for i := range calls {
		calls[i] = store.ToolCall{Turn: i / 5, Seq: i, Tool: []string{"read", "edit", "shell"}[i%3], OK: i%7 != 0, ErrorClass: "x"}
	}
	for i := 0; i < 2; i++ { // replace, not append
		if err := s.ReplaceToolCalls(ctx, "run-001", calls); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReplaceToolCalls(ctx, "run-002", calls[:10]); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Query(ctx, `SELECT tool, count(*) FILTER (WHERE NOT ok) FROM tool_calls WHERE run_id = ? GROUP BY tool ORDER BY tool`, "run-001")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for rows.Next() {
		var tool string
		var failed int
		if err := rows.Scan(&tool, &failed); err != nil {
			t.Fatal(err)
		}
		total += failed
	}
	rows.Close()
	if total != 72 { // multiples of 7 below 500
		t.Fatalf("failed calls = %d, want 72", total)
	}
	if err := s.ReplaceToolCalls(ctx, "run-001", nil); err != nil {
		t.Fatal(err)
	}
	var left int
	rows, _ = s.Query(ctx, `SELECT count(*) FROM tool_calls`)
	rows.Next()
	rows.Scan(&left)
	rows.Close()
	if left != 10 {
		t.Fatalf("tool calls left = %d, want 10", left)
	}

	if err := s.ReplaceFindings(ctx, "run-001", []store.Finding{{Detector: "sentence_repeat", DetectorVersion: "1", Severity: "high", Turn: 4, Count: 12, Evidence: json.RawMessage(`{"unit":"abc"}`)}}); err != nil {
		t.Fatal(err)
	}
}

func TestLaneState(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.duckdb"))
	defer s.Close()
	for i := 0; i < 200; i++ {
		if err := s.SetLaneState(ctx, store.LaneState{Lane: fmt.Sprintf("lane-%d", i%4), State: "running", RunID: fmt.Sprintf("r%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	lanes, err := s.LaneStates(ctx)
	if err != nil || len(lanes) != 4 {
		t.Fatalf("lanes = %v (err %v)", lanes, err)
	}
	if lanes[3].RunID != "r199" {
		t.Fatalf("lane-3 run = %s, want r199", lanes[3].RunID)
	}
	if err := s.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
}
