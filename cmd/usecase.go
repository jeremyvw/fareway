package main

import (
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
)

func buildSearchUsecase(providers []usecase.FlightClient) *usecase.Service {
	return usecase.New(usecase.DefaultConfig(), providers...)
}
