package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/arran4/golang-ical"
)

const defaultHolidayCalendarLink = "https://www.officeholidays.com/ics/spain/catalonia"

func LoadCalendar(ctx context.Context, link string) ([]*ics.VEvent, error) {
	slog.InfoContext(ctx, "Loading calendar", "link", link)

	cal, err := ics.ParseCalendarFromUrl(link, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse calendar: %w", err)
	}

	return cal.Events(), nil
}

func holidayCalendarLink() string {
	if v := os.Getenv("HOLIDAY_CALENDAR_LINK"); v != "" {
		return v
	}

	return defaultHolidayCalendarLink
}

var holidayEvents atomic.Pointer[[]*ics.VEvent]

// loadHolidayCalendar fetches and caches the holiday calendar once. It's used
// as holidaysTool's Init so the network fetch happens at startup instead of
// on every get_holidays call.
func loadHolidayCalendar(ctx context.Context) error {
	events, err := LoadCalendar(ctx, holidayCalendarLink())
	if err != nil {
		return fmt.Errorf("failed to load holiday calendar: %w", err)
	}

	holidayEvents.Store(&events)

	return nil
}

func handleGetHolidays(ctx context.Context, rawArgs string) (string, error) {
	var events []*ics.VEvent
	if cached := holidayEvents.Load(); cached != nil {
		events = *cached
	} else {
		var err error
		events, err = LoadCalendar(ctx, holidayCalendarLink())
		if err != nil {
			return "", errors.New("failed to load holiday events: " + err.Error())
		}
	}

	var args struct {
		BeforeDate time.Time `json:"before_date,omitempty"`
		AfterDate  time.Time `json:"after_date,omitempty"`
		MaxCount   int       `json:"max_count,omitempty"`
	}

	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", errors.New("failed to parse tool call arguments: " + err.Error())
	}

	var holidays []string
	for _, event := range events {
		date, err := event.GetAllDayStartAt()
		if err != nil {
			continue
		}

		if args.MaxCount > 0 && len(holidays) >= args.MaxCount {
			break
		}

		if !args.BeforeDate.IsZero() && date.After(args.BeforeDate) {
			continue
		}

		if !args.AfterDate.IsZero() && date.Before(args.AfterDate) {
			continue
		}

		holidays = append(holidays, date.Format(time.DateOnly)+": "+event.GetProperty(ics.ComponentPropertySummary).Value)
	}

	return strings.Join(holidays, "\n"), nil
}
