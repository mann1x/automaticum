package daemon

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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
		{ID: "r2", Batch: "k-pool-r1", Suite: "custom", TaskID: "manic_miner", Verdict: store.Pass},
		{ID: "r3", Batch: "k-pool-r1", Suite: "train-sh", TaskID: "sh_logtriage", Verdict: store.Invalid, VerdictReason: "inherited field"},
		{ID: "r4", Batch: "k-pool-r1", Suite: "train-sh", TaskID: "sh_logtriage", Verdict: store.Pass},
	} {
		g.state.UpsertRun(ctx, &r)
	}
	for _, id := range []string{"r1", "r2", "r3"} {
		g.state.refs = append(g.state.refs, store.BlobRef{RunID: id, Kind: "model_requests", Name: "requests", Hash: blob.Hash("cap")}, store.BlobRef{RunID: id, Kind: "events", Hash: blob.Hash("ev")})
	}

	for id, want := range map[string]int{"r2": http.StatusForbidden, "r3": http.StatusUnprocessableEntity, "r4": http.StatusNotFound, "nope": http.StatusNotFound} {
		if code := g.call("GET", "/v1/runs/"+id+"/export", nil, nil); code != want {
			t.Errorf("%s: %d, want %d", id, code, want)
		}
	}

	resp, err := http.Get(g.srv.URL + "/v1/runs/r1/export")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export: %v %v", err, resp.Status)
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
			if s.Request != stem || s.Field != "reasoning_content" || s.Start != len("Start.\n"+block) || s.End != len(reasoning) {
				t.Errorf("guard span: %s", l)
			}
		}
	}
	if !guard {
		t.Errorf("no guard span: %q", files["spans.jsonl"])
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
