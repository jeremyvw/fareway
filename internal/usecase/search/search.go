package search

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

type FlightClient interface {
	Name() string
	FetchFlights(ctx context.Context, req model.SearchRequest) ([]model.Flight, error)
}

type Cache interface {
	Get(key string) (Aggregate, bool)
	Set(key string, value Aggregate)
}

// Aggregate is everything the fan-out produced, before any request-specific filtering.
type Aggregate struct {
	Flights  []model.Flight
	Statuses []model.ProviderStatus
	Dropped  int
}

var ErrAllProvidersFailed = errors.New("all providers failed")

type Config struct {
	ProviderTimeout time.Duration

	OverallTimeout time.Duration
}

const (
	defaultProviderTimeout = 800 * time.Millisecond
	defaultOverallTimeout  = 2 * time.Second
)

func DefaultConfig() Config {
	return Config{
		ProviderTimeout: defaultProviderTimeout,
		OverallTimeout:  defaultOverallTimeout,
	}
}

type Service struct {
	clients []FlightClient
	cfg     Config
	cache   Cache
}

func New(cfg Config, cache Cache, clients ...FlightClient) *Service {
	if cfg.ProviderTimeout <= 0 || cfg.OverallTimeout <= 0 {
		cfg = DefaultConfig()
	}
	return &Service{clients: clients, cfg: cfg, cache: cache}
}

type providerResult struct {
	provider string
	flights  []model.Flight
	err      error
	duration time.Duration
}

// Search runs every leg of the request and returns the results.
func (s *Service) Search(ctx context.Context, req model.SearchRequest) (model.SearchResponse, error) {
	if req.HasBothForms() {
		return model.SearchResponse{}, fmt.Errorf(
			"%w: send either origin/destination/departureDate or a legs array, not both", ErrInvalidRequest)
	}

	req.Normalize()
	if err := validateRequest(req); err != nil {
		return model.SearchResponse{}, err
	}
	activeFilters, err := compileFilters(req.Filters)
	if err != nil {
		return model.SearchResponse{}, err
	}

	started := time.Now()
	outcomes := s.searchLegs(ctx, req, activeFilters)

	succeeded := 0
	for _, o := range outcomes {
		if o.err == nil {
			succeeded++
		}
	}
	if succeeded == 0 {
		return model.SearchResponse{}, ErrAllProvidersFailed
	}

	response := model.SearchResponse{
		SearchCriteria: criteriaFor(req, req.Legs[0]),
		Flights:        []model.FlightView{},
	}

	if len(outcomes) == 1 {
		response.Metadata = outcomes[0].metadata
		response.Flights = outcomes[0].flights
		response.Metadata.SearchTimeMS = time.Since(started).Milliseconds()
		return response, nil
	}

	summary := model.Metadata{
		CacheHit: true,
		SortedBy: string(req.Sort),
	}
	for i, o := range outcomes {
		response.Legs = append(response.Legs, model.LegResult{
			Leg:            i + 1,
			SearchCriteria: criteriaFor(req, req.Legs[i]),
			Metadata:       o.metadata,
			Flights:        o.flights,
		})

		summary.TotalResults += o.metadata.TotalResults
		summary.ProvidersQueried += o.metadata.ProvidersQueried
		summary.ProvidersSucceded += o.metadata.ProvidersSucceded
		summary.ProvidersFailed += o.metadata.ProvidersFailed
		summary.DroppedResults += o.metadata.DroppedResults
		summary.FilteredResults += o.metadata.FilteredResults
		summary.MergedDuplicates += o.metadata.MergedDuplicates

		// The search as a whole only avoided the providers if every leg did.
		if !o.metadata.CacheHit {
			summary.CacheHit = false
		}
	}
	summary.SearchTimeMS = time.Since(started).Milliseconds()
	response.Metadata = summary
	return response, nil
}

func (s *Service) aggregate(ctx context.Context, req model.SearchRequest, leg model.Leg) (Aggregate, bool, error) {
	key := cacheKey(req, leg)

	if s.cache != nil {
		if cached, ok := s.cache.Get(key); ok {
			return cached, true, nil
		}
	}

	results := s.fanOut(ctx, req)

	var (
		aggregate Aggregate
		succeeded int
	)
	for _, r := range results {
		status := model.ProviderStatus{
			Provider:   r.provider,
			OK:         r.err == nil,
			DurationMS: r.duration.Milliseconds(),
		}
		if r.err != nil {
			status.Error = r.err.Error()
		} else {
			succeeded++
			kept, skipped := s.accept(req, leg, r.flights)
			status.Results = len(kept)
			aggregate.Dropped += skipped
			aggregate.Flights = append(aggregate.Flights, kept...)
		}
		aggregate.Statuses = append(aggregate.Statuses, status)
	}

	if succeeded == 0 && len(s.clients) > 0 {
		return Aggregate{}, false, ErrAllProvidersFailed
	}

	if s.cache != nil {
		s.cache.Set(key, aggregate)
	}
	return aggregate, false, nil
}

func cacheKey(req model.SearchRequest, leg model.Leg) string {
	return strings.Join([]string{
		leg.Origin,
		leg.Destination,
		leg.DepartureDate,
		req.CabinClass,
		strconv.Itoa(req.Passengers),
	}, "|")
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

func (s *Service) accept(req model.SearchRequest, leg model.Leg, flights []model.Flight) (kept []model.Flight, dropped int) {
	kept = make([]model.Flight, 0, len(flights))
	for _, f := range flights {
		switch {
		case f.Validate() != nil:
		case !f.MatchesRoute(leg.Origin, leg.Destination):
		case !f.DepartsOn(leg.DepartureDate):
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
	if len(req.Legs) == 0 {
		return fmt.Errorf("%w: supply origin, destination and departureDate, or a legs array", ErrInvalidRequest)
	}
	if len(req.Legs) > model.MaxLegs {
		return fmt.Errorf("%w: %d legs requested, the maximum is %d",
			ErrInvalidRequest, len(req.Legs), model.MaxLegs)
	}

	var previous time.Time
	for i, leg := range req.Legs {
		if len(leg.Origin) != 3 {
			return fmt.Errorf("%w: leg %d origin must be a 3-letter IATA code, got %q",
				ErrInvalidRequest, i+1, leg.Origin)
		}
		if len(leg.Destination) != 3 {
			return fmt.Errorf("%w: leg %d destination must be a 3-letter IATA code, got %q",
				ErrInvalidRequest, i+1, leg.Destination)
		}
		if leg.Origin == leg.Destination {
			return fmt.Errorf("%w: leg %d departs and arrives at %s",
				ErrInvalidRequest, i+1, leg.Origin)
		}
		if leg.DepartureDate == "" {
			return fmt.Errorf("%w: leg %d departureDate is required (YYYY-MM-DD)",
				ErrInvalidRequest, i+1)
		}

		departs, err := time.Parse("2006-01-02", leg.DepartureDate)
		if err != nil {
			return fmt.Errorf("%w: leg %d departureDate %q must be YYYY-MM-DD",
				ErrInvalidRequest, i+1, leg.DepartureDate)
		}
		if i > 0 && departs.Before(previous) {
			return fmt.Errorf("%w: leg %d departs %s, before leg %d on %s",
				ErrInvalidRequest, i+1, leg.DepartureDate, i, req.Legs[i-1].DepartureDate)
		}
		previous = departs
	}

	return nil
}

var ErrInvalidRequest = errors.New("invalid search request")
