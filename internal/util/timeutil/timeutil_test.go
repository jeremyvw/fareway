package timeutil

import (
	"testing"
	"time"
)

// TestParseOffset covers the format Garuda and AirAsia use: RFC3339 with a colon in the
// offset. The expected epochs are computed from the stated wall-clock times, not copied
// from expected_result.json — that file's timestamps decode to December 2024 and disagree
// with its own stated duration, so it cannot be used as an oracle here.
func TestParseOffset(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		epoch int64
	}{
		{"airasia departure", "2025-12-15T15:15:00+07:00", 1765786500},
		{"airasia arrival", "2025-12-15T20:35:00+08:00", 1765802100},
		{"garuda departure", "2025-12-15T06:00:00+07:00", 1765753200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseOffset(tc.value)
			if err != nil {
				t.Fatalf("ParseOffset(%q): %v", tc.value, err)
			}
			if got.Unix() != tc.epoch {
				t.Errorf("epoch = %d, want %d", got.Unix(), tc.epoch)
			}
		})
	}
}

// TestParseCompactOffset covers Batik Air, whose offset omits the colon and so is not
// valid RFC3339.
func TestParseCompactOffset(t *testing.T) {
	got, err := ParseCompactOffset("2025-12-15T07:15:00+0700")
	if err != nil {
		t.Fatalf("ParseCompactOffset: %v", err)
	}
	if want := int64(1765757700); got.Unix() != want {
		t.Errorf("epoch = %d, want %d", got.Unix(), want)
	}
	if _, err := ParseOffset("2025-12-15T07:15:00+0700"); err == nil {
		t.Error("RFC3339 parser accepted a colon-less offset; the two layouts are not interchangeable")
	}
}

// TestParseInZone covers Lion Air, which sends naive timestamps alongside IANA zone names.
// Reading these as UTC would shift every affected flight by seven to nine hours, so the
// test pins the absolute instant rather than the wall clock.
func TestParseInZone(t *testing.T) {
	depart, err := ParseInZone("2025-12-15T05:30:00", "Asia/Jakarta")
	if err != nil {
		t.Fatalf("ParseInZone departure: %v", err)
	}
	if want := int64(1765751400); depart.Unix() != want {
		t.Errorf("departure epoch = %d, want %d", depart.Unix(), want)
	}

	arrive, err := ParseInZone("2025-12-15T08:15:00", "Asia/Makassar")
	if err != nil {
		t.Fatalf("ParseInZone arrival: %v", err)
	}
	if want := int64(1765757700); arrive.Unix() != want {
		t.Errorf("arrival epoch = %d, want %d", arrive.Unix(), want)
	}

	// The declared 105 minutes only comes out right if both zones were honoured.
	if got := DurationMinutes(depart, arrive); got != 105 {
		t.Errorf("duration = %d min, want 105", got)
	}

	// Same wall clock read as UTC is a different instant entirely — the bug this guards.
	naive, err := time.Parse(LayoutNaive, "2025-12-15T05:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if naive.Unix() == depart.Unix() {
		t.Error("zone-less parsing produced the same instant; the location was ignored")
	}
}

func TestParseInZoneRejectsUnknownZone(t *testing.T) {
	if _, err := ParseInZone("2025-12-15T05:30:00", "Asia/Atlantis"); err == nil {
		t.Error("expected an error for an unknown zone")
	}
	if _, err := ParseInZone("2025-12-15T05:30:00", ""); err == nil {
		t.Error("expected an error for an empty zone")
	}
}

// TestZoneDatabaseIsEmbedded fails on a host with no zoneinfo files unless the tzdata
// blank import is present, which is the whole point of carrying it.
func TestZoneDatabaseIsEmbedded(t *testing.T) {
	for _, zone := range []string{"Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura"} {
		if _, err := Location(zone); err != nil {
			t.Errorf("Location(%q): %v", zone, err)
		}
	}
}

func TestDurationMinutes(t *testing.T) {
	// GA315 end to end: 14:00+07:00 to 18:45+08:00 is 225 minutes, against a declared 90.
	depart, _ := ParseOffset("2025-12-15T14:00:00+07:00")
	arrive, _ := ParseOffset("2025-12-15T18:45:00+08:00")
	if got := DurationMinutes(depart, arrive); got != 225 {
		t.Errorf("duration = %d, want 225", got)
	}

	// Batik ID7042: declared "3h 5m", actually 245 minutes.
	depart, _ = ParseCompactOffset("2025-12-15T18:45:00+0700")
	arrive, _ = ParseCompactOffset("2025-12-15T23:50:00+0800")
	if got := DurationMinutes(depart, arrive); got != 245 {
		t.Errorf("duration = %d, want 245", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		want    string
	}{
		{260, "4h 20m"},
		{225, "3h 45m"},
		{120, "2h"},
		{45, "45m"},
		{0, "0m"},
		{-30, "-30m"},
	} {
		if got := FormatDuration(tc.minutes); got != tc.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tc.minutes, got, tc.want)
		}
	}
}

// TestLocalDate pins the rule that a calendar date is read in the instant's own zone. A
// UTC reading would place this departure on the previous day and drop it from the results.
func TestLocalDate(t *testing.T) {
	earlyMorning, err := ParseOffset("2025-12-15T00:30:00+07:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := LocalDate(earlyMorning); got != "2025-12-15" {
		t.Errorf("LocalDate = %q, want 2025-12-15", got)
	}
	if got := earlyMorning.UTC().Format(LayoutDate); got != "2025-12-14" {
		t.Fatalf("test premise broken: UTC date = %q, expected 2025-12-14", got)
	}
}

func TestMinutesSinceMidnight(t *testing.T) {
	arrive, err := ParseOffset("2025-12-15T20:35:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	// 20:35 local, not the 12:35 the same instant reads as in UTC.
	if got := MinutesSinceMidnight(arrive); got != 20*60+35 {
		t.Errorf("MinutesSinceMidnight = %d, want %d", got, 20*60+35)
	}
}

func TestParseClock(t *testing.T) {
	got, err := ParseClock("18:45")
	if err != nil {
		t.Fatalf("ParseClock: %v", err)
	}
	if want := 18*60 + 45; got != want {
		t.Errorf("ParseClock = %d, want %d", got, want)
	}
	if _, err := ParseClock("6pm"); err == nil {
		t.Error("expected an error for a non HH:MM value")
	}
}

func TestParseDate(t *testing.T) {
	got, err := ParseDate("2025-12-15")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	if got.Format(LayoutDate) != "2025-12-15" {
		t.Errorf("ParseDate = %v", got)
	}
	if _, err := ParseDate("15-12-2025"); err == nil {
		t.Error("expected an error for a non-ISO date")
	}
}
