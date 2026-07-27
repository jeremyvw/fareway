package search

import (
	"fmt"
	"testing"
)

func TestProbeSorts(t *testing.T) {
	for _, option := range SortOptions() {
		flights := spread(t)
		if err := sortFlights(flights, option); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%-16s", option)
		for _, f := range flights {
			fmt.Printf(" %-11s(%d/%dm)", f.ID, f.Price.Amount/1000, f.TotalMinutes())
		}
		fmt.Println()
	}
}
