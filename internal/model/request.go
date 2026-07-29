package model

import "strings"

type Leg struct {
	Origin        string `json:"origin"`
	Destination   string `json:"destination"`
	DepartureDate string `json:"departureDate"`
}

type TripType string

const (
	TripOneWay    TripType = "one_way"
	TripRoundTrip TripType = "round_trip"
	TripMultiCity TripType = "multi_city"
)

const MaxLegs = 6

type SearchRequest struct {
	Origin        string  `json:"origin"`
	Destination   string  `json:"destination"`
	DepartureDate string  `json:"departureDate"`
	ReturnDate    *string `json:"returnDate"`

	Legs []Leg `json:"legs,omitempty"`

	Passengers int    `json:"passengers"`
	CabinClass string `json:"cabinClass"`

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

	if len(r.Legs) == 0 && r.hasFlatForm() {
		r.Legs = r.legsFromFlatForm()
	}

	for i := range r.Legs {
		r.Legs[i].Origin = normalizeIATA(r.Legs[i].Origin)
		r.Legs[i].Destination = normalizeIATA(r.Legs[i].Destination)
		r.Legs[i].DepartureDate = strings.TrimSpace(r.Legs[i].DepartureDate)
	}

	if len(r.Legs) > 0 {
		r.Origin = r.Legs[0].Origin
		r.Destination = r.Legs[0].Destination
		r.DepartureDate = r.Legs[0].DepartureDate
	}
}

func (r *SearchRequest) legsFromFlatForm() []Leg {
	legs := []Leg{{
		Origin:        r.Origin,
		Destination:   r.Destination,
		DepartureDate: r.DepartureDate,
	}}
	if r.ReturnDate != nil && strings.TrimSpace(*r.ReturnDate) != "" {
		legs = append(legs, Leg{
			Origin:        r.Destination,
			Destination:   r.Origin,
			DepartureDate: strings.TrimSpace(*r.ReturnDate),
		})
	}
	return legs
}

func (r *SearchRequest) hasFlatForm() bool {
	return strings.TrimSpace(r.Origin) != "" ||
		strings.TrimSpace(r.Destination) != "" ||
		strings.TrimSpace(r.DepartureDate) != "" ||
		(r.ReturnDate != nil && strings.TrimSpace(*r.ReturnDate) != "")
}

func (r *SearchRequest) HasBothForms() bool {
	return len(r.Legs) > 0 && r.hasFlatForm()
}

func (r *SearchRequest) TripType() TripType {
	switch len(r.Legs) {
	case 0, 1:
		return TripOneWay
	case 2:
		if r.Legs[1].Origin == r.Legs[0].Destination && r.Legs[1].Destination == r.Legs[0].Origin {
			return TripRoundTrip
		}
		return TripMultiCity
	default:
		return TripMultiCity
	}
}

func normalizeIATA(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
