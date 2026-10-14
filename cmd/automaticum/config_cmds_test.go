package main

import (
	"flag"
	"reflect"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/task"
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

// A subset names exactly its tasks: finding the first must not make every
// later task of the suite match too.
func TestCellTasks(t *testing.T) {
	all := []*task.Task{{Suite: "demo", ID: "answer"}, {Suite: "demo", ID: "cancel"}, {Suite: "demo", ID: "scripted"}, {Suite: "other", ID: "x"}}
	for _, c := range []struct{ subset, want string }{
		{"", "demo/answer demo/cancel demo/scripted"},
		{"cancel", "demo/cancel"},
		{"answer", "demo/answer"},
		{"scripted, answer", "demo/answer demo/scripted"},
	} {
		got, err := cellTasks(all, "demo", c.subset)
		if err != nil || strings.Join(got, " ") != c.want {
			t.Errorf("subset %q: %v %v", c.subset, got, err)
		}
	}
	if _, err := cellTasks(all, "demo", "cancel,nope"); err == nil || !strings.Contains(err.Error(), "no task nope") {
		t.Errorf("a missing task: %v", err)
	}
}
