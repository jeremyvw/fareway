package normalize

import "testing"

func TestID(t *testing.T) {
	for _, tc := range []struct {
		name         string
		flightNumber string
		provider     string
		want         string
	}{
		// The identifier the output contract specifies.
		{"single word provider", "QZ7250", "AirAsia", "QZ7250_AirAsia"},
		// A multi-word provider must not leave a space inside an identifier.
		{"multi word provider", "GA315", "Garuda Indonesia", "GA315_GarudaIndonesia"},
		{"two word provider", "JT740", "Lion Air", "JT740_LionAir"},
		{"padded input", "  ID6514  ", "  Batik Air  ", "ID6514_BatikAir"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ID(tc.flightNumber, tc.provider); got != tc.want {
				t.Errorf("ID(%q, %q) = %q, want %q", tc.flightNumber, tc.provider, got, tc.want)
			}
		})
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Garuda Indonesia": "GarudaIndonesia",
		"Lion Air":         "LionAir",
		"AirAsia":          "AirAsia",
		"  Batik Air  ":    "BatikAir",
		"":                 "",
		"   ":              "",
		"a b c d":          "abcd",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOptionalString(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want *string
	}{
		{"empty is absent", "", nil},
		{"whitespace is absent", "   ", nil},
		{"tab and newline are absent", "\t\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := OptionalString(tc.in); got != nil {
				t.Errorf("OptionalString(%q) = %q, want nil", tc.in, *got)
			}
		})
	}

	t.Run("present value is trimmed", func(t *testing.T) {
		got := OptionalString("  Boeing 737-800  ")
		if got == nil {
			t.Fatal("OptionalString returned nil for a real value")
		}
		if *got != "Boeing 737-800" {
			t.Errorf("OptionalString = %q, want %q", *got, "Boeing 737-800")
		}
	})

	t.Run("returned pointer is independent of later mutation", func(t *testing.T) {
		first := OptionalString("Airbus A320")
		second := OptionalString("Boeing 737")
		if *first != "Airbus A320" {
			t.Errorf("first value became %q after a second call", *first)
		}
		if first == second {
			t.Error("both calls returned the same pointer")
		}
	})
}

func TestAmenities(t *testing.T) {
	t.Run("nil becomes an empty slice", func(t *testing.T) {
		got := Amenities(nil)
		if got == nil {
			t.Fatal("Amenities(nil) = nil; it must serialize as [] not null")
		}
		if len(got) != 0 {
			t.Errorf("Amenities(nil) = %v, want empty", got)
		}
	})

	t.Run("empty slice stays empty and non-nil", func(t *testing.T) {
		got := Amenities([]string{})
		if got == nil || len(got) != 0 {
			t.Errorf("Amenities([]) = %v", got)
		}
	})

	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{"lower-cased", []string{"Snack", "Beverage"}, []string{"snack", "beverage"}},
		{"already lower-case", []string{"wifi", "meal"}, []string{"wifi", "meal"}},
		{"padded values trimmed", []string{"  Meal  ", "Entertainment"}, []string{"meal", "entertainment"}},
		{"blank entries dropped", []string{"Snack", "", "   ", "Meal"}, []string{"snack", "meal"}},
		{"all blank yields empty", []string{"", "  "}, []string{}},
		{"order preserved", []string{"C", "A", "B"}, []string{"c", "a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Amenities(tc.in)
			if got == nil {
				t.Fatal("returned nil")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Amenities(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("Amenities(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}

	t.Run("input is not mutated", func(t *testing.T) {
		in := []string{"Snack", "Beverage"}
		Amenities(in)
		if in[0] != "Snack" {
			t.Errorf("input was modified in place: %v", in)
		}
	})
}

func TestCarrierCodeFromFlightNumber(t *testing.T) {
	for _, tc := range []struct {
		name   string
		number string
		want   string
	}{
		{"airasia", "QZ7250", "QZ"},
		{"garuda", "GA315", "GA"},
		{"batik air", "ID6514", "ID"},
		{"lion air", "JT740", "JT"},
		{"lower case input", "qz7250", "QZ"},
		{"padded input", "  QZ520  ", "QZ"},
		{"three letter prefix", "ABC123", "ABC"},
		{"empty", "", ""},
		{"no digits at all", "QZ", ""},
		{"leading digit yields nothing", "6E123", ""},
		{"all digits", "1234", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CarrierCodeFromFlightNumber(tc.number); got != tc.want {
				t.Errorf("CarrierCodeFromFlightNumber(%q) = %q, want %q", tc.number, got, tc.want)
			}
		})
	}
}

func TestCarrierCodeLimitation(t *testing.T) {
	if got := CarrierCodeFromFlightNumber("6E123"); got != "" {
		t.Errorf("CarrierCodeFromFlightNumber(6E123) = %q; a numeric-leading code is expected to yield an empty result", got)
	}
}
