// Package airasia is the external client for the AirAsia flight search API.
package airasia

// searchResponse is AirAsia's search payload.
//
// The shape is flat: no nested price or route objects, and the currency is baked into the
// field name rather than being stated.
type searchResponse struct {
	Status  string   `json:"status"`
	Flights []flight `json:"flights"`
}

// flight is one AirAsia itinerary.
type flight struct {
	FlightCode  string `json:"flight_code"`
	Airline     string `json:"airline"`
	FromAirport string `json:"from_airport"`
	ToAirport   string `json:"to_airport"`

	// Timestamps are RFC3339 with a colon in the offset.
	DepartTime string `json:"depart_time"`
	ArriveTime string `json:"arrive_time"`

	// DurationHours is advisory: we measure duration from the timestamps instead.
	DurationHours float64 `json:"duration_hours"`

	DirectFlight bool `json:"direct_flight"`

	// Stops is absent on direct flights.
	Stops []stop `json:"stops,omitempty"`

	// PriceIDR carries the currency in its name; there is no currency field.
	PriceIDR int64 `json:"price_idr"`

	Seats      int    `json:"seats"`
	CabinClass string `json:"cabin_class"`

	// BaggageNote is prose, not a structured allowance.
	BaggageNote string `json:"baggage_note"`
}

// stop is an intermediate stop on a connecting itinerary.
type stop struct {
	Airport         string `json:"airport"`
	WaitTimeMinutes int    `json:"wait_time_minutes"`
}
