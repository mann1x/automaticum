# go_workerpool notes

- Planted defects (pool/pool.go, pool/attempt.go): (1) results appended in completion order instead of job
  order; (2) retry loop `attempt <= maxAttempts` (one extra attempt); (3) `fmt.Errorf("job %s: %v")` loses the
  wrapped error (errors.Is fails for ErrPermanent / job errors / ctx errors); (4) cancellation: workers quit on
  ctx.Done while the dispatcher goroutine blocks forever on `queue <- job` (goroutine leak), and jobs never
  started get no Result.
- Reference: index-tagged tasks written into a pre-sized slice, dispatcher in Run selecting on ctx.Done,
  close(queue)+wg.Wait, unstarted jobs get `job <id>: %w ctx.Err()`, `%w`, `attempt < maxAttempts`;
  Run returns ctx.Err() when any result carries it.
- Determinism: ordering forced with channels (c finishes before b before a); concurrency peak via a barrier;
  leak via runtime.NumGoroutine polled for 2s; each test has a 5s guard so a deadlock is a FAIL, not a hang.
- Near-misses: `retry-off-by-one` (everything else fixed) and `cancel-returns-nil` (Run returns nil error
  after a cancelled batch).
- Needs an exec /tmp (it builds `./.chk` and runs it). automaticum mounts the oracle /tmp exec since 63499af and
  verify_task.py replays that by default; under `--tmp-noexec` the first step (exec probe) fails cleanly with
  stage "build" and the message "... the temp dir is mounted noexec here: run_tests needs an exec /tmp".
- Go gotcha: the runner sets TMPDIR=$WORK, and `go` IGNORES a go.mod that sits in the system temp root
  ("go: warning: ignoring go.mod in system temp root"). checker.json therefore sets TMPDIR=/tmp and
  GOTMPDIR=$WORK. Also GOPROXY=off, GOSUMDB=off, CGO_ENABLED=0, GOFLAGS=-mod=mod, GOTOOLCHAIN=local.
