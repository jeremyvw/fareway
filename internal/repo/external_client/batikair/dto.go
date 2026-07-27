// Package batikair is the external client for the Batik Air flight search API.
package batikair

// searchResponse is Batik Air's search payload.
//
// It uses camelCase throughout and repeats the HTTP status inside the body.
type searchResponse struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Results []flight `json:"results"`
}

// flight is one Batik Air itinerary.
type flight struct {
	FlightNumber string `json:"flightNumber"`
	AirlineName  string `json:"airlineName"`
	AirlineIATA  string `json:"airlineIATA"`
	Origin       string `json:"origin"`
	Destination  string `json:"destination"`

	// Timestamps are RFC3339 except the offset has no colon ("+0700"), which the standard
	// RFC3339 layout rejects.
	DepartureDateTime string `json:"departureDateTime"`
	ArrivalDateTime   string `json:"arrivalDateTime"`

	// TravelTime is prose ("1h 45m") and advisory: we measure from the timestamps.
	TravelTime string `json:"travelTime"`

	NumberOfStops int `json:"numberOfStops"`

	// Connections is absent on direct flights.
	Connections []connection `json:"connections,omitempty"`

	Fare           fare   `json:"fare"`
	SeatsAvailable int    `json:"seatsAvailable"`
	AircraftModel  string `json:"aircraftModel"`

	// BaggageInfo packs both allowances into one string ("7kg cabin, 20kg checked").
	BaggageInfo     string   `json:"baggageInfo"`
	OnboardServices []string `json:"onboardServices"`
}

// fare is Batik Air's price breakdown. TotalPrice is what a passenger pays; BasePrice
// excludes tax and would understate the fare.
type fare struct {
	BasePrice    int64  `json:"basePrice"`
	Taxes        int64  `json:"taxes"`
	TotalPrice   int64  `json:"totalPrice"`
	CurrencyCode string `json:"currencyCode"`

	// Class is a booking class letter ("Y"), not a cabin name.
	Class string `json:"class"`
}

// connection is an intermediate stop. StopDuration is prose ("55m").
type connection struct {
	StopAirport  string `json:"stopAirport"`
	StopDuration string `json:"stopDuration"`
}
