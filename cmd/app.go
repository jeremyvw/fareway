// Command fareway serves the flight search API.
//
// This package is a composition root only: it builds the object graph bottom-up and starts
// the server. All behaviour lives under internal/.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultAddr     = ":8080"
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 10 * time.Second
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	providers := buildProviders()
	service := buildSearchUsecase(providers, buildSearchCache())
	searchHandler := buildSearchHandler(service, log)

	addr := defaultAddr
	if fromEnv := os.Getenv("FAREWAY_ADDR"); fromEnv != "" {
		addr = fromEnv
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      routes(searchHandler),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", addr), slog.Int("providers", len(providers)))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Error("server failed", slog.String("error", err.Error()))
		os.Exit(1)
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
