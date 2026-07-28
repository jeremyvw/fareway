package search

import (
	"context"
	"fmt"

	"github.com/jeremyvw/fareway/internal/model"
)

// Limiter is the rate limit a throttled client waits on.
//
// Declared here, like the other ports, so the usecase does not depend on the implementation and
// a test can supply a trivial one.
type Limiter interface {
	Wait(ctx context.Context) error
}

// throttled wraps a provider with a rate limit.
//
// A decorator rather than a field on each client: the limit is a property of the agreement with
// a provider, not of how its responses are parsed, and every client would otherwise repeat the
// same five lines. It also means the limit can be added, changed or removed entirely in the
// wiring without touching provider code.
type throttled struct {
	client  FlightClient
	limiter Limiter
}

// Throttle returns a client that waits for the limiter before each call. A nil limiter returns
// the client unchanged, so rate limiting stays optional.
func Throttle(client FlightClient, limiter Limiter) FlightClient {
	if limiter == nil {
		return client
	}
	return &throttled{client: client, limiter: limiter}
}

// Name reports the wrapped provider's name, so metadata is unaffected by the decoration.
func (t *throttled) Name() string { return t.client.Name() }

// FetchFlights waits for an allowance, then delegates.
//
// The wait honours the caller's context, so a queue that would outlive the request budget
// surfaces as this provider timing out — reported in provider_status like any other failure —
// rather than as an unbounded stall. The provider is never called in that case, which is the
// point: the quota is not spent on a request nobody is still waiting for.
func (t *throttled) FetchFlights(ctx context.Context, req model.SearchRequest) ([]model.Flight, error) {
	if err := t.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", t.client.Name(), err)
	}
	return t.client.FetchFlights(ctx, req)
}
