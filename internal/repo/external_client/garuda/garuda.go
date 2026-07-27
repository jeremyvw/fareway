package garuda

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/normalize"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

// mockResponse stands in for Garuda's HTTP API. go:embed cannot reach outside the package
// directory, which is why each provider keeps its own fixture alongside its client.
//
//go:embed garuda_indonesia_search_response.json
var mockResponse []byte

// ProviderName is how this provider is reported in results and metadata.
const ProviderName = "Garuda Indonesia"

// The assignment specifies Garuda as the fast provider.
const (
	defaultMinDelay = 50 * time.Millisecond
	defaultMaxDelay = 100 * time.Millisecond
)

// CityResolver maps an IATA airport code to a city name.
//
// Garuda names cities on its top-level endpoints but not inside segments, so the final
// airport of a connecting itinerary has no city attached. The dependency is a function
// rather than a package import so this client stays testable on its own.
type CityResolver func(iata string) (string, bool)

// Client is the Garuda external client.
type Client struct {
	minDelay time.Duration
	maxDelay time.Duration
	city     CityResolver

	// mu guards rng: one Client is shared across concurrent searches, and rand.Rand is
	// not safe for concurrent use.
	mu  sync.Mutex
	rng *rand.Rand
}

// Option configures a Client.
type Option func(*Client)

// WithoutDelay removes the simulated latency. Tests use it so they assert normalization
// rather than waiting on a stopwatch.
func WithoutDelay() Option {
	return func(c *Client) { c.minDelay, c.maxDelay = 0, 0 }
}

// WithDelay overrides the simulated latency window.
func WithDelay(min, max time.Duration) Option {
	return func(c *Client) { c.minDelay, c.maxDelay = min, max }
}

// WithRand injects a seeded source so latency is reproducible in tests.
func WithRand(rng *rand.Rand) Option {
	return func(c *Client) { c.rng = rng }
}

// New builds a Garuda client. city may be nil, in which case cities are simply absent
// where the provider does not supply them.
func New(city CityResolver, opts ...Option) *Client {
	c := &Client{
		minDelay: defaultMinDelay,
		maxDelay: defaultMaxDelay,
		city:     city,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name identifies the provider.
func (c *Client) Name() string { return ProviderName }

// FetchFlights returns every itinerary the provider knows about, normalized. Route and
// date matching belong to the aggregator, so this layer's only job is "call the API and
// translate the reply".
func (c *Client) FetchFlights(ctx context.Context, _ model.SearchRequest) ([]model.Flight, error) {
	if err := c.simulateLatency(ctx); err != nil {
		return nil, err
	}

	var payload searchResponse
	if err := json.Unmarshal(mockResponse, &payload); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", ProviderName, err)
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("%s: provider reported status %q", ProviderName, payload.Status)
	}

	flights := make([]model.Flight, 0, len(payload.Flights))
	for _, raw := range payload.Flights {
		f, err := c.normalize(raw)
		if err != nil {
			// One unusable record must not cost us the other results.
			continue
		}
		flights = append(flights, f)
	}
	return flights, nil
}

// simulateLatency waits out the provider's response time while staying cancellable.
//
// A bare time.Sleep here would make the caller's per-provider timeout unenforceable: the
// goroutine would run to completion regardless, and the aggregator's WaitGroup would wait
// for it. The select is what turns the timeout budget into something real.
func (c *Client) simulateLatency(ctx context.Context) error {
	d := c.pickDelay()
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", ProviderName, ctx.Err())
	}
}

func (c *Client) pickDelay() time.Duration {
	if c.maxDelay <= c.minDelay {
		return c.minDelay
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.minDelay + time.Duration(c.rng.Int63n(int64(c.maxDelay-c.minDelay)))
}

// normalize converts one Garuda itinerary into the shared model.
func (c *Client) normalize(raw flight) (model.Flight, error) {
	// The top-level endpoints are the only place Garuda names cities, so harvest them
	// before deriving the real route from segments.
	cities := map[string]string{
		raw.Departure.Airport: raw.Departure.City,
		raw.Arrival.Airport:   raw.Arrival.City,
	}

	out := model.Flight{
		ID:             normalize.ID(raw.FlightID, ProviderName),
		Provider:       ProviderName,
		Airline:        model.Airline{Name: raw.Airline, Code: raw.AirlineCode},
		FlightNumber:   raw.FlightID,
		Price:          model.Money{Amount: raw.Price.Amount, Currency: raw.Price.Currency},
		AvailableSeats: raw.AvailableSeats,
		CabinClass:     strings.ToLower(strings.TrimSpace(raw.FareClass)),
		Aircraft:       normalize.OptionalString(raw.Aircraft),
		Amenities:      normalize.Amenities(raw.Amenities),
		Baggage: model.Baggage{
			CarryOn: pieces(raw.Baggage.CarryOn),
			Checked: pieces(raw.Baggage.Checked),
		},
	}

	segments, err := c.buildSegments(raw, cities)
	if err != nil {
		return model.Flight{}, err
	}
	out.Segments = segments

	c.recordDiscrepancies(&out, raw)

	if err := out.Validate(); err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightID, err)
	}
	return out, nil
}

