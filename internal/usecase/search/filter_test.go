package search

import (
	"errors"
	"testing"

	"github.com/jeremyvw/fareway/internal/model"
)

// filterable builds a flight with the attributes the filters act on.
func filterable(t *testing.T, id string, price int64, stops int, depart, arrive string, code string) model.Flight {
	t.Helper()
	f := model.Flight{
		ID:           id,
		Provider:     "Test",
		Airline:      model.Airline{Name: "Test Air", Code: code},
		FlightNumber: id,
		Segments: []model.Segment{{
			FlightNumber: id,
			From:         model.Place{Airport: "CGK", City: "Jakarta"},
			To:           model.Place{Airport: "DPS", City: "Denpasar"},
			Depart:       at(t, depart),
			Arrive:       at(t, arrive),
		}},
		Price:          model.Money{Amount: price, Currency: "IDR"},
		AvailableSeats: 10,
		CabinClass:     "economy",
		Amenities:      []string{},
	}
	for i := 0; i < stops; i++ {
		f.Stopovers = append(f.Stopovers, model.Stopover{
			Airport:     model.Place{Airport: "SUB", City: "Surabaya"},
			WaitMinutes: 60,
		})
	}
	return f
}

// sample is a small spread across price, stops, time of day and carrier.
func sample(t *testing.T) []model.Flight {
	t.Helper()
	return []model.Flight{
		// cheap, 1 stop, early, long
		filterable(t, "cheap-stop", 485000, 1, "2025-12-15T06:00:00+07:00", "2025-12-15T11:20:00+08:00", "QZ"),
		// mid, direct, midday, short
		filterable(t, "mid-direct", 950000, 0, "2025-12-15T12:00:00+07:00", "2025-12-15T14:45:00+08:00", "JT"),
		// pricey, direct, late, short
		filterable(t, "late-direct", 1450000, 0, "2025-12-15T20:00:00+07:00", "2025-12-15T22:45:00+08:00", "GA"),
	}
}

func idsOf(flights []model.Flight) []string {
	out := make([]string, 0, len(flights))
	for _, f := range flights {
		out = append(out, f.ID)
	}
	return out
}

func compile(t *testing.T, f model.Filters) filters {
	t.Helper()
	compiled, err := compileFilters(f)
	if err != nil {
		t.Fatalf("compileFilters: %v", err)
	}
	return compiled
}

func TestNoFiltersKeepsEverything(t *testing.T) {
	in := sample(t)
	kept, removed := applyFilters(in, compile(t, model.Filters{}))

	if len(kept) != len(in) || removed != 0 {
		t.Errorf("kept %v, removed %d; want everything", idsOf(kept), removed)
	}
}

func TestFilters(t *testing.T) {
	direct := 0
	oneStop := 1

	for name, tc := range map[string]struct {
		filters model.Filters
		want    []string
	}{
		"max price": {
			filters: model.Filters{MaxPrice: 1000000},
			want:    []string{"cheap-stop", "mid-direct"},
		},
		"min price": {
			filters: model.Filters{MinPrice: 900000},
			want:    []string{"mid-direct", "late-direct"},
		},
		"price band": {
			filters: model.Filters{MinPrice: 500000, MaxPrice: 1000000},
			want:    []string{"mid-direct"},
		},
		"direct only": {
			filters: model.Filters{MaxStops: &direct},
			want:    []string{"mid-direct", "late-direct"},
		},
		"up to one stop keeps all": {
			filters: model.Filters{MaxStops: &oneStop},
			want:    []string{"cheap-stop", "mid-direct", "late-direct"},
		},
		"departs after": {
			filters: model.Filters{DepartureAfter: "10:00"},
			want:    []string{"mid-direct", "late-direct"},
		},
		"departs before": {
			filters: model.Filters{DepartureBefore: "13:00"},
			want:    []string{"cheap-stop", "mid-direct"},
		},
		"arrives before": {
			filters: model.Filters{ArrivalBefore: "15:00"},
			want:    []string{"cheap-stop", "mid-direct"},
		},
		"arrives after": {
			filters: model.Filters{ArrivalAfter: "20:00"},
			want:    []string{"late-direct"},
		},
		"max duration": {
			filters: model.Filters{MaxDurationMinutes: 180},
			want:    []string{"mid-direct", "late-direct"},
		},
		"airline by code": {
			filters: model.Filters{Airlines: []string{"QZ", "GA"}},
			want:    []string{"cheap-stop", "late-direct"},
		},
		"airline code is case insensitive": {
			filters: model.Filters{Airlines: []string{"qz"}},
			want:    []string{"cheap-stop"},
		},
		"airline by name": {
			// A caller filtering by carrier name rather than IATA code is asking a
			// reasonable question and should not get an empty list.
			filters: model.Filters{Airlines: []string{"Test Air"}},
			want:    []string{"cheap-stop", "mid-direct", "late-direct"},
		},
		"combined filters are ANDed": {
			filters: model.Filters{MaxStops: &direct, DepartureBefore: "13:00"},
			want:    []string{"mid-direct"},
		},
		"impossible combination": {
			filters: model.Filters{MaxStops: &direct, MaxPrice: 100000},
			want:    []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			kept, removed := applyFilters(sample(t), compile(t, tc.filters))

			got := idsOf(kept)
			if len(got) != len(tc.want) {
				t.Fatalf("kept %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("kept %v, want %v", got, tc.want)
				}
			}
			if want := len(sample(t)) - len(tc.want); removed != want {
				t.Errorf("removed = %d, want %d", removed, want)
			}
		})
	}
}

// TestTimeFiltersUseLocalWallClock pins the decision behind the arrival bound: an arrival at
// 22:45+08:00 is late in the evening to a passenger, even though the same instant is 14:45 UTC.
func TestTimeFiltersUseLocalWallClock(t *testing.T) {
	kept, _ := applyFilters(sample(t), compile(t, model.Filters{ArrivalAfter: "22:00"}))

	if got := idsOf(kept); len(got) != 1 || got[0] != "late-direct" {
		t.Errorf("kept %v, want [late-direct]; the bound was compared against UTC", got)
	}
}

func TestFilterValidation(t *testing.T) {
	negative := -1

	for name, f := range map[string]model.Filters{
		"min above max":       {MinPrice: 900000, MaxPrice: 100000},
		"negative price":      {MinPrice: -1},
		"negative stops":      {MaxStops: &negative},
		"negative duration":   {MaxDurationMinutes: -30},
		"malformed depart at": {DepartureAfter: "9am"},
		"malformed arrive by": {ArrivalBefore: "25:00"},
		"malformed depart by": {DepartureBefore: "noon"},
		"malformed arrive at": {ArrivalAfter: "07"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := compileFilters(f); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestEmptyReportsWhetherAnyFilterWasRequested(t *testing.T) {
	if !compile(t, model.Filters{}).empty() {
		t.Error("empty() = false for a zero-value Filters")
	}

	direct := 0
	for name, f := range map[string]model.Filters{
		"price":    {MaxPrice: 1},
		"stops":    {MaxStops: &direct},
		"time":     {DepartureAfter: "09:00"},
		"airlines": {Airlines: []string{"QZ"}},
		"duration": {MaxDurationMinutes: 120},
	} {
		t.Run(name, func(t *testing.T) {
			if compile(t, f).empty() {
				t.Error("empty() = true despite a filter being set")
			}
		})
	}
}
