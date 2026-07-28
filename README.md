# fareway

A flight search and aggregation service. It queries four airline provider APIs in parallel,
normalizes four very different response formats into one, and returns filtered, compared,
ranked and cached results.

Written in Go with **no third-party dependencies** — standard library only.

---

## What is implemented

| Requirement | Where |
| --- | --- |
| Aggregate flight data from multiple sources | `usecase/search/search.go` — parallel fan-out over four providers |
| Normalize into a common format | `repo/external_client/*` — one adapter per provider |
| Handle different response structures | Four DTO shapes, four timestamp formats, three baggage formats |
| Search by origin, destination, date | `usecase/search/search.go` — matched on the *effective* route |
| Filter by price, stops, times, airlines, duration | `usecase/search/filter.go` |
| Sort by price, duration, departure, arrival | `usecase/search/sort.go` |
| Compare prices across providers | `usecase/search/compare.go` |
| Total trip duration including layovers | `model/model.go` — derived, gate-to-gate |
| Rank by "best value" | `usecase/search/ranking.go` |
| Handle data inconsistencies | Computed durations override declared ones; conflicts reported as warnings |
| Timezones (WIB / WITA / WIT) | `util/timeutil` + `util/airport`, IANA database embedded |
| Missing optional fields | `null` aircraft, `[]` amenities, derived airline codes and city names |
| Validation (arrival after departure, legs connect) | `model/validate.go` |
| Caching | `util/cache` + `usecase/search` — `cache_hit` in the output contract |
| Retry with exponential backoff | `util/retry`, wired into the flaky provider |
| Parallel queries with timeout handling | `usecase/search/search.go` |
| IDR formatting with thousands separators | `util/currency` |

---

## Setup

Requires Go 1.21 or newer. `go.mod` declares toolchain `1.26.5`, and with `GOTOOLCHAIN=auto` —
the default — the correct toolchain is fetched automatically if your local Go is older.

```bash
git clone https://github.com/jeremyvw/fareway.git
cd fareway
make check      # format, vet, and run every test with the race detector
make run        # serve on :8080
```

Override the listen address with `FAREWAY_ADDR=:9000 make run`.

`make help` lists all targets: `run`, `build`, `test`, `cover`, `vet`, `fmt`, `tidy`, `check`,
`clean`.

No database, no external services, no configuration file. The four provider APIs are mocked
in-process from the supplied JSON fixtures, each embedded in its own client package.

---

## Usage

### `POST /api/v1/flights/search`

```bash
curl -s -X POST localhost:8080/api/v1/flights/search \
  -H 'Content-Type: application/json' \
  -d '{
    "origin": "CGK",
    "destination": "DPS",
    "departureDate": "2025-12-15",
    "returnDate": null,
    "passengers": 1,
    "cabinClass": "economy"
  }' | jq
```

Response:

```json
{
  "search_criteria": {
    "origin": "CGK",
    "destination": "DPS",
    "departure_date": "2025-12-15",
    "passengers": 1,
    "cabin_class": "economy"
  },
  "metadata": {
    "total_results": 13,
    "providers_queried": 4,
    "providers_succeeded": 4,
    "providers_failed": 0,
    "search_time_ms": 384,
    "cache_hit": false,
    "provider_status": [
      { "provider": "Garuda Indonesia", "ok": true, "results": 3, "duration_ms": 84 },
      { "provider": "Lion Air",         "ok": true, "results": 3, "duration_ms": 195 },
      { "provider": "Batik Air",        "ok": true, "results": 3, "duration_ms": 384 },
      { "provider": "AirAsia",          "ok": true, "results": 4, "duration_ms": 138 }
    ],
    "sorted_by": "best_value"
  },
  "flights": [
    {
      "id": "QZ532_AirAsia",
      "provider": "AirAsia",
      "airline": { "name": "AirAsia", "code": "QZ" },
      "flight_number": "QZ532",
      "departure": {
        "airport": "CGK",
        "city": "Jakarta",
        "datetime": "2025-12-15T19:30:00+07:00",
        "timestamp": 1765801800
      },
      "arrival": {
        "airport": "DPS",
        "city": "Denpasar",
        "datetime": "2025-12-15T22:10:00+08:00",
        "timestamp": 1765807800
      },
      "duration": { "total_minutes": 100, "formatted": "1h 40m" },
      "stops": 0,
      "price": { "amount": 595000, "currency": "IDR", "formatted": "Rp595.000" },
      "available_seats": 72,
      "cabin_class": "economy",
      "aircraft": null,
      "amenities": [],
      "baggage": { "carry_on": "Cabin baggage only", "checked": "Additional fee" },
      "best_value_score": 96
    }
  ]
}
```

