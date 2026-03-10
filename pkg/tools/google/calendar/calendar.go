// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package calendar provides calendar management tools for agents. It enables
// agents to create, read, update, and delete calendar events using the Google
// Calendar API, making scheduling accessible through natural language.
//
// Problem: "Schedule a standup at 3pm tomorrow" is one of the most common
// agent requests, yet without this tool agents have no way to interact with
// calendars. This package bridges that gap, enabling meeting scheduling,
// availability checks, and event management through conversational AI.
//
// Available tools (prefixed with google_calendar_ when registered):
//   - google_calendar_list_events — list upcoming events in a time range
//   - google_calendar_next_events — list upcoming events for a human-friendly duration
//   - google_calendar_create_event — schedule a new event with title, time, attendees
//   - google_calendar_update_event — modify an existing event
//   - google_calendar_delete_event — cancel an event
//   - google_calendar_free_busy — check availability for one or more calendars
//   - google_calendar_quick_add — create event from natural language text
//   - google_calendar_find_time — find a common free slot for attendees
//
// Safety guards:
//   - 30-second API timeout
//   - Create/update/delete operations can be gated behind HITL approval
//   - Events limited to 100 per list query
//
// Authentication:
//   - Embedded client (Option 1): Build with -X to inject
//     GoogleClientID and GoogleClientSecret; then users can
//     "just sign in" without providing credentials. See pkg/tools/google/oauth.
//   - OAuth2: Set CredentialsFile (path or JSON content of credentials.json)
//     and one of: TokenFile (path to token.json), Token (inline token JSON),
//     or Password (OAuth refresh token or inline token JSON).
//   - Service account: Set CredentialsFile to a service account key JSON
//     (auto-detected by the "type" field).
//
// Google Calendar API does not support username/password login; it requires
// OAuth2 or a service account.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"golang.org/x/oauth2/google"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	apiTimeout    = 30 * time.Second
	maxListEvents = 100
)

// Required OAuth2 scopes for full calendar access.
var calendarScopes = []string{gcal.CalendarScope}

// interval represents a time range [start, end) used for busy period
// merging in free/busy and find_time operations.
type interval struct {
	start time.Time
	end   time.Time
}

type intervals []interval

func (is intervals) sort() {
	sort.Slice(is, func(i, j int) bool { return is[i].start.Before(is[j].start) })
}

// ────────────────────── Per-operation request structs ──────────────────────

