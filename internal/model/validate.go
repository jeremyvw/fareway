package model

import (
	"errors"
	"fmt"
)

// ErrNoSegments means normalization produced an itinerary with nothing in it.
var ErrNoSegments = errors.New("itinerary has no segments")

// Validate checks that a normalized flight is internally coherent.
func (f Flight) Validate() error {
	if len(f.Segments) == 0 {
		return ErrNoSegments
	}

	for i, s := range f.Segments {
		if s.From.Airport == "" || s.To.Airport == "" {
			return fmt.Errorf("segment %d: missing airport code", i)
		}
		if s.From.Airport == s.To.Airport {
			return fmt.Errorf("segment %d: departs and arrives at %s", i, s.From.Airport)
		}
		if s.Depart.IsZero() || s.Arrive.IsZero() {
			return fmt.Errorf("segment %d: missing timestamp", i)
		}
		if !s.Arrive.After(s.Depart) {
			return fmt.Errorf("segment %d: arrival %s is not after departure %s",
				i, s.Arrive.Format(timeLayoutErr), s.Depart.Format(timeLayoutErr))
		}
		if i > 0 {
			prev := f.Segments[i-1]
			if s.Depart.Before(prev.Arrive) {
				return fmt.Errorf("segment %d: departs before segment %d arrives", i, i-1)
			}
			if s.From.Airport != prev.To.Airport {
				return fmt.Errorf("segment %d: departs %s but segment %d arrived %s",
					i, s.From.Airport, i-1, prev.To.Airport)
			}
		}
	}

	if f.Price.Amount <= 0 {
		return fmt.Errorf("non-positive fare %d", f.Price.Amount)
	}
	if f.Price.Currency == "" {
		return errors.New("missing currency")
	}
	if f.AvailableSeats < 0 {
		return fmt.Errorf("negative seat count %d", f.AvailableSeats)
	}

	// A summarized connection must not claim more ground time than the journey lasts.
	if !f.HasTimedLegs() {
		if layover := f.LayoverMinutes(); layover >= f.TotalMinutes() {
			return fmt.Errorf("declared layover of %d min exceeds the %d min journey",
				layover, f.TotalMinutes())
		}
	}
	for i, s := range f.Stopovers {
		if s.Airport.Airport == "" {
			return fmt.Errorf("stopover %d: missing airport code", i)
		}
		if s.WaitMinutes < 0 {
			return fmt.Errorf("stopover %d: negative wait of %d min", i, s.WaitMinutes)
		}
	}
	return nil
}

// timeLayoutErr is only for readable error messages.
const timeLayoutErr = "2006-01-02T15:04:05-07:00"
