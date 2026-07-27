package main

import (
	"log/slog"

	handler "github.com/jeremyvw/fareway/internal/handler/search"
	usecase "github.com/jeremyvw/fareway/internal/usecase/search"
)

func buildSearchHandler(service *usecase.Service, log *slog.Logger) *handler.Handler {
	return handler.New(service, log)
}
