package model

import (
	"time"

	"github.com/jeremyvw/fareway/internal/util/currency"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

type SearchResponse struct {
	SearchCriteria Criteria     `json:"search_criteria"`
	Metadata       Metadata     `json:"metadata"`
	Flights        []FlightView `json:"flights"`
}

type Criteria struct {
	Origin        string `json:"origin"`
	Destination   string `json:"destination"`
	DepartureDate string `json:"departure_date"`
	Passengers    int    `json:"passengers"`
	CabinClass    string `json:"cabin_class"`
}

type Metadata struct {
	TotalResults      int              `json:"total_results"`
	ProvidersQueried  int              `json:"providers_queried"`
	ProvidersSucceded int              `json:"providers_succeeded"`
	ProvidersFailed   int              `json:"providers_failed"`
	SearchTimeMS      int64            `json:"search_time_ms"`
	CacheHit          bool             `json:"cache_hit"`
	ProviderStatus    []ProviderStatus `json:"provider_status,omitempty"`
	DroppedResults    int              `json:"dropped_results,omitempty"`
	FilteredResults   int              `json:"filtered_results,omitempty"`
	MergedDuplicates  int              `json:"merged_duplicates,omitempty"`
	SortedBy          string           `json:"sorted_by,omitempty"`
}

type ProviderStatus struct {
	Provider   string `json:"provider"`
	OK         bool   `json:"ok"`
	Results    int    `json:"results"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type FlightView struct {
	ID                string                 `json:"id"`
	Provider          string                 `json:"provider"`
	Airline           AirlineView            `json:"airline"`
	FlightNumber      string                 `json:"flight_number"`
	Departure         EndpointView           `json:"departure"`
	Arrival           EndpointView           `json:"arrival"`
	Duration          DurationView           `json:"duration"`
	Stops             int                    `json:"stops"`
	Price             PriceView              `json:"price"`
	AvailableSeats    int                    `json:"available_seats"`
	CabinClass        string                 `json:"cabin_class"`
	Aircraft          *string                `json:"aircraft"`
	Amenities         []string               `json:"amenities"`
	Baggage           BaggageView            `json:"baggage"`
	BestValueScore    float64                `json:"best_value_score"`
	AlternativePrices []AlternativePriceView `json:"alternative_prices,omitempty"`
	StopAirports      []string               `json:"stop_airports,omitempty"`
	LayoverMinutes    int                    `json:"layover_minutes,omitempty"`
	Warnings          []string               `json:"warnings,omitempty"`
}

type AlternativePriceView struct {
	Provider  string `json:"provider"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Formatted string `json:"formatted,omitempty"`
}

type AirlineView struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type EndpointView struct {
	Airport   string `json:"airport"`
	City      string `json:"city"`
	Datetime  string `json:"datetime"`
	Timestamp int64  `json:"timestamp"`
}

type DurationView struct {
	TotalMinutes int    `json:"total_minutes"`
	Formatted    string `json:"formatted"`
}

type PriceView struct {
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Formatted string `json:"formatted,omitempty"`
}

type BaggageView struct {
	CarryOn string `json:"carry_on"`
	Checked string `json:"checked"`
}

func NewFlightView(f Flight) FlightView {
	view := FlightView{
		ID:           f.ID,
		Provider:     f.Provider,
		Airline:      AirlineView{Name: f.Airline.Name, Code: f.Airline.Code},
		FlightNumber: f.FlightNumber,
		Departure:    newEndpointView(f.Origin(), f.DepartAt()),
		Arrival:      newEndpointView(f.Destination(), f.ArriveAt()),
		Duration: DurationView{
			TotalMinutes: f.TotalMinutes(),
			Formatted:    timeutil.FormatDuration(f.TotalMinutes()),
		},
		Stops: f.Stops(),
		Price: PriceView{
			Amount:    f.Price.Amount,
			Currency:  f.Price.Currency,
			Formatted: currency.Format(f.Price.Amount, f.Price.Currency),
		},
		AvailableSeats: f.AvailableSeats,
		CabinClass:     f.CabinClass,
		Aircraft:       f.Aircraft,
		Amenities:      f.Amenities,
		Baggage:        BaggageView{CarryOn: f.Baggage.CarryOn, Checked: f.Baggage.Checked},
		BestValueScore: f.BestValueScore,
		Warnings:       f.Warnings,
	}
	for _, alt := range f.AlternativePrices {
		view.AlternativePrices = append(view.AlternativePrices, AlternativePriceView{
			Provider:  alt.Provider,
			Amount:    alt.Amount,
			Currency:  alt.Currency,
			Formatted: currency.Format(alt.Amount, alt.Currency),
		})
	}
	if f.Stops() > 0 {
		view.StopAirports = f.StopAirports()
		view.LayoverMinutes = f.LayoverMinutes()
	}
	// Amenities must serialize as [] rather than null even if a mapper let a nil through.
	if view.Amenities == nil {
		view.Amenities = []string{}
	}
	return view
}

func newEndpointView(p Place, t time.Time) EndpointView {
	return EndpointView{
		Airport:   p.Airport,
		City:      p.City,
		Datetime:  t.Format(time.RFC3339),
		Timestamp: t.Unix(),
	}
}
