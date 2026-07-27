// Package search aggregates flight results from every configured provider.
package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

type FlightClient interface {
	Name() string
	FetchFlights(ctx context.Context, req model.SearchRequest) ([]model.Flight, error)
}

var ErrAllProvidersFailed = errors.New("all providers failed")

type Config struct {
	ProviderTimeout time.Duration

	OverallTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		ProviderTimeout: 800 * time.Millisecond,
		OverallTimeout:  2 * time.Second,
	}
}

type Service struct {
	clients []FlightClient
	cfg     Config
}

func New(cfg Config, clients ...FlightClient) *Service {
	if cfg.ProviderTimeout <= 0 || cfg.OverallTimeout <= 0 {
		cfg = DefaultConfig()
	}
	return &Service{clients: clients, cfg: cfg}
}

type providerResult struct {
	provider string
	flights  []model.Flight
	err      error
	duration time.Duration
}

func (s *Service) Search(ctx context.Context, req model.SearchRequest) (model.SearchResponse, error) {
	req.Normalize()
	if err := validateRequest(req); err != nil {
		return model.SearchResponse{}, err
	}

	activeFilters, err := compileFilters(req.Filters)
	if err != nil {
		return model.SearchResponse{}, err
	}

	started := time.Now()
	results := s.fanOut(ctx, req)
	elapsed := time.Since(started)

	response := model.SearchResponse{
		SearchCriteria: model.Criteria{
			Origin:        req.Origin,
			Destination:   req.Destination,
			DepartureDate: req.DepartureDate,
			Passengers:    req.Passengers,
			CabinClass:    req.CabinClass,
		},
		Metadata: model.Metadata{
			ProvidersQueried: len(s.clients),
			SearchTimeMS:     elapsed.Milliseconds(),
			CacheHit:         false,
		},
	}

	var (
		flights []model.Flight
		dropped int
	)
	for _, r := range results {
		status := model.ProviderStatus{
			Provider:   r.provider,
			OK:         r.err == nil,
			DurationMS: r.duration.Milliseconds(),
		}
		if r.err != nil {
			status.Error = r.err.Error()
			response.Metadata.ProvidersFailed++
		} else {
			response.Metadata.ProvidersSucceded++
			kept, skipped := s.accept(req, r.flights)
			status.Results = len(kept)
			dropped += skipped
			flights = append(flights, kept...)
		}
		response.Metadata.ProviderStatus = append(response.Metadata.ProviderStatus, status)
	}

	if response.Metadata.ProvidersSucceded == 0 && len(s.clients) > 0 {
		return model.SearchResponse{}, ErrAllProvidersFailed
	}

	flights, filtered := applyFilters(flights, activeFilters)

	// Sorting last, on the smallest set: filtering first means fewer comparisons.
	if err := sortFlights(flights, req.Sort); err != nil {
		return model.SearchResponse{}, err
	}

	response.Metadata.TotalResults = len(flights)
	response.Metadata.DroppedResults = dropped
	response.Metadata.FilteredResults = filtered
	response.Metadata.SortedBy = string(req.Sort)
	response.Flights = make([]model.FlightView, 0, len(flights))
	for _, f := range flights {
		response.Flights = append(response.Flights, model.NewFlightView(f))
	}
	return response, nil
}

func (s *Service) fanOut(ctx context.Context, req model.SearchRequest) []providerResult {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.OverallTimeout)
	defer cancel()

	results := make([]providerResult, len(s.clients))

	var wg sync.WaitGroup
	for i, client := range s.clients {
		wg.Add(1)
		go func(slot int, c FlightClient) {
			defer wg.Done()

			providerCtx, providerCancel := context.WithTimeout(ctx, s.cfg.ProviderTimeout)
			defer providerCancel()

			start := time.Now()
			flights, err := c.FetchFlights(providerCtx, req)
			results[slot] = providerResult{
				provider: c.Name(),
				flights:  flights,
				err:      err,
				duration: time.Since(start),
			}
		}(i, client)
	}
	wg.Wait()

	return results
}

func (s *Service) accept(req model.SearchRequest, flights []model.Flight) (kept []model.Flight, dropped int) {
	kept = make([]model.Flight, 0, len(flights))
	for _, f := range flights {
		switch {
		case f.Validate() != nil:
		case !f.MatchesRoute(req.Origin, req.Destination):
		case !f.DepartsOn(req.DepartureDate):
		case req.CabinClass != "" && f.CabinClass != req.CabinClass:
		case f.AvailableSeats < req.Passengers:
		default:
			kept = append(kept, f)
			continue
		}
		dropped++
	}
	return kept, dropped
}

func validateRequest(req model.SearchRequest) error {
	if len(req.Origin) != 3 {
		return fmt.Errorf("%w: origin must be a 3-letter IATA code, got %q", ErrInvalidRequest, req.Origin)
	}
	if len(req.Destination) != 3 {
		return fmt.Errorf("%w: destination must be a 3-letter IATA code, got %q", ErrInvalidRequest, req.Destination)
	}
	if req.Origin == req.Destination {
		return fmt.Errorf("%w: origin and destination are both %s", ErrInvalidRequest, req.Origin)
	}
	if req.DepartureDate == "" {
		return fmt.Errorf("%w: departureDate is required (YYYY-MM-DD)", ErrInvalidRequest)
	}
	if _, err := time.Parse("2006-01-02", req.DepartureDate); err != nil {
		return fmt.Errorf("%w: departureDate %q must be YYYY-MM-DD", ErrInvalidRequest, req.DepartureDate)
	}
	if req.ReturnDate != nil && *req.ReturnDate != "" {
		return fmt.Errorf("%w: round-trip search is not supported", ErrInvalidRequest)
	}
	return nil
}

var ErrInvalidRequest = errors.New("invalid search request")
