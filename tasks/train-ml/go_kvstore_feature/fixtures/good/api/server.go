// Package api is kvd's HTTP interface.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/northwind/kvd/store"
)

const maxBody = 1 << 20

// NewHandler returns the HTTP handler serving st.
func NewHandler(st *store.Store) http.Handler {
	s := &server{st: st}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/keys/{key...}", s.key)
	mux.HandleFunc("/v1/keys", s.list)
	return mux
}

type server struct {
	st *store.Store
}

type entryJSON struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Version   int64  `json:"version"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func writeEntry(w http.ResponseWriter, status int, e store.Entry) {
	out := entryJSON{Key: e.Key, Value: e.Value, Version: e.Version}
	if !e.ExpiresAt.IsZero() {
		out.ExpiresAt = e.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	w.Header().Set("ETag", fmt.Sprintf("%q", strconv.FormatInt(e.Version, 10)))
	writeJSON(w, status, out)
}

func (s *server) key(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	switch r.Method {
	case http.MethodGet:
		e, ok := s.st.Get(key)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "no such key: "+key)
			return
		}
		writeEntry(w, http.StatusOK, e)
	case http.MethodPut:
		s.put(w, r, key)
	case http.MethodDelete:
		if !s.st.Delete(key) {
			writeError(w, http.StatusNotFound, "not_found", "no such key: "+key)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not allowed here")
	}
}

func (s *server) put(w http.ResponseWriter, r *http.Request, key string) {
	var body struct {
		Value *string `json:"value"`
		TTLMs int64   `json:"ttl_ms"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err == nil {
		err = json.Unmarshal(raw, &body)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body must be JSON {\"value\": string, \"ttl_ms\": int}")
		return
	}
	if body.Value == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "value is required")
		return
	}
	if body.TTLMs < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "ttl_ms must not be negative")
		return
	}
	opt := store.PutOptions{TTL: time.Duration(body.TTLMs) * time.Millisecond}
	if m := r.Header.Get("If-Match"); m != "" {
		v, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(m), `"`), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "If-Match must be a version number")
			return
		}
		opt.IfMatch, opt.MatchVersion = true, v
	}
	opt.IfNoneMatch = strings.TrimSpace(r.Header.Get("If-None-Match")) == "*"

	e, created, err := s.st.Put(key, *body.Value, opt)
	switch {
	case errors.Is(err, store.ErrVersionMismatch):
		writeError(w, http.StatusPreconditionFailed, "version_mismatch", "the key is not at the version given in If-Match")
	case errors.Is(err, store.ErrKeyExists):
		writeError(w, http.StatusPreconditionFailed, "key_exists", "the key already exists")
	case created:
		writeEntry(w, http.StatusCreated, e)
	default:
		writeEntry(w, http.StatusOK, e)
	}
}

func (s *server) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not allowed here")
		return
	}
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be an integer in 1..1000")
			return
		}
		limit = n
	}
	keys, more := s.st.List(q.Get("prefix"), q.Get("after"), limit)
	out := struct {
		Keys []string `json:"keys"`
		Next string   `json:"next,omitempty"`
	}{Keys: keys}
	if more {
		out.Next = keys[len(keys)-1]
	}
	writeJSON(w, http.StatusOK, out)
}
