package search

import (
	"fmt"
	"testing"

	"github.com/jeremyvw/fareway/internal/model"
)

func TestProbeFilters(t *testing.T) {
	direct := 0

	for _, tc := range []struct {
		label   string
		filters model.Filters
	}{
		{"no filters", model.Filters{}},
		{"under 1,000,000", model.Filters{MaxPrice: 1000000}},
		{"direct only", model.Filters{MaxStops: &direct}},
		{"depart after 10:00", model.Filters{DepartureAfter: "10:00"}},
		{"arrive before 15:00", model.Filters{ArrivalBefore: "15:00"}},
		{"under 3h", model.Filters{MaxDurationMinutes: 180}},
		{"QZ only", model.Filters{Airlines: []string{"qz"}}},
		{"direct AND before 13:00", model.Filters{MaxStops: &direct, DepartureBefore: "13:00"}},
	} {
		compiled, err := compileFilters(tc.filters)
		if err != nil {
			t.Fatalf("%s: %v", tc.label, err)
		}
		kept, removed := applyFilters(sample(t), compiled)
		fmt.Printf("%-26s kept=%v removed=%d\n", tc.label, idsOf(kept), removed)
	}
}
