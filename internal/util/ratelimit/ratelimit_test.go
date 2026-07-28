package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a hand-cranked time source, so refill is tested exactly rather than by sleeping.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestStartsFull(t *testing.T) {
	l := New(10, 5, WithClock(newClock().Now))

	// A freshly built limiter must not penalize the first requests.
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			t.Fatalf("call %d denied on a full bucket", i+1)
		}
	}
	if l.Allow() {
		t.Error("call 6 allowed; the burst is 5")
	}
}

func TestTokensRefillOverTime(t *testing.T) {
	tick := newClock()
	l := New(10, 5, WithClock(tick.Now)) // 10/s means one token per 100ms

	for i := 0; i < 5; i++ {
		l.Allow()
	}
	if l.Allow() {
		t.Fatal("bucket should be empty")
	}

	tick.advance(100 * time.Millisecond)
	if !l.Allow() {
		t.Error("one token should have accrued after 100ms")
	}
	if l.Allow() {
		t.Error("only one token should have accrued")
	}

	tick.advance(300 * time.Millisecond)
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Errorf("token %d should have accrued after 300ms", i+1)
		}
	}
	if l.Allow() {
		t.Error("a fourth token accrued from 300ms at 10/s")
	}
}

// TestRefillIsCappedAtBurst is what separates a token bucket from an unbounded credit: idling for
// an hour must not buy an hour's worth of calls at once.
func TestRefillIsCappedAtBurst(t *testing.T) {
	tick := newClock()
	l := New(10, 5, WithClock(tick.Now))

	for i := 0; i < 5; i++ {
		l.Allow()
	}
	tick.advance(time.Hour)

	if got := l.Tokens(); got != 5 {
		t.Errorf("tokens = %v after an hour idle, want the burst of 5", got)
	}
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			t.Fatalf("call %d denied", i+1)
		}
	}
	if l.Allow() {
		t.Error("a sixth call was allowed; refill exceeded the burst")
	}
}

// TestWaitBlocksUntilATokenAccrues uses a real clock, since the point is that it actually waits.
func TestWaitBlocksUntilATokenAccrues(t *testing.T) {
	l := New(20, 1) // one token, then one per 50ms

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 30*time.Millisecond {
		t.Errorf("second wait returned after %v; it should have blocked for about 50ms", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("second wait took %v, far longer than the refill interval", elapsed)
	}
}

// TestWaitRespectsContext keeps a queued caller from outliving its own deadline.
func TestWaitRespectsContext(t *testing.T) {
	l := New(1, 1) // one token, then one per second

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := l.Wait(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a context error")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("wait returned after %v; it ignored cancellation", elapsed)
	}
}

func TestWaitOnACancelledContextDoesNotProceed(t *testing.T) {
	l := New(10, 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := l.Wait(ctx); err == nil {
		t.Error("wait succeeded on an already-cancelled context")
	}
}

// TestUnlimitedNeverBlocks covers the misconfiguration path: a zero rate must not deadlock every
// provider call.
func TestUnlimitedNeverBlocks(t *testing.T) {
	for name, l := range map[string]*Limiter{
		"zero rate":      New(0, 5),
		"zero burst":     New(10, 0),
		"negative rate":  New(-1, 5),
		"negative burst": New(10, -1),
	} {
		t.Run(name, func(t *testing.T) {
			if !l.Unlimited() {
				t.Fatal("Unlimited() = false")
			}
			for i := 0; i < 100; i++ {
				if !l.Allow() {
					t.Fatalf("call %d denied by an unlimited limiter", i+1)
				}
			}
			if err := l.Wait(context.Background()); err != nil {
				t.Errorf("Wait: %v", err)
			}
		})
	}
}

// TestConcurrentUseGrantsExactlyTheBurst is the reason for the mutex: one limiter is shared by
// every in-flight request for that provider. Meaningful under -race.
func TestConcurrentUseGrantsExactlyTheBurst(t *testing.T) {
	const burst = 10
	// Rate low enough that no token accrues during the test.
	l := New(0.001, burst)

	var (
		granted atomic.Int64
		wg      sync.WaitGroup
	)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow() {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := granted.Load(); got != burst {
		t.Errorf("granted %d calls, want exactly the burst of %d", got, burst)
	}
}

func TestTokensReportsRemainingAllowance(t *testing.T) {
	tick := newClock()
	l := New(10, 5, WithClock(tick.Now))

	if got := l.Tokens(); got != 5 {
		t.Errorf("tokens = %v on a fresh limiter, want 5", got)
	}
	l.Allow()
	l.Allow()
	if got := l.Tokens(); got != 3 {
		t.Errorf("tokens = %v after two calls, want 3", got)
	}

	// An unlimited limiter has no meaningful allowance to report.
	if got := New(0, 0).Tokens(); got != 0 {
		t.Errorf("unlimited tokens = %v, want 0", got)
	}
}
