package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter is a token bucket.
//
// Tokens accrue continuously at Rate and accumulate up to Burst, so a caller that has been idle
// may fire a short burst and then settles to the sustained rate. That matches how provider
// quotas are usually written — "N per second, bursts to M" — better than a fixed window, which
// allows 2N across a window boundary.
type Limiter struct {
	rate  float64 // tokens added per second
	burst float64 // maximum tokens held

	// now is injectable so expiry and refill can be tested without sleeping.
	now func() time.Time

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

type Option func(*Limiter)

func WithClock(now func() time.Time) Option {
	return func(l *Limiter) {
		l.now = now
		l.last = now()
	}
}

func New(ratePerSecond float64, burst int, opts ...Option) *Limiter {
	l := &Limiter{
		rate:  ratePerSecond,
		burst: float64(burst),
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.last.IsZero() {
		l.last = l.now()
	}
	// Start full, so a freshly built limiter does not penalize the first requests.
	l.tokens = l.burst
	return l
}

func (l *Limiter) Unlimited() bool {
	return l.rate <= 0 || l.burst <= 0
}

func (l *Limiter) Allow() bool {
	if l.Unlimited() {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked()
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

func (l *Limiter) Wait(ctx context.Context) error {
	if l.Unlimited() {
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		ok, delay := l.reserve()
		if ok {
			return nil
		}

		// Sleep outside the lock so other callers can still take tokens as they accrue.
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("rate limit wait: %w", ctx.Err())
		}
	}
}

func (l *Limiter) reserve() (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked()
	if l.tokens >= 1 {
		l.tokens--
		return true, 0
	}

	missing := 1 - l.tokens
	return false, time.Duration(missing / l.rate * float64(time.Second))
}

func (l *Limiter) refillLocked() {
	now := l.now()
	elapsed := now.Sub(l.last)
	if elapsed <= 0 {
		return
	}
	l.last = now

	l.tokens += elapsed.Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
}

func (l *Limiter) Tokens() float64 {
	if l.Unlimited() {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked()
	return l.tokens
}
