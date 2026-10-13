//go:build !cgo

package main

import (
	"context"
	"errors"
)

// The store needs cgo (DuckDB). A build without it has the commands that
// read configuration and tasks, and nothing that opens the store.
var errNoStore = errors.New("this build has no store; use the binary built in the link image")

func legacyImport(context.Context, []string) error { return errNoStore }
func storeCheck(context.Context, []string) error   { return errNoStore }
