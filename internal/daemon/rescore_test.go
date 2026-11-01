package daemon

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
	"github.com/mann1x/automaticum/internal/task"
)

func (s *memState) AddBlobRefs(_ context.Context, refs []store.BlobRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs = append(s.refs, refs...)
	return nil
}

// syncBlobs is a blob store tests can write to while the daemon reads.
type syncBlobs struct {
	mu sync.Mutex
	m  map[blob.Hash]string
}

func (b *syncBlobs) Put(_ context.Context, r io.Reader) (blob.Hash, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	h := blob.Hash(hex.EncodeToString(sum[:]))
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[h] = string(data)
	return h, int64(len(data)), nil
}

func (b *syncBlobs) Reader(_ context.Context, h blob.Hash) (io.ReadCloser, blob.Info, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return io.NopCloser(strings.NewReader(b.m[h])), blob.Info{}, nil
}

// workspaceTar is a stored workspace holding answer.txt, or none when
// answer is empty.
func workspaceTar(t *testing.T, answer string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0o755})
	if answer != "" {
		tw.WriteHeader(&tar.Header{Name: "answer.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(answer))})
		tw.Write([]byte(answer))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestRescore proves D121: a finished cell's counted runs are scored again
// by the task's current oracle on their stored workspaces, on their lane and
// never beside a run; the run records stay, the cell gives the latest
// scoring, and an oracle with no answer leaves the verdict standing.
func TestRescore(t *testing.T) {
	g := newRig(t, true)
	blobs := &syncBlobs{m: map[blob.Hash]string{}}
	release := make(chan struct{})
	var mu sync.Mutex
	seen := 0
	g.d.Blobs, g.d.Store, g.d.RunsDir = blobs, blobs.Put, t.TempDir()
	g.d.Oracle = func(ctx context.Context, tk *task.Task, o *task.Oracle, ws string, _ map[string]string) (task.OracleResult, error) {
		mu.Lock()
		seen++
		first := seen == 1
		mu.Unlock()
		if first {
			<-release
		}
		b, err := os.ReadFile(filepath.Join(ws, "answer.txt"))
		if err != nil {
			return task.OracleResult{Oracle: o.Name, Err: "no answer.txt"}, nil
		}
		ok := strings.TrimSpace(string(b)) == "42"
		return task.OracleResult{Oracle: o.Name, Passed: ok, Output: "checked " + string(b)}, nil
	}

	var cell StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Count: 4, Key: "rs"}, &cell)
	for _, id := range cell.RunIDs {
		g.waitFinished(id)
	}
	ids := cell.RunIDs
	// The first run's answer still holds, the second's no longer does, the
	// third's oracle gives no answer, the fourth has no stored workspace.
	for i, answer := range []string{"42", "41", ""} {
		h, n, _ := blobs.Put(context.Background(), strings.NewReader(workspaceTar(t, answer)))
		g.state.AddBlobRefs(context.Background(), []store.BlobRef{{RunID: ids[i], Kind: "workspace_after", Hash: h, Size: n}})
	}

	var dry RescoreStatus
	if code := g.call("POST", "/v1/batches/"+cell.Batch+"/rescore?dry_run=1", nil, &dry); code != 200 || !dry.DryRun || len(dry.Job.Todo) != 4 {
		t.Fatalf("dry run: %d %+v", code, dry.Job)
	}
	if code := g.call("GET", "/v1/batches/"+cell.Batch+"/rescore", nil, nil); code != 404 {
		t.Fatalf("a dry run queued the rescore: %d", code)
	}
	var st RescoreStatus
	if code := g.call("POST", "/v1/batches/"+cell.Batch+"/rescore", nil, &st); code != 202 {
		t.Fatalf("rescore: %d", code)
	}
	if code := g.call("POST", "/v1/batches/"+cell.Batch+"/rescore", nil, nil); code != 409 {
		t.Errorf("a second rescore while the first runs: %d", code)
	}

	// While the first oracle runs, the lane is the rescore's: a run queued
	// now waits.
	deadline := time.Now().Add(5 * time.Second)
	for g.status().Lanes[0].State != "rescoring" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ls := g.status().Lanes[0]; ls.State != "rescoring" || ls.RunID != ids[0] {
		t.Fatalf("lane while rescoring: %+v", ls)
	}
	var other StartResponse
	g.call("POST", "/v1/runs", StartRequest{Arm: "stub", Task: "answer", Key: "other"}, &other)
	g.stays(other.RunIDs[0])
	close(release)
	g.waitFinished(other.RunIDs[0])

	for time.Now().Before(deadline.Add(5 * time.Second)) {
		st = RescoreStatus{}
		g.call("GET", "/v1/batches/"+cell.Batch+"/rescore", nil, &st)
		if !st.Job.Finished.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Job.Finished.IsZero() || len(st.Job.Done) != 4 || len(st.Records) != 3 {
		t.Fatalf("rescore did not finish: %+v", st)
	}
	if why := st.Job.Failed[ids[2]]; !strings.Contains(why, "the oracle gave no answer") {
		t.Errorf("the run whose oracle gave no answer: %q", why)
	}
	if why := st.Job.Failed[ids[3]]; !strings.Contains(why, "no stored workspace") {
		t.Errorf("the run without a workspace: %q", why)
	}
	got := map[string]RescoreRecord{}
	for _, r := range st.Records {
		got[r.Run] = r
	}
	if r := got[ids[0]]; r.Verdict != store.Pass || r.Was != store.Pass {
		t.Errorf("first run: %+v", r)
	}
	if r := got[ids[1]]; r.Verdict != store.Fail || r.Was != store.Pass || r.Reason != "oracle ran and said no" {
		t.Errorf("second run: %+v", r)
	}
	if r := got[ids[2]]; r.Verdict != "" || !strings.Contains(r.Reason, "the run's verdict stands") {
		t.Errorf("third run: %+v", r)
	}
	if rec, _ := g.state.GetRun(context.Background(), ids[1]); rec.Verdict != store.Pass {
		t.Errorf("the run's own record changed: %s", rec.Verdict)
	}
	refs, _ := g.state.BlobRefs(context.Background(), ids[1])
	kinds := map[string]int{}
	for _, r := range refs {
		kinds[r.Kind]++
	}
	if kinds["rescore"] != 1 || kinds["rescore_oracle"] != 1 {
		t.Errorf("stored beside the run: %v", kinds)
	}

	var v CellView
	g.call("GET", "/v1/batches/"+cell.Batch+"/cell", nil, &v)
	if v.Result.Counts["pass"] != 3 || v.Result.Counts["fail"] != 1 {
		t.Errorf("cell counts: %v", v.Result.Counts)
	}
	if r := v.Result.Rescored; r == nil || r.Runs != 2 || r.Changed != 1 {
		t.Errorf("cell rescored: %+v", r)
	}
	for _, row := range v.Rows {
		if row.RunID == ids[1] && (row.Rescored == nil || row.Rescored.Verdict != "PASS" || row.Verdict != "FAIL") {
			t.Errorf("row: %+v", row)
		}
	}
	// A finished rescore may be followed by another.
	if code := g.call("POST", "/v1/batches/"+cell.Batch+"/rescore", nil, nil); code != 202 {
		t.Errorf("a second rescore after the first: %d", code)
	}
}
