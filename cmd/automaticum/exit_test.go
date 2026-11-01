package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitOf(t *testing.T) {
	for _, c := range []struct {
		err  error
		want int
	}{
		{errors.New("usage: automaticum run show <run>"), exitUsage},
		{&apiErr{Status: 404, Msg: "there is no run x"}, exitNotFound},
		{fmt.Errorf("run x: %w", &apiErr{Status: 409, Msg: "in flight"}), exitRefused},
		{errDown, exitDown},
		{errors.New("something else"), exitFailed},
	} {
		if got := exitOf(c.err); got != c.want {
			t.Errorf("%v: %d, want %d", c.err, got, c.want)
		}
	}
}
