package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

type fakeClient struct {
	name    string
	flights []model.Flight
	err     error
	delay   time.Duration
}

func (f fakeClient) Name() string { return f.name }

func (f fakeClient) FetchFlights(ctx context.Context, _ model.SearchRequest) ([]model.Flight, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.flights, nil
}

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := timeutil.ParseOffset(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func flight(t *testing.T, id string, price int64, seats int) model.Flight {
	t.Helper()
	return model.Flight{
		ID:           id,
		Provider:     "Test",
		Airline:      model.Airline{Name: "Test Air", Code: "TT"},
		FlightNumber: id,
		Segments: []model.Segment{{
			FlightNumber: id,
			From:         model.Place{Airport: "CGK", City: "Jakarta"},
			To:           model.Place{Airport: "DPS", City: "Denpasar"},
			Depart:       at(t, "2025-12-15T08:00:00+07:00"),
			Arrive:       at(t, "2025-12-15T10:50:00+08:00"),
		}},
		Price:          model.Money{Amount: price, Currency: "IDR"},
		AvailableSeats: seats,
		CabinClass:     "economy",
		Amenities:      []string{},
	}
}

func request() model.SearchRequest {
	return model.SearchRequest{
		Origin:        "CGK",
		Destination:   "DPS",
		DepartureDate: "2025-12-15",
		Passengers:    1,
		CabinClass:    "economy",
	}
}

func TestAggregatesEveryProvider(t *testing.T) {
	service := New(DefaultConfig(),
		fakeClient{name: "A", flights: []model.Flight{flight(t, "A1", 500000, 10)}},
		fakeClient{name: "B", flights: []model.Flight{flight(t, "B1", 400000, 10), flight(t, "B2", 600000, 10)}},
	)

	got, err := service.Search(context.Background(), request())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Metadata.TotalResults != 3 {
		t.Errorf("total_results = %d, want 3", got.Metadata.TotalResults)
	}
	if got.Metadata.ProvidersQueried != 2 || got.Metadata.ProvidersSucceded != 2 || got.Metadata.ProvidersFailed != 0 {
		t.Errorf("metadata = %+v", got.Metadata)
	}
	if got.SearchCriteria.Origin != "CGK" || got.SearchCriteria.DepartureDate != "2025-12-15" {
		t.Errorf("criteria = %+v", got.SearchCriteria)
	}
}

func TestProvidersRunInParallel(t *testing.T) {
	const delay = 150 * time.Millisecond
	service := New(DefaultConfig(),
		fakeClient{name: "A", delay: delay, flights: []model.Flight{flight(t, "A1", 1, 10)}},
		fakeClient{name: "B", delay: delay, flights: []model.Flight{flight(t, "B1", 2, 10)}},
		fakeClient{name: "C", delay: delay, flights: []model.Flight{flight(t, "C1", 3, 10)}},
		fakeClient{name: "D", delay: delay, flights: []model.Flight{flight(t, "D1", 4, 10)}},
	)

	start := time.Now()
	got, err := service.Search(context.Background(), request())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Metadata.TotalResults != 4 {
		t.Errorf("total_results = %d, want 4", got.Metadata.TotalResults)
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("took %v for four %v providers; the fan-out is running sequentially", elapsed, delay)
	}
}

func TestPartialFailureKeepsTheRest(t *testing.T) {
	service := New(DefaultConfig(),
		fakeClient{name: "Healthy", flights: []model.Flight{flight(t, "H1", 500000, 10)}},
		fakeClient{name: "Broken", err: errors.New("provider exploded")},
	)

	got, err := service.Search(context.Background(), request())
	if err != nil {
		t.Fatalf("a single provider failure must not fail the search: %v", err)
	}
	if got.Metadata.ProvidersSucceded != 1 || got.Metadata.ProvidersFailed != 1 {
		t.Errorf("metadata = %+v", got.Metadata)
	}
	if got.Metadata.TotalResults != 1 {
		t.Errorf("total_results = %d, want 1", got.Metadata.TotalResults)
	}

	var broken *model.ProviderStatus
	for i := range got.Metadata.ProviderStatus {
		if got.Metadata.ProviderStatus[i].Provider == "Broken" {
			broken = &got.Metadata.ProviderStatus[i]
		}
	}
	if broken == nil {
		t.Fatal("the failed provider is missing from provider_status")
	}
	if broken.OK || broken.Error == "" {
		t.Errorf("failed status = %+v; the caller needs to know who failed and why", *broken)
	}
}

func TestAllProvidersFailingIsAnError(t *testing.T) {
	down := errors.New("down")
	service := New(DefaultConfig(),
		fakeClient{name: "A", err: down},
		fakeClient{name: "B", err: down},
	)

	if _, err := service.Search(context.Background(), request()); !errors.Is(err, ErrAllProvidersFailed) {
		t.Errorf("err = %v, want ErrAllProvidersFailed", err)
	}
}

func TestSlowProviderIsCutOffAtItsOwnTimeout(t *testing.T) {
	service := New(Config{ProviderTimeout: 50 * time.Millisecond, OverallTimeout: time.Second},
		fakeClient{name: "Fast", flights: []model.Flight{flight(t, "F1", 500000, 10)}},
		fakeClient{name: "Glacial", delay: 5 * time.Second, flights: []model.Flight{flight(t, "G1", 1, 10)}},
	)

	start := time.Now()
	got, err := service.Search(context.Background(), request())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("took %v; the per-provider timeout was not enforced", elapsed)
	}
	if got.Metadata.ProvidersFailed != 1 {
		t.Errorf("providers_failed = %d, want 1", got.Metadata.ProvidersFailed)
	}
	if got.Metadata.TotalResults != 1 {
		t.Errorf("total_results = %d, want only the fast provider's flight", got.Metadata.TotalResults)
	}
}

