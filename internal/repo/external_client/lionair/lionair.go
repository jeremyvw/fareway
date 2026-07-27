package lionair

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/normalize"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

//go:embed lion_air_search_response.json
var mockResponse []byte

// ProviderName is how this provider is reported in results and metadata.
const ProviderName = "Lion Air"

// The assignment specifies Lion Air as the medium-latency provider.
const (
	defaultMinDelay = 100 * time.Millisecond
	defaultMaxDelay = 200 * time.Millisecond
)

// CityResolver maps an IATA airport code to a city name. Lion Air names the cities at both
// ends of the route but not its layover airports, so the fallback is still needed.
type CityResolver func(iata string) (string, bool)

// Client is the Lion Air external client.
type Client struct {
	minDelay time.Duration
	maxDelay time.Duration
	city     CityResolver

	mu  sync.Mutex
	rng *rand.Rand
}

// Option configures a Client.
type Option func(*Client)

// WithoutDelay removes the simulated latency, for tests.
func WithoutDelay() Option {
	return func(c *Client) { c.minDelay, c.maxDelay = 0, 0 }
}

// WithDelay overrides the simulated latency window.
func WithDelay(min, max time.Duration) Option {
	return func(c *Client) { c.minDelay, c.maxDelay = min, max }
}

// WithRand injects a seeded source so latency is reproducible.
func WithRand(rng *rand.Rand) Option {
	return func(c *Client) { c.rng = rng }
}

// New builds a Lion Air client.
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

// FetchFlights returns every itinerary the provider knows about, normalized.
func (c *Client) FetchFlights(ctx context.Context, _ model.SearchRequest) ([]model.Flight, error) {
	if err := c.simulateLatency(ctx); err != nil {
		return nil, err
	}

	var payload searchResponse
	if err := json.Unmarshal(mockResponse, &payload); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", ProviderName, err)
	}
	if !payload.Success {
		return nil, fmt.Errorf("%s: provider reported failure", ProviderName)
	}

	flights := make([]model.Flight, 0, len(payload.Data.AvailableFlights))
	for _, raw := range payload.Data.AvailableFlights {
		f, err := c.normalizeFlight(raw)
		if err != nil {
			continue
		}
		flights = append(flights, f)
	}
	return flights, nil
}

// simulateLatency waits out the provider's response time while staying cancellable, so a
// caller's per-provider timeout is enforceable.
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

// normalizeFlight converts one Lion Air itinerary into the shared model.
//
// The timestamps are the interesting part: they carry no offset, only a sibling IANA zone
// name. Parsed without their location they would read as UTC and every flight would shift
// by seven to nine hours, which is why ParseInZone is mandatory here.
func (c *Client) normalizeFlight(raw flight) (model.Flight, error) {
	depart, err := timeutil.ParseInZone(raw.Schedule.Departure, raw.Schedule.DepartureTimezone)
	if err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.ID, err)
	}
	arrive, err := timeutil.ParseInZone(raw.Schedule.Arrival, raw.Schedule.ArrivalTimezone)
	if err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.ID, err)
	}

	out := model.Flight{
		ID:           normalize.ID(raw.ID, ProviderName),
		Provider:     ProviderName,
		Airline:      model.Airline{Name: raw.Carrier.Name, Code: raw.Carrier.IATA},
		FlightNumber: raw.ID,
		Segments: []model.Segment{{
			FlightNumber: raw.ID,
			From:         c.place(raw.Route.From.Code, raw.Route.From.City),
			To:           c.place(raw.Route.To.Code, raw.Route.To.City),
			Depart:       depart,
			Arrive:       arrive,
		}},
		Stopovers:      c.stopovers(raw),
		Price:          model.Money{Amount: raw.Pricing.Total, Currency: raw.Pricing.Currency},
		AvailableSeats: raw.SeatsLeft,
		CabinClass:     strings.ToLower(strings.TrimSpace(raw.Pricing.FareType)),
		Aircraft:       normalize.OptionalString(raw.PlaneType),
		Amenities:      amenities(raw.Services),
		Baggage: model.Baggage{
			CarryOn: strings.TrimSpace(raw.Services.BaggageAllowance.Cabin),
			Checked: strings.TrimSpace(raw.Services.BaggageAllowance.Hold),
		},
	}

	c.recordDiscrepancies(&out, raw)

	if err := out.Validate(); err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.ID, err)
	}
	return out, nil
}

// stopovers records the intermediate stops. Lion Air gives an airport and a wait but no
// timestamps for the legs either side, so this is as much detail as exists.
func (c *Client) stopovers(raw flight) []model.Stopover {
	if len(raw.Layovers) == 0 {
		return nil
	}
	stops := make([]model.Stopover, 0, len(raw.Layovers))
	for _, l := range raw.Layovers {
		stops = append(stops, model.Stopover{
			Airport:     c.place(l.Airport, ""),
			WaitMinutes: l.DurationMinutes,
		})
	}
	return stops
}

// recordDiscrepancies notes where Lion Air's declared figures disagree with its own
// schedule or with each other.
func (c *Client) recordDiscrepancies(out *model.Flight, raw flight) {
	if raw.FlightTime > 0 && raw.FlightTime != out.TotalMinutes() {
		out.Warn("provider declared %d min but the schedule gives %d min",
			raw.FlightTime, out.TotalMinutes())
	}
	if raw.IsDirect != (out.Stops() == 0) {
		out.Warn("provider declared is_direct=%t but the itinerary has %d stop(s)",
			raw.IsDirect, out.Stops())
	}
	if raw.StopCount != 0 && raw.StopCount != out.Stops() {
		out.Warn("provider declared %d stop(s) but listed %d layover(s)",
			raw.StopCount, len(raw.Layovers))
	}
}

// place attaches a city to an airport code, preferring what the provider supplied.
func (c *Client) place(code, city string) model.Place {
	if strings.TrimSpace(city) != "" {
		return model.Place{Airport: code, City: city}
	}
	if c.city != nil {
		if resolved, ok := c.city(code); ok {
			return model.Place{Airport: code, City: resolved}
		}
	}
	return model.Place{Airport: code}
}

// amenities flattens Lion Air's booleans into the same lower-cased vocabulary the other
// providers use as lists, so an amenity filter behaves identically across providers.
func amenities(s services) []string {
	var out []string
	if s.WifiAvailable {
		out = append(out, "wifi")
	}
	if s.MealsIncluded {
		out = append(out, "meal")
	}
	return normalize.Amenities(out)
}

// ErrUnusable marks a record we could not normalize. Kept for callers that want to
// distinguish a bad record from a transport failure.
var ErrUnusable = errors.New("unusable flight record")
