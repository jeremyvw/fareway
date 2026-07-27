package main

import (
	"net/http"

	handler "github.com/jeremyvw/fareway/internal/handler/search"
)

func routes(search *handler.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/flights/search", search.Search)
	mux.HandleFunc("GET /health", search.Health)
	return mux
}
