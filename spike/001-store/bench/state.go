//go:build lance

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/duck"
)

// statsQuery is threshold T6: pass rate and percentiles grouped by three arm
// fields. censusQuery is T7: tool failures by model and tool.
const (
	statsQuery = `SELECT model, thinking, harness, count(*) AS n,
		avg(CASE WHEN verdict = 'PASS' THEN 1.0 ELSE 0 END) AS pass_rate,
		quantile_cont(wall_ms, 0.5) AS p50, quantile_cont(wall_ms, 0.9) AS p90
		FROM runs GROUP BY ALL ORDER BY n DESC`
	censusQuery = `SELECT r.model, t.tool, t.error_class, count(*) AS n
		FROM tool_calls t JOIN runs r ON r.id = t.run_id
		WHERE NOT t.ok GROUP BY ALL ORDER BY n DESC`
)

func cmdState(args []string) error {
	fs := flag.NewFlagSet("state", flag.ExitOnError)
	dir := fs.String("dir", "", "directory for the DuckDB file")
	runs := fs.Int("runs", 5000, "runs to generate if the database is empty")
	calls := fs.Int("calls", 400, "tool calls per run")
	dur := fs.Duration("duration", time.Hour, "how long to update lane state")
	rate := fs.Int("rate", 10, "lane-state updates per second")
	fs.Parse(args)
	if *dir == "" {
		return errors.New("-dir is required")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	ctx := context.Background()
	path := filepath.Join(*dir, "state.duckdb")
	st, err := duck.Open(ctx, path)
	if err != nil {
		return err
	}
	defer st.Close()
	out := map[string]any{"runs": *runs, "calls_per_run": *calls, "duration_s": dur.Seconds(), "rate": *rate}

	if n, _ := st.CountRuns(ctx, store.RunFilter{}); n == 0 {
		rng := rand.New(rand.NewSource(1))
		models := []string{"m-a", "m-b", "m-c", "m-d", "m-e", "m-f"}
		tools := []string{"read_file", "edit_file", "run_commands", "search", "list_files", "ask_question", "submit"}
		classes := []string{"not_found", "timeout", "schema", "permission", "exit_nonzero"}
		start := time.Now()
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < *runs; i++ {
			id := fmt.Sprintf("run-%06d", i)
			v := store.Pass
			switch r := rng.Intn(100); {
			case r < 22:
				v = store.Fail
			case r < 27:
				v = store.Timeout
			case r < 31:
				v = store.Aborted
			}
			r := store.Run{ID: id, Suite: "custom", TaskID: "t", TaskVersion: 1 + i%3, ArmID: fmt.Sprintf("arm-%d", i%40),
				Verdict: v, Model: models[i%len(models)], Thinking: []string{"off", "low", "high"}[i%3], Harness: fmt.Sprintf("4.100.%d", 240+i%5),
				StartedAt: base.Add(time.Duration(i) * 7 * time.Minute), WallMS: int64(60000 + rng.Intn(1800000))}
			if err := st.UpsertRun(ctx, &r); err != nil {
				return err
			}
			tc := make([]store.ToolCall, *calls)
			for j := range tc {
				ok := rng.Intn(12) != 0
				tc[j] = store.ToolCall{Turn: j / 3, Seq: j, Tool: tools[rng.Intn(len(tools))], OK: ok, ParsePath: "native", DurationMS: int64(rng.Intn(4000))}
				if !ok {
					tc[j].ErrorClass = classes[rng.Intn(len(classes))]
				}
			}
			if err := st.ReplaceToolCalls(ctx, id, tc); err != nil {
				return err
			}
		}
		if err := st.Checkpoint(ctx); err != nil {
			return err
		}
		el := time.Since(start)
		out["generate_s"] = el.Seconds()
		out["generate_runs_per_s"] = float64(*runs) / el.Seconds()
	}
	size := func() int64 {
		var total int64
		for _, p := range []string{path, path + ".wal"} {
			if fi, err := os.Stat(p); err == nil {
				total += fi.Size()
			}
		}
		return total
	}
	out["size_before_mib"] = float64(size()) / (1 << 20)

	run := func(q string) (time.Duration, int, error) {
		t := time.Now()
		rows, err := st.Query(ctx, q)
		if err != nil {
			return 0, 0, err
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		return time.Since(t), n, rows.Err()
	}
	// Cold-ish first execution, then warm ones on an idle database.
	for name, q := range map[string]string{"stats": statsQuery, "census": censusQuery} {
		first, n, err := run(q)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		var warm []time.Duration
		for i := 0; i < 20; i++ {
			d, _, err := run(q)
			if err != nil {
				return err
			}
			warm = append(warm, d)
		}
		out[name+"_rows"] = n
		out[name+"_first_ms"] = float64(first.Microseconds()) / 1000
		out[name+"_idle_ms"] = summarise(warm)
	}

	// T12: lane-state updates at a fixed rate while both queries loop.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	loaded := map[string][]time.Duration{}
	var queryErr error
	for name, q := range map[string]string{"stats": statsQuery, "census": censusQuery} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				d, _, err := run(q)
				mu.Lock()
				if err != nil && queryErr == nil {
					queryErr = err
				}
				loaded[name] = append(loaded[name], d)
				mu.Unlock()
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}
	var updates []time.Duration
	var late int
	tick := time.NewTicker(time.Second / time.Duration(*rate))
	deadline := time.Now().Add(*dur)
	for i := 0; time.Now().Before(deadline); i++ {
		<-tick.C
		t := time.Now()
		err := st.SetLaneState(ctx, store.LaneState{Lane: fmt.Sprintf("lane-%d", i%8), State: []string{"idle", "running"}[i%2], RunID: fmt.Sprintf("run-%06d", i%*runs)})
		if err != nil {
			close(stop)
			wg.Wait()
			return fmt.Errorf("lane-state update %d: %w", i, err)
		}
		d := time.Since(t)
		updates = append(updates, d)
		if d > time.Second/time.Duration(*rate) {
			late++
		}
	}
	tick.Stop()
	close(stop)
	wg.Wait()
	if queryErr != nil {
		return queryErr
	}
	out["update_ms"] = summarise(updates)
	out["updates_slower_than_interval"] = late
	out["stats_under_load_ms"] = summarise(loaded["stats"])
	out["census_under_load_ms"] = summarise(loaded["census"])
	out["size_after_mib"] = float64(size()) / (1 << 20)
	if err := st.Checkpoint(ctx); err != nil {
		return err
	}
	out["size_after_checkpoint_mib"] = float64(size()) / (1 << 20)
	rss, peak := memory()
	out["rss_mib"], out["peak_rss_mib"] = rss, peak
	return emit(out)
}
