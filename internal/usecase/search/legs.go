// Package search: multi-leg orchestration.
//
// A search is a list of legs — one for a one-way, two for a round trip, more for multi-city — and
// this file runs them. Everything downstream of a single leg lives in search.go and the pipeline
// files beside it; nothing here knows which request shape the caller used.
package search

import (
	"context"
	"sync"

	"github.com/jeremyvw/fareway/internal/model"
)

// legOutcome is one leg's results, or the reason it has none.
type legOutcome struct {
	flights  []model.FlightView
	metadata model.Metadata
	err      error
}

// searchLegs runs every leg concurrently.
//
// Legs are independent — nothing about the return flight depends on which outbound was found — so
// a round trip costs about the same wall-clock time as a one-way rather than twice as much. Each
// leg writes its own slot, so no mutex is needed and the order matches the request.
func (s *Service) searchLegs(ctx context.Context, req model.SearchRequest, active filters) []legOutcome {
	outcomes := make([]legOutcome, len(req.Legs))

	var wg sync.WaitGroup
	for i, leg := range req.Legs {
		wg.Add(1)
		go func(slot int, leg model.Leg) {
			defer wg.Done()
			outcomes[slot] = s.searchLeg(ctx, req, leg, active)
		}(i, leg)
	}
	wg.Wait()

	return outcomes
}

// searchLeg runs the whole pipeline for a single leg.
func (s *Service) searchLeg(ctx context.Context, req model.SearchRequest, leg model.Leg, active filters) legOutcome {
	aggregate, cacheHit, err := s.aggregate(ctx, req, leg)
	if err != nil {
		return legOutcome{
			flights: []model.FlightView{},
			metadata: model.Metadata{
				ProvidersQueried: len(s.clients),
				ProvidersFailed:  len(s.clients),
				SortedBy:         string(req.Sort),
			},
			err: err,
		}
	}

	metadata := model.Metadata{
		ProvidersQueried: len(s.clients),
		CacheHit:         cacheHit,
		ProviderStatus:   aggregate.Statuses,
		SortedBy:         string(req.Sort),
	}
	for _, status := range aggregate.Statuses {
		if status.OK {
			metadata.ProvidersSucceded++
		} else {
			metadata.ProvidersFailed++
		}
	}

	// Filters run once across the merged set rather than per provider, so provider_status keeps
	// reporting "how many this provider had for this route" instead of a number that shifts with
	// whatever the caller filtered on.
	flights, filtered := applyFilters(aggregate.Flights, active)

	// Deduplication before scoring, so a flight sold by several providers is scored once and at
	// the fare a caller would actually pay rather than at whichever offer arrived first.
	flights, merged := dedupeAcrossProviders(flights)

	// Scoring before sorting, because best-value ordering reads the scores. Scores are relative
	// to one leg's result set, which is the only set a caller is choosing within.
	scoreBestValue(flights)

	if err := sortFlights(flights, req.Sort); err != nil {
		return legOutcome{flights: []model.FlightView{}, metadata: metadata, err: err}
	}

	metadata.TotalResults = len(flights)
	metadata.DroppedResults = aggregate.Dropped
	metadata.FilteredResults = filtered
	metadata.MergedDuplicates = merged

	views := make([]model.FlightView, 0, len(flights))
	for _, f := range flights {
		views = append(views, model.NewFlightView(f))
	}
	return legOutcome{flights: views, metadata: metadata}
}

// criteriaFor echoes back what was searched for on one leg.
func criteriaFor(req model.SearchRequest, leg model.Leg) model.Criteria {
	return model.Criteria{
		Origin:        leg.Origin,
		Destination:   leg.Destination,
		DepartureDate: leg.DepartureDate,
		Passengers:    req.Passengers,
		CabinClass:    req.CabinClass,
		TripType:      string(req.TripType()),
	}
}
