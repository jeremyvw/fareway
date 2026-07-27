package timeutil

import (
	"fmt"
	"strings"
	"sync"
	"time"

	// Embed the IANA timezone database in the binary. Lion Air ships naive timestamps with
	// sibling IANA zone names, so LoadLocation is on the critical path — and the zoneinfo
	// files it would otherwise read are absent from a scratch/alpine container and from
	// Windows outside a Go installation. Costs ~450KB; the alternative is every Lion Air
	// flight failing to parse at runtime on a machine where the tests passed.
	_ "time/tzdata"
)

const (
	// LayoutCompactOffset is RFC3339 with no colon in the offset: "2025-12-15T07:15:00+0700".
	LayoutCompactOffset = "2006-01-02T15:04:05-0700"
	// LayoutNaive carries no zone at all: "2025-12-15T05:30:00".
	LayoutNaive = "2006-01-02T15:04:05"
	// LayoutDate is a plain calendar date.
	LayoutDate = "2006-01-02"
)

// ParseOffset parses a timestamp that carries its own offset, e.g. "2025-12-15T15:15:00+07:00".
func ParseOffset(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse RFC3339 timestamp %q: %w", value, err)
	}
	return t, nil
}

// ParseCompactOffset parses a timestamp whose offset omits the colon.
func ParseCompactOffset(value string) (time.Time, error) {
	t, err := time.Parse(LayoutCompactOffset, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse compact-offset timestamp %q: %w", value, err)
	}
	return t, nil
}

// ParseInZone parses a zone-less timestamp against an IANA location name.
func ParseInZone(value, ianaZone string) (time.Time, error) {
	loc, err := Location(ianaZone)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.ParseInLocation(LayoutNaive, strings.TrimSpace(value), loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse naive timestamp %q in zone %q: %w", value, ianaZone, err)
	}
	return t, nil
}

// ParseDate parses a calendar date such as "2025-12-15".
func ParseDate(value string) (time.Time, error) {
	t, err := time.Parse(LayoutDate, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", value, err)
	}
	return t, nil
}

// LocalDate renders an instant's calendar date in its own zone: the date a passenger
// would read off a boarding pass, not the UTC date.
func LocalDate(t time.Time) string {
	return t.Format(LayoutDate)
}

// locationCache avoids re-reading the zoneinfo database on every lookup.
var locationCache sync.Map // name -> *time.Location

// Location resolves an IANA zone name, caching the result.
func Location(ianaZone string) (*time.Location, error) {
	name := strings.TrimSpace(ianaZone)
	if name == "" {
		return nil, fmt.Errorf("empty timezone name")
	}
	if cached, ok := locationCache.Load(name); ok {
		return cached.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", name, err)
	}
	locationCache.Store(name, loc)
	return loc, nil
}

// DurationMinutes returns whole minutes between two instants.
func DurationMinutes(from, to time.Time) int {
	return int(to.Sub(from).Round(time.Minute) / time.Minute)
}

// FormatDuration renders minutes as "4h 20m", "45m" or "2h".
func FormatDuration(minutes int) string {
	if minutes < 0 {
		return "-" + FormatDuration(-minutes)
	}
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

// MinutesSinceMidnight gives an instant's local wall-clock position, for time-of-day
// filters. A 20:35+08:00 arrival reads as 20:35, not its UTC equivalent.
func MinutesSinceMidnight(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// ParseClock turns "HH:MM" into minutes since midnight.
func ParseClock(value string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse time-of-day %q, want HH:MM: %w", value, err)
	}
	return t.Hour()*60 + t.Minute(), nil
}
