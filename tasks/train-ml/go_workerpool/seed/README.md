# batchrun

`pool` runs a batch of jobs on a bounded number of goroutines, with retries and cancellation. It is used by
our nightly exporters (see `cmd/batchrun` for a small example). Standard library only, Go 1.23.

```go
type Job struct {
    ID  string
    Run func(ctx context.Context) (string, error)
}

type Result struct {
    ID       string
    Value    string // the job's value when it succeeded
    Err      error  // nil when the job succeeded
    Attempts int    // how many times Run was called for this job
}

type Options struct {
    Workers     int // jobs running at the same time; must be >= 1
    MaxAttempts int // total attempts per job, first one included; values < 1 mean 1
}

func Run(ctx context.Context, opts Options, jobs []Job) ([]Result, error)
```

## Contract

1. `Run` returns exactly one `Result` per job, **in the order of `jobs`** (`results[i]` is `jobs[i]`),
   whatever order the jobs finish in. An empty batch gives an empty, non-nil slice and a nil error.
2. `opts.Workers < 1` returns `nil, ErrInvalidWorkers` (test with `errors.Is`).
3. Never more than `opts.Workers` jobs run at the same time, and every job runs (at most once per attempt).
4. A job whose `Run` returns an error is retried until it succeeds or `MaxAttempts` attempts were made in
   total. An error that wraps `ErrPermanent` is not retried.
5. A job that finally fails gets `Err = "job <ID>: <last error>"`, wrapping the last error so
   `errors.Is` / `errors.As` still see the job's own error.
6. Cancellation: once `ctx` is done no new job and no new attempt starts. Jobs still running get the
   cancelled `ctx` and are waited for. Jobs that never started get `Attempts == 0` and
   `Err = "job <ID>: <ctx.Err()>"` (wrapping `ctx.Err()`). If `ctx` ends before every job has finished,
   `Run` returns the results **and** `ctx.Err()`; otherwise the error is nil.
7. `Run` does not return before every goroutine it started has exited -- the exporters call it in a loop
   and must not leak goroutines.

`go test ./...` runs the package's own (small) tests.
