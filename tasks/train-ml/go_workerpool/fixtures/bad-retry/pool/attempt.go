package pool

import (
	"context"
	"errors"
	"fmt"
)

// runJob calls job.Run until it succeeds, fails permanently, runs out of attempts or ctx ends.
func runJob(ctx context.Context, job Job, maxAttempts int) Result {
	res := Result{ID: job.ID}
	var err error
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			break
		}
		res.Attempts++
		var v string
		v, err = job.Run(ctx)
		if err == nil {
			res.Value = v
			return res
		}
		if errors.Is(err, ErrPermanent) {
			break
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	res.Err = fmt.Errorf("job %s: %w", job.ID, err)
	return res
}
