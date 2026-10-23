// Package pool runs batches of jobs on a bounded set of worker goroutines.
package pool

import (
	"context"
	"errors"
	"sync"
)

// ErrInvalidWorkers is returned by Run when Options.Workers is less than 1.
var ErrInvalidWorkers = errors.New("pool: workers must be at least 1")

// ErrPermanent marks a job error that must not be retried. Jobs wrap it:
//
//	return "", fmt.Errorf("bad record %d: %w", n, pool.ErrPermanent)
var ErrPermanent = errors.New("permanent failure")

// Job is one unit of work.
type Job struct {
	ID  string
	Run func(ctx context.Context) (string, error)
}

// Result is the outcome of one Job.
type Result struct {
	ID       string
	Value    string
	Err      error
	Attempts int
}

// Options configures Run.
type Options struct {
	Workers     int
	MaxAttempts int
}

// Run executes jobs on at most opts.Workers goroutines. See README.md for the full contract.
func Run(ctx context.Context, opts Options, jobs []Job) ([]Result, error) {
	if opts.Workers < 1 {
		return nil, ErrInvalidWorkers
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	workers := opts.Workers
	if workers > len(jobs) {
		workers = len(jobs)
	}

	queue := make(chan Job)
	done := make(chan Result)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-queue:
					if !ok {
						return
					}
					done <- runJob(ctx, job, maxAttempts)
				}
			}
		}()
	}

	go func() {
		for _, job := range jobs {
			queue <- job
		}
		close(queue)
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	results := make([]Result, 0, len(jobs))
	for r := range done {
		results = append(results, r)
	}
	return results, ctx.Err()
}
