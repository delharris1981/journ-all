package handlers

import (
	"net/http"
	"strconv"
	"time"

	"journall/internal/auth"
)

type CalendarDay struct {
	Date       string
	Day        int
	HasEntry   bool
	IsToday    bool
	OtherMonth bool
}

type CalendarData struct {
	Year      int
	Month     int
	MonthName string
	Days      []CalendarDay
	PrevYear  int
	PrevMonth int
	NextYear  int
	NextMonth int
}

// buildMonth returns whole weeks (leading and trailing days from the
// neighbouring months) so the 7-column grid has no empty cells.
func buildMonth(first time.Time, entryDates map[string]bool, today string) []CalendarDay {
	monthEnd := first.AddDate(0, 1, 0)
	last := monthEnd.AddDate(0, 0, -1)

	var days []CalendarDay
	startWeekday := int(first.Weekday())
	for i := 0; i < startWeekday; i++ {
		d := first.AddDate(0, 0, -startWeekday+i)
		days = append(days, CalendarDay{
			Date: d.Format("2006-01-02"), Day: d.Day(), OtherMonth: true,
		})
	}
	for d := first; d.Before(monthEnd); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		days = append(days, CalendarDay{
			Date: ds, Day: d.Day(), HasEntry: entryDates[ds], IsToday: ds == today,
		})
	}
	lastWeekday := int(last.Weekday())
	for i := lastWeekday + 1; i < 7; i++ {
		d := last.AddDate(0, 0, i-lastWeekday)
		days = append(days, CalendarDay{
			Date: d.Format("2006-01-02"), Day: d.Day(), OtherMonth: true,
		})
	}
	return days
}

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	now := time.Now()
	year, month := now.Year(), int(now.Month())

	if y := r.URL.Query().Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil {
			year = v
		}
	}
	if m := r.URL.Query().Get("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v >= 1 && v <= 12 {
			month = v
		}
	}

	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := first.AddDate(0, 1, 0)

	entryDates, err := h.store.EntryDates(userID, first, monthEnd)
	if err != nil {
		entryDates = map[string]bool{}
	}

	days := buildMonth(first, entryDates, time.Now().Format("2006-01-02"))

	prevMonth := month - 1
	prevYear := year
	if prevMonth < 1 {
		prevMonth = 12
		prevYear--
	}
	nextMonth := month + 1
	nextYear := year
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}

	h.render(w, "calendar", PageData{
		User:  userID,
		Title: "Calendar",
		Calendar: &CalendarData{
			Year:      year,
			Month:     month,
			MonthName: time.Month(month).String(),
			Days:      days,
			PrevYear:  prevYear,
			PrevMonth: prevMonth,
			NextYear:  nextYear,
			NextMonth: nextMonth,
		},
	})
}
