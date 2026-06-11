package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/log"
)

// PollOptions controls Poll's retry behaviour.
//
// In the Groovy original these came from instance fields
// (SLEEPTIME=1000ms, DEFAULT_RETRIES=120). Here we pass them in explicitly
// or let DefaultPollOptions cover the common case.
type PollOptions struct {
	// Interval is the delay between attempts.
	Interval time.Duration
	// Timeout is the maximum wall-clock time before Poll gives up. Zero
	// means "use the context's deadline".
	Timeout time.Duration
	// Description is rendered into log lines and the timeout error.
	Description string
}

// DefaultPollOptions matches the old Groovy defaults: 1 s interval, 120 s
// total timeout (= 120 retries × 1 s).
func DefaultPollOptions(description string) PollOptions {
	return PollOptions{
		Interval:    time.Second,
		Timeout:     120 * time.Second,
		Description: description,
	}
}

// ErrPollTimeout is returned (wrapped) when Poll exits without success.
var ErrPollTimeout = errors.New("poll timed out")

// Poll repeatedly invokes fetch until it returns (value, true, nil),
// or until the context / Timeout is exceeded. fetch may return errors;
// non-fatal errors are logged at trace level and treated as "not ready
// yet".
//
// fetch must return:
//
//	value, true, nil      → success, returned to caller
//	zero,  false, nil     → not ready, will retry after Interval
//	_,     _,    error    → transient failure (trace-logged + retried)
//
// This consolidates the duplicated waitForResourceWithRetry /
// waitForResourcePhase pattern from the Groovy source.
func Poll[T any](ctx context.Context, opts PollOptions, fetch func(context.Context) (T, bool, error)) (T, error) {
	var zero T
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}

	pollCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Try once immediately so very short polls succeed without waiting an
	// extra Interval.
	value, done, err := fetch(pollCtx)
	if err != nil {
		log.Trace("poll attempt failed", "desc", opts.Description, "err", err)
	}
	if done && err == nil {
		return value, nil
	}

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	attempt := 1
	for {
		select {
		case <-pollCtx.Done():
			// If the parent context is still alive, we hit our Timeout.
			// Otherwise the caller cancelled.
			if ctx.Err() == nil {
				return zero, fmt.Errorf("%w waiting for %s after %d attempts: %v",
					ErrPollTimeout, opts.Description, attempt, pollCtx.Err())
			}
			return zero, fmt.Errorf("context cancelled while waiting for %s: %w",
				opts.Description, pollCtx.Err())
		case <-ticker.C:
			attempt++
			value, done, err = fetch(pollCtx)
			if err != nil {
				log.Trace("poll attempt failed",
					"desc", opts.Description, "attempt", attempt, "err", err)
			}
			if done && err == nil {
				return value, nil
			}
			log.Trace("still waiting", "desc", opts.Description, "attempt", attempt)
		}
	}
}