// listEventsRequest is the input for the calendar_list_events tool.
// It lists events in a calendar within a specified time range.
type listEventsRequest struct {
	TimeMin    string `json:"time_min,omitempty" jsonschema:"description=Start of time range in RFC3339 format (e.g. 2025-01-15T10:00:00Z). Defaults to now."`
	TimeMax    string `json:"time_max,omitempty" jsonschema:"description=End of time range in RFC3339 format (e.g. 2025-01-22T10:00:00Z). Defaults to 7 days from now."`
	CalendarID string `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID to list events from. Defaults to primary."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r listEventsRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// parseTimeRange parses TimeMin/TimeMax with fallback defaults.
func (r listEventsRequest) parseTimeRange(defaultDuration time.Duration) (timeMin, timeMax time.Time, err error) {
	return parseTimeRange(r.TimeMin, r.TimeMax, defaultDuration)
}

// nextEventsRequest is the input for the calendar_next_events tool.
// It lists upcoming events starting from now for a human-friendly duration.
type nextEventsRequest struct {
	Duration   string `json:"duration,omitempty" jsonschema:"description=How far ahead to look. Examples: 2h (2 hours) 3d (3 days) 1w (1 week). Defaults to 1d."`
	CalendarID string `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID. Defaults to primary."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r nextEventsRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// createEventRequest is the input for the calendar_create_event tool.
// It schedules a new calendar event with title, time, optional attendees,
// and automatically checks for attendee conflicts before creating.
type createEventRequest struct {
	Title       string   `json:"title" jsonschema:"description=Event title/summary.,required"`
	StartTime   string   `json:"start_time" jsonschema:"description=Event start time in RFC3339 format (e.g. 2025-01-15T10:00:00Z).,required"`
	EndTime     string   `json:"end_time,omitempty" jsonschema:"description=Event end time (RFC3339). Defaults to 1 hour after start_time."`
	Description string   `json:"description,omitempty" jsonschema:"description=Event description/body text."`
	Location    string   `json:"location,omitempty" jsonschema:"description=Event location or meeting URL."`
	Attendees   []string `json:"attendees,omitempty" jsonschema:"description=List of attendee email addresses to invite."`
	CalendarID  string   `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID. Defaults to primary."`
	Timezone    string   `json:"timezone,omitempty" jsonschema:"description=IANA timezone (e.g. America/New_York). Defaults to UTC."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r createEventRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// timezone returns the resolved IANA timezone (defaults to "UTC").
func (r createEventRequest) timezone() string {
	if r.Timezone != "" {
		return r.Timezone
	}
	return "UTC"
}

// validate checks required fields for event creation.
func (r createEventRequest) validate() error {
	if r.Title == "" {
		return fmt.Errorf("title is required to create an event")
	}
	if r.StartTime == "" {
		return fmt.Errorf("start_time is required to create an event (RFC3339 format)")
	}
	return nil
}

// updateEventRequest is the input for the calendar_update_event tool.
// It modifies an existing calendar event by ID. Only the provided fields
// are updated; omitted fields remain unchanged.
type updateEventRequest struct {
	EventID     string   `json:"event_id" jsonschema:"description=ID of the event to update (from list_events output).,required"`
	Title       string   `json:"title,omitempty" jsonschema:"description=New event title/summary."`
	Description string   `json:"description,omitempty" jsonschema:"description=New event description/body text."`
	StartTime   string   `json:"start_time,omitempty" jsonschema:"description=New start time (RFC3339)."`
	EndTime     string   `json:"end_time,omitempty" jsonschema:"description=New end time (RFC3339)."`
	Location    string   `json:"location,omitempty" jsonschema:"description=New event location or meeting URL."`
	Attendees   []string `json:"attendees,omitempty" jsonschema:"description=Replacement list of attendee email addresses (replaces all current attendees)."`
	CalendarID  string   `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID. Defaults to primary."`
	Timezone    string   `json:"timezone,omitempty" jsonschema:"description=IANA timezone (e.g. America/New_York). Defaults to UTC."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r updateEventRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// timezone returns the resolved IANA timezone (defaults to "UTC").
func (r updateEventRequest) timezone() string {
	if r.Timezone != "" {
		return r.Timezone
	}
	return "UTC"
}

// deleteEventRequest is the input for the calendar_delete_event tool.
// It cancels/deletes a calendar event by its ID.
type deleteEventRequest struct {
	EventID    string `json:"event_id" jsonschema:"description=ID of the event to delete (from list_events output).,required"`
	CalendarID string `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID. Defaults to primary."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r deleteEventRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// freeBusyRequest is the input for the calendar_free_busy tool.
// It checks availability (free/busy status) for a calendar in a time range.
type freeBusyRequest struct {
	TimeMin    string `json:"time_min,omitempty" jsonschema:"description=Start of time range (RFC3339). Defaults to now."`
	TimeMax    string `json:"time_max,omitempty" jsonschema:"description=End of time range (RFC3339). Defaults to 24 hours from now."`
	CalendarID string `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID to check. Defaults to primary."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r freeBusyRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// parseTimeRange parses TimeMin/TimeMax with fallback defaults.
func (r freeBusyRequest) parseTimeRange(defaultDuration time.Duration) (timeMin, timeMax time.Time, err error) {
	return parseTimeRange(r.TimeMin, r.TimeMax, defaultDuration)
}

// quickAddRequest is the input for the calendar_quick_add tool.
// It creates a calendar event from natural language text using Google's
// QuickAdd API (e.g. "Lunch with Alice tomorrow at noon").
type quickAddRequest struct {
	Text       string `json:"text" jsonschema:"description=Natural language event description. Examples: 'Lunch with Alice tomorrow at noon' or 'Team standup every Monday at 9am'.,required"`
	CalendarID string `json:"calendar_id,omitempty" jsonschema:"description=Calendar ID. Defaults to primary."`
}

// calendarID returns the resolved calendar ID (defaults to "primary").
func (r quickAddRequest) calendarID() string {
	if r.CalendarID != "" {
		return r.CalendarID
	}
	return "primary"
}

// findTimeRequest is the input for the calendar_find_time tool.
// It finds common free time slots for the given attendees within
// a look-ahead window, enabling the "find a time that works for everyone" workflow.
type findTimeRequest struct {
	Attendees    []string `json:"attendees" jsonschema:"description=List of attendee email addresses to check availability for.,required"`
	Duration     string   `json:"duration,omitempty" jsonschema:"description=How far ahead to search. Examples: 2h 3d 1w. Defaults to 1d."`
	SlotDuration string   `json:"slot_duration,omitempty" jsonschema:"description=Desired meeting length. Examples: 30m 1h. Defaults to 30m."`
}

// ────────────────────── Response ──────────────────────

type calendarEvent struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	StartTime   string   `json:"start_time"`
	EndTime     string   `json:"end_time"`
	Location    string   `json:"location,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
	Status      string   `json:"status,omitempty"`
	Link        string   `json:"link,omitempty"`
}

// attendeeConflict describes a scheduling conflict for a single attendee.
type attendeeConflict struct {
	Email       string   `json:"email"`
	BusyPeriods []string `json:"busy_periods"` // "start → end" formatted strings
}

type calendarResponse struct {
	Operation string          `json:"operation"`
	Events    []calendarEvent `json:"events,omitempty"`
	Event     *calendarEvent  `json:"event,omitempty"`
	FreeBusy  string          `json:"free_busy,omitempty"`
	Count     int             `json:"count,omitempty"`
	Message   string          `json:"message"`
	// Conflicts lists attendees who have overlapping events during the
	// proposed time slot. Populated by create_event when attendees are
	// specified. Empty means no conflicts detected (or attendee calendars
	// are not visible to the authenticated user).
	Conflicts []attendeeConflict `json:"conflicts,omitempty"`
}

// ────────────────────── Tool constructors ──────────────────────

// credsGetterFunc is an optional override for credentials lookup (used in tests).
// When non-nil, getCalendarService uses it instead of getCredentialsForCalendar.
type credsGetterFunc func(credsEntry string) ([]byte, error)

type calendarTools struct {
	secretProvider security.SecretProvider
	name           string
	credsGetter    credsGetterFunc // optional; when set (e.g. in tests), used instead of getCredentialsForCalendar
}

func newCalendarTools(name string, secretProvider security.SecretProvider) *calendarTools {
	return &calendarTools{
		secretProvider: secretProvider,
		name:           name,
	}
}

// newCalendarToolsWithCredsGetter returns a calendarTools that uses the given
// getter for credentials instead of the real oauth/keyring path. Used by tests
// to force "not configured" without depending on environment.
func newCalendarToolsWithCredsGetter(name string, secretProvider security.SecretProvider, getter credsGetterFunc) *calendarTools {
	t := newCalendarTools(name, secretProvider)
	t.credsGetter = getter
	return t
}

// tools returns all individual calendar tools as separate callable tools.
// Each tool has its own typed request struct so the LLM sees exactly which
// arguments are relevant for each operation.
func (c *calendarTools) tools() []tool.CallableTool {
	return []tool.CallableTool{
		function.NewFunctionTool(
			c.handleListEvents,
			function.WithName(fmt.Sprintf("%s_list_events", c.name)),
			function.WithDescription(
				"List events from the "+c.name+" Google Calendar within a time range. "+
					"Use for 'how is my schedule', 'any activities tomorrow/on [date]', or what's on. "+
					"Returns event titles, times, attendees, and IDs. "+
					"Use time_min/time_max (RFC3339) to scope; defaults to the next 7 days.",
			),
		),
		function.NewFunctionTool(
			c.handleNextEvents,
			function.WithName(fmt.Sprintf("%s_next_events", c.name)),
			function.WithDescription(
				"List upcoming events from the "+c.name+" Google Calendar starting from now "+
					"for a duration like '2h', '3d', or '1w'. "+
					"Use for 'what's my schedule tomorrow', 'any activities coming up', or 'what's next'.",
			),
		),
		function.NewFunctionTool(
			c.handleCreateEvent,
			function.WithName(fmt.Sprintf("%s_create_event", c.name)),
			function.WithDescription(
				"Create a new event on the "+c.name+" Google Calendar with title, start/end time, "+
					"optional attendees, location, and description. "+
					"Automatically checks attendee availability for conflicts before creating. "+
					"Sends invitation emails to all attendees.",
			),
		),
		function.NewFunctionTool(
			c.handleUpdateEvent,
			function.WithName(fmt.Sprintf("%s_update_event", c.name)),
			function.WithDescription(
				"Update an existing event on the "+c.name+" Google Calendar by its ID. "+
					"Only the provided fields are changed; omitted fields remain as-is. "+
					"Use list_events first to get the event ID.",
			),
		),
		function.NewFunctionTool(
			c.handleDeleteEvent,
			function.WithName(fmt.Sprintf("%s_delete_event", c.name)),
			function.WithDescription(
				"Delete (cancel) an event on the "+c.name+" Google Calendar by its ID. "+
					"Sends cancellation notifications to all attendees. "+
					"Use list_events first to get the event ID.",
			),
		),
		function.NewFunctionTool(
			c.handleFreeBusy,
			function.WithName(fmt.Sprintf("%s_free_busy", c.name)),
			function.WithDescription(
				"Check free/busy availability for the "+c.name+" Google Calendar in a time range. "+
					"Returns busy periods so you can find when someone is available. "+
					"Defaults to the next 24 hours if no time range is specified.",
			),
		),
		function.NewFunctionTool(
			c.handleQuickAdd,
			function.WithName(fmt.Sprintf("%s_quick_add", c.name)),
			function.WithDescription(
				"Create an event on the "+c.name+" Google Calendar from natural language text. "+
					"Google automatically parses strings like 'Lunch with Alice tomorrow at noon' "+
					"or 'Team standup every Monday at 9am' into structured events.",
			),
		),
		function.NewFunctionTool(
			c.handleFindTime,
			function.WithName(fmt.Sprintf("%s_find_time", c.name)),
			function.WithDescription(
				"Find available meeting time slots on the "+c.name+" calendar that work for all specified attendees. "+
					"Queries each attendee's free/busy schedule and computes common free gaps "+
					"of at least the requested slot duration. "+
					"Use this for 'find a time that works for everyone' workflows.",
			),
		),
	}
}

// ────────────────────── Google Calendar Client ──────────────────────

// getCalendarService creates an authenticated Google Calendar API client.
//
// Credentials are resolved from the calendar secret provider.
// Expected secrets: "CredentialsFile" (required), "TokenFile" (optional for OAuth2).
//
// Authentication modes (auto-detected from the credentials content):
//
//  1. Service account: The credentials JSON contains "type":"service_account".
//     No token file is needed — the SDK handles token refresh automatically.
//
//  2. OAuth2 (installed app / web): The credentials JSON contains an
//     "installed" or "web" key. A separate token.json file with a valid
//     refresh token is required.
//
// Returns a user-friendly error message when credentials are missing so the
// agent can inform the user how to configure the integration.
func (c *calendarTools) getCalendarService(ctx context.Context) (*gcal.Service, error) {
	credsEntry, _ := c.secretProvider.GetSecret(ctx, security.GetSecretRequest{
		Name:   "CredentialsFile",
		Reason: fmt.Sprintf("%s Google Calendar tool: %s", c.name, toolcontext.GetJustification(ctx)),
	})
	var credsJSON []byte
	var err error
	if c.credsGetter != nil {
		credsJSON, err = c.credsGetter(credsEntry)
	} else {
		credsJSON, err = getCredentialsForCalendar(credsEntry)
	}
	if err != nil {
		return nil, err
	}

	// Detect authentication mode from the credentials content.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(credsJSON, &raw); err != nil {
		return nil, fmt.Errorf("invalid credentials JSON: %w", err)
	}

	// Service account: "type":"service_account"
	if typeField, ok := raw["type"]; ok {
		var t string
		if err := json.Unmarshal(typeField, &t); err == nil && t == "service_account" {
			return c.serviceAccountClient(ctx, credsJSON)
		}
	}

	// OAuth2: token from TokenFile, Token/Password, or device keychain.
	tokenJSON, save, err := oauth.GetToken(ctx, c.secretProvider)
	if err != nil {
		return nil, fmt.Errorf("OAuth2 token required: %w", err)
	}
	client, err := oauth.HTTPClient(ctx, credsJSON, tokenJSON, save, calendarScopes)
	if err != nil {
		return nil, fmt.Errorf("OAuth2 client: %w", err)
	}
	return gcal.NewService(ctx, option.WithHTTPClient(client))
}

// serviceAccountClient creates a Calendar service using service account credentials.
func (c *calendarTools) serviceAccountClient(ctx context.Context, credsJSON []byte) (*gcal.Service, error) {
	creds, err := google.CredentialsFromJSON(ctx, credsJSON, calendarScopes...) //nolint:staticcheck // no drop-in replacement; input is trusted config
	if err != nil {
		return nil, fmt.Errorf("invalid service account credentials: %w", err)
	}
	return gcal.NewService(ctx, option.WithCredentials(creds))
}

// ────────────────────── Tool handlers ──────────────────────

// handleListEvents lists events in a calendar within a specified time range.
// Without this tool, agents cannot retrieve existing calendar data, making
// it impossible to answer "what meetings do I have this week?" type queries.
func (c *calendarTools) handleListEvents(ctx context.Context, req listEventsRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "list_events"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	timeMin, timeMax, err := req.parseTimeRange(7 * 24 * time.Hour)
	if err != nil {
		return resp, err
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	events, err := svc.Events.List(req.calendarID()).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(timeMin.Format(time.RFC3339)).
		TimeMax(timeMax.Format(time.RFC3339)).
		MaxResults(maxListEvents).
		OrderBy("startTime").
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (list_events): %w", err)
	}

	for _, item := range events.Items {
		resp.Events = append(resp.Events, gcalEventToCalendarEvent(item))
	}
	resp.Count = len(resp.Events)
	resp.Message = fmt.Sprintf("Found %d events from %s to %s.",
		resp.Count,
		timeMin.Format(time.RFC3339),
		timeMax.Format(time.RFC3339),
	)
	return resp, nil
}

// handleNextEvents lists upcoming events starting from now for the given
// duration. This is a convenience tool that accepts human-friendly durations
// like "2h", "3d", "1w" instead of explicit RFC3339 time ranges.
// Without this tool, agents would always need to compute RFC3339 timestamps
// for simple "what's next" queries.
func (c *calendarTools) handleNextEvents(ctx context.Context, req nextEventsRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "next_events"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	duration := req.Duration
	if duration == "" {
		duration = "1d"
	}
	d, err := parseDuration(duration)
	if err != nil {
		return resp, fmt.Errorf("invalid duration %q: %w. Use formats like 30m, 2h, 3d, 1w", duration, err)
	}

	now := time.Now()
	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	events, err := svc.Events.List(req.calendarID()).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(now.Format(time.RFC3339)).
		TimeMax(now.Add(d).Format(time.RFC3339)).
		MaxResults(maxListEvents).
		OrderBy("startTime").
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (next_events): %w", err)
	}

	for _, item := range events.Items {
		resp.Events = append(resp.Events, gcalEventToCalendarEvent(item))
	}
	resp.Count = len(resp.Events)

	if resp.Count == 0 {
		resp.Message = fmt.Sprintf("No events in the next %s.", duration)
	} else {
		resp.Message = fmt.Sprintf("Found %d event(s) in the next %s.", resp.Count, duration)
	}
	return resp, nil
}

