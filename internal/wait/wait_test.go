package wait

import (
	"testing"
	"time"
)

func batch(states ...string) []Run {
	var out []Run
	t0 := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	for i, s := range states {
		r := Run{ID: string(rune('a' + i))}
		switch s {
		case "queued", "running":
			r.State = s
		default:
			r.State, r.Verdict, r.Ended = "finished", s, t0.Add(time.Duration(10-i)*time.Minute)
		}
		out = append(out, r)
	}
	return out
}

func TestParseAndEval(t *testing.T) {
	s := Runs(KindBatch, "b", batch("PASS", "PASS", "FAIL", "TIMEOUT", "running", "queued"))
	cases := []struct {
		cond string
		want bool
	}{
		{"pass>=2", true},
		{"pass>=5 or fail>=1", true},
		{"pass>=5 or fail>=3", false},
		{"pass>=2 and timeout==1", true},
		{"pass >= 2 && (fail > 1 || queued == 1)", true},
		{"finished>4", false},
		{"finished>3", true},
		{"done", false},
		{"started", true},
		{"", false}, // the default: done
		{"PASS>=2", true},
		{"skipped!=0", false},
		{"running<1", false},
	}
	for _, c := range cases {
		cond, err := Parse(KindBatch, c.cond)
		if err != nil {
			t.Fatalf("%q: %v", c.cond, err)
		}
		if got := cond.Eval(s); got != c.want {
			t.Errorf("%q: %v, want %v", c.cond, got, c.want)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	for kind, conds := range map[string][]string{
		KindBatch:    {"pass>=", "pass>>1", "passes>=1", "idle", "pass>=1 or", "(pass>=1", "pass & fail", "pass>=-1", "pass ! 1", "pass>=1 pass"},
		KindLane:     {"pass>=1", "done"},
		KindCampaign: {"idle"},
		"nothing":    {"done"},
	} {
		for _, c := range conds {
			if _, err := Parse(kind, c); err == nil {
				t.Errorf("%s %q: parsed", kind, c)
			}
		}
	}
}

func TestRunsDone(t *testing.T) {
	s := Runs(KindBatch, "b", batch("PASS", "SKIPPED"))
	if !s.Over() || s.State != "done" || s.Counts["finished"] != 2 || s.Counts["skipped"] != 1 {
		t.Fatalf("%+v", s)
	}
	// Finished in the order they ended: b ended before a.
	if s.Finished[0].ID != "b" || s.Finished[1].ID != "a" {
		t.Errorf("order %v", s.Finished)
	}
	if e := Runs(KindBatch, "b", nil); e.Over() {
		t.Error("an empty batch is over")
	}
}

func TestRunKind(t *testing.T) {
	q := Runs(KindRun, "a", batch("queued"))
	started, _ := Parse(KindRun, "started")
	if started.Eval(q) || q.State != "queued" {
		t.Errorf("queued: %+v", q)
	}
	r := Runs(KindRun, "a", batch("running"))
	if !started.Eval(r) || r.Over() {
		t.Errorf("running: %+v", r)
	}
	f := Runs(KindRun, "a", batch("TIMEOUT"))
	fin, _ := Parse(KindRun, "finished and timeout==1")
	if !fin.Eval(f) || !f.Over() || f.State != "finished" {
		t.Errorf("finished: %+v", f)
	}
}

func TestLaneIdleCampaign(t *testing.T) {
	idle, _ := Parse(KindLane, "")
	free, _ := Parse(KindLane, "free")
	if idle.Eval(Lane("l", "running", true, 0)) || idle.Eval(Lane("l", "idle", false, 2)) || !idle.Eval(Lane("l", "idle", false, 0)) {
		t.Error("lane idle")
	}
	if !free.Eval(Lane("l", "waiting", false, 2)) {
		t.Error("lane free")
	}
	d, _ := Parse(KindIdle, "")
	if d.Eval(Idle(0, 1)) || !d.Eval(Idle(0, 0)) {
		t.Error("idle")
	}
	c, _ := Parse(KindCampaign, "batches_finished>=2 or done")
	if c.Eval(Campaign("c", "running", "", []string{"finished", "running"})) {
		t.Error("campaign early")
	}
	if !c.Eval(Campaign("c", "running", "", []string{"finished", "finished", "waiting"})) {
		t.Error("campaign count")
	}
	if s := Campaign("c", "stopped", "asked", nil); !s.Over() || !c.Eval(s) {
		t.Error("campaign stopped")
	}
}

func TestSeenAny(t *testing.T) {
	s := Runs(KindBatch, "b", batch("PASS", "running"))
	if Seen(1).Eval(s) || !Seen(0).Eval(s) {
		t.Error("seen")
	}
	done, _ := Parse(KindBatch, "")
	if !Any(Seen(0), done).Eval(s) || Any(Seen(1), done).Eval(s) {
		t.Error("any")
	}
}
