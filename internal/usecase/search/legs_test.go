package search

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
)

type routedClient struct {
	name    string
	byRoute map[string][]model.Flight
	calls   atomic.Int64
	delay   time.Duration
}

func (c *routedClient) Name() string { return c.name }

func (c *routedClient) FetchFlights(ctx context.Context, _ model.SearchRequest) ([]model.Flight, error) {
	c.calls.Add(1)
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var all []model.Flight
	for _, flights := range c.byRoute {
		all = append(all, flights...)
	}
	return all, nil
}

type legCache struct {
	mu      sync.Mutex
	entries map[string]Aggregate
}

func newLegCache() *legCache {
	return &legCache{entries: make(map[string]Aggregate)}
}

func (c *legCache) Get(key string) (Aggregate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.entries[key]
	return value, ok
}

func (c *legCache) Set(key string, value Aggregate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = value
}

func (c *legCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func routeFlight(t *testing.T, id, origin, destination, date string, price int64) model.Flight {
	t.Helper()
	depart := at(t, date+"T08:00:00+07:00")
	return model.Flight{
		ID:           id,
		Provider:     "Test",
		Airline:      model.Airline{Name: "Test Air", Code: "TT"},
		FlightNumber: id,
		Segments: []model.Segment{{
			FlightNumber: id,
			From:         model.Place{Airport: origin, City: origin},
			To:           model.Place{Airport: destination, City: destination},
			Depart:       depart,
			Arrive:       depart.Add(2 * time.Hour),
		}},
		Price:          model.Money{Amount: price, Currency: "IDR"},
		AvailableSeats: 10,
		CabinClass:     "economy",
		Amenities:      []string{},
	}
}

func twoWayClient(t *testing.T, name string) *routedClient {
	t.Helper()
	return &routedClient{
		name: name,
		byRoute: map[string][]model.Flight{
			"out": {
				routeFlight(t, "OUT1", "CGK", "DPS", "2025-12-15", 500000),
				routeFlight(t, "OUT2", "CGK", "DPS", "2025-12-15", 700000),
			},
			"back": {
				routeFlight(t, "BACK1", "DPS", "CGK", "2025-12-22", 600000),
			},
			"onward": {
				routeFlight(t, "ONWARD1", "DPS", "SUB", "2025-12-20", 400000),
			},
		},
	}
}

func roundTripRequest() model.SearchRequest {
	returnDate := "2025-12-22"
	return model.SearchRequest{
		Origin:        "CGK",
		Destination:   "DPS",
		DepartureDate: "2025-12-15",
		ReturnDate:    &returnDate,
		Passengers:    1,
		CabinClass:    "economy",
	}
}

func TestOneWayResponseKeepsTheFlatShape(t *testing.T) {
	service := New(DefaultConfig(), nil, twoWayClient(t, "A"))

	got, err := service.Search(context.Background(), request())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got.Legs != nil {
		t.Errorf("legs = %+v; a one-way search must keep the flat shape", got.Legs)
	}
	if len(got.Flights) != 2 {
		t.Fatalf("flights = %d, want 2 outbound results at top level", len(got.Flights))
	}
	if got.SearchCriteria.TripType != string(model.TripOneWay) {
		t.Errorf("trip_type = %q, want one_way", got.SearchCriteria.TripType)
	}
	if got.Metadata.TotalResults != 2 {
		t.Errorf("total_results = %d, want 2", got.Metadata.TotalResults)
	}
	if len(got.Metadata.ProviderStatus) != 1 {
		t.Errorf("provider_status = %+v, want one entry", got.Metadata.ProviderStatus)
	}
}

func TestRoundTripReturnsBothLegsSeparately(t *testing.T) {
	service := New(DefaultConfig(), nil, twoWayClient(t, "A"))

	got, err := service.Search(context.Background(), roundTripRequest())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got.SearchCriteria.TripType != string(model.TripRoundTrip) {
		t.Errorf("trip_type = %q, want round_trip", got.SearchCriteria.TripType)
	}
	if len(got.Legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(got.Legs))
	}

	outbound, inbound := got.Legs[0], got.Legs[1]

	if outbound.Leg != 1 || inbound.Leg != 2 {
		t.Errorf("leg numbers = %d, %d; want 1, 2", outbound.Leg, inbound.Leg)
	}
	if outbound.SearchCriteria.Origin != "CGK" || outbound.SearchCriteria.Destination != "DPS" {
		t.Errorf("outbound criteria = %+v", outbound.SearchCriteria)
	}
	if inbound.SearchCriteria.Origin != "DPS" || inbound.SearchCriteria.Destination != "CGK" {
		t.Errorf("inbound criteria = %+v", inbound.SearchCriteria)
	}
	if inbound.SearchCriteria.DepartureDate != "2025-12-22" {
		t.Errorf("inbound date = %q, want 2025-12-22", inbound.SearchCriteria.DepartureDate)
	}

	if len(outbound.Flights) != 2 {
		t.Errorf("outbound flights = %d, want 2", len(outbound.Flights))
	}
	if len(inbound.Flights) != 1 || inbound.Flights[0].ID != "BACK1" {
		t.Errorf("inbound flights = %+v, want just BACK1", inbound.Flights)
	}

	if len(got.Flights) != 0 {
		t.Errorf("top-level flights = %d, want none on a multi-leg search", len(got.Flights))
	}
	if got.Metadata.TotalResults != 3 {
		t.Errorf("summary total_results = %d, want 3 across both legs", got.Metadata.TotalResults)
	}
}

