package daemon

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/export"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// captureTar is a capture blob as the run keeps it: the SDK's requests
// directory, one request and its response, cut by the guard.
func captureTar(t *testing.T, reasoning string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"reasoning_content": reasoning}}}})
	resp, _ := json.Marshal(map[string]any{"at": "2026-10-24T04:06:30.300Z", "status": 200, "body": "data: " + string(chunk) + "\n\n", "complete": false})
	tw.WriteHeader(&tar.Header{Name: "run-r1/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, b := range map[string][]byte{"run-r1/2026-10-24T04-06-00-000Z-0001.json": []byte(`{"messages":[]}`), "run-r1/2026-10-24T04-06-00-000Z-0001.response.json": resp} {
		tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(b))})
		tw.Write(b)
	}
	tw.Close()
	return buf.String()
}

// privateTrainingRoot is a private tasks root holding one training task,
// train-own/sh_own, a copy of train-sh/sh_logtriage marked private.
func privateTrainingRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dst := filepath.Join(root, "train-own", "sh_own")
	if err := os.CopyFS(dst, os.DirFS("../../tasks/train-sh/sh_logtriage")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "task.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(b), `id = "sh_logtriage"
suite = "train-sh"`, `id = "sh_own"
suite = "train-own"
private = true
canary = "own-canary-0123456789"`, 1)
	if err := os.WriteFile(filepath.Join(dst, "task.toml"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestExport(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	block := "Wait, check w[1] again.\nIt is the method.\n"
	reasoning := "Start.\n" + strings.Repeat(block, 3)
	notice := `{"t":"2026-10-24T04:06:30.337Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"notice","metadata":{"kind":"reasoning_loop","phase":"cut","iteration":3,"verdict":{"kind":"block-cycle","period":2,"cycles":3,"chars":` +
		itoa(len(reasoning)) + `},"channel":"reasoning"}}}}}` + "\n"
	blobs := g.d.Blobs.(memBlobs)
	blobs["cap"], blobs["ev"] = captureTar(t, reasoning), notice
	for _, r := range []store.Run{
		{ID: "r1", Batch: "k-pool-r1", Suite: "train-sh", TaskID: "sh_logtriage", Verdict: store.Fail},
		{ID: "r2", Batch: "k-pool-r1", Suite: "custom", TaskID: "manic_miner", Verdict: store.Fail, VerdictReason: "fixture says: the cavern does not advance"},
		{ID: "r3", Batch: "k-pool-r1", Suite: "train-sh", TaskID: "sh_logtriage", Verdict: store.Invalid, VerdictReason: "inherited field"},
		{ID: "r4", Batch: "k-pool-r1", Suite: "train-sh", TaskID: "sh_logtriage", Verdict: store.Pass},
		{ID: "r5", Batch: "k-heldout-v1", Suite: "train-sh", TaskID: "sh_logtriage", Verdict: store.Pass},
		{ID: "r6", Batch: "k-pool-r1", Suite: "train-own", TaskID: "sh_own", Verdict: store.Fail},
		{ID: "r7", Batch: "k-pool-r1", Suite: "custom", TaskID: "manic_miner", Verdict: store.Fail},
	} {
		g.state.UpsertRun(ctx, &r)
	}
	g.d.ctl.Heldout["k-heldout-v1"] = true
	// A private training task, in the private root, exports too (D137).
	g.d.Local.Daemon.PrivateTasks = privateTrainingRoot(t)
	for _, id := range []string{"r1", "r2", "r3", "r5", "r6"} {
		g.state.refs = append(g.state.refs, store.BlobRef{RunID: id, Kind: "model_requests", Name: "requests", Hash: blob.Hash("cap")}, store.BlobRef{RunID: id, Kind: "events", Hash: blob.Hash("ev")})
	}
	// r7 never loops.
	blobs["quiet"] = captureTar(t, "Start.\nThe cavern is drawn.\n")
	g.state.refs = append(g.state.refs, store.BlobRef{RunID: "r7", Kind: "model_requests", Name: "requests", Hash: blob.Hash("quiet")})

	for id, want := range map[string]int{"r2": http.StatusForbidden, "r3": http.StatusUnprocessableEntity, "r4": http.StatusNotFound, "nope": http.StatusNotFound,
		// A held-out batch (D115) only when asked for by name; a held-out
		// task never, even when asked.
		"r5": http.StatusForbidden, "r5?heldout=1": http.StatusOK, "r2?heldout=1": http.StatusForbidden, "r1?heldout=0": http.StatusOK, "r6": http.StatusOK,
		// An anti-loop export (D158): a held-out task's run up to its
		// loop's onset; one without a loop not at all; a held-out batch
		// still only with heldout=1.
		"r2?anti_loop=1": http.StatusOK, "r1?anti_loop=1": http.StatusOK, "r7?anti_loop=1": http.StatusUnprocessableEntity, "r7": http.StatusForbidden,
		"r5?anti_loop=1": http.StatusForbidden, "r5?anti_loop=1&heldout=1": http.StatusOK} {
		path, q, _ := strings.Cut(id, "?")
		if q != "" {
			q = "?" + q
		}
		if code := g.call("GET", "/v1/runs/"+path+"/export"+q, nil, nil); code != want {
			t.Errorf("%s: %d, want %d", id, code, want)
		}
	}

	files := exportFiles(t, g, "r1")
	stem := "2026-10-24T04-06-00-000Z-0001"
	if files[stem+".json"] != `{"messages":[]}` || files[stem+".response.json"] == "" || len(files) != 4 {
		t.Fatalf("files: %v", keys(files))
	}
	var meta ExportMeta
	if err := json.Unmarshal([]byte(files["meta.json"]), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Run.ID != "r1" || len(meta.Requests) != 1 || len(meta.Cuts) != 1 || meta.Cuts[0].Request != stem {
		t.Fatalf("meta: %+v", meta)
	}
	var guard bool
	for _, l := range strings.Split(strings.TrimSpace(files["spans.jsonl"]), "\n") {
		var s struct {
			Request, Field, Source string
			Start, End             int
		}
		json.Unmarshal([]byte(l), &s)
		if s.Source == "guard" {
			guard = true
			if s.Request != stem || s.Field != "reasoning" || s.Start != len("Start.\n"+block) || s.End != len(reasoning) {
				t.Errorf("guard span: %s", l)
			}
		}
	}
	if !guard {
		t.Errorf("no guard span: %q", files["spans.jsonl"])
	}
}

func TestExportAntiLoop(t *testing.T) {
	g := newRig(t, false)
	ctx := context.Background()
	block := "Wait, check w[1] again.\nIt is the method.\n"
	reasoning := "Start.\n" + strings.Repeat(block, 3)
	blobs := g.d.Blobs.(memBlobs)
	blobs["cap"] = captureTar(t, reasoning)
	blobs["ev"] = `{"t":"2026-10-24T04:06:30.337Z","k":"event","event":{"type":"agent_event","payload":{"event":{"type":"notice","metadata":{"kind":"reasoning_loop","phase":"cut","iteration":3,"verdict":{"kind":"block-cycle","period":2,"cycles":3,"chars":` +
		itoa(len(reasoning)) + `},"channel":"reasoning"}}}}}` + "\n"
	g.state.refs = append(g.state.refs, store.BlobRef{RunID: "r2", Kind: "events", Hash: blob.Hash("ev")})
	g.state.UpsertRun(ctx, &store.Run{ID: "r2", Batch: "k-pool-r1", Suite: "custom", TaskID: "manic_miner", Verdict: store.Fail, VerdictReason: "fixture says: the cavern does not advance"})
	g.state.refs = append(g.state.refs, store.BlobRef{RunID: "r2", Kind: "model_requests", Name: "requests", Hash: blob.Hash("cap")})

	files := exportFiles(t, g, "r2?anti_loop=1")
	stem := "2026-10-24T04-06-00-000Z-0001"
	if len(files) != 4 || files[stem+".json"] != `{"messages":[]}` {
		t.Fatalf("files: %v", keys(files))
	}
	var meta ExportMeta
	if err := json.Unmarshal([]byte(files["meta.json"]), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Use != UseAntiLoop || !meta.HeldOutTask || meta.Onset == nil || meta.Onset.Request != stem || meta.Onset.Field != "reasoning" {
		t.Fatalf("meta: %+v", meta)
	}
	// Nothing the oracle wrote.
	if meta.Run.ID != "r2" || meta.Run.Verdict != "FAIL" || meta.Run.VerdictReason != "" || meta.Run.Findings != nil || meta.Run.Metrics != nil || meta.Run.Blobs != nil {
		t.Fatalf("run: %+v", meta.Run)
	}
	if strings.Contains(files["meta.json"], "cavern") {
		t.Fatal("the verdict's reason is in the export")
	}
	// The response ends where the loop's first repeat starts.
	r, err := export.ReadResponse(stem, []byte(files[stem+".response.json"]))
	if err != nil {
		t.Fatal(err)
	}
	if want := reasoning[:meta.Onset.Start]; r.Fields["reasoning_content"] != want || r.Complete {
		t.Fatalf("cut response: %q, complete %v; want %q", r.Fields["reasoning_content"], r.Complete, want)
	}
	lines := strings.Split(strings.TrimSpace(files["spans.jsonl"]), "\n")
	var l export.Line
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &l) != nil || l.Start != meta.Onset.Start || l.End != l.Start || meta.Onset.End <= l.Start {
		t.Fatalf("spans: %q, onset %+v", files["spans.jsonl"], meta.Onset)
	}
}

// exportFiles is an export's tar, by file name.
func exportFiles(t *testing.T, g *rig, run string) map[string]string {
	t.Helper()
	id, q, _ := strings.Cut(run, "?")
	if q != "" {
		q = "?" + q
	}
	resp, err := http.Get(g.srv.URL + "/v1/runs/" + id + "/export" + q)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export %s: %v %v", run, err, resp.Status)
	}
	defer resp.Body.Close()
	files := map[string]string{}
	tr := tar.NewReader(resp.Body)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		files[h.Name] = string(b)
	}
	return files
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
