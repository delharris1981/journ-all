package handlers

import (
	"testing"
	"time"
)

func TestBuildMonthFillsWholeWeeks(t *testing.T) {
	for year := 2024; year <= 2027; year++ {
		for month := 1; month <= 12; month++ {
			first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			days := buildMonth(first, nil, "")

			if len(days)%7 != 0 {
				t.Fatalf("%d-%02d: %d cells is not a whole number of weeks", year, month, len(days))
			}
			inMonth := 0
			for _, d := range days {
				if !d.OtherMonth {
					inMonth++
				}
			}
			if want := first.AddDate(0, 1, -1).Day(); inMonth != want {
				t.Fatalf("%d-%02d: %d in-month days, want %d", year, month, inMonth, want)
			}
			if days[0].Date != first.AddDate(0, 0, -int(first.Weekday())).Format("2006-01-02") {
				t.Fatalf("%d-%02d: first cell %s is not the preceding Sunday", year, month, days[0].Date)
			}
		}
	}
}
