package airasia

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/retry"
)

func testCities(iata string) (string, bool) {
	cities := map[string]string{"CGK": "Jakarta", "DPS": "Denpasar", "SOC": "Surakarta"}
	city, ok := cities[iata]
	return city, ok
}

// reliable is the client with flakiness switched off, so normalization assertions never
// depend on a coin flip.
func reliable() *Client {
	return New(testCities, WithoutDelay(), WithFailureRate(0), WithRand(rand.New(rand.NewSource(1))))
}

func fetch(t *testing.T) []model.Flight {
	t.Helper()
	flights, err := reliable().FetchFlights(context.Background(), model.SearchRequest{})
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
	if got := len(fetch(t)); got != 4 {
		t.Fatalf("got %d flights, want 4", got)
	}
}

// TestQZ7250MatchesTheOutputContract checks the one flight the assignment spells out in
// expected_result.json, field by field — except the timestamps, which that file gets wrong
// by a year and which are asserted against computed values instead.
func TestQZ7250MatchesTheOutputContract(t *testing.T) {
	f := byNumber(t, fetch(t), "QZ7250")

	if f.ID != "QZ7250_AirAsia" {
		t.Errorf("id = %q, want QZ7250_AirAsia", f.ID)
	}
	if f.Provider != "AirAsia" {
		t.Errorf("provider = %q", f.Provider)
	}
	if f.Airline.Name != "AirAsia" || f.Airline.Code != "QZ" {
		t.Errorf("airline = %+v, want AirAsia/QZ", f.Airline)
	}
	if f.Origin().Airport != "CGK" || f.Origin().City != "Jakarta" {
		t.Errorf("origin = %+v, want CGK/Jakarta", f.Origin())
	}
	if f.Destination().Airport != "DPS" || f.Destination().City != "Denpasar" {
		t.Errorf("destination = %+v, want DPS/Denpasar", f.Destination())
	}
	if got := f.DepartAt().Unix(); got != 1765786500 {
		t.Errorf("departure epoch = %d, want 1765786500", got)
	}
	if got := f.ArriveAt().Unix(); got != 1765802100 {
		t.Errorf("arrival epoch = %d, want 1765802100", got)
	}
	if got := f.TotalMinutes(); got != 260 {
		t.Errorf("duration = %d min, want 260", got)
	}
	if got := f.Stops(); got != 1 {
		t.Errorf("stops = %d, want 1", got)
	}
	if f.Price.Amount != 485000 || f.Price.Currency != "IDR" {
		t.Errorf("price = %+v, want 485000 IDR", f.Price)
	}
	if f.AvailableSeats != 88 {
		t.Errorf("seats = %d, want 88", f.AvailableSeats)
	}
	if f.CabinClass != "economy" {
		t.Errorf("cabin class = %q, want economy", f.CabinClass)
	}
	if f.Aircraft != nil {
		t.Errorf("aircraft = %v, want nil so it serializes as null", *f.Aircraft)
	}
	if f.Amenities == nil || len(f.Amenities) != 0 {
		t.Errorf("amenities = %v, want an empty slice so it serializes as []", f.Amenities)
	}
	if f.Baggage.CarryOn != "Cabin baggage only" {
		t.Errorf("carry-on = %q, want %q", f.Baggage.CarryOn, "Cabin baggage only")
	}
	if f.Baggage.Checked != "Additional fee" {
		t.Errorf("checked = %q, want %q", f.Baggage.Checked, "Additional fee")
	}
}

// TestCarrierCodeIsDerived covers the field AirAsia never sends.
func TestCarrierCodeIsDerived(t *testing.T) {
	for _, f := range fetch(t) {
		if f.Airline.Code != "QZ" {
			t.Errorf("%s airline code = %q, want QZ derived from the flight number", f.FlightNumber, f.Airline.Code)
		}
	}
}

// TestDeclaredFractionalHoursDoNotFalselyConflict guards a rounding trap: 4.33 hours is
// 259.8 minutes and must reconcile with a computed 260 rather than raise a warning.
func TestDeclaredFractionalHoursDoNotFalselyConflict(t *testing.T) {
	for _, f := range fetch(t) {
		if len(f.Warnings) != 0 {
			t.Errorf("%s raised warnings on consistent data: %v", f.FlightNumber, f.Warnings)
		}
	}
}

func TestStopoverIsRecorded(t *testing.T) {
	f := byNumber(t, fetch(t), "QZ7250")
	if got := f.StopAirports(); len(got) != 1 || got[0] != "SOC" {
		t.Fatalf("stop airports = %v, want [SOC]", got)
	}
	if got := f.Stopovers[0].WaitMinutes; got != 95 {
		t.Errorf("wait = %d min, want 95", got)
	}
	if got := f.Stopovers[0].Airport.City; got != "Surakarta" {
		t.Errorf("stop city = %q, want Surakarta", got)
	}
	if got := f.AirborneMinutes(); got != 165 {
		t.Errorf("airborne = %d min, want 165 (260 total less 95 on the ground)", got)
	}
}

// TestFailureIsSurfacedAsASentinel lets a caller tell a flaky provider apart from a
// malformed response. Rate 1 forces the path deterministically rather than seed-hunting.
func TestFailureIsSurfacedAsASentinel(t *testing.T) {
	client := New(testCities, WithoutDelay(), WithFailureRate(1),
		WithRetry(retry.Config{Attempts: 2, Base: time.Millisecond, Max: time.Millisecond}))

	_, err := client.FetchFlights(context.Background(), model.SearchRequest{})
	if err == nil {
		t.Fatal("expected an error with a 100% failure rate")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error = %v, want it to wrap ErrUnavailable", err)
	}
}

func TestNoFailureWhenRateIsZero(t *testing.T) {
	// Repeated so a stray coin flip would show up rather than passing by luck.
	client := reliable()
	for i := 0; i < 50; i++ {
		if _, err := client.FetchFlights(context.Background(), model.SearchRequest{}); err != nil {
			t.Fatalf("attempt %d failed with a zero failure rate: %v", i, err)
		}
	}
}

func TestLatencyRespectsContext(t *testing.T) {
	client := New(testCities, WithDelay(2*time.Second, 2*time.Second), WithFailureRate(0))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := client.FetchFlights(ctx, model.SearchRequest{}); err == nil {
		t.Fatal("expected a context error, got nil")
	}
	// Must not retry past the caller's deadline either.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("returned after %v; the delay or the retry loop ignored cancellation", elapsed)
	}
}

func TestEveryFlightValidates(t *testing.T) {
	for _, f := range fetch(t) {
		if err := f.Validate(); err != nil {
			t.Errorf("flight %s failed validation: %v", f.FlightNumber, err)
		}
	}
}