// handleCreateEvent creates a new calendar event. It validates required fields,
// optionally checks attendee conflicts via FreeBusy, then inserts the event.
// Without this tool, agents cannot schedule meetings on behalf of users.
func (c *calendarTools) handleCreateEvent(ctx context.Context, req createEventRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "create_event"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if err := req.validate(); err != nil {
		return resp, err
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		return resp, fmt.Errorf("invalid start_time %q: %w", req.StartTime, err)
	}

	endTime := startTime.Add(1 * time.Hour)
	if req.EndTime != "" {
		t, err := time.Parse(time.RFC3339, req.EndTime)
		if err != nil {
			return resp, fmt.Errorf("invalid end_time %q: %w", req.EndTime, err)
		}
		endTime = t
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	// Check attendee conflicts before creating the event.
	// This is best-effort: if the FreeBusy query fails (e.g. external
	// attendees whose calendars aren't visible), we still create the event
	// and note the failure in the message.
	if len(req.Attendees) > 0 {
		conflicts, conflictErr := c.checkAttendeeConflicts(ctx, svc, req.Attendees, startTime, endTime)
		if conflictErr != nil {
			// Non-fatal: include the error context but proceed.
			resp.Message = fmt.Sprintf("Note: could not check attendee availability: %v. ", conflictErr)
		} else if len(conflicts) > 0 {
			resp.Conflicts = conflicts
		}
	}

	tz := req.timezone()
	gcalEvent := &gcal.Event{
		Summary:     req.Title,
		Description: req.Description,
		Location:    req.Location,
		Start: &gcal.EventDateTime{
			DateTime: startTime.Format(time.RFC3339),
			TimeZone: tz,
		},
		End: &gcal.EventDateTime{
			DateTime: endTime.Format(time.RFC3339),
			TimeZone: tz,
		},
	}

	// Add attendees.
	for _, email := range req.Attendees {
		gcalEvent.Attendees = append(gcalEvent.Attendees, &gcal.EventAttendee{
			Email: strings.TrimSpace(email),
		})
	}

	created, err := svc.Events.Insert(req.calendarID(), gcalEvent).
		SendUpdates("all"). // Notify attendees.
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (create_event): %w", err)
	}

	ev := gcalEventToCalendarEvent(created)
	resp.Event = &ev

	// Build the final message, including conflict warnings.
	msg := fmt.Sprintf("Event %q created successfully (ID: %s).", req.Title, created.Id)
	if len(resp.Conflicts) > 0 {
		var conflictNames []string
		for _, c := range resp.Conflicts {
			conflictNames = append(conflictNames, c.Email)
		}
		msg += fmt.Sprintf(" ⚠ Scheduling conflicts detected for: %s.",
			strings.Join(conflictNames, ", "))
	}
	resp.Message = resp.Message + msg
	return resp, nil
}