func TestResultsAreOrderedDeterministically(t *testing.T) {
	// Staggered delays so completion order differs from the expected output order.
	service := New(DefaultConfig(),
		fakeClient{name: "Slow", delay: 60 * time.Millisecond, flights: []model.Flight{flight(t, "cheap", 100000, 10)}},
		fakeClient{name: "Fast", flights: []model.Flight{flight(t, "pricey", 900000, 10)}},
	)

	for i := 0; i < 5; i++ {
		got, err := service.Search(context.Background(), request())
		if err != nil {
			t.Fatal(err)
		}
		if got.Flights[0].ID != "cheap" || got.Flights[1].ID != "pricey" {
			t.Fatalf("run %d order = %s, %s; want cheapest first regardless of who answered first",
				i, got.Flights[0].ID, got.Flights[1].ID)
		}
	}
}

func TestFlightsThatDoNotAnswerTheRequestAreDropped(t *testing.T) {
	wrongRoute := flight(t, "wrong-route", 500000, 10)
	wrongRoute.Segments[0].To = model.Place{Airport: "SUB", City: "Surabaya"}

	wrongDate := flight(t, "wrong-date", 500000, 10)
	wrongDate.Segments[0].Depart = at(t, "2025-12-20T08:00:00+07:00")
	wrongDate.Segments[0].Arrive = at(t, "2025-12-20T10:50:00+08:00")

	wrongCabin := flight(t, "wrong-cabin", 500000, 10)
	wrongCabin.CabinClass = "business"

	service := New(DefaultConfig(), fakeClient{
		name: "Mixed",
		flights: []model.Flight{
			wrongRoute, wrongDate, wrongCabin,
			flight(t, "too-few-seats", 500000, 1),
			flight(t, "keep", 500000, 10),
		},
	})

	req := request()
	req.Passengers = 2

	got, err := service.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.TotalResults != 1 {
		t.Fatalf("total_results = %d, want 1", got.Metadata.TotalResults)
	}
	if got.Flights[0].ID != "keep" {
		t.Errorf("kept %q, want keep", got.Flights[0].ID)
	}
	if got.Metadata.DroppedResults != 4 {
		t.Errorf("dropped_results = %d, want 4", got.Metadata.DroppedResults)
	}
}

