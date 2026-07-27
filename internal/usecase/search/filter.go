package search

import (
	"fmt"
	"strings"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

type filters struct {
	minPrice int64
	maxPrice int64

	maxStops *int

	departAfter  *int
	departBefore *int
	arriveAfter  *int
	arriveBefore *int

	airlines map[string]bool

	maxDuration int
}

func compileFilters(f model.Filters) (filters, error) {
	out := filters{
		minPrice:    f.MinPrice,
		maxPrice:    f.MaxPrice,
		maxStops:    f.MaxStops,
		maxDuration: f.MaxDurationMinutes,
	}

	if f.MinPrice < 0 || f.MaxPrice < 0 {
		return filters{}, fmt.Errorf("%w: prices cannot be negative", ErrInvalidRequest)
	}
	if f.MaxPrice > 0 && f.MinPrice > f.MaxPrice {
		return filters{}, fmt.Errorf("%w: min_price %d exceeds max_price %d",
			ErrInvalidRequest, f.MinPrice, f.MaxPrice)
	}
	if f.MaxStops != nil && *f.MaxStops < 0 {
		return filters{}, fmt.Errorf("%w: max_stops cannot be negative", ErrInvalidRequest)
	}
	if f.MaxDurationMinutes < 0 {
		return filters{}, fmt.Errorf("%w: max_duration_minutes cannot be negative", ErrInvalidRequest)
	}

	for _, bound := range []struct {
		name  string
		value string
		into  **int
	}{
		{"departure_after", f.DepartureAfter, &out.departAfter},
		{"departure_before", f.DepartureBefore, &out.departBefore},
		{"arrival_after", f.ArrivalAfter, &out.arriveAfter},
		{"arrival_before", f.ArrivalBefore, &out.arriveBefore},
	} {
		if strings.TrimSpace(bound.value) == "" {
			continue
		}
		minutes, err := timeutil.ParseClock(bound.value)
		if err != nil {
			return filters{}, fmt.Errorf("%w: %s must be HH:MM, got %q",
				ErrInvalidRequest, bound.name, bound.value)
		}
		parsed := minutes
		*bound.into = &parsed
	}

	if len(f.Airlines) > 0 {
		out.airlines = make(map[string]bool, len(f.Airlines))
		for _, a := range f.Airlines {
			if trimmed := strings.ToUpper(strings.TrimSpace(a)); trimmed != "" {
				out.airlines[trimmed] = true
			}
		}
	}

	return out, nil
}

func (f filters) empty() bool {
	return f.minPrice == 0 && f.maxPrice == 0 && f.maxStops == nil &&
		f.departAfter == nil && f.departBefore == nil &&
		f.arriveAfter == nil && f.arriveBefore == nil &&
		len(f.airlines) == 0 && f.maxDuration == 0
}

func (f filters) keep(flight model.Flight) bool {
	if f.minPrice > 0 && flight.Price.Amount < f.minPrice {
		return false
	}
	if f.maxPrice > 0 && flight.Price.Amount > f.maxPrice {
		return false
	}
	if f.maxStops != nil && flight.Stops() > *f.maxStops {
		return false
	}
	if f.maxDuration > 0 && flight.TotalMinutes() > f.maxDuration {
		return false
	}

	depart := timeutil.MinutesSinceMidnight(flight.DepartAt())
	if f.departAfter != nil && depart < *f.departAfter {
		return false
	}
	if f.departBefore != nil && depart > *f.departBefore {
		return false
	}

	arrive := timeutil.MinutesSinceMidnight(flight.ArriveAt())
	if f.arriveAfter != nil && arrive < *f.arriveAfter {
		return false
	}
	if f.arriveBefore != nil && arrive > *f.arriveBefore {
		return false
	}

	if len(f.airlines) > 0 && !f.matchesAirline(flight) {
		return false
	}
	return true
}

// matchesAirline accepts either an IATA code or a carrier name, since a caller filtering by
// "AirAsia" rather than "QZ" is asking a reasonable question and should not get an empty list.
func (f filters) matchesAirline(flight model.Flight) bool {
	return f.airlines[strings.ToUpper(flight.Airline.Code)] ||
		f.airlines[strings.ToUpper(flight.Airline.Name)]
}

func applyFilters(flights []model.Flight, f filters) (kept []model.Flight, removed int) {
	if f.empty() {
		return flights, 0
	}
	kept = make([]model.Flight, 0, len(flights))
	for _, flight := range flights {
		if f.keep(flight) {
			kept = append(kept, flight)
			continue
		}
		removed++
	}
	return kept, removed
}
