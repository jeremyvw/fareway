package currency

import "testing"

func TestFormatIDR(t *testing.T) {
	for _, tc := range []struct {
		amount int64
		want   string
	}{
		{485000, "Rp485.000"},
		{950000, "Rp950.000"},
		{1100000, "Rp1.100.000"},
		{1250000, "Rp1.250.000"},
		{1850000, "Rp1.850.000"},
		{0, "Rp0"},
		{999, "Rp999"},
		{1000, "Rp1.000"},
		{12345678901, "Rp12.345.678.901"},
	} {
		if got := FormatIDR(tc.amount); got != tc.want {
			t.Errorf("FormatIDR(%d) = %q, want %q", tc.amount, got, tc.want)
		}
	}
}

// TestGroupBoundaries checks the digit-count cases where off-by-one grouping bugs live:
// the leftmost group must absorb the remainder.
func TestGroupBoundaries(t *testing.T) {
	for _, tc := range []struct {
		amount int64
		want   string
	}{
		{1, "1"},
		{12, "12"},
		{123, "123"},
		{1234, "1.234"},
		{12345, "12.345"},
		{123456, "123.456"},
		{1234567, "1.234.567"},
		{-1234567, "-1.234.567"},
	} {
		if got := Group(tc.amount, "."); got != tc.want {
			t.Errorf("Group(%d) = %q, want %q", tc.amount, got, tc.want)
		}
	}
}

func TestFormatFallsBackForOtherCurrencies(t *testing.T) {
	if got := Format(1250000, "IDR"); got != "Rp1.250.000" {
		t.Errorf("Format IDR = %q", got)
	}
	if got := Format(1250000, "idr"); got != "Rp1.250.000" {
		t.Errorf("Format lowercase idr = %q; currency codes should match case-insensitively", got)
	}
	// An unexpected currency must stay legible rather than be mislabelled as rupiah.
	if got := Format(1250, "SGD"); got != "SGD 1,250" {
		t.Errorf("Format SGD = %q, want %q", got, "SGD 1,250")
	}
}
