package model

import "testing"

func legsOf(r SearchRequest) []string {
	out := make([]string, 0, len(r.Legs))
	for _, l := range r.Legs {
		out = append(out, l.Origin+"-"+l.Destination+"@"+l.DepartureDate)
	}
	return out
}

func assertLegs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("legs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("legs = %v, want %v", got, want)
		}
	}
}

func ptr(s string) *string { return &s }

// TestFlatFormBecomesOneLeg covers the request shape the assignment specifies. It has to keep
// working untouched, so this is the compatibility test.
func TestFlatFormBecomesOneLeg(t *testing.T) {
	r := SearchRequest{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"}
	r.Normalize()

	assertLegs(t, legsOf(r), "CGK-DPS@2025-12-15")
	if got := r.TripType(); got != TripOneWay {
		t.Errorf("trip type = %q, want one_way", got)
	}
}

// TestReturnDateBecomesAReversedSecondLeg is the round-trip conversion: the return leg travels
// back the way the first came, on the return date.
func TestReturnDateBecomesAReversedSecondLeg(t *testing.T) {
	r := SearchRequest{
		Origin:        "CGK",
		Destination:   "DPS",
		DepartureDate: "2025-12-15",
		ReturnDate:    ptr("2025-12-22"),
	}
	r.Normalize()

	assertLegs(t, legsOf(r), "CGK-DPS@2025-12-15", "DPS-CGK@2025-12-22")
	if got := r.TripType(); got != TripRoundTrip {
		t.Errorf("trip type = %q, want round_trip", got)
	}
}

func TestEmptyReturnDateIsNotARoundTrip(t *testing.T) {
	for name, returnDate := range map[string]*string{
		"null":       nil,
		"empty":      ptr(""),
		"whitespace": ptr("   "),
	} {
		t.Run(name, func(t *testing.T) {
			r := SearchRequest{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", ReturnDate: returnDate}
			r.Normalize()

			if len(r.Legs) != 1 {
				t.Errorf("legs = %v, want a single leg", legsOf(r))
			}
			if got := r.TripType(); got != TripOneWay {
				t.Errorf("trip type = %q, want one_way", got)
			}
		})
	}
}

func TestExplicitLegsArePreserved(t *testing.T) {
	r := SearchRequest{Legs: []Leg{
		{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"},
		{Origin: "DPS", Destination: "SUB", DepartureDate: "2025-12-20"},
		{Origin: "SUB", Destination: "CGK", DepartureDate: "2025-12-24"},
	}}
	r.Normalize()

	assertLegs(t, legsOf(r), "CGK-DPS@2025-12-15", "DPS-SUB@2025-12-20", "SUB-CGK@2025-12-24")
	if got := r.TripType(); got != TripMultiCity {
		t.Errorf("trip type = %q, want multi_city", got)
	}
}

// TestTwoLegsThatDoNotRetraceAreMultiCity is the distinction the derived trip type exists for:
// "CGK to DPS, then DPS to SUB" is not a round trip even though it has two legs.
func TestTwoLegsThatDoNotRetraceAreMultiCity(t *testing.T) {
	r := SearchRequest{Legs: []Leg{
		{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"},
		{Origin: "DPS", Destination: "SUB", DepartureDate: "2025-12-20"},
	}}
	r.Normalize()

	if got := r.TripType(); got != TripMultiCity {
		t.Errorf("trip type = %q, want multi_city", got)
	}
}

// TestExplicitLegsMatchingAReturnAreARoundTrip checks the two request shapes agree: expressing a
// return trip as legs must classify the same as sending returnDate.
func TestExplicitLegsMatchingAReturnAreARoundTrip(t *testing.T) {
	viaLegs := SearchRequest{Legs: []Leg{
		{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"},
		{Origin: "DPS", Destination: "CGK", DepartureDate: "2025-12-22"},
	}}
	viaLegs.Normalize()

	viaFlat := SearchRequest{
		Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", ReturnDate: ptr("2025-12-22"),
	}
	viaFlat.Normalize()

	if viaLegs.TripType() != TripRoundTrip {
		t.Errorf("legs form = %q, want round_trip", viaLegs.TripType())
	}
	assertLegs(t, legsOf(viaLegs), legsOf(viaFlat)...)
}

func TestAirportCodesAreNormalized(t *testing.T) {
	r := SearchRequest{Legs: []Leg{
		{Origin: " cgk ", Destination: "dps", DepartureDate: " 2025-12-15 "},
	}}
	r.Normalize()

	assertLegs(t, legsOf(r), "CGK-DPS@2025-12-15")
}

// TestFlatFieldsTrackTheFirstLeg keeps the criteria echoed back to the caller consistent with the
// legs, whichever shape was sent.
func TestFlatFieldsTrackTheFirstLeg(t *testing.T) {
	r := SearchRequest{Legs: []Leg{
		{Origin: "DPS", Destination: "SUB", DepartureDate: "2025-12-20"},
	}}
	r.Normalize()

	if r.Origin != "DPS" || r.Destination != "SUB" || r.DepartureDate != "2025-12-20" {
		t.Errorf("flat fields = %s/%s/%s, want them to mirror the first leg",
			r.Origin, r.Destination, r.DepartureDate)
	}
}

// TestHasBothForms guards against a request that says two different things. It has to be checked
// before Normalize, which back-fills the flat fields and would make every legs request look mixed.
func TestHasBothForms(t *testing.T) {
	mixed := SearchRequest{
		Origin:      "CGK",
		Destination: "DPS",
		Legs:        []Leg{{Origin: "SUB", Destination: "UPG", DepartureDate: "2025-12-15"}},
	}
	if !mixed.HasBothForms() {
		t.Error("HasBothForms() = false for a request carrying both shapes")
	}

	for name, r := range map[string]SearchRequest{
		"flat only": {Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"},
		"legs only": {Legs: []Leg{{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"}}},
		"neither":   {},
	} {
		t.Run(name, func(t *testing.T) {
			if r.HasBothForms() {
				t.Error("HasBothForms() = true")
			}
		})
	}

	t.Run("return date alone counts as the flat form", func(t *testing.T) {
		r := SearchRequest{
			ReturnDate: ptr("2025-12-22"),
			Legs:       []Leg{{Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15"}},
		}
		if !r.HasBothForms() {
			t.Error("HasBothForms() = false; returnDate alongside legs is ambiguous")
		}
	})
}

// TestNormalizeDoesNotInventLegs keeps an empty request empty: defaults are covered by
// TestNormalizeFillsDefaults, and a leg conjured from nothing would be searched as CGK-to-nowhere.
func TestNormalizeDoesNotInventLegs(t *testing.T) {
	var r SearchRequest
	r.Normalize()

	if len(r.Legs) != 0 {
		t.Errorf("legs = %v, want none", legsOf(r))
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	r := SearchRequest{
		Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", ReturnDate: ptr("2025-12-22"),
	}
	r.Normalize()
	first := legsOf(r)

	// A second pass must not append the return leg again.
	r.Normalize()
	assertLegs(t, legsOf(r), first...)
}
