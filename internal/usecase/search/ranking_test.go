package search

import (
	"testing"

	"github.com/jeremyvw/fareway/internal/model"
)

func scoresByID(flights []model.Flight) map[string]float64 {
	out := make(map[string]float64, len(flights))
	for _, f := range flights {
		out[f.ID] = f.BestValueScore
	}
	return out
}

// TestBestValueBalancesPriceAgainstConvenience is the whole point of the score: the cheapest
// flight does not automatically win when it is also the slowest and carries a stop.
//
// Working through spread(t) by hand, price range 485000-1450000 and duration range 105-260:
//
//	cheap-slow  best price, worst duration, worst stops -> 1-(0.5*0 + 0.3*1 + 0.2*1) = 0.50
//	mid-fast    price 0.4818, best duration, best stops -> 1-(0.5*0.4818)             = 0.7591
//	high-late   worst price, best duration, best stops  -> 1-(0.5*1)                  = 0.50
func TestBestValueBalancesPriceAgainstConvenience(t *testing.T) {
	flights := spread(t)
	scoreBestValue(flights)
	scores := scoresByID(flights)

	if got := scores["cheap-slow"]; got != 50 {
		t.Errorf("cheap-slow score = %.1f, want 50", got)
	}
	if got := scores["mid-fast"]; got != 75.9 {
		t.Errorf("mid-fast score = %.1f, want 75.9", got)
	}
	if got := scores["high-late"]; got != 50 {
		t.Errorf("high-late score = %.1f, want 50", got)
	}

	// The cheapest flight must not top the ranking on price alone.
	if scores["cheap-slow"] >= scores["mid-fast"] {
		t.Errorf("cheapest (%.1f) outranks the balanced option (%.1f); price is dominating entirely",
			scores["cheap-slow"], scores["mid-fast"])
	}
}

func TestBestValueOrderingPutsTheBalancedOptionFirst(t *testing.T) {
	flights := spread(t)
	scoreBestValue(flights)

	if err := sortFlights(flights, model.SortBestValue); err != nil {
		t.Fatalf("sortFlights: %v", err)
	}
	if flights[0].ID != "mid-fast" {
		t.Errorf("best value = %q, want mid-fast; order was %v", flights[0].ID, idsOf(flights))
	}
	if flights[1].ID != "cheap-slow" {
		t.Errorf("second = %q, want cheap-slow on the price tie-break; order was %v",
			flights[1].ID, idsOf(flights))
	}
}

func TestLayoverTimeIsPenalized(t *testing.T) {
	short := filterable(t, "short-wait", 800000, 1, "2025-12-15T08:00:00+07:00", "2025-12-15T12:00:00+08:00", "QZ")
	long := filterable(t, "long-wait", 800000, 1, "2025-12-15T08:00:00+07:00", "2025-12-15T16:00:00+08:00", "JT")

	flights := []model.Flight{short, long}
	scoreBestValue(flights)
	scores := scoresByID(flights)

	if scores["short-wait"] <= scores["long-wait"] {
		t.Errorf("short-wait %.1f should outscore long-wait %.1f; gate-to-gate duration is not being used",
			scores["short-wait"], scores["long-wait"])
	}
	if flights[0].TotalMinutes() >= flights[1].TotalMinutes() {
		t.Fatalf("fixture broken: durations are %d and %d", flights[0].TotalMinutes(), flights[1].TotalMinutes())
	}
}

func TestSingleResultDoesNotDivideByZero(t *testing.T) {
	flights := []model.Flight{
		filterable(t, "only", 500000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:50:00+08:00", "QZ"),
	}
	scoreBestValue(flights)

	if got := flights[0].BestValueScore; got != maxScore {
		t.Errorf("score = %v, want %v for a single result", got, maxScore)
	}
}

func TestIdenticalFlightsAllScoreTopOfScale(t *testing.T) {
	flights := []model.Flight{
		filterable(t, "a", 500000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:50:00+08:00", "QZ"),
		filterable(t, "b", 500000, 0, "2025-12-15T08:00:00+07:00", "2025-12-15T10:50:00+08:00", "JT"),
	}
	scoreBestValue(flights)

	for _, f := range flights {
		if f.BestValueScore != maxScore {
			t.Errorf("%s score = %v, want %v", f.ID, f.BestValueScore, maxScore)
		}
	}
}

func TestScoresStayWithinScale(t *testing.T) {
	flights := spread(t)
	scoreBestValue(flights)

	for _, f := range flights {
		if f.BestValueScore < 0 || f.BestValueScore > maxScore {
			t.Errorf("%s score = %v, outside 0-%v", f.ID, f.BestValueScore, maxScore)
		}
	}
}

func TestScoringEmptyInputIsSafe(t *testing.T) {
	var flights []model.Flight
	scoreBestValue(flights)
}

func TestWeightsSumToOne(t *testing.T) {
	// If they do not, a score of 100 becomes unreachable and the scale silently shifts.
	if total := weightPrice + weightDuration + weightStops; total != 1.0 {
		t.Errorf("weights sum to %v, want 1.0", total)
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		name             string
		value, low, high float64
		want             float64
	}{
		{"best in set", 100, 100, 200, 0},
		{"worst in set", 200, 100, 200, 1},
		{"midpoint", 150, 100, 200, 0.5},
		{"zero range contributes nothing", 100, 100, 100, 0},
		{"inverted range is treated as zero range", 100, 200, 100, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.value, tc.low, tc.high); got != tc.want {
				t.Errorf("normalize(%v, %v, %v) = %v, want %v", tc.value, tc.low, tc.high, got, tc.want)
			}
		})
	}
}
