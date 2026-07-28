package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// clock is a hand-cranked time source, so expiry is tested without sleeping.
type clock struct {
	mu  sync.Mutex
	now time.Time
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

func newClock() *clock {
	return &clock{now: time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)}
}

func TestSetThenGet(t *testing.T) {
	c := New[string](time.Minute)

	if _, ok := c.Get("missing"); ok {
		t.Error("Get on an empty cache reported a hit")
	}

	c.Set("k", "v")
	got, ok := c.Get("k")
	if !ok || got != "v" {
		t.Errorf("Get = %q, %v; want v, true", got, ok)
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
}

func TestEntryExpires(t *testing.T) {
	tick := newClock()
	c := New[string](30*time.Second, WithClock[string](tick.Now))

	c.Set("k", "v")

	tick.advance(29 * time.Second)
	if _, ok := c.Get("k"); !ok {
		t.Error("entry expired early")
	}

	tick.advance(2 * time.Second)
	if _, ok := c.Get("k"); ok {
		t.Error("entry survived past its TTL")
	}
	// The expired entry is reaped on read rather than left to accumulate.
	if c.Len() != 0 {
		t.Errorf("Len = %d after expiry, want 0", c.Len())
	}
}

func TestSetRefreshesTheDeadline(t *testing.T) {
	tick := newClock()
	c := New[string](30*time.Second, WithClock[string](tick.Now))

	c.Set("k", "first")
	tick.advance(20 * time.Second)
	c.Set("k", "second")
	tick.advance(20 * time.Second)

	got, ok := c.Get("k")
	if !ok {
		t.Fatal("entry expired despite being overwritten 20s ago")
	}
	if got != "second" {
		t.Errorf("Get = %q, want second", got)
	}
}

func TestZeroTTLFallsBackToDefault(t *testing.T) {
	c := New[string](0)
	c.Set("k", "v")

	if _, ok := c.Get("k"); !ok {
		t.Error("a zero TTL must fall back to DefaultTTL, not expire immediately")
	}
}

func TestPurge(t *testing.T) {
	c := New[int](time.Minute)
	c.Set("a", 1)
	c.Set("b", 2)

	c.Purge()

	if c.Len() != 0 {
		t.Errorf("Len = %d after Purge, want 0", c.Len())
	}
	if _, ok := c.Get("a"); ok {
		t.Error("entry survived Purge")
	}
}

// TestConcurrentAccess is the reason for the mutex: the cache is shared across every in-flight
// request. Run with -race for this to mean anything.
func TestConcurrentAccess(t *testing.T) {
	c := New[int](time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n%5)
			c.Set(key, n)
			c.Get(key)
			c.Len()
		}(i)
	}
	wg.Wait()

	if c.Len() != 5 {
		t.Errorf("Len = %d, want 5 distinct keys", c.Len())
	}
}

// TestStructValuesRoundTrip covers the actual usage: the cache holds a composite, not a scalar.
func TestStructValuesRoundTrip(t *testing.T) {
	type payload struct {
		Names []string
		Count int
	}

	c := New[payload](time.Minute)
	c.Set("k", payload{Names: []string{"a", "b"}, Count: 2})

	got, ok := c.Get("k")
	if !ok {
		t.Fatal("miss")
	}
	if got.Count != 2 || len(got.Names) != 2 || got.Names[0] != "a" {
		t.Errorf("Get = %+v", got)
	}
}
