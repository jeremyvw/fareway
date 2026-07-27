package model

import (
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := timeutil.ParseOffset(value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func timedConnection(t *testing.T) Flight {
	return Flight{
		Segments: []Segment{
			{
				FlightNumber: "GA315",
				From:         Place{Airport: "CGK", City: "Jakarta"},
				To:           Place{Airport: "SUB", City: "Surabaya"},
				Depart:       at(t, "2025-12-15T14:00:00+07:00"),
				Arrive:       at(t, "2025-12-15T15:30:00+07:00"),
			},
			{
				FlightNumber: "GA332",
				From:         Place{Airport: "SUB", City: "Surabaya"},
				To:           Place{Airport: "DPS", City: "Denpasar"},
				Depart:       at(t, "2025-12-15T17:15:00+07:00"),
				Arrive:       at(t, "2025-12-15T18:45:00+08:00"),
			},
		},
		Price:     Money{Amount: 1850000, Currency: "IDR"},
		Amenities: []string{},
	}
}

func summarizedConnection(t *testing.T) Flight {
	return Flight{
		Segments: []Segment{{
			FlightNumber: "JT650",
			From:         Place{Airport: "CGK", City: "Jakarta"},
			To:           Place{Airport: "DPS", City: "Denpasar"},
			Depart:       at(t, "2025-12-15T16:20:00+07:00"),
			Arrive:       at(t, "2025-12-15T21:10:00+08:00"),
		}},
		Stopovers: []Stopover{{Airport: Place{Airport: "SUB", City: "Surabaya"}, WaitMinutes: 75}},
		Price:     Money{Amount: 780000, Currency: "IDR"},
		Amenities: []string{},
	}
}

func TestTimedLegsDeriveEverything(t *testing.T) {
	f := timedConnection(t)

	if !f.HasTimedLegs() {
		t.Error("HasTimedLegs() = false")
	}
	if got := f.Origin().Airport; got != "CGK" {
		t.Errorf("origin = %q", got)
	}
	if got := f.Destination().Airport; got != "DPS" {
		t.Errorf("destination = %q, want the last leg's arrival", got)
	}
	if got := f.Stops(); got != 1 {
		t.Errorf("stops = %d, want 1", got)
	}
	if got := f.StopAirports(); len(got) != 1 || got[0] != "SUB" {
		t.Errorf("stop airports = %v, want [SUB]", got)
	}
	if got := f.TotalMinutes(); got != 225 {
		t.Errorf("total = %d, want 225", got)
	}
	if got := f.AirborneMinutes(); got != 120 {
		t.Errorf("airborne = %d, want 120 (90 + 30 across the timezone boundary)", got)
	}
	if got := f.LayoverMinutes(); got != 105 {
		t.Errorf("layover = %d, want 105", got)
	}
}

func TestSummarizedConnectionDerivesFromStopovers(t *testing.T) {
	f := summarizedConnection(t)

	if f.HasTimedLegs() {
		t.Error("HasTimedLegs() = true, but there is only one segment")
	}
	if got := f.Stops(); got != 1 {
		t.Errorf("stops = %d, want 1 from the stopover list", got)
	}
	if got := f.TotalMinutes(); got != 230 {
		t.Errorf("total = %d, want 230", got)
	}
	if got := f.LayoverMinutes(); got != 75 {
		t.Errorf("layover = %d, want the declared 75", got)
	}
	if got := f.AirborneMinutes(); got != 155 {
		t.Errorf("airborne = %d, want 155", got)
	}
	if f.IsDirect() {
		t.Error("IsDirect() = true")
	}
}

func TestDirectFlightHasNoStops(t *testing.T) {
	f := summarizedConnection(t)
	f.Stopovers = nil

	if got := f.Stops(); got != 0 {
		t.Errorf("stops = %d, want 0", got)
	}
	if !f.IsDirect() {
		t.Error("IsDirect() = false")
	}
	if got := f.LayoverMinutes(); got != 0 {
		t.Errorf("layover = %d, want 0", got)
	}
	if f.AirborneMinutes() != f.TotalMinutes() {
		t.Errorf("airborne %d != total %d on a direct flight", f.AirborneMinutes(), f.TotalMinutes())
	}
}

func TestAccessorsAreSafeWhenEmpty(t *testing.T) {
	var f Flight

	if !f.DepartAt().IsZero() || !f.ArriveAt().IsZero() {
		t.Error("expected zero times")
	}
	if f.Origin() != (Place{}) || f.Destination() != (Place{}) {
		t.Error("expected zero places")
	}
	if f.Stops() != 0 || f.TotalMinutes() != 0 || f.LayoverMinutes() != 0 || f.AirborneMinutes() != 0 {
		t.Error("expected zero derived values")
	}
	if len(f.StopAirports()) != 0 {
		t.Error("expected no stop airports")
	}
}

func TestMatchesRouteUsesTheEffectiveEndpoints(t *testing.T) {
	f := timedConnection(t)

	if !f.MatchesRoute("CGK", "DPS") {
		t.Error("MatchesRoute(CGK, DPS) = false; the real destination is the last leg's arrival")
	}
	// SUB is a stop, not the destination, and must not match.
	if f.MatchesRoute("CGK", "SUB") {
		t.Error("MatchesRoute(CGK, SUB) = true, but SUB is an intermediate stop")
	}
}

func TestDepartsOnUsesTheLocalCalendarDate(t *testing.T) {
	f := summarizedConnection(t)
	f.Segments[0].Depart = at(t, "2025-12-15T00:30:00+07:00")
	f.Segments[0].Arrive = at(t, "2025-12-15T04:30:00+08:00")

	if !f.DepartsOn("2025-12-15") {
		t.Error("DepartsOn(2025-12-15) = false for a 00:30+07:00 departure")
	}
	if f.DepartsOn("2025-12-14") {
		t.Error("DepartsOn(2025-12-14) = true; that is the UTC date, not the local one")
	}
}

func TestWarnAccumulates(t *testing.T) {
	f := timedConnection(t)
	f.Warn("declared %d stop(s) but found %d", 0, 1)
	f.Warn("second issue")

	if len(f.Warnings) != 2 {
		t.Fatalf("warnings = %v, want 2", f.Warnings)
	}
	if f.Warnings[0] != "declared 0 stop(s) but found 1" {
		t.Errorf("warning = %q", f.Warnings[0])
	}
}

func TestValidateAcceptsBothShapes(t *testing.T) {
	if err := timedConnection(t).Validate(); err != nil {
		t.Errorf("timed connection: %v", err)
	}
	if err := summarizedConnection(t).Validate(); err != nil {
		t.Errorf("summarized connection: %v", err)
	}
}

func TestValidateRejectsIncoherentFlights(t *testing.T) {
	t.Run("no segments", func(t *testing.T) {
		var f Flight
		if err := f.Validate(); err != ErrNoSegments {
			t.Errorf("err = %v, want ErrNoSegments", err)
		}
	})

	t.Run("arrival before departure", func(t *testing.T) {
		f := summarizedConnection(t)
		f.Segments[0].Arrive = f.Segments[0].Depart.Add(-time.Hour)
		if err := f.Validate(); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("legs do not connect", func(t *testing.T) {
		f := timedConnection(t)
		f.Segments[1].From = Place{Airport: "UPG"}
		if err := f.Validate(); err == nil {
			t.Error("expected an error when leg two departs somewhere leg one did not land")
		}
	})

	t.Run("second leg departs before the first lands", func(t *testing.T) {
		f := timedConnection(t)
		f.Segments[1].Depart = f.Segments[0].Arrive.Add(-time.Minute)
		if err := f.Validate(); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("layover longer than the journey", func(t *testing.T) {
		f := summarizedConnection(t)
		f.Stopovers[0].WaitMinutes = 10_000
		if err := f.Validate(); err == nil {
			t.Error("expected an error when the declared layover exceeds the journey")
		}
	})

	t.Run("non-positive fare", func(t *testing.T) {
		f := summarizedConnection(t)
		f.Price.Amount = 0
		if err := f.Validate(); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("missing currency", func(t *testing.T) {
		f := summarizedConnection(t)
		f.Price.Currency = ""
		if err := f.Validate(); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("same airport both ends", func(t *testing.T) {
		f := summarizedConnection(t)
		f.Segments[0].To = Place{Airport: "CGK"}
		if err := f.Validate(); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestNormalizeFillsDefaults(t *testing.T) {
	var r SearchRequest
	r.Normalize()

	if r.Passengers != 1 {
		t.Errorf("passengers = %d, want 1", r.Passengers)
	}
	if r.CabinClass != "economy" {
		t.Errorf("cabin class = %q, want economy", r.CabinClass)
	}
	if r.Sort != SortPriceAsc {
		t.Errorf("sort = %q, want price_asc", r.Sort)
	}
}