// buildSegments prefers the segments array, which is the only complete view of a
// connecting itinerary, and falls back to the top-level endpoints for direct flights.
func (c *Client) buildSegments(raw flight, cities map[string]string) ([]model.Segment, error) {
	if len(raw.Segments) == 0 {
		depart, err := timeutil.ParseOffset(raw.Departure.Time)
		if err != nil {
			return nil, err
		}
		arrive, err := timeutil.ParseOffset(raw.Arrival.Time)
		if err != nil {
			return nil, err
		}
		return []model.Segment{{
			FlightNumber: raw.FlightID,
			From:         c.place(raw.Departure.Airport, cities),
			To:           c.place(raw.Arrival.Airport, cities),
			Depart:       depart,
			Arrive:       arrive,
		}}, nil
	}

	segments := make([]model.Segment, 0, len(raw.Segments))
	for i, seg := range raw.Segments {
		depart, err := timeutil.ParseOffset(seg.Departure.Time)
		if err != nil {
			return nil, fmt.Errorf("segment %d: %w", i, err)
		}
		arrive, err := timeutil.ParseOffset(seg.Arrival.Time)
		if err != nil {
			return nil, fmt.Errorf("segment %d: %w", i, err)
		}
		segments = append(segments, model.Segment{
			FlightNumber: seg.FlightNumber,
			From:         c.place(seg.Departure.Airport, cities),
			To:           c.place(seg.Arrival.Airport, cities),
			Depart:       depart,
			Arrive:       arrive,
		})
	}
	return segments, nil
}

// recordDiscrepancies notes where Garuda's declared figures disagree with its own
// timestamps. We keep the computed values and report the conflict rather than dropping a
// flight over a provider's bookkeeping.
//
// Only per-segment durations and the stop count are checked. The top-level duration is
// deliberately left alone: on a connecting itinerary it legitimately describes the first
// leg, so comparing it to the whole journey would raise a false alarm on every connection.
func (c *Client) recordDiscrepancies(out *model.Flight, raw flight) {
	if len(raw.Segments) > 0 && raw.Stops != out.Stops() {
		out.Warn("provider declared %d stop(s) but the itinerary has %d", raw.Stops, out.Stops())
	}

	if len(raw.Segments) == 0 {
		if raw.DurationMinutes > 0 && raw.DurationMinutes != out.TotalMinutes() {
			out.Warn("provider declared %d min but timestamps give %d min",
				raw.DurationMinutes, out.TotalMinutes())
		}
		return
	}

	for i, seg := range raw.Segments {
		if i >= len(out.Segments) {
			break
		}
		if computed := out.Segments[i].Minutes(); seg.DurationMinutes > 0 && seg.DurationMinutes != computed {
			out.Warn("segment %d (%s) declared %d min but timestamps give %d min",
				i, seg.FlightNumber, seg.DurationMinutes, computed)
		}
		if i == 0 || seg.LayoverMinutes == 0 {
			continue
		}
		computed := timeutil.DurationMinutes(out.Segments[i-1].Arrive, out.Segments[i].Depart)
		if seg.LayoverMinutes != computed {
			out.Warn("segment %d declared a %d min layover but timestamps give %d min",
				i, seg.LayoverMinutes, computed)
		}
	}
}

// place attaches a city to an airport code, preferring what the provider supplied.
func (c *Client) place(code string, cities map[string]string) model.Place {
	if city := cities[code]; city != "" {
		return model.Place{Airport: code, City: city}
	}
	if c.city != nil {
		if city, ok := c.city(code); ok {
			return model.Place{Airport: code, City: city}
		}
	}
	return model.Place{Airport: code}
}

// pieces renders Garuda's piece-count baggage allowance as prose, since the other
// providers express baggage as weights or free text and the shared model is a string.
func pieces(n int) string {
	switch {
	case n <= 0:
		return "Not included"
	case n == 1:
		return "1 piece"
	default:
		return fmt.Sprintf("%d pieces", n)
	}
}
