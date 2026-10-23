// Command batchrun is a small demo of package pool: it "exports" a few fake tables.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/northwind/batchrun/pool"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tables := []string{"customers", "orders", "invoices", "shipments", "returns", "audit"}
	jobs := make([]pool.Job, 0, len(tables))
	for _, table := range tables {
		table := table
		jobs = append(jobs, pool.Job{ID: table, Run: func(ctx context.Context) (string, error) {
			select {
			case <-time.After(time.Duration(50+rand.Intn(200)) * time.Millisecond):
			case <-ctx.Done():
				return "", ctx.Err()
			}
			if table == "audit" {
				return "", fmt.Errorf("audit export disabled: %w", pool.ErrPermanent)
			}
			if rand.Intn(4) == 0 {
				return "", errors.New("connection reset")
			}
			return fmt.Sprintf("%s.csv", table), nil
		}})
	}

	results, err := pool.Run(ctx, pool.Options{Workers: 3, MaxAttempts: 3}, jobs)
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("%-10s FAILED after %d attempt(s): %v\n", r.ID, r.Attempts, r.Err)
			continue
		}
		fmt.Printf("%-10s %s (%d attempt(s))\n", r.ID, r.Value, r.Attempts)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "batch interrupted:", err)
		os.Exit(1)
	}
}
