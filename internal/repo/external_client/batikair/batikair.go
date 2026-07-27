package batikair

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyvw/fareway/internal/model"
	"github.com/jeremyvw/fareway/internal/util/normalize"
	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

//go:embed batik_air_search_response.json
var mockResponse []byte

// ProviderName is how this provider is reported in results and metadata.
const ProviderName = "Batik Air"

// The assignment specifies Batik Air as the slowest provider.
const (
	defaultMinDelay = 200 * time.Millisecond
	defaultMaxDelay = 400 * time.Millisecond
)

// CityResolver maps an IATA airport code to a city name. Batik Air sends bare codes and no
// city names at all, so this is the only source of them.
type CityResolver func(iata string) (string, bool)

// Client is the Batik Air external client.
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

// New builds a Batik Air client.
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
	// The provider repeats an HTTP status in the body; anything outside 2xx is a failure
	// even though the transport succeeded.
	if payload.Code < 200 || payload.Code > 299 {
		return nil, fmt.Errorf("%s: provider returned code %d (%s)", ProviderName, payload.Code, payload.Message)
	}

	flights := make([]model.Flight, 0, len(payload.Results))
	for _, raw := range payload.Results {
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

// normalizeFlight converts one Batik Air itinerary into the shared model.
func (c *Client) normalizeFlight(raw flight) (model.Flight, error) {
	depart, err := timeutil.ParseCompactOffset(raw.DepartureDateTime)
	if err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightNumber, err)
	}
	arrive, err := timeutil.ParseCompactOffset(raw.ArrivalDateTime)
	if err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightNumber, err)
	}

	carryOn, checked := splitBaggage(raw.BaggageInfo)

	out := model.Flight{
		ID:           normalize.ID(raw.FlightNumber, ProviderName),
		Provider:     ProviderName,
		Airline:      model.Airline{Name: raw.AirlineName, Code: raw.AirlineIATA},
		FlightNumber: raw.FlightNumber,
		Segments: []model.Segment{{
			FlightNumber: raw.FlightNumber,
			From:         c.place(raw.Origin),
			To:           c.place(raw.Destination),
			Depart:       depart,
			Arrive:       arrive,
		}},
		Stopovers: c.stopovers(raw),
		// TotalPrice, not BasePrice: the latter excludes tax and would undercut every
		// comparison against providers that quote an all-in fare.
		Price:          model.Money{Amount: raw.Fare.TotalPrice, Currency: raw.Fare.CurrencyCode},
		AvailableSeats: raw.SeatsAvailable,
		CabinClass:     cabinFromBookingClass(raw.Fare.Class),
		Aircraft:       normalize.OptionalString(raw.AircraftModel),
		Amenities:      normalize.Amenities(raw.OnboardServices),
		Baggage:        model.Baggage{CarryOn: carryOn, Checked: checked},
	}

	c.recordDiscrepancies(&out, raw)

	if err := out.Validate(); err != nil {
		return model.Flight{}, fmt.Errorf("%s: flight %s: %w", ProviderName, raw.FlightNumber, err)
	}
	return out, nil
}

// stopovers records the intermediate stops. Batik Air states the wait as prose ("55m").
func (c *Client) stopovers(raw flight) []model.Stopover {
	if len(raw.Connections) == 0 {
		return nil
	}
	stops := make([]model.Stopover, 0, len(raw.Connections))
	for _, conn := range raw.Connections {
		stops = append(stops, model.Stopover{
			Airport:     c.place(conn.StopAirport),
			WaitMinutes: parseProseDuration(conn.StopDuration),
		})
	}
	return stops
}

// recordDiscrepancies notes where Batik Air's declared figures disagree with its own
// timestamps. ID7042 in the sample data declares "3h 5m" against 245 real minutes, so this
// path is exercised by the provider's own data.
func (c *Client) recordDiscrepancies(out *model.Flight, raw flight) {
	if declared := parseProseDuration(raw.TravelTime); declared > 0 && declared != out.TotalMinutes() {
		out.Warn("provider declared %q (%d min) but timestamps give %d min",
			raw.TravelTime, declared, out.TotalMinutes())
	}
	if raw.NumberOfStops != out.Stops() {
		out.Warn("provider declared %d stop(s) but listed %d connection(s)",
			raw.NumberOfStops, len(raw.Connections))
	}
	if raw.Fare.BasePrice+raw.Fare.Taxes != raw.Fare.TotalPrice {
		out.Warn("fare breakdown does not add up: %d + %d != %d",
			raw.Fare.BasePrice, raw.Fare.Taxes, raw.Fare.TotalPrice)
	}
}

// place attaches a city to an airport code. Batik Air supplies none, so this always falls
// through to the resolver.
func (c *Client) place(code string) model.Place {
	if c.city != nil {
		if city, ok := c.city(code); ok {
			return model.Place{Airport: code, City: city}
		}
	}
	return model.Place{Airport: code}
}

// proseDuration matches the provider's human-readable durations: "1h 45m", "3h 5m", "55m".
var proseDuration = regexp.MustCompile(`^(?:(\d+)\s*h)?\s*(?:(\d+)\s*m)?$`)

// parseProseDuration reads a prose duration into minutes, returning 0 when it cannot be
// understood. Zero is safe: callers treat a declared duration as advisory and fall back to
// the timestamps, so an unparseable value costs a warning rather than a wrong answer.
func parseProseDuration(value string) int {
	match := proseDuration.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if match == nil {
		return 0
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	return hours*60 + minutes
}

// bookingClasses maps IATA booking-class letters to cabin names. Batik Air is the only
// provider that sends a letter rather than a cabin name.
var bookingClasses = map[string]string{
	"Y": "economy",
	"W": "premium_economy",
	"C": "business",
	"J": "business",
	"F": "first",
}

// cabinFromBookingClass resolves a booking-class letter, falling back to the raw value
// lower-cased rather than guessing at economy — a wrong cabin would silently mismatch the
// caller's requested class.
func cabinFromBookingClass(class string) string {
	key := strings.ToUpper(strings.TrimSpace(class))
	if cabin, ok := bookingClasses[key]; ok {
		return cabin
	}
	return strings.ToLower(key)
}

// splitBaggage unpacks the single baggage string ("7kg cabin, 20kg checked") into the two
// allowances the output contract expects. Unrecognized fragments are dropped rather than
// guessed at.
func splitBaggage(info string) (carryOn, checked string) {
	for _, part := range strings.Split(info, ",") {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		switch {
		case strings.Contains(lower, "cabin"):
			carryOn = strings.TrimSpace(strings.ReplaceAll(lower, "cabin", ""))
		case strings.Contains(lower, "checked"), strings.Contains(lower, "hold"):
			checked = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(lower, "checked", ""), "hold", ""))
		}
	}
	return carryOn, checked
}
