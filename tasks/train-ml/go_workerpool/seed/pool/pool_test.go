package pool

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunSucceeds(t *testing.T) {
	jobs := []Job{
		{ID: "one", Run: func(context.Context) (string, error) { return "1", nil }},
	}
	got, err := Run(context.Background(), Options{Workers: 2}, jobs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 || got[0].Value != "1" || got[0].Err != nil || got[0].Attempts != 1 {
		t.Fatalf("unexpected result %+v", got)
	}
}

func TestInvalidWorkers(t *testing.T) {
	if _, err := Run(context.Background(), Options{Workers: 0}, nil); !errors.Is(err, ErrInvalidWorkers) {
		t.Fatalf("want ErrInvalidWorkers, got %v", err)
	}
}

func TestFailureMessage(t *testing.T) {
	jobs := []Job{
		{ID: "x", Run: func(context.Context) (string, error) { return "", errors.New("disk full") }},
	}
	got, _ := Run(context.Background(), Options{Workers: 1}, jobs)
	if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "disk full") {
		t.Fatalf("want the job error, got %v", got[0].Err)
	}
}