### Filters

All optional and combined with AND. Time bounds are `HH:MM` in each endpoint's **local**
timezone.

| Field | Type | Meaning |
| --- | --- | --- |
| `min_price` / `max_price` | integer | Fare bounds, whole rupiah |
| `max_stops` | integer | `0` for direct only |
| `departure_after` / `departure_before` | `HH:MM` | Departure window, local time |
| `arrival_after` / `arrival_before` | `HH:MM` | Arrival window, local time |
| `airlines` | string array | IATA codes or carrier names, case-insensitive |
| `max_duration_minutes` | integer | Gate-to-gate ceiling, layovers included |

### Sorts

`best_value` (default), `price_asc`, `price_desc`, `duration_asc`, `duration_desc`,
`departure_time`, `arrival_time`.

```bash
curl -s -X POST localhost:8080/api/v1/flights/search \
  -H 'Content-Type: application/json' \
  -d '{
    "origin": "CGK", "destination": "DPS", "departureDate": "2025-12-15",
    "passengers": 1, "cabinClass": "economy",
    "filters": { "max_stops": 0, "max_price": 1000000 },
    "sort": "duration_asc"
  }' | jq '.metadata'
```

### `GET /health`

```bash
curl -s localhost:8080/health
# {"status":"ok"}
```

Returns 200 unconditionally. The providers are in-process, so there is no upstream connection to
probe; against real HTTP providers this would split into liveness and readiness.

### Status codes

| Code | When |
| --- | --- |
| `200` | Success, **including partial provider failure** — check `providers_failed` |
| `400` | Malformed body, unknown field, invalid IATA code or date, bad filter, unknown sort, round-trip requested |
| `405` | Wrong method on a valid path |
| `502` | Every provider failed |

---

## Architecture

```
cmd/                            composition root — dependency wiring only, no logic
  app.go                        main(): server lifecycle, graceful shutdown
  route.go                      method-aware routes
  handler.go  usecase.go  repository.go

internal/
  model/                        innermost layer; imports nothing of ours
    model.go                    Flight, Segment, Stopover + derived accessors
    request.go  response.go  validate.go

  handler/search/               HTTP: decode, delegate, encode, map errors to status

  usecase/search/               orchestration
    search.go                   FlightClient and Cache ports, fan-out, timeouts, validation
    filter.go                   price / stops / time window / airline / duration
    sort.go                     the seven orderings, with deterministic tie-breaking
    compare.go                  cross-provider price comparison
    ranking.go                  best-value scoring

  repo/external_client/         one package per provider, each owning its own fixture
    garuda/  lionair/  batikair/  airasia/

  util/                         leaf packages
    timeutil/  airport/  currency/  normalize/  retry/  cache/
```

Dependencies point inward. `model` imports nothing of ours; `util` packages are leaves; no
provider package imports the usecase. Both the `FlightClient` and `Cache` interfaces are declared
in the usecase — the consumer — so implementations satisfy them structurally without importing
it.

---

## Design choices

### Segments are the source of truth, and duration is always derived

No provider can be trusted about its own itinerary. Garuda labels a connecting flight with the
**first leg's** arrival airport, stop count and duration: `GA315` reads as a 90-minute direct
flight to Surabaya, but its `segments` array shows a second leg onward to Denpasar. Read at face
value, it becomes a `CGK→SUB` result that a `CGK→DPS` search then discards — a valid flight
silently lost.

So `Flight` stores segments, and route, stop count and duration are computed from them. They
cannot disagree with each other because they come from the same place. `GA315` normalizes to
`CGK→DPS`, 1 stop, 225 minutes.

