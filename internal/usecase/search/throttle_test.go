package search

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

// throttleClient counts how many times it was actually reached, which is the only way to tell a
// call the limiter blocked from one it merely delayed.
type throttleClient struct {
	name    string
	flights []model.Flight
	calls   atomic.Int64
}

func (c *throttleClient) Name() string { return c.name }

func (c *throttleClient) FetchFlights(context.Context, model.SearchRequest) ([]model.Flight, error) {
	c.calls.Add(1)
	return c.flights, nil
}

// stubLimiter records how it was used and can be made to fail on demand.
type stubLimiter struct {
	waits int
	err   error
	delay time.Duration
}

func (s *stubLimiter) Wait(ctx context.Context) error {
	s.waits++
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
}

func TestThrottleWaitsBeforeEveryCall(t *testing.T) {
	client := &throttleClient{name: "A", flights: []model.Flight{flight(t, "A1", 500000, 10)}}
	limiter := &stubLimiter{}
	wrapped := Throttle(client, limiter)

	for i := 0; i < 3; i++ {
		if _, err := wrapped.FetchFlights(context.Background(), request()); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	if limiter.waits != 3 {
		t.Errorf("limiter consulted %d times, want 3", limiter.waits)
	}
	if got := client.calls.Load(); got != 3 {
		t.Errorf("provider called %d times, want 3", got)
	}
}

// TestThrottleDoesNotSpendTheQuotaOnADeniedCall is the property that makes this worth having: if
// the allowance is unavailable the provider is never contacted, so the quota is not consumed by a
// request that was already going to fail.
func TestThrottleDoesNotSpendTheQuotaOnADeniedCall(t *testing.T) {
	client := &throttleClient{name: "A", flights: []model.Flight{flight(t, "A1", 500000, 10)}}
	limiter := &stubLimiter{err: errors.New("no allowance")}
	wrapped := Throttle(client, limiter)

	if _, err := wrapped.FetchFlights(context.Background(), request()); err == nil {
		t.Fatal("expected an error when the limiter denies the call")
	}
	if got := client.calls.Load(); got != 0 {
		t.Errorf("provider called %d times despite being throttled", got)
	}
}

// TestThrottleErrorNamesTheProvider keeps provider_status readable: an unattributed "no allowance"
// tells a caller nothing about which upstream was throttled.
func TestThrottleErrorNamesTheProvider(t *testing.T) {
	wrapped := Throttle(
		&throttleClient{name: "Batik Air"},
		&stubLimiter{err: errors.New("no allowance")},
	)

	_, err := wrapped.FetchFlights(context.Background(), request())
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !strings.Contains(got, "Batik Air") {
		t.Errorf("error = %q, want it to name the provider", got)
	}
}

func TestThrottlePreservesTheProviderName(t *testing.T) {
	wrapped := Throttle(&throttleClient{name: "Lion Air"}, &stubLimiter{})
	if got := wrapped.Name(); got != "Lion Air" {
		t.Errorf("Name() = %q, want Lion Air; metadata would be wrong", got)
	}
}

// TestThrottleIsOptional keeps rate limiting a wiring decision rather than a requirement.
func TestThrottleIsOptional(t *testing.T) {
	client := &throttleClient{name: "A", flights: []model.Flight{flight(t, "A1", 500000, 10)}}

	wrapped := Throttle(client, nil)
	if wrapped != FlightClient(client) {
		t.Error("Throttle with a nil limiter should return the client unchanged")
	}
	if _, err := wrapped.FetchFlights(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	if got := client.calls.Load(); got != 1 {
		t.Errorf("provider called %d times, want 1", got)
	}
}

// TestThrottledProviderSurfacesAsAFailedProvider checks the whole path: a client stuck behind its
// rate limit past the request budget is reported like any other provider failure, and the search
// still returns the others.
func TestThrottledProviderSurfacesAsAFailedProvider(t *testing.T) {
	fast := &throttleClient{name: "Fast", flights: []model.Flight{flight(t, "F1", 500000, 10)}}
	slow := &throttleClient{name: "Queued", flights: []model.Flight{flight(t, "Q1", 100000, 10)}}

	service := New(
		Config{ProviderTimeout: 30 * time.Millisecond, OverallTimeout: time.Second},
		nil,
		Throttle(fast, &stubLimiter{}),
		Throttle(slow, &stubLimiter{delay: 5 * time.Second}),
	)

	got, err := service.Search(context.Background(), request())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Metadata.ProvidersFailed != 1 || got.Metadata.ProvidersSucceded != 1 {
		t.Errorf("metadata = %+v, want one failed and one succeeded", got.Metadata)
	}
	if got.Metadata.TotalResults != 1 {
		t.Errorf("total_results = %d, want only the unthrottled provider's flight", got.Metadata.TotalResults)
	}
	// The queued provider must never have been contacted.
	if calls := slow.calls.Load(); calls != 0 {
		t.Errorf("throttled provider was called %d times", calls)
	}
}