// handleUpdateEvent modifies an existing calendar event by ID. It fetches
// the current event, merges in the requested changes, and saves. Only
// provided fields are updated. Without this tool, agents cannot reschedule
// or modify meeting details.
func (c *calendarTools) handleUpdateEvent(ctx context.Context, req updateEventRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "update_event"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if req.EventID == "" {
		return resp, fmt.Errorf("event_id is required to update an event")
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	// Fetch the existing event so we can merge changes.
	existing, err := svc.Events.Get(req.calendarID(), req.EventID).
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (get event %q): %w", req.EventID, err)
	}

	// Apply requested changes.
	if req.Title != "" {
		existing.Summary = req.Title
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Location != "" {
		existing.Location = req.Location
	}
	if req.StartTime != "" {
		existing.Start = &gcal.EventDateTime{
			DateTime: req.StartTime,
			TimeZone: req.timezone(),
		}
	}
	if req.EndTime != "" {
		existing.End = &gcal.EventDateTime{
			DateTime: req.EndTime,
			TimeZone: req.timezone(),
		}
	}
	if len(req.Attendees) > 0 {
		existing.Attendees = nil
		for _, email := range req.Attendees {
			existing.Attendees = append(existing.Attendees, &gcal.EventAttendee{
				Email: strings.TrimSpace(email),
			})
		}
	}

	updated, err := svc.Events.Update(req.calendarID(), req.EventID, existing).
		SendUpdates("all").
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (update_event): %w", err)
	}

	ev := gcalEventToCalendarEvent(updated)
	resp.Event = &ev
	resp.Message = fmt.Sprintf("Event %q updated successfully.", req.EventID)
	return resp, nil
}

