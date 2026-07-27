package garuda

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

// testCities stands in for the airport lookup. Garuda omits cities inside segments, so
// DPS as the final stop of GA315 can only be named from here.
func testCities(iata string) (string, bool) {
	cities := map[string]string{
		"CGK": "Jakarta",
		"DPS": "Denpasar",
		"SUB": "Surabaya",
	}
	city, ok := cities[iata]
	return city, ok
}

func fetch(t *testing.T) []model.Flight {
	t.Helper()
	client := New(testCities, WithoutDelay())
	flights, err := client.FetchFlights(context.Background(), model.SearchRequest{})
	if err != nil {
		t.Fatalf("FetchFlights: %v", err)
	}
	return flights
}

func byID(t *testing.T, flights []model.Flight, number string) model.Flight {
	t.Helper()
	for _, f := range flights {
		if f.FlightNumber == number {
			return f
		}
	}
	t.Fatalf("flight %s not found in %d results", number, len(flights))
	return model.Flight{}
}

func TestFetchFlightsNormalizesEveryRecord(t *testing.T) {
	flights := fetch(t)
	if len(flights) != 3 {
		t.Fatalf("got %d flights, want 3", len(flights))
	}
}

// TestGA315ResolvesEffectiveItinerary is the case the whole design exists for: Garuda
// labels this connecting itinerary with its first leg's arrival airport, stop count and
// duration. Reading those fields at face value yields a CGK->SUB flight that a search for
// CGK->DPS would silently discard.
func TestGA315ResolvesEffectiveItinerary(t *testing.T) {
	f := byID(t, fetch(t), "GA315")

	if got := f.Destination().Airport; got != "DPS" {
		t.Errorf("destination = %q, want DPS (not the top-level SUB)", got)
	}
	if got := f.Destination().City; got != "Denpasar" {
		t.Errorf("destination city = %q, want Denpasar", got)
	}
	if got := f.Origin().Airport; got != "CGK" {
		t.Errorf("origin = %q, want CGK", got)
	}
	if got := f.Stops(); got != 1 {
		t.Errorf("stops = %d, want 1 (provider declared 0)", got)
	}
	if got := f.TotalMinutes(); got != 225 {
		t.Errorf("total duration = %d min, want 225 (provider declared 90)", got)
	}
	if got := f.LayoverMinutes(); got != 105 {
		t.Errorf("layover = %d min, want 105", got)
	}
	if got := f.MatchesRoute("CGK", "DPS"); !got {
		t.Error("MatchesRoute(CGK, DPS) = false, want true")
	}
	if got := f.DepartsOn("2025-12-15"); !got {
		t.Error("DepartsOn(2025-12-15) = false, want true")
	}
	if f.IsDirect() {
		t.Error("IsDirect() = true, want false")
	}
}

// TestGA315RecordsDeclaredDurationConflict pins the warning path to real provider data:
// leg two claims 90 minutes while its own timestamps span 30, and the declared stop count
// is wrong. Both are reported, neither is fatal.
func TestGA315RecordsDeclaredDurationConflict(t *testing.T) {
	f := byID(t, fetch(t), "GA315")

	if len(f.Warnings) == 0 {
		t.Fatal("expected normalization warnings, got none")
	}
	joined := strings.Join(f.Warnings, "; ")
	if !strings.Contains(joined, "GA332") {
		t.Errorf("warnings do not mention the inconsistent leg GA332: %q", joined)
	}
	if !strings.Contains(joined, "stop") {
		t.Errorf("warnings do not mention the stop-count conflict: %q", joined)
	}
}

func TestDirectFlightsAreConsistentWithTheirDeclaredValues(t *testing.T) {
	flights := fetch(t)

	for _, tc := range []struct {
		number   string
		minutes  int
		seats    int
		price    int64
		aircraft string
	}{
		{"GA400", 110, 28, 1250000, "Boeing 737-800"},
		{"GA410", 115, 15, 1450000, "Airbus A330-300"},
	} {
		f := byID(t, flights, tc.number)
		if got := f.TotalMinutes(); got != tc.minutes {
			t.Errorf("%s duration = %d, want %d", tc.number, got, tc.minutes)
		}
		if got := f.Stops(); got != 0 {
			t.Errorf("%s stops = %d, want 0", tc.number, got)
		}
		if got := f.Destination().Airport; got != "DPS" {
			t.Errorf("%s destination = %q, want DPS", tc.number, got)
		}
		if got := f.AvailableSeats; got != tc.seats {
			t.Errorf("%s seats = %d, want %d", tc.number, got, tc.seats)
		}
		if got := f.Price.Amount; got != tc.price {
			t.Errorf("%s price = %d, want %d", tc.number, got, tc.price)
		}
		if f.Aircraft == nil || *f.Aircraft != tc.aircraft {
			t.Errorf("%s aircraft = %v, want %q", tc.number, f.Aircraft, tc.aircraft)
		}
		if len(f.Warnings) != 0 {
			t.Errorf("%s raised unexpected warnings: %v", tc.number, f.Warnings)
		}
	}
}

func TestOptionalFieldsNormalizeToTheSchemaContract(t *testing.T) {
	flights := fetch(t)

	// GA315 carries no amenities key at all; it must serialize as [] rather than null.
	if got := byID(t, flights, "GA315").Amenities; got == nil {
		t.Error("GA315 amenities = nil, want empty slice")
	} else if len(got) != 0 {
		t.Errorf("GA315 amenities = %v, want empty", got)
	}

	if got := byID(t, flights, "GA400").Amenities; len(got) != 3 {
		t.Errorf("GA400 amenities = %v, want 3 entries", got)
	}

	f := byID(t, flights, "GA400")
	if f.Baggage.CarryOn != "1 piece" {
		t.Errorf("carry-on = %q, want %q", f.Baggage.CarryOn, "1 piece")
	}
	if f.Baggage.Checked != "2 pieces" {
		t.Errorf("checked = %q, want %q", f.Baggage.Checked, "2 pieces")
	}
	if f.CabinClass != "economy" {
		t.Errorf("cabin class = %q, want economy", f.CabinClass)
	}
	if f.ID != "GA400_GarudaIndonesia" {
		t.Errorf("id = %q, want GA400_GarudaIndonesia", f.ID)
	}
	if f.Airline.Code != "GA" {
		t.Errorf("airline code = %q, want GA", f.Airline.Code)
	}
}

// TestLatencyRespectsContext guards the property that makes a per-provider timeout
// meaningful: an already-cancelled context must abort the call instead of sleeping it out.
func TestLatencyRespectsContext(t *testing.T) {
	client := New(testCities, WithDelay(2*time.Second, 2*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := client.FetchFlights(ctx, model.SearchRequest{}); err == nil {
		t.Fatal("expected a context error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("returned after %v; the simulated delay ignored cancellation", elapsed)
	}
}

func TestEveryFlightValidates(t *testing.T) {
	for _, f := range fetch(t) {
		if err := f.Validate(); err != nil {
			t.Errorf("flight %s failed validation: %v", f.FlightNumber, err)
		}
	}
}
