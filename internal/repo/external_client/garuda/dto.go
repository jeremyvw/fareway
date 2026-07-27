// Package garuda is the external client for the Garuda Indonesia flight search API.
package garuda

// searchResponse is Garuda's search payload.
type searchResponse struct {
	Status  string   `json:"status"`
	Flights []flight `json:"flights"`
}

// flight is one Garuda itinerary.
//
// Connecting itineraries are the trap here: when Segments is present the top-level
// Arrival, Stops and DurationMinutes describe only the first leg, so they understate the
// journey and name the wrong final airport. Segments is the authoritative view.
type flight struct {
	FlightID    string   `json:"flight_id"`
	Airline     string   `json:"airline"`
	AirlineCode string   `json:"airline_code"`
	Departure   endpoint `json:"departure"`
	Arrival     endpoint `json:"arrival"`

	// DurationMinutes and Stops describe the first leg only when Segments is set.
	DurationMinutes int `json:"duration_minutes"`
	Stops           int `json:"stops"`

	Aircraft       string  `json:"aircraft"`
	Price          price   `json:"price"`
	AvailableSeats int     `json:"available_seats"`
	FareClass      string  `json:"fare_class"`
	Baggage        baggage `json:"baggage"`

	// Amenities is absent on some flights.
	Amenities []string `json:"amenities,omitempty"`

	// Segments is present only for connecting itineraries.
	Segments []segment `json:"segments,omitempty"`
}

// endpoint is one end of a journey. Time is RFC3339 with a colon in the offset.
type endpoint struct {
	Airport  string `json:"airport"`
	City     string `json:"city"`
	Time     string `json:"time"`
	Terminal string `json:"terminal"`
}

// price is Garuda's fare, already a total.
type price struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// baggage is expressed as piece counts rather than weights.
type baggage struct {
	CarryOn int `json:"carry_on"`
	Checked int `json:"checked"`
}

// segment is one leg of a connecting itinerary.
type segment struct {
	FlightNumber    string          `json:"flight_number"`
	Departure       segmentEndpoint `json:"departure"`
	Arrival         segmentEndpoint `json:"arrival"`
	DurationMinutes int             `json:"duration_minutes"`

	// LayoverMinutes is the wait before this leg, so it is absent on the first segment.
	LayoverMinutes int `json:"layover_minutes,omitempty"`
}

// segmentEndpoint is the reduced endpoint used inside segments: no city or terminal.
type segmentEndpoint struct {
	Airport string `json:"airport"`
	Time    string `json:"time"`
}
