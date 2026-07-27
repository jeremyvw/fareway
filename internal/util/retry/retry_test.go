package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errFlaky = errors.New("flaky")

func fast() Config {
	return Config{Attempts: 3, Base: time.Millisecond, Max: 2 * time.Millisecond}
}

func TestSucceedsFirstTime(t *testing.T) {
	calls := 0
	got, err := Do(context.Background(), fast(), func(context.Context) (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil || got != "ok" {
		t.Fatalf("Do = %q, %v", got, err)
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
}

func TestRecoversOnALaterAttempt(t *testing.T) {
	calls := 0
	got, err := Do(context.Background(), fast(), func(context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, errFlaky
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got != 42 {
		t.Errorf("Do = %d, want 42", got)
	}
	if calls != 3 {
		t.Errorf("called %d times, want 3", calls)
	}
}

func TestGivesUpAndKeepsTheLastError(t *testing.T) {
	calls := 0
	_, err := Do(context.Background(), fast(), func(context.Context) (int, error) {
		calls++
		return 0, errFlaky
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, errFlaky) {
		t.Errorf("error = %v, want it to wrap the operation's own error", err)
	}
	if calls != 3 {
		t.Errorf("called %d times, want 3 (the configured attempt count)", calls)
	}
}

// TestStopsOnCancellation is the property that keeps a retry loop from outliving the budget
// its caller set.
func TestStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	_, err := Do(ctx, Config{Attempts: 5, Base: 10 * time.Millisecond, Max: 10 * time.Millisecond},
		func(context.Context) (int, error) {
			calls++
			cancel()
			return 0, errFlaky
		})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("called %d times after cancellation, want 1", calls)
	}
}

// TestDoesNotRetryAContextError distinguishes "the provider hiccuped" from "we are out of
// time"; retrying the latter cannot help.
func TestDoesNotRetryAContextError(t *testing.T) {
	calls := 0
	_, err := Do(context.Background(), fast(), func(context.Context) (int, error) {
		calls++
		return 0, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want DeadlineExceeded", err)
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
}

func TestBackoffGrowsButIsCapped(t *testing.T) {
	start := time.Now()
	calls := 0
	_, _ = Do(context.Background(), Config{Attempts: 4, Base: 10 * time.Millisecond, Max: 20 * time.Millisecond},
		func(context.Context) (int, error) {
			calls++
			return 0, errFlaky
		})

	// Waits are 10ms, 20ms, 20ms: doubling, then held at the cap.
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("elapsed %v, expected at least 40ms of backoff", elapsed)
	}
	if calls != 4 {
		t.Errorf("called %d times, want 4", calls)
	}
}

func TestZeroConfigStillRunsOnce(t *testing.T) {
	calls := 0
	_, err := Do(context.Background(), Config{}, func(context.Context) (int, error) {
		calls++
		return 0, errFlaky
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
}
