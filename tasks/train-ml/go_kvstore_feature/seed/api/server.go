// Package api is kvd's HTTP interface.
package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/northwind/kvd/store"
)

const maxBody = 1 << 20

// NewHandler returns the HTTP handler serving st.
func NewHandler(st *store.Store) http.Handler {
	s := &server{st: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/keys/{key...}", s.get)
	mux.HandleFunc("PUT /v1/keys/{key...}", s.put)
	mux.HandleFunc("DELETE /v1/keys/{key...}", s.del)
	return mux
}

type server struct {
	st *store.Store
}

func (s *server) get(w http.ResponseWriter, r *http.Request) {
	e, ok := s.st.Get(r.PathValue("key"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Version", strconv.FormatInt(e.Version, 10))
	io.WriteString(w, e.Value)
}

func (s *server) put(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}
	e, created := s.st.Put(r.PathValue("key"), string(body))
	w.Header().Set("X-Version", strconv.FormatInt(e.Version, 10))
	if created {
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) del(w http.ResponseWriter, r *http.Request) {
	if !s.st.Delete(r.PathValue("key")) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