### Two shapes of connection, neither of them invented

Only Garuda decomposes a connection into legs with real timestamps. Lion Air, Batik Air and
AirAsia give endpoint timestamps plus a layover duration — `JT650` is "16:20 Jakarta, 21:10
Makassar, 75 minutes at SUB", with nothing to say when each leg actually flew.

Rather than fabricate per-leg times to fit one model, `Flight` supports both shapes: `Segments`
for timed legs, `Stopovers` for summarized ones. `Stops()`, `LayoverMinutes()` and
`AirborneMinutes()` read whichever is present. `HasTimedLegs()` tells a caller which they have.
Nothing is guessed.

### Computed durations win, and conflicts are reported

Several providers state durations that contradict their own timestamps:

- Batik Air `ID7042` declares `3h 5m`; its timestamps span **245 minutes**.
- Garuda's `GA332` leg declares 90 minutes; its timestamps span **30**.

Both cases are a timezone boundary: `WIB → WITA` makes elapsed time an hour shorter than the
clock faces suggest. Timestamps are treated as authoritative, duration is computed, and the
disagreement is recorded in a per-flight `warnings` array rather than being hidden or treated as
fatal. The flight is still returned, and a caller can see why it was doubted.

### One parser per provider format

Four providers, four timestamp formats:

| Provider | Format | Handling |
| --- | --- | --- |
| Garuda, AirAsia | `2025-12-15T06:00:00+07:00` | RFC3339 |
| Batik Air | `2025-12-15T07:15:00+0700` — no colon | Custom layout; not valid RFC3339 |
| Lion Air | `2025-12-15T05:30:00` + `Asia/Jakarta` | `ParseInLocation` — reading it as UTC would shift the flight 7–9 hours |

Named parsers rather than one permissive fallback, so a format change fails loudly instead of
producing a plausible but wrong instant. The IANA database is embedded via `time/tzdata`: Lion
Air's naive timestamps depend on `LoadLocation`, and the zoneinfo files it would otherwise read
are absent from a slim container image.

`util/airport` maps each IATA code to its IANA zone — `Asia/Jakarta`, `Asia/Makassar`,
`Asia/Jayapura` — and reports the Indonesian label (WIB, WITA, WIT). Anything outside those three
returns an empty label rather than a guess.

### Normalization lives in the provider packages

Each client owns the shape of its own external data, so no provider-specific field reaches the
usecase. That is also where the divergences get resolved:

- **Baggage** arrives three ways — Garuda piece counts, Lion Air weights, Batik Air and AirAsia
  as prose. Per-provider mapping to strings, not one shared parser.
- **Cabin class** is `Y` from Batik Air, `ECONOMY` from Lion Air, `economy` elsewhere. Booking
  class letters map to cabin names, with unknown letters passed through rather than assumed to be
  economy — a wrong cabin would silently mismatch the caller's request.
- **Airline code** is absent from AirAsia, so it is derived from the flight number (`QZ7250` →
  `QZ`).
- **City names** are absent from AirAsia and Batik Air, and from Garuda's segments. A static IATA
  lookup fills the gap; unknown codes are reported, never guessed.
- **Price** uses Batik Air's `totalPrice`, not `basePrice` — the latter excludes tax and would
  win every comparison unfairly.
- **Absent values** map to `null` for aircraft and `[]` for amenities, so absence is
  distinguishable from an empty string.

### Money is integer, always

Rupiah are quoted in whole units, and float arithmetic would put rounding differences into price
comparisons. `int64` throughout; formatting happens only at the edge, and the numeric amount is
always kept alongside the display string so clients can still sort on it.

### Parallel fan-out with enforceable timeouts

Providers are queried concurrently, each with its own deadline derived from an overall budget. A
search costs the slowest provider (~200–400ms), not the sum (~625ms).

The timeout is only enforceable because each client selects on `ctx.Done()` rather than sleeping
blindly — a bare `time.Sleep` would let a goroutine run to completion regardless, and `wg.Wait()`
would wait for it, making the budget decorative. There is a test asserting *elapsed time*, not
just that an error was returned.

