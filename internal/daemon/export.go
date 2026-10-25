package daemon

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/export"
	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/task"
)

// ExportMeta is a run's meta.json in an export (110 §3.5): the run as
// `run show` sees it, and what the export found of the guards' cuts.
type ExportMeta struct {
	Run RunView `json:"run"`
	// Requests are the capture's stems, in order: <stem>.json is the
	// request body as sent, <stem>.response.json the response as read.
	Requests []string `json:"requests"`
	// Cuts are the SDK guards' cuts, each with the response it is in or
	// why none was found.
	Cuts []export.Cut `json:"cuts"`
	// Offsets says what a span's start and end count.
	Offsets       string `json:"offsets"`
	DetectVersion string `json:"detect_version"`
}

const exportOffsets = "Unicode code points in the field's text, the stream's deltas joined; end not included"

// handleExport hands out one run's capture as training traces (D102, D104,
// D106): a tar of meta.json, spans.jsonl and the capture's files as the SDK
// wrote them. Only a finished run of a training task (D105) that is not
// INVALID and was started with its requests captured is exported; a
// held-out task such as manic_miner never is.
func (d *Daemon) handleExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	rec, err := d.State.GetRun(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "there is no finished run %s", id)
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	t, err := task.Find(d.taskRoots(), rec.Suite+"/"+rec.TaskID)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "run %s: %v", id, err)
		return
	}
	if !t.Training || t.Private {
		fail(w, http.StatusForbidden, "run %s is of %s/%s, which is not a training task (D105); only training runs are exported (D102)", id, t.Suite, t.ID)
		return
	}
	if rec.Verdict == store.Invalid {
		fail(w, http.StatusUnprocessableEntity, "run %s is INVALID: %s", id, rec.VerdictReason)
		return
	}
	refs, err := d.State.BlobRefs(ctx, id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	var capture, events *store.BlobRef
	for i := range refs {
		switch refs[i].Kind {
		case "model_requests":
			capture = &refs[i]
		case "events":
			events = &refs[i]
		}
	}
	if capture == nil {
		fail(w, http.StatusNotFound, "run %s has no capture: it was not started with -capture-requests", id)
		return
	}
	v, err := d.fullView(ctx, rec)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}

	// First the responses and the cuts, to place the spans.
	var resps []export.Response
	var stems []string
	err = d.eachCaptured(r, capture, func(name string, b []byte) error {
		if stem, ok := strings.CutSuffix(name, ".response.json"); ok {
			resp, err := export.ReadResponse(stem, b)
			if err != nil {
				return err
			}
			resps = append(resps, resp)
		} else if stem, ok := strings.CutSuffix(name, ".json"); ok {
			stems = append(stems, stem)
		}
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "run %s: reading the capture: %v", id, err)
		return
	}
	var cuts []export.Cut
	if events != nil {
		rd, _, err := d.Blobs.Reader(ctx, events.Hash)
		if err != nil {
			fail(w, http.StatusInternalServerError, "%v", err)
			return
		}
		cuts, err = export.ReadCuts(rd)
		rd.Close()
		if err != nil {
			fail(w, http.StatusInternalServerError, "run %s: reading the event log: %v", id, err)
			return
		}
	}
	spans, cuts := export.Spans(resps, cuts)
	if cuts == nil {
		cuts = []export.Cut{}
	}
	meta, _ := json.MarshalIndent(ExportMeta{Run: v, Requests: stems, Cuts: cuts, Offsets: exportOffsets, DetectVersion: detect.Version}, "", "  ")
	var lines strings.Builder
	enc := json.NewEncoder(&lines)
	for _, s := range spans {
		enc.Encode(s)
	}

	w.Header().Set("Content-Type", "application/x-tar")
	tw := tar.NewWriter(w)
	now := time.Now().UTC()
	add := func(name string, b []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), ModTime: now, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tw.Write(b)
		return err
	}
	if add("meta.json", append(meta, '\n')) != nil || add("spans.jsonl", []byte(lines.String())) != nil {
		return
	}
	// Then the capture itself, file for file.
	if d.eachCaptured(r, capture, add) != nil {
		return // the client sees a cut tar
	}
	tw.Close()
}

// eachCaptured calls f with every file of a run's capture blob (a tar of
// the SDK's requests directory), by its name without the directory.
func (d *Daemon) eachCaptured(r *http.Request, ref *store.BlobRef, f func(name string, b []byte) error) error {
	rd, _, err := d.Blobs.Reader(r.Context(), ref.Hash)
	if err != nil {
		return err
	}
	defer rd.Close()
	tr := tar.NewReader(rd)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := f(path.Base(h.Name), b); err != nil {
			return err
		}
	}
}
