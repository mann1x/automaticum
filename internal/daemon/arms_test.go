package daemon

import (
	"slices"
	"testing"
)

// TestActiveArms: runs not finished are found by their arm and model, as
// finished ones are, and GET /v1/arms lists the arms running and queued,
// an arm changed under a run in flight as two entries.
func TestActiveArms(t *testing.T) {
	g, dir := liveRig(t)
	write(t, dir, map[string]string{"arms/act.toml": "extends = \"stub\"\n"})
	g.runner.mu.Lock()
	g.runner.hold = make(chan struct{})
	g.runner.started = make(chan string, 4)
	g.runner.mu.Unlock()
	var started StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "act", Task: "answer", Count: 3}, &started); code != 202 {
		t.Fatalf("start: %d", code)
	}
	first := <-g.runner.started

	count := func(q string) (int, []RunView) {
		t.Helper()
		var list []RunView
		code := g.call("GET", "/v1/runs?"+q, nil, &list)
		return code, list
	}
	for q, want := range map[string]int{
		"arm_id=" + started.ArmID: 3, "model=stub:latest": 3, "arm_id=nothing": 0,
		"state=running": 1, "state=queued": 2, "state=finished": 0,
		"arm_id=" + started.ArmID + "&state=queued": 2,
	} {
		if code, list := count(q); code != 200 || len(list) != want {
			t.Errorf("%s: %d, %d runs, want %d", q, code, len(list), want)
		}
	}
	if code, list := count("arm_id=" + started.ArmID + "&state=running"); len(list) != 1 || list[0].ID != first || list[0].ArmID != started.ArmID || list[0].Model != "stub:latest" || list[0].SDKRelease == "" {
		t.Errorf("the running run: %d %+v", code, list)
	}
	if code := g.call("GET", "/v1/runs?state=done", nil, nil); code != 400 {
		t.Errorf("an unknown state: %d", code)
	}

	var arms []ArmActivity
	g.call("GET", "/v1/arms", nil, &arms)
	if len(arms) != 1 || arms[0].Arm != "act" || arms[0].ArmID != started.ArmID || arms[0].Running != 1 || arms[0].Queued != 2 || arms[0].SDK == "" ||
		!slices.Equal(arms[0].Lanes, []string{"lane-1"}) || !slices.Equal(arms[0].RunningRuns, []string{first}) || len(arms[0].Batches) != 1 {
		t.Fatalf("active arms: %+v", arms)
	}

	// The arm's file changes: the run in flight keeps the arm it started
	// with, the queued ones take the new one.
	write(t, dir, map[string]string{"arms/act.toml": "extends = \"stub\"\n[limits]\nwall_s = 200\n"})
	arms = nil
	g.call("GET", "/v1/arms", nil, &arms)
	if len(arms) != 2 || arms[0].ArmID != started.ArmID || arms[0].Running != 1 || arms[0].Queued != 0 ||
		arms[1].ArmID == started.ArmID || arms[1].Running != 0 || arms[1].Queued != 2 {
		t.Fatalf("active arms after the arm changed: %+v", arms)
	}
	arms = nil
	if g.call("GET", "/v1/arms?running=1", nil, &arms); len(arms) != 1 || arms[0].Running != 1 {
		t.Fatalf("running arms: %+v", arms)
	}
	if code, list := count("arm_id=" + started.ArmID); len(list) != 1 {
		t.Errorf("the old arm after the change: %d %+v", code, list)
	}

	close(g.runner.hold)
	for _, id := range started.RunIDs {
		g.waitFinished(id)
	}
	arms = nil
	if g.call("GET", "/v1/arms", nil, &arms); len(arms) != 0 {
		t.Fatalf("active arms once all finished: %+v", arms)
	}
}