Each goroutine writes its own slot in a pre-sized slice, so results stay in provider order
without a mutex.

### Partial failure returns 200

AirAsia simulates a 10% failure rate, wrapped in retry with exponential backoff. A provider that
fails, times out or returns garbage costs only its own results: it is counted in
`providers_failed`, named in `provider_status` with its error, and the search proceeds.

Only a total wipeout is an error, because that is the single case where an empty list would lie to
the caller — there may well be flights, we just could not reach anyone who knows. That maps to
`502`, not `500`: nothing is wrong with this service when every upstream is unreachable.

The retry lives inside the AirAsia client rather than in the aggregator, because the flakiness is
specific to that provider and the other three should not pay for a retry loop they do not need.
The `retry` package itself is generic, so that can change without rewriting it.

### Caching the aggregate, not the response

A 30-second in-memory TTL cache stores the **normalized provider results**, keyed on origin,
destination, date, cabin class and passenger count — the fields that change what providers
return. Filters and sort are applied *after* the lookup and are deliberately not part of the key.

That distinction is the whole point. Two callers searching the same route with different filters
share one entry:

```
1st  cache_hit=false  384ms  13 results
2nd  cache_hit=true     0ms  13 results
3rd  cache_hit=true     0ms   9 results   (max_stops=0, sort=price_desc — still a hit)
4th  cache_hit=false  346ms   0 results   (different date — a genuine miss)
```

Caching finished responses instead would fragment the cache across every filter and sort
combination and would almost never hit.

A **total** provider outage is not cached: storing it would extend the outage past its own cause,
telling the next caller everything is down without anyone having checked. A **partial** failure
*is* cached, because three good providers out of four is a usable result and re-querying to
re-learn it wastes four calls. On a cache hit, `provider_status.duration_ms` describes the fetch
that produced the cached data, not the current request.

Expiry is lazy on read rather than swept by a background goroutine — with a bounded key space
there is nothing to reclaim urgently, and no goroutine means nothing to shut down.

The store is in-memory rather than Redis because these providers are in-process mocks with no
network cost to amortize, and a second process would be an operational dependency a reviewer has
to install for no behavioural gain. The `Cache` port is declared in the usecase, so a distributed
store would be a wiring change in `cmd` only.

### Pipeline order

```
fan-out (or cache) → accept → filter → dedupe → score → sort
```

- **accept** drops records that are unusable or do not answer the request (route, date, cabin,
  seats). Route matching uses the *effective* origin and destination, which is what saves
  `GA315`.
- **filter** applies the caller's own criteria, once across the merged set rather than per
  provider — so `provider_status.results` keeps meaning "how many this provider had for this
  route" instead of a number that shifts with whatever was filtered.
- **dedupe** before scoring, so a flight sold by several providers is scored once, at the fare a
  caller would actually pay.
- **score** before sorting, because best-value ordering reads the scores.

`dropped_results` and `filtered_results` are separate counters. One means "unusable or did not
match the route"; the other means "your filter removed it". Merging them would hide which of the
two emptied a result set.

### Cross-provider price comparison

Two records are the same flight when the flight number **and** the departure instant both match.
Number alone is reused across days and rotations; time alone would merge genuinely different
flights that happen to leave together. The cheapest offer becomes the headline result and the
others are attached as `alternative_prices`, cheapest first.

The supplied fixtures contain no cross-provider duplicate — each airline appears through exactly
one provider — so `merged_duplicates` is `0` in practice and the behaviour is covered by unit
tests rather than by the sample data. It becomes live the moment a reseller-style provider is
added.

### Best-value scoring

Price, gate-to-gate duration and stop count are each min-max normalized across the result set,
then combined:

```
penalty = 0.5·price + 0.3·duration + 0.2·stops       (0 = best in set)
score   = (1 − penalty) × 100                        (higher = better value)
```

Price dominates because it is what a traveller on this route is overwhelmingly choosing on.
Duration is the next largest cost. Stop count carries the least weight because a connection
already costs time, which duration has counted — weight it heavily and a stop is punished twice.
The weights are named constants, and a test asserts they sum to 1.0; otherwise a score of 100
becomes unreachable and the scale silently shifts.

