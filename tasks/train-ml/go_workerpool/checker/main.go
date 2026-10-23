package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/northwind/batchrun/pool"
)

func run(name string, f func() error) {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- f()
	}()
	select {
	case err := <-done:
		if err != nil {
			fmt.Printf("TEST %s FAIL %s\n", name, strings.ReplaceAll(err.Error(), "\n", " "))
			return
		}
		fmt.Printf("TEST %s PASS\n", name)
	case <-time.After(5 * time.Second):
		fmt.Printf("TEST %s FAIL timed out after 5s (Run never returned: deadlock?)\n", name)
	}
}

func value(v string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return v, nil }
}

func ids(rs []pool.Result) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = r.ID
	}
	return "[" + strings.Join(s, " ") + "]"
}

func main() {
	bg := context.Background()

	run("results_in_job_order", func() error {
		cDone := make(chan struct{})
		bDone := make(chan struct{})
		jobs := []pool.Job{
			{ID: "a", Run: func(context.Context) (string, error) { <-bDone; return "A", nil }},
			{ID: "b", Run: func(context.Context) (string, error) { <-cDone; defer close(bDone); return "B", nil }},
			{ID: "c", Run: func(context.Context) (string, error) { defer close(cDone); return "C", nil }},
		}
		rs, err := pool.Run(bg, pool.Options{Workers: 3}, jobs)
		if err != nil {
			return fmt.Errorf("Run returned error %v", err)
		}
		if ids(rs) != "[a b c]" {
			return fmt.Errorf("jobs a,b,c finishing in order c,b,a: expected results [a b c], got %s", ids(rs))
		}
		if rs[0].Value != "A" || rs[2].Value != "C" {
			return fmt.Errorf("values: expected A..C, got %q..%q", rs[0].Value, rs[2].Value)
		}
		return nil
	})

	run("each_job_runs_once", func() error {
		const n = 50
		var calls [n]int32
		jobs := make([]pool.Job, n)
		for i := range jobs {
			i := i
			jobs[i] = pool.Job{ID: fmt.Sprintf("j%02d", i), Run: func(context.Context) (string, error) {
				atomic.AddInt32(&calls[i], 1)
				return fmt.Sprint(i * i), nil
			}}
		}
		rs, err := pool.Run(bg, pool.Options{Workers: 4, MaxAttempts: 3}, jobs)
		if err != nil {
			return fmt.Errorf("Run returned error %v", err)
		}
		if len(rs) != n {
			return fmt.Errorf("expected %d results, got %d", n, len(rs))
		}
		for i := range jobs {
			if c := atomic.LoadInt32(&calls[i]); c != 1 {
				return fmt.Errorf("job j%02d ran %d times, expected once", i, c)
			}
			if rs[i].ID != jobs[i].ID || rs[i].Value != fmt.Sprint(i*i) || rs[i].Attempts != 1 || rs[i].Err != nil {
				return fmt.Errorf("results[%d]: expected {%s %d 1 attempt}, got {%s %s %d attempts err=%v}", i, jobs[i].ID, i*i, rs[i].ID, rs[i].Value, rs[i].Attempts, rs[i].Err)
			}
		}
		return nil
	})

	run("respects_worker_limit", func() error {
		var running, peak int32
		reached := make(chan struct{})
		var once sync.Once
		jobs := make([]pool.Job, 12)
		for i := range jobs {
			jobs[i] = pool.Job{ID: fmt.Sprint(i), Run: func(context.Context) (string, error) {
				now := atomic.AddInt32(&running, 1)
				for {
					p := atomic.LoadInt32(&peak)
					if now <= p || atomic.CompareAndSwapInt32(&peak, p, now) {
						break
					}
				}
				if now >= 3 {
					once.Do(func() { close(reached) })
				}
				select {
				case <-reached:
				case <-time.After(time.Second):
				}
				time.Sleep(2 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return "ok", nil
			}}
		}
		if _, err := pool.Run(bg, pool.Options{Workers: 3}, jobs); err != nil {
			return fmt.Errorf("Run returned error %v", err)
		}
		if p := atomic.LoadInt32(&peak); p != 3 {
			return fmt.Errorf("with Workers=3 expected exactly 3 jobs running at the peak, saw %d", p)
		}
		return nil
	})

	run("invalid_workers", func() error {
		for _, w := range []int{0, -2} {
			rs, err := pool.Run(bg, pool.Options{Workers: w}, []pool.Job{{ID: "x", Run: value("x")}})
			if !errors.Is(err, pool.ErrInvalidWorkers) || rs != nil {
				return fmt.Errorf("Workers=%d: expected nil, ErrInvalidWorkers; got %d results, %v", w, len(rs), err)
			}
		}
		return nil
	})

	run("empty_batch", func() error {
		rs, err := pool.Run(bg, pool.Options{Workers: 2}, nil)
		if err != nil || rs == nil || len(rs) != 0 {
			return fmt.Errorf("empty batch: expected an empty non-nil slice and nil error, got %v (nil=%t), %v", rs, rs == nil, err)
		}
		return nil
	})

	run("retry_attempt_count", func() error {
		for _, tc := range []struct{ max, want int }{{3, 3}, {1, 1}, {0, 1}} {
			var calls int32
			job := pool.Job{ID: "flaky", Run: func(context.Context) (string, error) {
				atomic.AddInt32(&calls, 1)
				return "", errors.New("connection reset")
			}}
			rs, _ := pool.Run(bg, pool.Options{Workers: 1, MaxAttempts: tc.max}, []pool.Job{job})
			if len(rs) != 1 {
				return fmt.Errorf("expected 1 result, got %d", len(rs))
			}
			if calls != int32(tc.want) || rs[0].Attempts != tc.want {
				return fmt.Errorf("MaxAttempts=%d on an always-failing job: expected %d calls and Attempts=%d, got %d calls, Attempts=%d", tc.max, tc.want, tc.want, calls, rs[0].Attempts)
			}
		}
		return nil
	})

	run("retry_then_success", func() error {
		var calls int32
		job := pool.Job{ID: "eventually", Run: func(context.Context) (string, error) {
			if atomic.AddInt32(&calls, 1) < 3 {
				return "", errors.New("timeout")
			}
			return "done", nil
		}}
		rs, _ := pool.Run(bg, pool.Options{Workers: 2, MaxAttempts: 5}, []pool.Job{job})
		if len(rs) != 1 || rs[0].Err != nil || rs[0].Value != "done" || rs[0].Attempts != 3 {
			return fmt.Errorf("job failing twice then succeeding: expected {done, 3 attempts, nil}, got %+v", rs)
		}
		return nil
	})

	run("permanent_error_not_retried", func() error {
		var calls int32
		job := pool.Job{ID: "bad", Run: func(context.Context) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "", fmt.Errorf("record 7 malformed: %w", pool.ErrPermanent)
		}}
		rs, _ := pool.Run(bg, pool.Options{Workers: 1, MaxAttempts: 4}, []pool.Job{job})
		if len(rs) != 1 || calls != 1 || rs[0].Attempts != 1 {
			return fmt.Errorf("permanent error: expected 1 call and Attempts=1, got %d calls, results %+v", calls, rs)
		}
		if !errors.Is(rs[0].Err, pool.ErrPermanent) {
			return fmt.Errorf("Result.Err %q does not wrap ErrPermanent (errors.Is is false)", rs[0].Err)
		}
		return nil
	})

	run("error_wrapping", func() error {
		errQuota := errors.New("quota exceeded")
		job := pool.Job{ID: "j7", Run: func(context.Context) (string, error) { return "", errQuota }}
		rs, _ := pool.Run(bg, pool.Options{Workers: 1, MaxAttempts: 2}, []pool.Job{job})
		if len(rs) != 1 || rs[0].Err == nil {
			return fmt.Errorf("expected one failed result, got %+v", rs)
		}
		if got := rs[0].Err.Error(); got != "job j7: quota exceeded" {
			return fmt.Errorf("Err message: expected %q, got %q", "job j7: quota exceeded", got)
		}
		if !errors.Is(rs[0].Err, errQuota) {
			return fmt.Errorf("errors.Is(Result.Err, jobErr) is false: the job error is not wrapped")
		}
		return nil
	})

	cancelBatch := func(n, workers int) ([]pool.Result, error, *int32) {
		ctx, cancel := context.WithCancel(bg)
		defer cancel()
		var calls int32
		jobs := make([]pool.Job, n)
		for i := range jobs {
			i := i
			jobs[i] = pool.Job{ID: fmt.Sprintf("t%d", i), Run: func(ctx context.Context) (string, error) {
				atomic.AddInt32(&calls, 1)
				if i == 0 {
					cancel()
					return "first", nil
				}
				return "late", nil
			}}
		}
		rs, err := pool.Run(ctx, pool.Options{Workers: workers, MaxAttempts: 2}, jobs)
		return rs, err, &calls
	}

	run("cancel_marks_unstarted_jobs", func() error {
		rs, err, calls := cancelBatch(5, 1)
		if !errors.Is(err, context.Canceled) {
			return fmt.Errorf("Run after cancellation: expected context.Canceled, got %v", err)
		}
		if len(rs) != 5 {
			return fmt.Errorf("cancelled batch of 5: expected 5 results, got %d %s", len(rs), ids(rs))
		}
		if rs[0].ID != "t0" || rs[0].Value != "first" || rs[0].Err != nil {
			return fmt.Errorf("results[0]: expected t0 succeeded with \"first\", got %+v", rs[0])
		}
		if c := atomic.LoadInt32(calls); c != 1 {
			return fmt.Errorf("%d jobs ran after the context was cancelled", c-1)
		}
		for i := 1; i < 5; i++ {
			r := rs[i]
			want := fmt.Sprintf("job t%d: context canceled", i)
			if r.ID != fmt.Sprintf("t%d", i) || r.Attempts != 0 || r.Err == nil || r.Err.Error() != want || !errors.Is(r.Err, context.Canceled) {
				return fmt.Errorf("results[%d]: expected ID t%d, 0 attempts, Err %q wrapping context.Canceled; got %+v", i, i, want, r)
			}
		}
		return nil
	})

	run("no_goroutine_leak", func() error {
		time.Sleep(50 * time.Millisecond)
		before := runtime.NumGoroutine()
		for round := 0; round < 5; round++ {
			cancelBatch(20, 2)
			pool.Run(bg, pool.Options{Workers: 3}, []pool.Job{{ID: "x", Run: value("x")}, {ID: "y", Run: value("y")}})
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			after := runtime.NumGoroutine()
			if after <= before {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%d goroutines still running 2s after 10 calls to Run returned (5 of them cancelled)", after-before)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}
