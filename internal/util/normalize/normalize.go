// Package normalize holds the small conversions every external client needs, so the four
// adapters agree on the shape of the output rather than each inventing its own.
package normalize

import "strings"

// ID builds a stable identifier from a flight number and provider name, e.g.
// "QZ7250_AirAsia". Whitespace is removed so a multi-word provider still yields a usable
// identifier: "Garuda Indonesia" gives GA315_GarudaIndonesia.
func ID(flightNumber, provider string) string {
	return strings.TrimSpace(flightNumber) + "_" + Slug(provider)
}

// Slug removes whitespace from a name.
func Slug(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "")
}

// OptionalString maps a blank provider field to nil, so absence serializes as null rather
// than as an empty string that looks like real data.
func OptionalString(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// Amenities normalizes an amenity list: never nil, so it serializes as [], and lower-cased
// so "Snack" from one provider and "snack" from another are the same value to a filter.
func Amenities(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		if trimmed := strings.ToLower(strings.TrimSpace(a)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// CarrierCodeFromFlightNumber pulls the leading letters off a flight number, for providers
// that ship no IATA code of their own: "QZ7250" gives "QZ".
func CarrierCodeFromFlightNumber(number string) string {
	for i, r := range strings.TrimSpace(number) {
		if r >= '0' && r <= '9' {
			return strings.ToUpper(strings.TrimSpace(number)[:i])
		}
	}
	return ""
}
