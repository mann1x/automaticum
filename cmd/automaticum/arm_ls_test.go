package main

import (
	"testing"

	"github.com/mann1x/automaticum/internal/daemon"
)

// The daemon's activity lands on the arm files' rows by name and id; an
// arm changed under its runs, or gone, is a row of its own.
func TestMergeArms(t *testing.T) {
	files := []armRow{{Name: "a", ID: "a1"}, {Name: "b", ID: "b1"}, {Name: "broken", Problem: "no endpoint"}}
	acts := []daemon.ArmActivity{
		{Arm: "a", ArmID: "a1", Running: 1, Queued: 2, Lanes: []string{"lane-1"}},
		{Arm: "a", ArmID: "a0", Running: 1, Lanes: []string{"lane-2"}},
		{Arm: "gone", Problem: "there is no arm gone", Queued: 3},
	}
	rows := mergeArms(files, acts, false)
	if len(rows) != 5 {
		t.Fatalf("rows: %+v", rows)
	}
	if r := rows[0]; r.Running != 1 || r.Queued != 2 || r.Lanes[0] != "lane-1" || r.Problem != "" {
		t.Errorf("a: %+v", r)
	}
	if r := rows[1]; r.Running != 0 || r.Queued != 0 {
		t.Errorf("b: %+v", r)
	}
	if r := rows[3]; r.Name != "a" || r.ID != "a0" || r.Running != 1 || r.Problem != "not the arm its file says now" {
		t.Errorf("a's earlier version: %+v", r)
	}
	if r := rows[4]; r.Name != "gone" || r.Queued != 3 || r.Problem != "there is no arm gone" {
		t.Errorf("gone: %+v", r)
	}
	if rows := mergeArms(nil, acts[:2], true); len(rows) != 2 || rows[1].Problem != "" {
		t.Errorf("only the activity: %+v", rows)
	}
	if rows := mergeArms(nil, nil, true); rows == nil || len(rows) != 0 {
		t.Errorf("nothing active must be an empty list, not null: %#v", rows)
	}
}