func TestScoresAreRelativeToTheirOwnLeg(t *testing.T) {
	service := New(DefaultConfig(), nil, twoWayClient(t, "A"))

	got, err := service.Search(context.Background(), roundTripRequest())
	if err != nil {
		t.Fatal(err)
	}

	if score := got.Legs[1].Flights[0].BestValueScore; score != 100 {
		t.Errorf("inbound score = %v, want 100 as the only option on its leg", score)
	}
	if got.Legs[0].Flights[0].ID != "OUT1" {
		t.Errorf("outbound leader = %q, want OUT1", got.Legs[0].Flights[0].ID)
	}
}

func TestMultiCityRunsEveryLeg(t *testing.T) {
	service := New(DefaultConfig(), nil, twoWayClient(t, "A"))

	req := model.SearchRequest{
		Legs: []model.Leg{
			{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"},
			{Origin: "DPS", Destination: "SUB", DepartureDate: "2025-12-20"},
			{Origin: "DPS", Destination: "CGK", DepartureDate: "2025-12-22"},
		},
		Passengers: 1,
		CabinClass: "economy",
	}

	got, err := service.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got.SearchCriteria.TripType != string(model.TripMultiCity) {
		t.Errorf("trip_type = %q, want multi_city", got.SearchCriteria.TripType)
	}
	if len(got.Legs) != 3 {
		t.Fatalf("legs = %d, want 3", len(got.Legs))
	}
	for i, want := range []int{2, 1, 1} {
		if got := len(got.Legs[i].Flights); got != want {
			t.Errorf("leg %d flights = %d, want %d", i+1, got, want)
		}
	}
}

func TestLegsRunConcurrently(t *testing.T) {
	const delay = 150 * time.Millisecond
	client := twoWayClient(t, "Slow")
	client.delay = delay

	service := New(DefaultConfig(), nil, client)

	req := model.SearchRequest{
		Legs: []model.Leg{
			{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"},
			{Origin: "DPS", Destination: "SUB", DepartureDate: "2025-12-20"},
			{Origin: "DPS", Destination: "CGK", DepartureDate: "2025-12-22"},
		},
		Passengers: 1,
		CabinClass: "economy",
	}

	start := time.Now()
	if _, err := service.Search(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	if elapsed > 350*time.Millisecond {
		t.Errorf("three %v legs took %v; they are running sequentially", delay, elapsed)
	}
}

func TestEachLegIsCachedIndependently(t *testing.T) {
	client := twoWayClient(t, "A")
	store := newLegCache()
	service := New(DefaultConfig(), store, client)

	if _, err := service.Search(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("provider called %d times for the one-way, want 1", got)
	}

	got, err := service.Search(context.Background(), roundTripRequest())
	if err != nil {
		t.Fatal(err)
	}

	if calls := client.calls.Load(); calls != 2 {
		t.Errorf("provider called %d times in total, want 2: the outbound leg should have hit the cache", calls)
	}
	if !got.Legs[0].Metadata.CacheHit {
		t.Error("outbound leg reported cache_hit=false despite being searched already")
	}
	if got.Legs[1].Metadata.CacheHit {
		t.Error("return leg reported cache_hit=true on its first search")
	}
	if got.Metadata.CacheHit {
		t.Error("summary cache_hit=true, but one leg was fetched")
	}

	if got := store.len(); got != 2 {
		t.Errorf("cache holds %d entries, want 2 (one per leg)", got)
	}
}

func TestOneFailedLegDoesNotDiscardTheOthers(t *testing.T) {
	healthy := twoWayClient(t, "Healthy")

	service := New(DefaultConfig(), nil, healthy, fakeClient{name: "Broken", err: errors.New("down")})

	got, err := service.Search(context.Background(), roundTripRequest())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(got.Legs))
	}
	for i, leg := range got.Legs {
		if leg.Metadata.ProvidersFailed != 1 || leg.Metadata.ProvidersSucceded != 1 {
			t.Errorf("leg %d metadata = %+v, want one failed and one succeeded", i+1, leg.Metadata)
		}
	}
	if got.Metadata.ProvidersQueried != 4 {
		t.Errorf("summary providers_queried = %d, want 4 (two providers across two legs)",
			got.Metadata.ProvidersQueried)
	}
}

func TestEveryLegFailingIsAnError(t *testing.T) {
	service := New(DefaultConfig(), nil, fakeClient{name: "A", err: errors.New("down")})

	if _, err := service.Search(context.Background(), roundTripRequest()); !errors.Is(err, ErrAllProvidersFailed) {
		t.Errorf("err = %v, want ErrAllProvidersFailed", err)
	}
}

func TestFiltersApplyToEveryLeg(t *testing.T) {
	service := New(DefaultConfig(), nil, twoWayClient(t, "A"))

	req := roundTripRequest()
	req.Filters = model.Filters{MaxPrice: 550000}

	got, err := service.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Legs[0].Flights) != 1 || got.Legs[0].Flights[0].ID != "OUT1" {
		t.Errorf("outbound = %+v, want only the 500000 fare", got.Legs[0].Flights)
	}
	if len(got.Legs[1].Flights) != 0 {
		t.Errorf("inbound = %+v, want none under a 550000 ceiling", got.Legs[1].Flights)
	}
	if got.Legs[1].Metadata.FilteredResults != 1 {
		t.Errorf("inbound filtered_results = %d, want 1", got.Legs[1].Metadata.FilteredResults)
	}
}
