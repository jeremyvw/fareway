package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Config describes a backoff policy. Attempts counts the first try, so Attempts: 3 means
// one call and two retries.
type Config struct {
	Attempts int
	Base     time.Duration
	Max      time.Duration
}

// Default is a short policy suited to a provider that fails roughly one call in ten: two
// retries, far enough apart to clear a blip but well inside a request budget.
func Default() Config {
	return Config{Attempts: 3, Base: 50 * time.Millisecond, Max: 200 * time.Millisecond}
}

// Do runs fn until it succeeds or the attempts are exhausted, returning the last error.
//
// A cancelled or expired context stops the loop immediately: retrying past the caller's
// deadline would burn the budget it set without any chance of a useful answer.
func Do[T any](ctx context.Context, cfg Config, fn func(context.Context) (T, error)) (T, error) {
	var zero T

	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	if cfg.Base <= 0 {
		cfg.Base = time.Millisecond
	}
	if cfg.Max < cfg.Base {
		cfg.Max = cfg.Base
	}

	delay := cfg.Base
	var lastErr error

	for attempt := 1; attempt <= cfg.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, wrap(err, attempt-1, lastErr)
		}

		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if isContextError(err) {
			return zero, err
		}
		if attempt == cfg.Attempts {
			break
		}
		if err := sleep(ctx, delay); err != nil {
			return zero, wrap(err, attempt, lastErr)
		}
		if delay *= 2; delay > cfg.Max {
			delay = cfg.Max
		}
	}

	return zero, fmt.Errorf("gave up after %d attempt(s): %w", cfg.Attempts, lastErr)
}

// sleep waits, but abandons the wait if the context ends first.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// wrap keeps the reason we stopped and what the operation last complained about, since
// either one alone leaves the failure hard to diagnose.
func wrap(ctxErr error, attempts int, lastErr error) error {
	if lastErr == nil {
		return ctxErr
	}
	return fmt.Errorf("%w after %d attempt(s), last error: %v", ctxErr, attempts, lastErr)
}
