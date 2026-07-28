package cache

import (
	"sync"
	"time"
)

const DefaultTTL = 30 * time.Second

type entry[T any] struct {
	value     T
	expiresAt time.Time
}

type Cache[T any] struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.RWMutex
	entries map[string]entry[T]
}

type Option[T any] func(*Cache[T])

func WithClock[T any](now func() time.Time) Option[T] {
	return func(c *Cache[T]) { c.now = now }
}

func New[T any](ttl time.Duration, opts ...Option[T]) *Cache[T] {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	c := &Cache[T]{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]entry[T]),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	found, ok := c.entries[key]
	c.mu.RUnlock()

	var zero T
	if !ok {
		return zero, false
	}
	if c.now().After(found.expiresAt) {
		c.mu.Lock()
		if current, still := c.entries[key]; still && c.now().After(current.expiresAt) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return zero, false
	}
	return found.value, true
}

func (c *Cache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry[T]{value: value, expiresAt: c.now().Add(c.ttl)}
}

func (c *Cache[T]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *Cache[T]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]entry[T])
}
