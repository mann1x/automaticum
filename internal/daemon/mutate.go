package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Every mutating route takes a dry run and an idempotency key (050 §4,
// 090 §1). A dry run is ?dry_run=1 (or "dry_run": true in the body where
// the route already read one): the route checks everything it would check,
// answers with what it would do and the field dry_run, and changes
// nothing. A key is the Idempotency-Key header: the first request with it
// is done and its answer kept; a request again with the same key gets that
// answer back (with the header Idempotent-Replay) and changes nothing
// more. The same key on another request is refused.

// dryRun says whether a request asks for a dry run in its query.
func dryRun(r *http.Request) bool {
	v := strings.ToLower(r.URL.Query().Get("dry_run"))
	return v == "1" || v == "true" || v == "yes"
}

// IdemEntry is a kept answer to a request made with an idempotency key.
type IdemEntry struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   string          `json:"body_sha256"`
	Status int             `json:"status"`
	Answer json.RawMessage `json:"answer"`
	At     time.Time       `json:"at"`
}

// idemKeep is how long a key's answer is kept.
const idemKeep = 30 * 24 * time.Hour

// recorder keeps a copy of what a handler writes.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// idempotent wraps the API: a mutating request with an Idempotency-Key is
// done once. Keyed requests are served one at a time, so two at once with
// the same key do not both run.
func (d *Daemon) idempotent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" || r.Method == http.MethodGet || dryRun(r) {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 200 {
			fail(w, http.StatusBadRequest, "an idempotency key is at most 200 characters")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			fail(w, http.StatusBadRequest, "the request is not readable: %v", err)
			return
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		r.Body = io.NopCloser(bytes.NewReader(body))
		d.idemMu.Lock()
		defer d.idemMu.Unlock()
		d.mu.Lock()
		e := d.ctl.Idem[key]
		d.mu.Unlock()
		if e != nil && time.Since(e.At) < idemKeep {
			if e.Method != r.Method || e.Path != r.URL.Path || e.Body != hash {
				fail(w, http.StatusUnprocessableEntity, "idempotency key %q was used at %s for %s %s; a key names one request", key,
					e.At.Format(time.RFC3339), e.Method, e.Path)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotent-Replay", "true")
			w.WriteHeader(e.Status)
			w.Write(e.Answer)
			return
		}
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		// A failure of the daemon's own is not kept: the request may be
		// made again with the same key.
		if rec.status == 0 || rec.status >= 500 {
			return
		}
		d.mu.Lock()
		for k, old := range d.ctl.Idem {
			if time.Since(old.At) >= idemKeep {
				delete(d.ctl.Idem, k)
			}
		}
		d.ctl.Idem[key] = &IdemEntry{Method: r.Method, Path: r.URL.Path, Body: hash, Status: rec.status,
			Answer: json.RawMessage(bytes.TrimSpace(rec.body.Bytes())), At: time.Now().UTC()}
		err = d.saveControl()
		d.mu.Unlock()
		if err != nil {
			d.Log.Error("the control file: an idempotency key was not kept", "error", err)
		}
	})
}

// Done is the answer of a control that is not about a run's record: what
// it acted on, the state it reached or would reach, and whether it was a
// dry run.
type Done struct {
	RunID  string `json:"run_id,omitempty"`
	State  string `json:"state"`
	DryRun bool   `json:"dry_run,omitempty"`
	Detail string `json:"detail,omitempty"`
}
