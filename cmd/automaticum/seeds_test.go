package main

import (
	"slices"
	"testing"
)

func TestParseSeeds(t *testing.T) {
	for in, want := range map[string][]int{"1-10": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, "7": {7}, "5, 3,9-10": {5, 3, 9, 10}, "0-2": {0, 1, 2}} {
		got, err := parseSeeds(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "a", "3-1", "1-", "-3", "1-5000"} {
		if got, err := parseSeeds(in); err == nil {
			t.Errorf("%q: %v", in, got)
		}
	}
}
