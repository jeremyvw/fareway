package main

import (
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
	"github.com/jeremyvw/fareway/internal/util/cache"
)

func buildSearchCache() usecase.Cache {
	return cache.New[usecase.Aggregate](cache.DefaultTTL)
}

func buildSearchUsecase(providers []usecase.FlightClient, store usecase.Cache) *usecase.Service {
	return usecase.New(usecase.DefaultConfig(), store, providers...)
}
