package search

import (
	"testing"

	"github.com/jeremyvw/fareway/internal/model"
)

func sold(t *testing.T, provider, flightNumber string, price int64, depart string) model.Flight {
	t.Helper()
	return model.Flight{
		ID:           flightNumber + "_" + provider,
		Provider:     provider,
		Airline:      model.Airline{Name: "Shared Air", Code: "SA"},
		FlightNumber: flightNumber,
		Segments: []model.Segment{{
			FlightNumber: flightNumber,
			From:         model.Place{Airport: "CGK", City: "Jakarta"},
			To:           model.Place{Airport: "DPS", City: "Denpasar"},
			Depart:       at(t, depart),
			Arrive:       at(t, "2025-12-15T11:00:00+08:00"),
		}},
		Price:          model.Money{Amount: price, Currency: "IDR"},
		AvailableSeats: 10,
		CabinClass:     "economy",
		Amenities:      []string{},
	}
}

func TestDedupeKeepsTheCheapestOffer(t *testing.T) {
	flights := []model.Flight{
		sold(t, "Expensive", "SA100", 1200000, "2025-12-15T08:00:00+07:00"),
		sold(t, "Cheapest", "SA100", 800000, "2025-12-15T08:00:00+07:00"),
		sold(t, "Middle", "SA100", 950000, "2025-12-15T08:00:00+07:00"),
	}

	kept, merged := dedupeAcrossProviders(flights)

	if len(kept) != 1 {
		t.Fatalf("kept %d results, want 1", len(kept))
	}
	if merged != 2 {
		t.Errorf("merged = %d, want 2", merged)
	}

	winner := kept[0]
	if winner.Provider != "Cheapest" || winner.Price.Amount != 800000 {
		t.Errorf("headline = %s at %d, want Cheapest at 800000", winner.Provider, winner.Price.Amount)
	}
	if len(winner.AlternativePrices) != 2 {
		t.Fatalf("alternatives = %+v, want 2", winner.AlternativePrices)
	}
	// Cheapest alternative first, so the next-best price is always the first entry.
	if winner.AlternativePrices[0].Amount != 950000 || winner.AlternativePrices[1].Amount != 1200000 {
		t.Errorf("alternatives not ordered cheapest first: %+v", winner.AlternativePrices)
	}
	if winner.AlternativePrices[0].Provider != "Middle" {
		t.Errorf("first alternative = %q, want Middle", winner.AlternativePrices[0].Provider)
	}
}

func TestDedupeMatchesOnNumberAndDeparture(t *testing.T) {
	flights := []model.Flight{
		sold(t, "A", "SA100", 800000, "2025-12-15T08:00:00+07:00"),
		sold(t, "B", "SA100", 700000, "2025-12-15T15:00:00+07:00"),
	}

	kept, merged := dedupeAcrossProviders(flights)

	if len(kept) != 2 || merged != 0 {
		t.Errorf("kept %d, merged %d; want 2 and 0 — different departure times are different flights",
			len(kept), merged)
	}
}

func TestDedupeLeavesDistinctFlightsAlone(t *testing.T) {
	flights := []model.Flight{
		sold(t, "A", "SA100", 800000, "2025-12-15T08:00:00+07:00"),
		sold(t, "A", "SA200", 900000, "2025-12-15T08:00:00+07:00"),
	}

	kept, merged := dedupeAcrossProviders(flights)

	if len(kept) != 2 || merged != 0 {
		t.Errorf("kept %d, merged %d; want 2 and 0", len(kept), merged)
	}
	for _, f := range kept {
		if len(f.AlternativePrices) != 0 {
			t.Errorf("%s carries alternatives it should not: %+v", f.ID, f.AlternativePrices)
		}
	}
}

func TestDedupeCollectsAlternativesWhenTheWinnerChangesLate(t *testing.T) {
	flights := []model.Flight{
		sold(t, "First", "SA100", 1000000, "2025-12-15T08:00:00+07:00"),
		sold(t, "Second", "SA100", 1100000, "2025-12-15T08:00:00+07:00"),
		sold(t, "Last", "SA100", 700000, "2025-12-15T08:00:00+07:00"),
	}

	kept, _ := dedupeAcrossProviders(flights)

	if len(kept) != 1 {
		t.Fatalf("kept %d, want 1", len(kept))
	}
	if kept[0].Provider != "Last" {
		t.Errorf("headline = %q, want Last", kept[0].Provider)
	}
	if len(kept[0].AlternativePrices) != 2 {
		t.Errorf("alternatives = %+v, want both earlier offers preserved", kept[0].AlternativePrices)
	}
}

func TestDedupePreservesInputOrderOfFirstAppearance(t *testing.T) {
	flights := []model.Flight{
		sold(t, "A", "SA300", 500000, "2025-12-15T06:00:00+07:00"),
		sold(t, "B", "SA100", 900000, "2025-12-15T08:00:00+07:00"),
		sold(t, "C", "SA100", 800000, "2025-12-15T08:00:00+07:00"),
		sold(t, "D", "SA200", 700000, "2025-12-15T10:00:00+07:00"),
	}

	kept, merged := dedupeAcrossProviders(flights)

	if merged != 1 {
		t.Errorf("merged = %d, want 1", merged)
	}
	want := []string{"SA300", "SA100", "SA200"}
	got := make([]string, 0, len(kept))
	for _, f := range kept {
		got = append(got, f.FlightNumber)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestDedupeHandlesEmptyInput(t *testing.T) {
	kept, merged := dedupeAcrossProviders(nil)
	if len(kept) != 0 || merged != 0 {
		t.Errorf("kept %d, merged %d; want 0 and 0", len(kept), merged)
	}
}
