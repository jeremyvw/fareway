package search

import (
	"sort"

	"github.com/jeremyvw/fareway/internal/model"
)

func dedupeAcrossProviders(flights []model.Flight) (kept []model.Flight, merged int) {
	type key struct {
		flightNumber string
		departUnix   int64
	}

	position := make(map[key]int, len(flights))
	kept = make([]model.Flight, 0, len(flights))

	for _, flight := range flights {
		k := key{flightNumber: flight.FlightNumber, departUnix: flight.DepartAt().Unix()}

		at, seen := position[k]
		if !seen {
			position[k] = len(kept)
			kept = append(kept, flight)
			continue
		}

		merged++
		existing := kept[at]

		if flight.Price.Amount < existing.Price.Amount {
			flight.AlternativePrices = append(flight.AlternativePrices, existing.AlternativePrices...)
			flight.AlternativePrices = append(flight.AlternativePrices, model.AlternativePrice{
				Provider: existing.Provider,
				Amount:   existing.Price.Amount,
				Currency: existing.Price.Currency,
			})
			flight.AlternativePrices = sortAlternatives(flight.AlternativePrices)
			kept[at] = flight
			continue
		}

		existing.AlternativePrices = append(existing.AlternativePrices, model.AlternativePrice{
			Provider: flight.Provider,
			Amount:   flight.Price.Amount,
			Currency: flight.Price.Currency,
		})
		existing.AlternativePrices = sortAlternatives(existing.AlternativePrices)
		kept[at] = existing
	}

	return kept, merged
}

func sortAlternatives(alternatives []model.AlternativePrice) []model.AlternativePrice {
	sort.SliceStable(alternatives, func(i, j int) bool {
		if alternatives[i].Amount != alternatives[j].Amount {
			return alternatives[i].Amount < alternatives[j].Amount
		}
		return alternatives[i].Provider < alternatives[j].Provider
	})
	return alternatives
}
