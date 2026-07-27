package batikair

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

func testCities(iata string) (string, bool) {
	cities := map[string]string{"CGK": "Jakarta", "DPS": "Denpasar", "UPG": "Makassar"}
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

// TestColonLessOffsetIsParsed covers the format that the standard RFC3339 layout rejects.
func TestColonLessOffsetIsParsed(t *testing.T) {
	f := byNumber(t, fetch(t), "ID6514")

	if got := f.DepartAt().Unix(); got != 1765757700 {
		t.Errorf("departure epoch = %d, want 1765757700 (07:15+0700)", got)
	}
	if got := f.TotalMinutes(); got != 105 {
		t.Errorf("duration = %d min, want 105", got)
	}
}

// TestID7042ReportsTheDurationConflict pins the warning path to the provider's own data:
// Batik Air declares "3h 5m" for a journey its timestamps put at 245 minutes. The computed
// value wins and the conflict is reported rather than hidden.
func TestID7042ReportsTheDurationConflict(t *testing.T) {
	f := byNumber(t, fetch(t), "ID7042")

	if got := f.TotalMinutes(); got != 245 {
		t.Errorf("duration = %d min, want 245 (declared 3h 5m)", got)
	}
	if len(f.Warnings) == 0 {
		t.Fatal("expected a warning about the declared duration, got none")
	}
	joined := strings.Join(f.Warnings, "; ")
	if !strings.Contains(joined, "3h 5m") || !strings.Contains(joined, "245") {
		t.Errorf("warning does not name both figures: %q", joined)
	}
}

func TestID7042SummarizedConnection(t *testing.T) {
	f := byNumber(t, fetch(t), "ID7042")

	if got := f.Stops(); got != 1 {
		t.Errorf("stops = %d, want 1", got)
	}
	if got := f.StopAirports(); len(got) != 1 || got[0] != "UPG" {
		t.Errorf("stop airports = %v, want [UPG]", got)
	}
	// The wait arrives as prose ("55m") and must survive as minutes.
	if got := f.LayoverMinutes(); got != 55 {
		t.Errorf("layover = %d min, want 55", got)
	}
	if got := f.Stopovers[0].Airport.City; got != "Makassar" {
		t.Errorf("stop city = %q, want Makassar", got)
	}
}

// TestFareUsesTotalNotBase guards a mistake that would silently win every price comparison:
// basePrice excludes tax.
func TestFareUsesTotalNotBase(t *testing.T) {
	f := byNumber(t, fetch(t), "ID6514")
	if f.Price.Amount != 1100000 {
		t.Errorf("price = %d, want 1100000 (total, not the 980000 base)", f.Price.Amount)
	}
	if f.Price.Currency != "IDR" {
		t.Errorf("currency = %q, want IDR", f.Price.Currency)
	}
}

func TestBookingClassBecomesCabinName(t *testing.T) {
	if got := byNumber(t, fetch(t), "ID6514").CabinClass; got != "economy" {
		t.Errorf("cabin class = %q, want economy (from booking class Y)", got)
	}
	for class, want := range map[string]string{
		"Y": "economy", "W": "premium_economy", "C": "business", "J": "business", "F": "first",
		// An unknown letter must not be guessed at as economy.
		"Q": "q", "": "",
	} {
		if got := cabinFromBookingClass(class); got != want {
			t.Errorf("cabinFromBookingClass(%q) = %q, want %q", class, got, want)
		}
	}
}

func TestBaggageStringIsSplit(t *testing.T) {
	f := byNumber(t, fetch(t), "ID6514")
	if f.Baggage.CarryOn != "7kg" || f.Baggage.Checked != "20kg" {
		t.Errorf("baggage = %+v, want 7kg / 20kg from %q", f.Baggage, "7kg cabin, 20kg checked")
	}
}

func TestAmenitiesAreLowerCased(t *testing.T) {
	// The provider sends "Snack", "Beverage"; a filter must not care about the casing.
	got := byNumber(t, fetch(t), "ID6514").Amenities
	if len(got) != 2 || got[0] != "snack" || got[1] != "beverage" {
		t.Errorf("amenities = %v, want [snack beverage]", got)
	}
}

func TestParseProseDuration(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"1h 45m", 105},
		{"3h 5m", 185},
		{"55m", 55},
		{"2h", 120},
		{"", 0},
		{"about an hour", 0},
	} {
		if got := parseProseDuration(tc.in); got != tc.want {
			t.Errorf("parseProseDuration(%q) = %d, want %d", tc.in, got, tc.want)
		}
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
