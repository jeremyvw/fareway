package lionair

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

func testCities(iata string) (string, bool) {
	cities := map[string]string{"CGK": "Jakarta", "DPS": "Denpasar", "SUB": "Surabaya"}
	city, ok := cities[iata]
	return city, ok
}

func fetch(t *testing.T) []model.Flight {
	t.Helper()
	flights, err := New(testCities, WithoutDelay()).FetchFlights(context.Background(), model.SearchRequest{})
	if err != nil {
		t.Fatalf("FetchFlights: %v", err)
	}
	return flights
}

func byNumber(t *testing.T, flights []model.Flight, number string) model.Flight {
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
	if got := len(fetch(t)); got != 3 {
		t.Fatalf("got %d flights, want 3", got)
	}
}

// TestZonesAreHonoured is the point of this adapter: the timestamps carry no offset, only a
// sibling IANA zone name. Read as UTC they would be seven to nine hours out, so the test
// pins absolute instants rather than wall-clock fields.
func TestZonesAreHonoured(t *testing.T) {
	f := byNumber(t, fetch(t), "JT740")

	if got := f.DepartAt().Unix(); got != 1765751400 {
		t.Errorf("departure epoch = %d, want 1765751400 (05:30 WIB)", got)
	}
	if got := f.ArriveAt().Unix(); got != 1765757700 {
		t.Errorf("arrival epoch = %d, want 1765757700 (08:15 WITA)", got)
	}
	// The declared 105 minutes only reconciles if both zones were applied.
	if got := f.TotalMinutes(); got != 105 {
		t.Errorf("duration = %d min, want 105", got)
	}
	if got := f.DepartAt().Location().String(); got != "Asia/Jakarta" {
		t.Errorf("departure location = %q, want Asia/Jakarta", got)
	}
	if got := f.ArriveAt().Location().String(); got != "Asia/Makassar" {
		t.Errorf("arrival location = %q, want Asia/Makassar", got)
	}
}

// TestJT650SummarizedConnection covers the shape Lion Air uses for connections: endpoint
// timestamps plus a layover duration, with no per-leg times to build segments from.
func TestJT650SummarizedConnection(t *testing.T) {
	f := byNumber(t, fetch(t), "JT650")

	if f.HasTimedLegs() {
		t.Error("HasTimedLegs() = true; Lion Air supplies no per-leg timestamps")
	}
	if got := f.Stops(); got != 1 {
		t.Errorf("stops = %d, want 1", got)
	}
	if got := f.StopAirports(); len(got) != 1 || got[0] != "SUB" {
		t.Errorf("stop airports = %v, want [SUB]", got)
	}
	if got := f.Stopovers[0].Airport.City; got != "Surabaya" {
		t.Errorf("stop city = %q, want Surabaya; the layover airport needs the resolver", got)
	}
	if got := f.TotalMinutes(); got != 230 {
		t.Errorf("total = %d min, want 230", got)
	}
	if got := f.LayoverMinutes(); got != 75 {
		t.Errorf("layover = %d min, want 75", got)
	}
	if got := f.AirborneMinutes(); got != 155 {
		t.Errorf("airborne = %d min, want 155 (230 total less 75 on the ground)", got)
	}
	if f.IsDirect() {
		t.Error("IsDirect() = true, want false")
	}
	if !f.MatchesRoute("CGK", "DPS") {
		t.Error("MatchesRoute(CGK, DPS) = false, want true")
	}
}

// TestLionAirIsTheConsistentProvider is a control case: all three of its flights agree with
// their own declared figures, so any warning here means the normalizer invented a conflict.
func TestLionAirIsTheConsistentProvider(t *testing.T) {
	for _, f := range fetch(t) {
		if len(f.Warnings) != 0 {
			t.Errorf("%s raised warnings on self-consistent data: %v", f.FlightNumber, f.Warnings)
		}
	}
}

func TestFieldMapping(t *testing.T) {
	f := byNumber(t, fetch(t), "JT742")

	if f.ID != "JT742_LionAir" {
		t.Errorf("id = %q, want JT742_LionAir", f.ID)
	}
	if f.Airline.Code != "JT" || f.Airline.Name != "Lion Air" {
		t.Errorf("airline = %+v", f.Airline)
	}
	if f.CabinClass != "economy" {
		t.Errorf("cabin class = %q, want economy (from ECONOMY)", f.CabinClass)
	}
	if f.Price.Amount != 890000 || f.Price.Currency != "IDR" {
		t.Errorf("price = %+v", f.Price)
	}
	if f.AvailableSeats != 38 {
		t.Errorf("seats = %d, want 38", f.AvailableSeats)
	}
	if f.Aircraft == nil || *f.Aircraft != "Boeing 737-800" {
		t.Errorf("aircraft = %v", f.Aircraft)
	}
	if f.Baggage.CarryOn != "7 kg" || f.Baggage.Checked != "20 kg" {
		t.Errorf("baggage = %+v", f.Baggage)
	}
	// Both service flags are false across the fixture, so amenities must be empty, not nil.
	if f.Amenities == nil || len(f.Amenities) != 0 {
		t.Errorf("amenities = %v, want an empty slice", f.Amenities)
	}
}

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
