package main

import (
	"github.com/jeremyvw/fareway/internal/repo/external_client/airasia"
	"github.com/jeremyvw/fareway/internal/repo/external_client/batikair"
	"github.com/jeremyvw/fareway/internal/repo/external_client/garuda"
	"github.com/jeremyvw/fareway/internal/repo/external_client/lionair"
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
	"github.com/jeremyvw/fareway/internal/util/airport"
	"github.com/jeremyvw/fareway/internal/util/ratelimit"
)

// Per-provider request budgets.
//
// Each provider gets its own limiter because quotas are agreed per contract, not pooled: one
// chatty provider must not consume another's allowance. The values here are generous enough that
// a normal search is never delayed — the mocked providers have no real quota to protect — and are
// the single place to change if a provider's terms did.
const (
	providerRatePerSecond = 20
	providerBurst         = 10
)

func buildProviders() []usecase.FlightClient {
	clients := []usecase.FlightClient{
		garuda.New(airport.City),
		lionair.New(airport.City),
		batikair.New(airport.City),
		airasia.New(airport.City),
	}

	throttled := make([]usecase.FlightClient, 0, len(clients))
	for _, client := range clients {
		throttled = append(throttled, usecase.Throttle(client, ratelimit.New(providerRatePerSecond, providerBurst)))
	}
	return throttled
}
