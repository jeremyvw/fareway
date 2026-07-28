package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jeremyvw/fareway/internal/model"
)

func sortFlights(flights []model.Flight, option model.SortOption) error {
	less, err := comparator(option)
	if err != nil {
		return err
	}

	sort.SliceStable(flights, func(i, j int) bool {
		a, b := flights[i], flights[j]
		if less(a, b) {
			return true
		}
		if less(b, a) {
			return false
		}
		return tieBreak(a, b)
	})
	return nil
}

func comparator(option model.SortOption) (func(a, b model.Flight) bool, error) {
	switch option {
	case model.SortBestValue:
		return func(a, b model.Flight) bool { return a.BestValueScore > b.BestValueScore }, nil
	case model.SortPriceAsc:
		return func(a, b model.Flight) bool { return a.Price.Amount < b.Price.Amount }, nil
	case model.SortPriceDesc:
		return func(a, b model.Flight) bool { return a.Price.Amount > b.Price.Amount }, nil
	case model.SortDurationAsc:
		return func(a, b model.Flight) bool { return a.TotalMinutes() < b.TotalMinutes() }, nil
	case model.SortDurationDesc:
		return func(a, b model.Flight) bool { return a.TotalMinutes() > b.TotalMinutes() }, nil
	case model.SortDepartureTime:
		return func(a, b model.Flight) bool { return a.DepartAt().Before(b.DepartAt()) }, nil
	case model.SortArrivalTime:
		return func(a, b model.Flight) bool { return a.ArriveAt().Before(b.ArriveAt()) }, nil
	default:
		return nil, fmt.Errorf("%w: unknown sort %q (supported: %s)",
			ErrInvalidRequest, option, strings.Join(sortOptionNames(), ", "))
	}
}

// tie breaker but prioritize price
func tieBreak(a, b model.Flight) bool {
	if a.Price.Amount != b.Price.Amount {
		return a.Price.Amount < b.Price.Amount
	}
	if !a.DepartAt().Equal(b.DepartAt()) {
		return a.DepartAt().Before(b.DepartAt())
	}
	return a.ID < b.ID
}

func SortOptions() []model.SortOption {
	return []model.SortOption{
		model.SortPriceAsc,
		model.SortPriceDesc,
		model.SortDurationAsc,
		model.SortDurationDesc,
		model.SortDepartureTime,
		model.SortArrivalTime,
	}
}

func sortOptionNames() []string {
	options := SortOptions()
	names := make([]string, 0, len(options))
	for _, option := range options {
		names = append(names, string(option))
	}
	return names
}
