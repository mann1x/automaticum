package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mann1x/automaticum/internal/stubmodel"
)

// stubServe runs the scripted model server. It measures nothing: it exists
// so the harness can be tried end to end without a GPU.
func stubServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stub serve", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:7412", "address to listen on; a lane's container reaches the host through the bridge's gateway")
	script := fs.String("script", "", "the script: a JSON file with the turns the stub answers")
	parse(fs, args)
	if *script == "" {
		return errors.New("usage: automaticum stub serve --script <file> [--listen <address>]")
	}
	data, err := os.ReadFile(*script)
	if err != nil {
		return err
	}
	var sc stubmodel.Script
	if err := json.Unmarshal(data, &sc); err != nil {
		return fmt.Errorf("%s: %w", *script, err)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: stubmodel.New(sc), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	fmt.Fprintf(os.Stderr, "stub model server: %d turns, listening on %s\n", len(sc.Turns), ln.Addr())
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
