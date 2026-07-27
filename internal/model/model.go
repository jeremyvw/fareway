package model

import (
	"fmt"
	"time"

	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

type Money struct {
	Amount   int64
	Currency string
}

type Airline struct {
	Name string
	Code string
}

type Place struct {
	Airport string
	City    string
}

type Baggage struct {
	CarryOn string
	Checked string
}

type Stopover struct {
	Airport     Place
	WaitMinutes int
}

type Segment struct {
	FlightNumber string
	From         Place
	To           Place
	Depart       time.Time
	Arrive       time.Time
}

func (s Segment) Minutes() int {
	return timeutil.DurationMinutes(s.Depart, s.Arrive)
}

type Flight struct {
	ID             string
	Provider       string
	Airline        Airline
	FlightNumber   string
	Segments       []Segment
	Stopovers      []Stopover
	Price          Money
	AvailableSeats int
	CabinClass     string

	// Aircraft is a pointer because absence is meaningful and must serialize as null.
	Aircraft *string

	// Amenities is never nil, so it serializes as [] rather than null.
	Amenities []string

	Baggage Baggage

	// Warnings records inconsistencies found while normalizing — a provider's declared
	// duration contradicting its own timestamps, for instance. The flight is still
	// returned; the discrepancy is reported rather than hidden or fatal.
	Warnings []string
}

func (f Flight) Origin() Place {
	if len(f.Segments) == 0 {
		return Place{}
	}
	return f.Segments[0].From
}

func (f Flight) Destination() Place {
	if len(f.Segments) == 0 {
		return Place{}
	}
	return f.Segments[len(f.Segments)-1].To
}

func (f Flight) DepartAt() time.Time {
	if len(f.Segments) == 0 {
		return time.Time{}
	}
	return f.Segments[0].Depart
}

func (f Flight) ArriveAt() time.Time {
	if len(f.Segments) == 0 {
		return time.Time{}
	}
	return f.Segments[len(f.Segments)-1].Arrive
}

func (f Flight) Stops() int {
	if n := len(f.Segments) - 1; n > 0 {
		return n
	}
	return len(f.Stopovers)
}

func (f Flight) HasTimedLegs() bool {
	return len(f.Segments) > 1
}

func (f Flight) TotalMinutes() int {
	if len(f.Segments) == 0 {
		return 0
	}
	return timeutil.DurationMinutes(f.DepartAt(), f.ArriveAt())
}

func (f Flight) AirborneMinutes() int {
	if f.HasTimedLegs() {
		return f.legMinutes()
	}
	return f.TotalMinutes() - f.LayoverMinutes()
}

func (f Flight) LayoverMinutes() int {
	if f.HasTimedLegs() {
		return f.TotalMinutes() - f.legMinutes()
	}
	total := 0
	for _, s := range f.Stopovers {
		total += s.WaitMinutes
	}
	return total
}

func (f Flight) legMinutes() int {
	total := 0
	for _, s := range f.Segments {
		total += s.Minutes()
	}
	return total
}

func (f Flight) StopAirports() []string {
	codes := make([]string, 0, f.Stops())
	if f.HasTimedLegs() {
		for _, s := range f.Segments[:len(f.Segments)-1] {
			codes = append(codes, s.To.Airport)
		}
		return codes
	}
	for _, s := range f.Stopovers {
		codes = append(codes, s.Airport.Airport)
	}
	return codes
}

func (f Flight) IsDirect() bool {
	return f.Stops() == 0
}

func (f Flight) MatchesRoute(origin, destination string) bool {
	return f.Origin().Airport == origin && f.Destination().Airport == destination
}

func (f Flight) DepartsOn(date string) bool {
	if len(f.Segments) == 0 {
		return false
	}
	return timeutil.LocalDate(f.DepartAt()) == date
}

func (f *Flight) Warn(format string, args ...any) {
	f.Warnings = append(f.Warnings, fmt.Sprintf(format, args...))
}