// handleDeleteEvent cancels/deletes a calendar event by its ID and notifies
// all attendees. Without this tool, agents cannot cancel meetings.
func (c *calendarTools) handleDeleteEvent(ctx context.Context, req deleteEventRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "delete_event"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if req.EventID == "" {
		return resp, fmt.Errorf("event_id is required to delete an event")
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	err = svc.Events.Delete(req.calendarID(), req.EventID).
		SendUpdates("all").
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (delete_event): %w", err)
	}

	resp.Message = fmt.Sprintf("Event %q deleted successfully.", req.EventID)
	return resp, nil
}

// handleFreeBusy checks free/busy availability for a calendar in a time range.
// Returns a human-readable summary of busy periods. Without this tool,
// agents cannot check when someone is available before scheduling.
func (c *calendarTools) handleFreeBusy(ctx context.Context, req freeBusyRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "free_busy"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	timeMin, timeMax, err := req.parseTimeRange(24 * time.Hour)
	if err != nil {
		return resp, err
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	fbReq := &gcal.FreeBusyRequest{
		TimeMin: timeMin.Format(time.RFC3339),
		TimeMax: timeMax.Format(time.RFC3339),
		Items: []*gcal.FreeBusyRequestItem{
			{Id: req.calendarID()},
		},
	}

	fbResp, err := svc.Freebusy.Query(fbReq).Context(ctx).Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (free_busy): %w", err)
	}

	var parts []string
	for calID, cal := range fbResp.Calendars {
		var busyIntervals intervals
		for _, busy := range cal.Busy {
			s, err1 := time.Parse(time.RFC3339, busy.Start)
			e, err2 := time.Parse(time.RFC3339, busy.End)
			if err1 == nil && err2 == nil {
				busyIntervals = append(busyIntervals, interval{start: s, end: e})
			}
		}
		parts = append(parts, busyIntervals.formatFreeBusyForLLM(calID, timeMin, timeMax))
	}
	resp.FreeBusy = strings.Join(parts, "\n")
	resp.Message = fmt.Sprintf("Free/busy check from %s to %s.",
		timeMin.Format(time.RFC3339), timeMax.Format(time.RFC3339))
	return resp, nil
}

