package search

import (
	"math"

	"github.com/jeremyvw/fareway/internal/model"
)

// Best-value weights. Price dominates because it is what a traveller on this route is
// overwhelmingly choosing on. Duration is the next largest cost to them. Stop count carries the
// smallest weight because a connection already costs time, which duration has counted — weight
// it heavily and a stop gets punished twice.
//
// Named constants rather than literals because these are the most arguable numbers in the
// project, and a reviewer should be able to find and challenge them in one place.
const (
	weightPrice    = 0.5
	weightDuration = 0.3
	weightStops    = 0.2
)

// maxScore is the top of the reported scale. Scores run 0-100 with higher meaning better value,
// because "87.5" reads as a rating while a raw 0-1 penalty reads as an error metric.
const maxScore = 100.0

// scoreRounding keeps scores to one decimal place. The next digit is noise given the weights are
// a judgement call, and it keeps the JSON readable.
const scoreRounding = 10

// scoreBestValue attaches a best-value score to every flight, in place.
//
// Price, total duration and stop count are each min-max normalized across the current result
// set, so the score answers "how good is this among the options actually returned" rather than
// against an absolute scale that would need per-route tuning to mean anything. The consequence
// worth documenting: scores are comparable within one response and not between responses.
//
// Duration comes from model.Flight.TotalMinutes, which is gate-to-gate including layovers, so a
// cheap itinerary with a long wait is penalized for the wait rather than only for the stop.
func scoreBestValue(flights []model.Flight) {
	if len(flights) == 0 {
		return
	}

	// A single result has nothing to be normalized against and every range would be zero.
	if len(flights) == 1 {
		flights[0].BestValueScore = maxScore
		return
	}

	minPrice, maxPrice := flights[0].Price.Amount, flights[0].Price.Amount
	minDuration, maxDuration := flights[0].TotalMinutes(), flights[0].TotalMinutes()
	minStops, maxStops := flights[0].Stops(), flights[0].Stops()

	for _, f := range flights[1:] {
		minPrice = min(minPrice, f.Price.Amount)
		maxPrice = max(maxPrice, f.Price.Amount)
		minDuration = min(minDuration, f.TotalMinutes())
		maxDuration = max(maxDuration, f.TotalMinutes())
		minStops = min(minStops, f.Stops())
		maxStops = max(maxStops, f.Stops())
	}

	for i := range flights {
		penalty := weightPrice*normalize(float64(flights[i].Price.Amount), float64(minPrice), float64(maxPrice)) +
			weightDuration*normalize(float64(flights[i].TotalMinutes()), float64(minDuration), float64(maxDuration)) +
			weightStops*normalize(float64(flights[i].Stops()), float64(minStops), float64(maxStops))

		// Rounded to one decimal: the next digit is noise given the weights are a judgement
		// call, and it keeps the JSON readable.
		flights[i].BestValueScore = math.Round((1-penalty)*maxScore*scoreRounding) / scoreRounding
	}
}

// normalize maps a value onto 0-1 where 0 is the best in the set.
//
// When every flight shares a value the range is zero, so the dimension cannot distinguish them
// and must contribute no penalty. Returning 0 rather than dividing is what keeps a
// single-price result set from producing NaN.
func normalize(value, low, high float64) float64 {
	if high <= low {
		return 0
	}
	return (value - low) / (high - low)
}
