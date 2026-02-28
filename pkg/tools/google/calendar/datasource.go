// Package calendar provides a DataSource connector that enumerates Google
// Calendar events for configured calendars. It uses the Calendar API to list
// events in a time window and returns one NormalizedItem per event for the
// sync pipeline to vectorize.
package calendar

import (
	"context"
	"fmt"
	"time"

	"github.com/stackgenhq/genie/pkg/datasource"
	gcal "google.golang.org/api/calendar/v3"
)

const (
	datasourceNameCalendar = "calendar"
	calendarListWindow     = 30 * 24 * time.Hour
)

// CalendarConnector implements datasource.DataSource for Google Calendar.
// It lists events for each calendar in scope.CalendarIDs over a forward
// window (e.g. 30 days) and returns one NormalizedItem per event.
type CalendarConnector struct {
	svc *gcal.Service
}

// NewCalendarConnector returns a DataSource that lists events from the given
// Google Calendar service. The caller must provide an initialised *gcal.Service
// (e.g. from the same OAuth or service-account setup as the calendar tools).
func NewCalendarConnector(svc *gcal.Service) *CalendarConnector {
	return &CalendarConnector{svc: svc}
}

// Name returns the source identifier for Calendar.
func (c *CalendarConnector) Name() string {
	return datasourceNameCalendar
}

// ListItems lists events for each calendar in scope.CalendarIDs from now
// through the next 30 days, and returns one NormalizedItem per event with ID
// "calendar:calendarID:eventID". Content is summary + description; metadata
// includes start time and location.
func (c *CalendarConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	if len(scope.CalendarIDs) == 0 {
		return nil, nil
	}
	if c.svc == nil {
		return nil, fmt.Errorf("calendar: service is nil")
	}
	now := time.Now()
	timeMin := now.Format(time.RFC3339)
	timeMax := now.Add(calendarListWindow).Format(time.RFC3339)
	var out []datasource.NormalizedItem
	for _, calID := range scope.CalendarIDs {
		events, err := c.svc.Events.List(calID).
			ShowDeleted(false).
			SingleEvents(true).
			TimeMin(timeMin).
			TimeMax(timeMax).
			MaxResults(maxListEvents).
			OrderBy("startTime").
			Context(ctx).
			Do()
		if err != nil {
			return nil, fmt.Errorf("calendar %s: %w", calID, err)
		}
		for _, item := range events.Items {
			if item.Id == "" {
				continue
			}
			updatedAt, _ := parseCalendarTime(item.Updated)
			content := item.Summary
			if item.Description != "" {
				content = item.Summary + "\n\n" + item.Description
			}
			meta := map[string]string{"title": item.Summary}
			if item.Start != nil && item.Start.DateTime != "" {
				meta["start"] = item.Start.DateTime
			}
			if item.Location != "" {
				meta["location"] = item.Location
			}
			out = append(out, datasource.NormalizedItem{
				ID:        "calendar:" + calID + ":" + item.Id,
				Source:    datasourceNameCalendar,
				UpdatedAt: updatedAt,
				Content:   content,
				Metadata:  meta,
			})
		}
	}
	return out, nil
}

func parseCalendarTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

// Ensure CalendarConnector implements datasource.DataSource at compile time.
var _ datasource.DataSource = (*CalendarConnector)(nil)
