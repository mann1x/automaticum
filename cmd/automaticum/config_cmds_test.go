package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	n := fs.Int("repeats", 3, "")
	j := fs.Bool("json", false, "")
	got := parse(fs, []string{"a", "-repeats", "5", "b", "--json"})
	if !reflect.DeepEqual(got, []string{"a", "b"}) || *n != 5 || !*j {
		t.Fatalf("got %v, repeats %d, json %v", got, *n, *j)
	}
}