func TestConnectingItineraryIsMatchedOnItsRealDestination(t *testing.T) {
	connecting := model.Flight{
		ID:           "GA315",
		Provider:     "Garuda Indonesia",
		FlightNumber: "GA315",
		Segments: []model.Segment{
			{
				FlightNumber: "GA315",
				From:         model.Place{Airport: "CGK", City: "Jakarta"},
				To:           model.Place{Airport: "SUB", City: "Surabaya"},
				Depart:       at(t, "2025-12-15T14:00:00+07:00"),
				Arrive:       at(t, "2025-12-15T15:30:00+07:00"),
			},
			{
				FlightNumber: "GA332",
				From:         model.Place{Airport: "SUB", City: "Surabaya"},
				To:           model.Place{Airport: "DPS", City: "Denpasar"},
				Depart:       at(t, "2025-12-15T17:15:00+07:00"),
				Arrive:       at(t, "2025-12-15T18:45:00+08:00"),
			},
		},
		Price:          model.Money{Amount: 1850000, Currency: "IDR"},
		AvailableSeats: 22,
		CabinClass:     "economy",
		Amenities:      []string{},
	}

	service := New(DefaultConfig(), fakeClient{name: "Garuda Indonesia", flights: []model.Flight{connecting}})

	got, err := service.Search(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.TotalResults != 1 {
		t.Fatalf("total_results = %d; a CGK-DPS connection via SUB must match", got.Metadata.TotalResults)
	}
	view := got.Flights[0]
	if view.Arrival.Airport != "DPS" || view.Stops != 1 || view.Duration.TotalMinutes != 225 {
		t.Errorf("view = arrival %s, stops %d, %d min; want DPS, 1, 225",
			view.Arrival.Airport, view.Stops, view.Duration.TotalMinutes)
	}
}

func TestRequestValidation(t *testing.T) {
	service := New(DefaultConfig(), fakeClient{name: "A", flights: []model.Flight{flight(t, "A1", 1, 10)}})
	roundTrip := "2025-12-22"

	for name, mutate := range map[string]func(*model.SearchRequest){
		"short origin":         func(r *model.SearchRequest) { r.Origin = "CG" },
		"missing destination":  func(r *model.SearchRequest) { r.Destination = "" },
		"same endpoints":       func(r *model.SearchRequest) { r.Destination = "CGK" },
		"missing date":         func(r *model.SearchRequest) { r.DepartureDate = "" },
		"malformed date":       func(r *model.SearchRequest) { r.DepartureDate = "15-12-2025" },
		"round trip requested": func(r *model.SearchRequest) { r.ReturnDate = &roundTrip },
	} {
		t.Run(name, func(t *testing.T) {
			req := request()
			mutate(&req)
			if _, err := service.Search(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestSearchTimeIsRecordedAndCacheIsNotClaimed(t *testing.T) {
	service := New(DefaultConfig(),
		fakeClient{name: "A", delay: 30 * time.Millisecond, flights: []model.Flight{flight(t, "A1", 1, 10)}})

	got, err := service.Search(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.SearchTimeMS < 25 {
		t.Errorf("search_time_ms = %d, expected it to reflect a 30ms provider", got.Metadata.SearchTimeMS)
	}
	if got.Metadata.CacheHit {
		t.Error("cache_hit = true, but no cache is wired yet")
	}
}

func TestEmptyResultIsAnArrayNotNull(t *testing.T) {
	got, err := New(DefaultConfig()).Search(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.TotalResults != 0 {
		t.Errorf("total_results = %d, want 0", got.Metadata.TotalResults)
	}
	// A client should be able to iterate without a nil check.
	if got.Flights == nil {
		t.Error("flights = null, want an empty array")
	}
}
