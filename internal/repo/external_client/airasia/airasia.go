package airasia

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/normalize"
	"github.com/jeremyvw/fareway/internal/util/retry"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

//go:embed airasia_search_response.json
var mockResponse []byte

const ProviderName = "AirAsia"

const currencyCode = "IDR"

const (
	defaultMinDelay    = 50 * time.Millisecond
	defaultMaxDelay    = 150 * time.Millisecond
	defaultFailureRate = 0.10
)

var ErrUnavailable = errors.New("provider temporarily unavailable")

type CityResolver func(iata string) (string, bool)

type Client struct {
	minDelay    time.Duration
	maxDelay    time.Duration
	failureRate float64
	retry       retry.Config
	city        CityResolver

	// mu guards rng, which drives both the latency and the failure decision. One Client is
	// shared across concurrent searches and rand.Rand is not safe for concurrent use.
	mu  sync.Mutex
	rng *rand.Rand
}

type Option func(*Client)

// WithoutDelay removes the simulated latency, for tests.
func WithoutDelay() Option {
	return func(c *Client) { c.minDelay, c.maxDelay = 0, 0 }
}

// WithDelay overrides the simulated latency window.
func WithDelay(min, max time.Duration) Option {
	return func(c *Client) { c.minDelay, c.maxDelay = min, max }
}

// WithFailureRate overrides the simulated failure probability. Tests use 0 for a reliable
// provider and 1 to force the failure path deterministically.
func WithFailureRate(rate float64) Option {
	return func(c *Client) { c.failureRate = rate }
}

// WithRetry overrides the backoff policy.
func WithRetry(cfg retry.Config) Option {
	return func(c *Client) { c.retry = cfg }
}

// WithRand injects a seeded source, so which calls fail is reproducible.
func WithRand(rng *rand.Rand) Option {
	return func(c *Client) { c.rng = rng }
}

// New builds an AirAsia client.
func New(city CityResolver, opts ...Option) *Client {
	c := &Client{
		minDelay:    defaultMinDelay,
		maxDelay:    defaultMaxDelay,
		failureRate: defaultFailureRate,
		retry:       retry.Default(),
		city:        city,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name identifies the provider.
func (c *Client) Name() string { return ProviderName }

// FetchFlights returns every itinerary the provider knows about, normalized.
func (c *Client) FetchFlights(ctx context.Context, req model.SearchRequest) ([]model.Flight, error) {
	return retry.Do(ctx, c.retry, func(ctx context.Context) ([]model.Flight, error) {
		return c.fetchOnce(ctx, req)
	})
}

// fetchOnce is a single attempt: latency, then the coin flip, then the response.
func (c *Client) fetchOnce(ctx context.Context, _ model.SearchRequest) ([]model.Flight, error) {
	if err := c.simulateLatency(ctx); err != nil {
		return nil, err
	}
	if c.shouldFail() {
		return nil, fmt.Errorf("%s: %w", ProviderName, ErrUnavailable)
	}

	var payload searchResponse
	if err := json.Unmarshal(mockResponse, &payload); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", ProviderName, err)
	}
	if payload.Status != "ok" {
		return nil, fmt.Errorf("%s: provider reported status %q", ProviderName, payload.Status)
	}

	flights := make([]model.Flight, 0, len(payload.Flights))
	for _, raw := range payload.Flights {
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

func (c *Client) shouldFail() bool {
	if c.failureRate <= 0 {
		return false
	}
	if c.failureRate >= 1 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rng.Float64() < c.failureRate
}

// normalizeFlight converts one AirAsia itinerary into the shared model.
func (c *Client) normalizeFlight(raw flight) (model.Flight, error) {
	depart, err := timeutil.ParseOffset(raw.DepartTime)
	if err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightCode, err)
	}
	arrive, err := timeutil.ParseOffset(raw.ArriveTime)
	if err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightCode, err)
	}

	carryOn, checked := splitBaggageNote(raw.BaggageNote)

	out := model.Flight{
		ID:       normalize.ID(raw.FlightCode, ProviderName),
		Provider: ProviderName,
		Airline: model.Airline{
			Name: raw.Airline,
			// AirAsia ships no IATA code, so it comes off the flight number: QZ7250 -> QZ.
			Code: normalize.CarrierCodeFromFlightNumber(raw.FlightCode),
		},
		FlightNumber: raw.FlightCode,
		Segments: []model.Segment{{
			FlightNumber: raw.FlightCode,
			From:         c.place(raw.FromAirport),
			To:           c.place(raw.ToAirport),
			Depart:       depart,
			Arrive:       arrive,
		}},
		Stopovers:      c.stopovers(raw),
		Price:          model.Money{Amount: raw.PriceIDR, Currency: currencyCode},
		AvailableSeats: raw.Seats,
		CabinClass:     strings.ToLower(strings.TrimSpace(raw.CabinClass)),
		// AirAsia states no aircraft type at all, so this stays nil and serializes as null.
		Aircraft:  nil,
		Amenities: normalize.Amenities(nil),
		Baggage:   model.Baggage{CarryOn: carryOn, Checked: checked},
	}

	c.recordDiscrepancies(&out, raw)

	if err := out.Validate(); err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightCode, err)
	}
	return out, nil
}

func (c *Client) stopovers(raw flight) []model.Stopover {
	if len(raw.Stops) == 0 {
		return nil
	}
	stops := make([]model.Stopover, 0, len(raw.Stops))
	for _, s := range raw.Stops {
		stops = append(stops, model.Stopover{
			Airport:     c.place(s.Airport),
			WaitMinutes: s.WaitTimeMinutes,
		})
	}
	return stops
}

// recordDiscrepancies notes where AirAsia's declared figures disagree with its timestamps.
//
// The declared duration is in fractional hours, so it is compared at whole-minute
// resolution: 4.33 hours is 259.8 minutes and must not be reported as conflicting with a
// computed 260.
func (c *Client) recordDiscrepancies(out *model.Flight, raw flight) {
	if raw.DurationHours > 0 {
		declared := int(math.Round(raw.DurationHours * 60))
		if declared != out.TotalMinutes() {
			out.Warn("provider declared %.2fh (%d min) but timestamps give %d min",
				raw.DurationHours, declared, out.TotalMinutes())
		}
	}
	if raw.DirectFlight != (out.Stops() == 0) {
		out.Warn("provider declared direct_flight=%t but the itinerary has %d stop(s)",
			raw.DirectFlight, out.Stops())
	}
}

// place attaches a city to an airport code. AirAsia supplies none, so this always falls
// through to the resolver.
func (c *Client) place(code string) model.Place {
	if c.city != nil {
		if city, ok := c.city(code); ok {
			return model.Place{Airport: code, City: city}
		}
	}
	return model.Place{Airport: code}
}

// splitBaggageNote unpacks the free-text allowance into the two fields the output contract
// expects: "Cabin baggage only, checked bags additional fee" becomes "Cabin baggage only"
// and "Additional fee".
//
// This is best-effort by nature — the provider sends prose, not data — so an unrecognized
// fragment is dropped rather than guessed at, and the exact fixture wording is pinned by a
// test.
func splitBaggageNote(note string) (carryOn, checked string) {
	for _, part := range strings.Split(note, ",") {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		switch {
		case strings.Contains(lower, "cabin"):
			carryOn = capitalize(part)
		case strings.Contains(lower, "checked"):
			checked = capitalize(strings.TrimSpace(strings.ReplaceAll(lower, "checked bags", "")))
		}
	}
	return carryOn, checked
}

// capitalize upper-cases the first letter, leaving the rest alone.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
