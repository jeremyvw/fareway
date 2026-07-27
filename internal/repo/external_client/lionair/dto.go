// Package lionair is the external client for the Lion Air flight search API.
package lionair

// searchResponse is Lion Air's search payload. Results sit two levels deep, behind a
// success flag and a data envelope.
type searchResponse struct {
	Success bool `json:"success"`
	Data    struct {
		AvailableFlights []flight `json:"available_flights"`
	} `json:"data"`
}

// flight is one Lion Air itinerary.
type flight struct {
	ID      string  `json:"id"`
	Carrier carrier `json:"carrier"`
	Route   route   `json:"route"`

	Schedule schedule `json:"schedule"`

	// FlightTime is advisory: we measure duration from the schedule instead.
	FlightTime int  `json:"flight_time"`
	IsDirect   bool `json:"is_direct"`

	// StopCount and Layovers are absent on direct flights.
	StopCount int       `json:"stop_count,omitempty"`
	Layovers  []layover `json:"layovers,omitempty"`

	Pricing   pricing  `json:"pricing"`
	SeatsLeft int      `json:"seats_left"`
	PlaneType string   `json:"plane_type"`
	Services  services `json:"services"`
}

// carrier identifies the airline.
type carrier struct {
	Name string `json:"name"`
	IATA string `json:"iata"`
}

// route names both ends of the journey, including city names.
type route struct {
	From station `json:"from"`
	To   station `json:"to"`
}

// station is an airport with its full name and city.
type station struct {
	Code string `json:"code"`
	Name string `json:"name"`
	City string `json:"city"`
}

// schedule holds zone-less timestamps paired with separate IANA zone names.
//
// This is the format that must never be parsed without its location: read as UTC, every
// Lion Air flight shifts by 7-9 hours.
type schedule struct {
	Departure         string `json:"departure"`
	DepartureTimezone string `json:"departure_timezone"`
	Arrival           string `json:"arrival"`
	ArrivalTimezone   string `json:"arrival_timezone"`
}

// layover is an intermediate stop.
type layover struct {
	Airport         string `json:"airport"`
	DurationMinutes int    `json:"duration_minutes"`
}

// pricing is Lion Air's fare. FareType is an upper-cased cabin name ("ECONOMY").
type pricing struct {
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
	FareType string `json:"fare_type"`
}

// services covers onboard amenities and the baggage allowance.
type services struct {
	WifiAvailable    bool             `json:"wifi_available"`
	MealsIncluded    bool             `json:"meals_included"`
	BaggageAllowance baggageAllowance `json:"baggage_allowance"`
}

// baggageAllowance is expressed as weights ("7 kg"), unlike Garuda's piece counts.
type baggageAllowance struct {
	Cabin string `json:"cabin"`
	Hold  string `json:"hold"`
}
