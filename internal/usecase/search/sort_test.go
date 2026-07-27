package search

import (
	"errors"
	"strings"
	"testing"

	"github.com/jeremyvw/fareway/internal/model"
)

func spread(t *testing.T) []model.Flight {
	t.Helper()
	return []model.Flight{
		filterable(t, "cheap-slow", 485000, 1, "2025-12-15T06:00:00+07:00", "2025-12-15T11:20:00+08:00", "QZ"),
		filterable(t, "mid-fast", 950000, 0, "2025-12-15T12:00:00+07:00", "2025-12-15T14:45:00+08:00", "JT"),
		filterable(t, "high-late", 1450000, 0, "2025-12-15T20:00:00+07:00", "2025-12-15T22:45:00+08:00", "GA"),
	}
}

func sortedIDs(t *testing.T, flights []model.Flight, option model.SortOption) []string {
	t.Helper()
	if err := sortFlights(flights, option); err != nil {
		t.Fatalf("sortFlights(%q): %v", option, err)
	}
	return idsOf(flights)
}

func TestSorts(t *testing.T) {
	for option, want := range map[model.SortOption][]string{
		model.SortPriceAsc:      {"cheap-slow", "mid-fast", "high-late"},
		model.SortPriceDesc:     {"high-late", "mid-fast", "cheap-slow"},
		model.SortDurationAsc:   {"mid-fast", "high-late", "cheap-slow"},
		model.SortDurationDesc:  {"cheap-slow", "mid-fast", "high-late"},
		model.SortDepartureTime: {"cheap-slow", "mid-fast", "high-late"},
		model.SortArrivalTime:   {"cheap-slow", "mid-fast", "high-late"},
	} {
		t.Run(string(option), func(t *testing.T) {
			got := sortedIDs(t, spread(t), option)
			if len(got) != len(want) {
				t.Fatalf("order = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("order = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestDurationSortUsesComputedDuration(t *testing.T) {
	flights := spread(t)
	got := sortedIDs(t, flights, model.SortDurationAsc)

	if got[len(got)-1] != "cheap-slow" {
		t.Errorf("order = %v; the 320-minute itinerary must sort last by duration", got)
	}
	if flights[0].TotalMinutes() != 105 {
		t.Errorf("shortest = %d min, want 105", flights[0].TotalMinutes())
	}
}

func TestArrivalSortComparesInstantsNotWallClocks(t *testing.T) {
	got := sortedIDs(t, spread(t), model.SortArrivalTime)

	if got[0] != "cheap-slow" || got[2] != "high-late" {
		t.Errorf("order = %v, want cheap-slow first and high-late last", got)
	}
}

func TestTiesBreakDeterministically(t *testing.T) {
	build := func() []model.Flight {
		return []model.Flight{
			filterable(t, "zebra", 500000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:50:00+08:00", "QZ"),
			filterable(t, "alpha", 500000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:50:00+08:00", "JT"),
		}
	}

	for i := 0; i < 5; i++ {
		got := sortedIDs(t, build(), model.SortPriceAsc)
		if got[0] != "alpha" {
			t.Fatalf("run %d: order = %v, want alpha first on the id tie-break", i, got)
		}
	}
}

func TestTieBreakPrefersPriceThenDeparture(t *testing.T) {
	// Same 105-minute duration, different fares.
	flights := []model.Flight{
		filterable(t, "pricey", 900000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:45:00+08:00", "QZ"),
		filterable(t, "cheap", 500000, 0, "2025-12-15T12:00:00+07:00", "2025-12-15T14:45:00+08:00", "JT"),
	}

	got := sortedIDs(t, flights, model.SortDurationAsc)
	if got[0] != "cheap" {
		t.Errorf("order = %v; equal durations should break on price", got)
	}
}

func TestUnknownSortIsRejected(t *testing.T) {
	err := sortFlights(spread(t), model.SortOption("cheapest"))

	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	// The message must name the valid options, or a caller has to read the source to recover.
	if !strings.Contains(err.Error(), "price_asc") {
		t.Errorf("error does not list the supported sorts: %v", err)
	}
}

func TestEmptySortIsRejected(t *testing.T) {
	if err := sortFlights(spread(t), model.SortOption("")); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestSortOptionsCoversEveryHandledCase(t *testing.T) {
	for _, option := range SortOptions() {
		if err := sortFlights(spread(t), option); err != nil {
			t.Errorf("SortOptions() lists %q but sortFlights rejects it: %v", option, err)
		}
	}
}

func TestEmptyAndSingleInputAreSafe(t *testing.T) {
	var empty []model.Flight
	if err := sortFlights(empty, model.SortPriceAsc); err != nil {
		t.Errorf("sortFlights on nil: %v", err)
	}

	one := []model.Flight{filterable(t, "only", 500000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:50:00+08:00", "QZ")}
	if err := sortFlights(one, model.SortPriceAsc); err != nil {
		t.Errorf("sortFlights on a single element: %v", err)
	}
	if one[0].ID != "only" {
		t.Errorf("single element became %q", one[0].ID)
	}
}
