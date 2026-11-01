package daemon

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The [notify] command gets one JSON object per notification on its
// standard input: each run's end, the batch's end, a stopped batch.
func TestNotifyCommand(t *testing.T) {
	g := newRig(t, false)
	out := filepath.Join(t.TempDir(), "notes.jsonl")
	g.d.Local.Daemon.Notify.Command = []string{"sh", "-c", "cat >> " + out}
	g.d.Local.Daemon.Notify.Events = []string{"run", "batch"}
	go func() { g.d.Serve(g.ctx()); close(g.served) }()
	var resp StartResponse
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	read := func(n int) []Notification {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			var notes []Notification
			if f, err := os.Open(out); err == nil {
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					var n Notification
					if err := json.Unmarshal(sc.Bytes(), &n); err != nil {
						t.Fatalf("%s: %v", sc.Text(), err)
					}
					notes = append(notes, n)
				}
				f.Close()
			}
			if len(notes) >= n || time.Now().After(deadline) {
				return notes
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	notes := read(3)
	if len(notes) != 3 || notes[0].Kind != "run" || notes[0].Status != "PASS" || notes[0].Batch != resp.Batch ||
		notes[1].Kind != "run" || notes[2].Kind != "batch" || notes[2].Status != "finished" || notes[2].Counts["PASS"] != 2 {
		t.Fatalf("notes %+v", notes)
	}

	// A stopped batch is told, and a kind not in events is not.
	g.runner.hold, g.runner.started = make(chan struct{}), make(chan string, 4)
	if code := g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 2}, &resp); code != 202 {
		t.Fatalf("start: %d", code)
	}
	<-g.runner.started
	if code := g.call("POST", "/v1/batches/"+resp.Batch+"/stop", nil, nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	close(g.runner.hold)
	notes = read(6)
	var stopped bool
	for _, n := range notes[3:] {
		stopped = stopped || n.Kind == "batch" && n.Status == "stopped" && n.Batch == resp.Batch && n.Run == resp.RunIDs[0]
	}
	if !stopped {
		t.Errorf("no stop told: %+v", notes[3:])
	}
	before := len(read(0))
	g.d.Local.Daemon.Notify.Events = []string{"gate"}
	g.d.notify(g.ctx(), Notification{Kind: "run", Status: "PASS"})
	if after := len(read(0)); after != before {
		t.Errorf("a kind not in events was sent: %d notes, then %d", before, after)
	}
}