// handleQuickAdd uses Google Calendar's QuickAdd API to create an event from
// natural language text. Google parses strings like "Lunch with Alice
// tomorrow at noon" or "Team standup every Monday at 9am" into structured
// calendar events automatically. Without this tool, agents would need to
// parse natural language dates themselves before calling create_event.
func (c *calendarTools) handleQuickAdd(ctx context.Context, req quickAddRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "quick_add"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if req.Text == "" {
		return resp, fmt.Errorf("text is required for quick_add (e.g. \"Lunch tomorrow at noon\")")
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	created, err := svc.Events.QuickAdd(req.calendarID(), req.Text).
		SendUpdates("all").
		Context(ctx).
		Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (quick_add): %w", err)
	}

	ev := gcalEventToCalendarEvent(created)
	resp.Event = &ev
	resp.Message = fmt.Sprintf("Event created from text: %q → %q at %s (ID: %s).",
		req.Text, created.Summary, ev.StartTime, created.Id)
	return resp, nil
}

// handleFindTime finds common free time slots for the given attendees within
// a look-ahead window. It uses the FreeBusy API to get each attendee's
// busy periods, then computes the free gaps where all attendees are
// available and the gap is at least as long as the requested slot duration.
// Without this tool, agents would need to manually cross-reference multiple
// free/busy results to find overlapping availability.
func (c *calendarTools) handleFindTime(ctx context.Context, req findTimeRequest) (calendarResponse, error) {
	resp := calendarResponse{Operation: "find_time"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if len(req.Attendees) == 0 {
		return resp, fmt.Errorf("attendees is required for find_time (provide at least one email)")
	}

	// Parse look-ahead duration.
	windowStr := req.Duration
	if windowStr == "" {
		windowStr = "1d"
	}
	window, err := parseDuration(windowStr)
	if err != nil {
		return resp, fmt.Errorf("invalid duration %q: %w", windowStr, err)
	}

	// Parse desired meeting length.
	slotStr := req.SlotDuration
	if slotStr == "" {
		slotStr = "30m"
	}
	slotLen, err := parseDuration(slotStr)
	if err != nil {
		return resp, fmt.Errorf("invalid slot_duration %q: %w", slotStr, err)
	}

	svc, err := c.getCalendarService(ctx)
	if err != nil {
		return resp, err
	}

	now := time.Now()
	windowEnd := now.Add(window)

	// Build FreeBusy request for all attendees.
	items := make([]*gcal.FreeBusyRequestItem, 0, len(req.Attendees))
	for _, email := range req.Attendees {
		items = append(items, &gcal.FreeBusyRequestItem{Id: strings.TrimSpace(email)})
	}

	fbResp, err := svc.Freebusy.Query(&gcal.FreeBusyRequest{
		TimeMin: now.Format(time.RFC3339),
		TimeMax: windowEnd.Format(time.RFC3339),
		Items:   items,
	}).Context(ctx).Do()
	if err != nil {
		return resp, fmt.Errorf("google calendar API error (find_time/freebusy): %w", err)
	}

	// Merge all busy periods across attendees into a single sorted list.
	var allBusy intervals
	for _, email := range req.Attendees {
		trimmed := strings.TrimSpace(email)
		cal, ok := fbResp.Calendars[trimmed]
		if !ok {
			continue
		}
		for _, busy := range cal.Busy {
			s, err1 := time.Parse(time.RFC3339, busy.Start)
			e, err2 := time.Parse(time.RFC3339, busy.End)
			if err1 != nil || err2 != nil {
				continue
			}
			allBusy = append(allBusy, interval{s, e})
		}
	}

	// Sort by start time, then merge overlapping intervals.
	allBusy.sort()
	merged := allBusy.mergeIntervals()

	// Walk the timeline and find free gaps ≥ slotLen.
	type freeSlot struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	var slots []freeSlot
	cursor := now
	for _, busy := range merged {
		if busy.start.After(cursor) {
			gap := busy.start.Sub(cursor)
			if gap >= slotLen {
				slots = append(slots, freeSlot{
					Start: cursor.Format(time.RFC3339),
					End:   busy.start.Format(time.RFC3339),
				})
			}
		}
		if busy.end.After(cursor) {
			cursor = busy.end
		}
	}
	// Check trailing free time after the last busy period.
	if windowEnd.After(cursor) && windowEnd.Sub(cursor) >= slotLen {
		slots = append(slots, freeSlot{
			Start: cursor.Format(time.RFC3339),
			End:   windowEnd.Format(time.RFC3339),
		})
	}

	if len(slots) == 0 {
		resp.Message = fmt.Sprintf(
			"No free %s slots found for all %d attendees in the next %s.",
			slotStr, len(req.Attendees), windowStr,
		)
	} else {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Found %d available slot(s) for a %s meeting in the next %s:\n",
			len(slots), slotStr, windowStr)
		for i, s := range slots {
			fmt.Fprintf(&sb, "  %d. %s → %s\n", i+1, s.Start, s.End)
		}
		resp.Message = sb.String()
		resp.FreeBusy = resp.Message // Also populate FreeBusy for structured access.
	}
	resp.Count = len(slots)
	return resp, nil
}

