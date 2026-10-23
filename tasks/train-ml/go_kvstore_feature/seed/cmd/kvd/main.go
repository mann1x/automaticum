// Command kvd serves an in-memory key-value store over HTTP.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/northwind/kvd/api"
	"github.com/northwind/kvd/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.NewHandler(store.New(nil)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("kvd listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
