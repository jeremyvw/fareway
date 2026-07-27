package main

import (
	"github.com/jeremyvw/fareway/internal/repo/external_client/airasia"
	"github.com/jeremyvw/fareway/internal/repo/external_client/batikair"
	"github.com/jeremyvw/fareway/internal/repo/external_client/garuda"
	"github.com/jeremyvw/fareway/internal/repo/external_client/lionair"
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
	"github.com/jeremyvw/fareway/internal/util/airport"
)

func buildProviders() []usecase.FlightClient {
	return []usecase.FlightClient{
		garuda.New(airport.City),
		lionair.New(airport.City),
		batikair.New(airport.City),
		airasia.New(airport.City),
		// airasia.New(airport.City, airasia.WithFailureRate(1)),
	}
}
