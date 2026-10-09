// Package fetch retrieves postings from job sources and normalizes them.
package fetch

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/0xataru/workerpool"

	"github.com/0xataru/jobster/internal/job"
)

// Fetcher retrieves postings from one source.
type Fetcher interface {
	// Name identifies the source in logs and in Job.Source.
	Name() string
	Fetch(ctx context.Context) ([]job.Job, error)
}

// Result is the outcome of running one fetcher.
type Result struct {
	Source string
	Jobs   []job.Job
	Err    error
	Took   time.Duration
}

// Run executes fetchers on a pool of workers, giving each its own timeout.
// A failing or panicking fetcher only affects its own Result. Results are
// returned in the order of fetchers.
func Run(ctx context.Context, fetchers []Fetcher, workers int, timeout time.Duration) []Result {
	pooled := workerpool.Map(ctx, workers, fetchers, func(ctx context.Context, f Fetcher) (Result, error) {
		return runOne(ctx, f, timeout), nil
	})
	results := make([]Result, len(pooled))
	for i, r := range pooled {
		results[i] = r.Value
		if r.Err != nil { // the run was cancelled before this fetcher started
			results[i] = Result{Source: r.Input.Name(), Err: r.Err}
		}
	}
	return results
}

// runOne recovers panics itself because workerpool deliberately does not: a
// parser bug in one source must not take down the whole run.
func runOne(ctx context.Context, f Fetcher, timeout time.Duration) (res Result) {
	start := time.Now()
	res.Source = f.Name()
	defer func() {
		if r := recover(); r != nil {
			res.Jobs = nil
			res.Err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
		res.Took = time.Since(start)
	}()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res.Jobs, res.Err = f.Fetch(ctx)
	return res
}
