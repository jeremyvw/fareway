package airport

import (
	"strings"
	"time"

	"github.com/jeremyvw/fareway/internal/util/timeutil"
)

// Indonesian civil time zones. The three IANA names below are what the WIB/WITA/WIT
// labels actually mean, and the labels are what an Indonesian traveller reads on a ticket.
const (
	ZoneWIB  = "Asia/Jakarta"  // UTC+7
	ZoneWITA = "Asia/Makassar" // UTC+8
	ZoneWIT  = "Asia/Jayapura" // UTC+9
)

// Info is what we know about an airport.
type Info struct {
	IATA string
	Name string
	City string

	// Zone is an IANA location name, not a fixed offset, so any future rule change comes
	// from the tzdata database rather than from a constant we would have to maintain.
	Zone string
}

// Label renders the Indonesian short name for the airport's zone: WIB, WITA or WIT.
// Anything outside those three returns an empty string rather than a guess.
func (i Info) Label() string {
	switch i.Zone {
	case ZoneWIB:
		return "WIB"
	case ZoneWITA:
		return "WITA"
	case ZoneWIT:
		return "WIT"
	default:
		return ""
	}
}

// registry is a static table. A real service would read this from a reference data store;
// for a fixed set of mock providers a compiled-in map is faster, allocation-free at
// lookup time, and impossible to misconfigure.
//
// Coverage is deliberately limited to Indonesian airports, which is all the mock data
// contains. Unknown codes are reported as unknown rather than guessed at.
var registry = map[string]Info{
	"CGK": {IATA: "CGK", Name: "Soekarno-Hatta International", City: "Jakarta", Zone: ZoneWIB},
	"HLP": {IATA: "HLP", Name: "Halim Perdanakusuma", City: "Jakarta", Zone: ZoneWIB},
	"SUB": {IATA: "SUB", Name: "Juanda International", City: "Surabaya", Zone: ZoneWIB},
	"SOC": {IATA: "SOC", Name: "Adi Soemarmo", City: "Surakarta", Zone: ZoneWIB},
	"JOG": {IATA: "JOG", Name: "Adisutjipto", City: "Yogyakarta", Zone: ZoneWIB},
	"SRG": {IATA: "SRG", Name: "Ahmad Yani", City: "Semarang", Zone: ZoneWIB},
	"KNO": {IATA: "KNO", Name: "Kualanamu International", City: "Medan", Zone: ZoneWIB},
	"BTH": {IATA: "BTH", Name: "Hang Nadim", City: "Batam", Zone: ZoneWIB},
	"PNK": {IATA: "PNK", Name: "Supadio", City: "Pontianak", Zone: ZoneWIB},
	"DPS": {IATA: "DPS", Name: "Ngurah Rai International", City: "Denpasar", Zone: ZoneWITA},
	"UPG": {IATA: "UPG", Name: "Sultan Hasanuddin International", City: "Makassar", Zone: ZoneWITA},
	"BPN": {IATA: "BPN", Name: "Sultan Aji Muhammad Sulaiman", City: "Balikpapan", Zone: ZoneWITA},
	"BDJ": {IATA: "BDJ", Name: "Syamsudin Noor", City: "Banjarmasin", Zone: ZoneWITA},
	"MDC": {IATA: "MDC", Name: "Sam Ratulangi", City: "Manado", Zone: ZoneWITA},
	"LOP": {IATA: "LOP", Name: "Zainuddin Abdul Madjid", City: "Praya", Zone: ZoneWITA},
	"AMQ": {IATA: "AMQ", Name: "Pattimura", City: "Ambon", Zone: ZoneWIT},
	"DJJ": {IATA: "DJJ", Name: "Sentani", City: "Jayapura", Zone: ZoneWIT},
}

// Lookup resolves an airport code. Codes are matched case-insensitively and trimmed,
// since provider payloads are not guaranteed to be tidy.
func Lookup(iata string) (Info, bool) {
	info, ok := registry[normalize(iata)]
	return info, ok
}

// City resolves an airport code to its city name.
//
// The signature matches the CityResolver each external client accepts, so this function
// can be passed straight in during wiring without an adapter.
func City(iata string) (string, bool) {
	info, ok := Lookup(iata)
	if !ok {
		return "", false
	}
	return info.City, true
}

// Location resolves an airport's timezone, for reading a naive provider timestamp in the
// zone it was actually written in.
func Location(iata string) (*time.Location, error) {
	info, ok := Lookup(iata)
	if !ok {
		return nil, &UnknownError{IATA: normalize(iata)}
	}
	return timeutil.Location(info.Zone)
}

// UnknownError reports an airport code missing from the registry.
type UnknownError struct {
	IATA string
}

func (e *UnknownError) Error() string {
	return "unknown airport code " + e.IATA
}

// Known reports how many airports the registry covers. Useful in tests and in a
// diagnostics endpoint.
func Known() int { return len(registry) }

func normalize(iata string) string {
	return strings.ToUpper(strings.TrimSpace(iata))
}
