// Package pool runs batches of jobs on a bounded set of worker goroutines.
package pool

import (
	"context"
	"errors"
	"fmt"
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

type task struct {
	idx int
	job Job
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

	results := make([]Result, len(jobs))
	started := make([]bool, len(jobs))
	queue := make(chan task)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range queue {
				results[t.idx] = runJob(ctx, t.job, maxAttempts)
			}
		}()
	}

dispatch:
	for i, job := range jobs {
		select {
		case <-ctx.Done():
			break dispatch
		case queue <- task{idx: i, job: job}:
			started[i] = true
		}
	}
	close(queue)
	wg.Wait()

	var cut bool
	for i, job := range jobs {
		if !started[i] {
			results[i] = Result{ID: job.ID, Err: fmt.Errorf("job %s: %w", job.ID, ctx.Err())}
		}
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(results[i].Err, ctxErr) {
			cut = true
		}
	}
	if cut {
		return results, ctx.Err()
	}
	return results, nil
}