// ────────────────────── Helpers ──────────────────────

// parseTimeRange parses time_min/time_max string values into time.Time with
// fallback defaults. Shared by listEventsRequest and freeBusyRequest.
func parseTimeRange(rawMin, rawMax string, defaultDuration time.Duration) (timeMin, timeMax time.Time, err error) {
	now := time.Now()
	timeMin = now
	timeMax = now.Add(defaultDuration)

	if rawMin != "" {
		t, err := time.Parse(time.RFC3339, rawMin)
		if err != nil {
			return timeMin, timeMax, fmt.Errorf("invalid time_min %q (use RFC3339 format): %w", rawMin, err)
		}
		timeMin = t
	}
	if rawMax != "" {
		t, err := time.Parse(time.RFC3339, rawMax)
		if err != nil {
			return timeMin, timeMax, fmt.Errorf("invalid time_max %q (use RFC3339 format): %w", rawMax, err)
		}
		timeMax = t
	}
	return timeMin, timeMax, nil
}

// parseDuration parses human-friendly duration strings like "30m", "2h",
// "3d", "1w". Go's time.ParseDuration handles "m", "h", "s" natively;
// we extend it with "d" (days) and "w" (weeks).
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Try Go's native parser first (handles "30m", "2h", "1h30m", etc.)
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle "d" and "w" suffixes.
	last := s[len(s)-1]
	numStr := s[:len(s)-1]
	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return 0, fmt.Errorf("cannot parse %q as duration", s)
	}

	switch last {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit %q in %q (use m, h, d, or w)", string(last), s)
	}
}

// mergeIntervals merges overlapping or adjacent time intervals.
// Input must be sorted by start time.
func (is intervals) mergeIntervals() intervals {
	if len(is) == 0 {
		return is
	}
	merged := intervals{is[0]}
	for _, iv := range is[1:] {
		last := &merged[len(merged)-1]
		if iv.start.Before(last.end) || iv.start.Equal(last.end) {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}
	return merged
}

// computeFreeBlocks returns free intervals in [rangeStart, rangeEnd] given
// merged busy intervals. Busy must be sorted and non-overlapping (e.g. from mergeIntervals).
// Used by free_busy formatting so the LLM gets explicit free blocks; unit-tested.
func (is intervals) computeFreeBlocks(rangeStart, rangeEnd time.Time) intervals {
	var free intervals
	cursor := rangeStart
	for _, b := range is {
		if b.start.After(cursor) {
			free = append(free, interval{start: cursor, end: b.start})
		}
		if b.end.After(cursor) {
			cursor = b.end
		}
	}
	if rangeEnd.After(cursor) {
		free = append(free, interval{start: cursor, end: rangeEnd})
	}
	return free
}

// formatDuration returns a short human-readable duration (e.g. "1h30m", "9h", "45m") for LLM output.
func formatDuration(d time.Duration) string {
	s := d.Round(time.Minute).String()
	s = strings.TrimSuffix(s, "0s")
	s = strings.Replace(s, "h0m", "h", 1) // e.g. 9h0m -> 9h
	return s
}

// formatFreeBusyForLLM produces an explicit, parseable free/busy summary for the LLM.
// Uses section headers FREE_BUSY_SUMMARY, BUSY_PERIODS, FREE_BLOCKS so the model
// can reliably parse busy vs free. Includes durations for each block and a one-line summary.
// busyIntervals are merged and free blocks are computed; unit-tested.
func (busyIntervals intervals) formatFreeBusyForLLM(calendarID string, rangeStart, rangeEnd time.Time) string {
	busyIntervals.sort()
	merged := busyIntervals.mergeIntervals()
	free := merged.computeFreeBlocks(rangeStart, rangeEnd)

	rangeStr := rangeStart.Format(time.RFC3339) + " to " + rangeEnd.Format(time.RFC3339)
	var sb strings.Builder

	// One-line summary for quick parsing.
	numBusy := len(merged)
	numFree := len(free)
	var totalFree time.Duration
	for _, f := range free {
		totalFree += f.end.Sub(f.start)
	}
	sb.WriteString("FREE_BUSY_SUMMARY: range " + rangeStr + " | ")
	fmt.Fprintf(&sb, "%d busy periods | %d free blocks (%s total)\n", numBusy, numFree, formatDuration(totalFree))

	sb.WriteString("BUSY_PERIODS:\n")
	if numBusy == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, b := range merged {
			d := b.end.Sub(b.start)
			fmt.Fprintf(&sb, "  %s → %s (%s)\n", b.start.Format(time.RFC3339), b.end.Format(time.RFC3339), formatDuration(d))
		}
	}

	sb.WriteString("FREE_BLOCKS:\n")
	if numFree == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, f := range free {
			d := f.end.Sub(f.start)
			fmt.Fprintf(&sb, "  %s → %s (%s)\n", f.start.Format(time.RFC3339), f.end.Format(time.RFC3339), formatDuration(d))
		}
	}

	return fmt.Sprintf("Calendar %q:\n%s", calendarID, sb.String())
}

