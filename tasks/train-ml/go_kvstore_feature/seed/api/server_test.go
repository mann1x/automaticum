package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/northwind/kvd/store"
)

func TestDeleteMissing(t *testing.T) {
	srv := httptest.NewServer(NewHandler(store.New(nil)))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/keys/nope", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE of a missing key: want 404, got %d", resp.StatusCode)
	}
}

func TestPutCreates(t *testing.T) {
	srv := httptest.NewServer(NewHandler(store.New(nil)))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/keys/greeting", strings.NewReader(`{"value":"hello"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first PUT: want 201, got %d", resp.StatusCode)
	}
}
