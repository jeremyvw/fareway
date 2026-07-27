package model

type SearchRequest struct {
	Origin        string  `json:"origin"`
	Destination   string  `json:"destination"`
	DepartureDate string  `json:"departureDate"`
	ReturnDate    *string `json:"returnDate"`
	Passengers    int     `json:"passengers"`
	CabinClass    string  `json:"cabinClass"`

	Filters Filters    `json:"filters"`
	Sort    SortOption `json:"sort"`
}

type Filters struct {
	MinPrice int64 `json:"min_price"`
	MaxPrice int64 `json:"max_price"`

	// MaxStops is a pointer so that 0 ("direct only") is distinguishable from unset.
	MaxStops *int `json:"max_stops"`

	// Time-of-day bounds are "HH:MM" in each endpoint's local zone, which is what a
	// passenger means by "land before 21:00".
	DepartureAfter  string `json:"departure_after"`
	DepartureBefore string `json:"departure_before"`
	ArrivalAfter    string `json:"arrival_after"`
	ArrivalBefore   string `json:"arrival_before"`

	// Airlines is an allow-list of IATA carrier codes.
	Airlines []string `json:"airlines"`

	MaxDurationMinutes int `json:"max_duration_minutes"`
}

// SortOption names a result ordering.
type SortOption string

const (
	SortBestValue     SortOption = "best_value"
	SortPriceAsc      SortOption = "price_asc"
	SortPriceDesc     SortOption = "price_desc"
	SortDurationAsc   SortOption = "duration_asc"
	SortDurationDesc  SortOption = "duration_desc"
	SortDepartureTime SortOption = "departure_time"
	SortArrivalTime   SortOption = "arrival_time"
)

// Normalize fills in defaults so downstream layers never see a zero-value request.
func (r *SearchRequest) Normalize() {
	if r.Passengers <= 0 {
		r.Passengers = 1
	}
	if r.CabinClass == "" {
		r.CabinClass = "economy"
	}
	if r.Sort == "" {
		r.Sort = SortBestValue
	}
}