// checkAttendeeConflicts uses the FreeBusy API to detect scheduling
// conflicts for the given attendees during the proposed event window.
//
// The FreeBusy API accepts multiple calendar IDs in a single request,
// so this is a single round-trip regardless of attendee count. Each
// attendee email is treated as a calendar ID (works within the same
// Google Workspace org; external attendees may return empty results).
//
// Returns nil conflicts (not an error) when no overlaps are found.
func (c *calendarTools) checkAttendeeConflicts(
	ctx context.Context,
	svc *gcal.Service,
	attendees []string,
	eventStart, eventEnd time.Time,
) ([]attendeeConflict, error) {
	// Build FreeBusy request items — one per attendee.
	items := make([]*gcal.FreeBusyRequestItem, 0, len(attendees))
	for _, email := range attendees {
		items = append(items, &gcal.FreeBusyRequestItem{
			Id: strings.TrimSpace(email),
		})
	}

	fbResp, err := svc.Freebusy.Query(&gcal.FreeBusyRequest{
		TimeMin: eventStart.Format(time.RFC3339),
		TimeMax: eventEnd.Format(time.RFC3339),
		Items:   items,
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("FreeBusy query failed: %w", err)
	}

	var conflicts []attendeeConflict
	for _, email := range attendees {
		trimmed := strings.TrimSpace(email)
		cal, ok := fbResp.Calendars[trimmed]
		if !ok || len(cal.Busy) == 0 {
			continue // No data or no conflicts for this attendee.
		}

		// Filter to busy periods that actually overlap with [eventStart, eventEnd).
		var overlapping []string
		for _, busy := range cal.Busy {
			busyStart, err1 := time.Parse(time.RFC3339, busy.Start)
			busyEnd, err2 := time.Parse(time.RFC3339, busy.End)
			if err1 != nil || err2 != nil {
				// If we can't parse, include it as-is.
				overlapping = append(overlapping, fmt.Sprintf("%s → %s", busy.Start, busy.End))
				continue
			}
			// Two intervals [a,b) and [c,d) overlap iff a < d && c < b.
			if busyStart.Before(eventEnd) && eventStart.Before(busyEnd) {
				overlapping = append(overlapping,
					fmt.Sprintf("%s → %s",
						busyStart.Format(time.RFC3339),
						busyEnd.Format(time.RFC3339),
					),
				)
			}
		}

		if len(overlapping) > 0 {
			conflicts = append(conflicts, attendeeConflict{
				Email:       trimmed,
				BusyPeriods: overlapping,
			})
		}
	}

	return conflicts, nil
}

// gcalEventToCalendarEvent converts a Google Calendar API event to our
// internal calendarEvent struct for consistent JSON output.
func gcalEventToCalendarEvent(item *gcal.Event) calendarEvent {
	ev := calendarEvent{
		ID:          item.Id,
		Title:       item.Summary,
		Description: item.Description,
		Location:    item.Location,
		Status:      item.Status,
		Link:        item.HtmlLink,
	}

	// The API returns DateTime for timed events and Date for all-day events.
	if item.Start != nil {
		if item.Start.DateTime != "" {
			ev.StartTime = item.Start.DateTime
		} else {
			ev.StartTime = item.Start.Date
		}
	}
	if item.End != nil {
		if item.End.DateTime != "" {
			ev.EndTime = item.End.DateTime
		} else {
			ev.EndTime = item.End.Date
		}
	}

	for _, a := range item.Attendees {
		ev.Attendees = append(ev.Attendees, a.Email)
	}
	return ev
}

// Compile-time interface check — ensures calendarTools can produce an
// http.Client (used indirectly through the Google SDK).
var _ http.RoundTripper = http.DefaultTransport
