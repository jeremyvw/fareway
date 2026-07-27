package airport

import (
	"errors"
	"testing"
	"time"
)

var _ func(string) (string, bool) = City

func TestCityResolvesAsAClientResolverWould(t *testing.T) {
	if city, ok := City("CGK"); !ok || city != "Jakarta" {
		t.Errorf("City(CGK) = %q, %v; want Jakarta, true", city, ok)
	}
}

func TestLookupCoversEveryAirportInTheMockData(t *testing.T) {
	for _, tc := range []struct {
		iata  string
		city  string
		zone  string
		label string
	}{
		{"CGK", "Jakarta", ZoneWIB, "WIB"},
		{"DPS", "Denpasar", ZoneWITA, "WITA"},
		{"SUB", "Surabaya", ZoneWIB, "WIB"},
		{"UPG", "Makassar", ZoneWITA, "WITA"},
		{"SOC", "Surakarta", ZoneWIB, "WIB"},
	} {
		info, ok := Lookup(tc.iata)
		if !ok {
			t.Fatalf("Lookup(%q) not found; it appears in the provider fixtures", tc.iata)
		}
		if info.City != tc.city {
			t.Errorf("%s city = %q, want %q", tc.iata, info.City, tc.city)
		}
		if info.Zone != tc.zone {
			t.Errorf("%s zone = %q, want %q", tc.iata, info.Zone, tc.zone)
		}
		if info.Label() != tc.label {
			t.Errorf("%s label = %q, want %q", tc.iata, info.Label(), tc.label)
		}
	}
}

func TestAllThreeIndonesianZonesAreRepresented(t *testing.T) {
	seen := map[string]bool{}
	for _, info := range registry {
		if label := info.Label(); label != "" {
			seen[label] = true
		}
	}
	for _, label := range []string{"WIB", "WITA", "WIT"} {
		if !seen[label] {
			t.Errorf("no airport in the registry uses %s", label)
		}
	}
}

func TestLookupIsCaseAndSpaceInsensitive(t *testing.T) {
	for _, in := range []string{"cgk", " CGK ", "Cgk"} {
		if _, ok := Lookup(in); !ok {
			t.Errorf("Lookup(%q) not found", in)
		}
	}
}

func TestUnknownCodeIsReportedNotGuessed(t *testing.T) {
	if _, ok := Lookup("ZZZ"); ok {
		t.Error("Lookup(ZZZ) reported a match")
	}
	if _, ok := City("ZZZ"); ok {
		t.Error("City(ZZZ) reported a match")
	}

	_, err := Location("ZZZ")
	var unknown *UnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("Location(ZZZ) error = %v, want *UnknownError", err)
	}
	if unknown.IATA != "ZZZ" {
		t.Errorf("error IATA = %q, want ZZZ", unknown.IATA)
	}
}

func TestLocationResolvesOffsets(t *testing.T) {
	for _, tc := range []struct {
		iata          string
		offsetSeconds int
	}{
		{"CGK", 7 * 3600},
		{"DPS", 8 * 3600},
		{"DJJ", 9 * 3600},
	} {
		loc, err := Location(tc.iata)
		if err != nil {
			t.Fatalf("Location(%q): %v", tc.iata, err)
		}
		// Indonesia has no DST, so a fixed reference instant is enough.
		_, offset := timeReference().In(loc).Zone()
		if offset != tc.offsetSeconds {
			t.Errorf("%s offset = %d, want %d", tc.iata, offset, tc.offsetSeconds)
		}
	}
}

func timeReference() time.Time {
	return time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)
}