Because duration is gate-to-gate, a cheap itinerary with a long layover is penalized for the
wait, not merely for having a stop.

What this buys, on the real data: `QZ7250` is the cheapest flight in the set at Rp485.000 and
ranks **tenth**, because it is also 2h40m slower than a direct with a 95-minute wait. A pure price
sort would put it first and mislead most travellers.

**Scores are relative to one response, not absolute.** Filter to direct flights only and the top
score rises from 96 to 100, because the comparison set changed. This is deliberate — an absolute
scale would need per-route tuning to mean anything — but it means two responses' scores should not
be compared.

### Standard library only

No router, no logging framework, no assertion library. `http.ServeMux` has supported
method-aware patterns since Go 1.22, so `"POST /api/v1/flights/search"` handles routing and
returns `405` for a wrong method for free. `log/slog` covers structured logging. This keeps the
dependency surface at zero, which for a service whose job is calling other services is worth more
than the convenience.

### Requests reject unknown fields

A misspelled `cabinKlass` returns `400` rather than being silently ignored. Quietly accepting it
would let a caller believe a filter had been applied when it had not.

---

## Testing

```bash
make test     # go test ./... -race -cover
make cover    # HTML coverage report
```

| Package | Coverage |
| --- | --- |
| `util/cache` | 100% |
| `util/currency` | 100% |
| `util/normalize` | 100% |
| `usecase/search` | 97.9% |
| `handler/search` | 95.7% |
| `util/timeutil` | 95.3% |
| `util/retry` | 91.4% |
| `util/airport` | 83.3% |
| `model` | 82.1% |
| `repo/external_client/*` | 73–80% |
| `cmd` | 0% — wiring only |

Everything runs under `-race`, since the fan-out is concurrent.

Notable cases, chosen because each one guards a mistake that would otherwise be invisible:

- **`GA315`** normalizes to `CGK→DPS`, 1 stop, 225 minutes, and both provider inconsistencies
  surface as warnings — asserted against the real fixture rather than a synthetic one.
- **Lion Air's timestamps** are asserted as absolute instants, and a companion assertion proves
  that parsing them without their timezone yields a *different* instant. That is the actual bug,
  not a proxy for it.
- **Batik Air's colon-less offset** is asserted to be *rejected* by the RFC3339 parser, so the two
  layouts cannot later be "simplified" into one.
- **Provider latency** is asserted by elapsed time, so a timeout that does not actually cut a
  provider short fails the test.
- **Fan-out parallelism** — four 150ms providers must complete inside 400ms.
- **Cache hits** are asserted by counting provider invocations, not by reading `cache_hit`.
  Checking the flag alone would pass even if it were hardcoded.
- **A total outage is not cached** — the provider fails, nothing is stored, then it recovers and
  the next search must actually retry it.
- **Deterministic ordering** — the same search runs five times with staggered provider delays and
  must return an identical order.
- **Output contract details** — `null` for an absent aircraft rather than `""`, `[]` for absent
  amenities rather than `null`, and the derived-carrier-code fallback including the case it
  cannot handle (a carrier whose IATA code begins with a digit).
- **Pipeline wiring** — scoring and sorting are unit-tested in isolation, so both can pass while
  the usecase forgets to call one of them. That happened during development: with every score left
  at zero, best-value ordering fell through to the price tie-break and produced a correct-looking
  list with the wrong label on it. There is now an assertion that goes through the real entry
  point.

Provider latency and failure rates are injectable, with a seeded random source, so the
10%-failure path is deterministic in tests instead of flaky. Forcing AirAsia's failure rate to 1
gives a `200` with nine results and `providers_failed: 1`.

---

## Not implemented

- **Round-trip search** — `returnDate` is accepted and explicitly rejected with a clear message
  rather than silently ignored. The fixtures contain one route on one date with no return
  inventory, so implementing it would mean fabricating provider data.
- **Multi-city search** — the same constraint, twice over.
- **Provider rate limiting** — the providers are in-process function calls; throttling them would
  demonstrate nothing about handling a real upstream quota.
