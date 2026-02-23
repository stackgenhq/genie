// Package datetime provides time and date tools for agents. LLMs are
// notoriously poor at temporal reasoning — this package gives them precise
// control over current time queries, timezone conversions, date arithmetic,
// duration calculations, and format parsing.
//
// Problem: LLMs have no concept of "now" and cannot reliably compute time
// differences, timezone conversions, or date arithmetic. This package provides
// a deterministic time engine that handles these operations correctly.
//
// Supported operations:
//   - now — current time in any timezone
//   - convert — timezone conversion between IANA zones
//   - add / subtract — date arithmetic (add 3 days, subtract 2 hours, etc.)
//   - diff — duration between two dates
//   - parse — auto-detect and parse 100+ date formats
//   - format — format dates into any Go time layout
//
// Dependencies:
//   - github.com/araddon/dateparse — auto-detects 100+ date formats (2.1k+ ⭐)
//   - Go stdlib time — IANA timezone database
//   - No external system dependencies
package datetime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/araddon/dateparse"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ────────────────────── Request / Response ──────────────────────

type datetimeRequest struct {
	Operation string `json:"operation" jsonschema:"description=The operation to perform. One of: now, parse, format, add, diff, convert_tz.,enum=now,enum=parse,enum=format,enum=add,enum=diff,enum=convert_tz"`
	// Timezone for 'now' and 'convert_tz' (IANA format e.g. 'America/New_York').
	Timezone string `json:"timezone,omitempty" jsonschema:"description=IANA timezone (e.g. 'America/New_York', 'Europe/London', 'UTC'). Used by now and convert_tz."`
	// DateTime input for parse, format, add, diff, convert_tz.
	DateTime string `json:"datetime,omitempty" jsonschema:"description=Date/time string to operate on. Accepts virtually any common date format — RFC3339, ISO 8601, US/EU dates, natural dates like 'May 8 2009', Unix timestamps, etc."`
	// DateTime2 is the second datetime for 'diff'.
	DateTime2 string `json:"datetime2,omitempty" jsonschema:"description=Second date/time for diff operation. Same format as datetime."`
	// Duration for 'add' (Go duration format: '2h30m', '24h', '-48h', '30m').
	Duration string `json:"duration,omitempty" jsonschema:"description=Duration to add (Go format: '2h30m', '24h', '-48h', '7d' as '168h'). Used by add."`
	// Format for 'format' (Go reference time layout or preset name).
	Format string `json:"format,omitempty" jsonschema:"description=Output format. Presets: 'date' (2006-01-02), 'time' (15:04:05), 'datetime' (2006-01-02 15:04:05), 'rfc3339', 'unix'. Or a Go reference time layout."`
}

type datetimeResponse struct {
	Operation string `json:"operation"`
	Result    string `json:"result"`
	Unix      int64  `json:"unix,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	Weekday   string `json:"weekday,omitempty"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type datetimeTools struct{}

func newDatetimeTools() *datetimeTools { return &datetimeTools{} }

func (d *datetimeTools) datetimeTool() tool.CallableTool {
	return function.NewFunctionTool(
		d.datetime,
		function.WithName("datetime"),
		function.WithDescription(
			"Perform date and time operations. Supports: "+
				"now (current time in any timezone), "+
				"parse (parse virtually any date/time string — RFC3339, ISO 8601, US/EU dates, natural dates like 'May 8 2009', etc.), "+
				"format (format a datetime to a specific layout), "+
				"add (add/subtract a duration from a datetime), "+
				"diff (calculate duration between two datetimes), "+
				"convert_tz (convert a datetime between timezones).",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (d *datetimeTools) datetime(_ context.Context, req datetimeRequest) (datetimeResponse, error) {
	op := strings.ToLower(strings.TrimSpace(req.Operation))
	resp := datetimeResponse{Operation: op}

	switch op {
	case "now":
		return d.now(req, resp)
	case "parse":
		return d.parse(req, resp)
	case "format":
		return d.formatDT(req, resp)
	case "add":
		return d.add(req, resp)
	case "diff":
		return d.diff(req, resp)
	case "convert_tz":
		return d.convertTZ(req, resp)
	default:
		return resp, fmt.Errorf("unsupported operation %q: must be one of now, parse, format, add, diff, convert_tz", req.Operation)
	}
}

func (d *datetimeTools) now(req datetimeRequest, resp datetimeResponse) (datetimeResponse, error) {
	loc, err := d.loadLocation(req.Timezone)
	if err != nil {
		return resp, err
	}
	now := time.Now().In(loc)
	resp.Result = now.Format(time.RFC3339)
	resp.Unix = now.Unix()
	resp.Timezone = loc.String()
	resp.Weekday = now.Weekday().String()
	resp.Message = fmt.Sprintf("Current time in %s: %s (%s)", loc, now.Format("2006-01-02 15:04:05"), resp.Weekday)
	return resp, nil
}

func (d *datetimeTools) parse(req datetimeRequest, resp datetimeResponse) (datetimeResponse, error) {
	if req.DateTime == "" {
		return resp, fmt.Errorf("datetime is required for parse operation")
	}
	t, err := d.parseTime(req.DateTime)
	if err != nil {
		return resp, fmt.Errorf("failed to parse datetime %q: %w", req.DateTime, err)
	}
	resp.Result = t.Format(time.RFC3339)
	resp.Unix = t.Unix()
	resp.Timezone = t.Location().String()
	resp.Weekday = t.Weekday().String()
	resp.Message = fmt.Sprintf("Parsed: %s (Unix: %d, %s)", resp.Result, resp.Unix, resp.Weekday)
	return resp, nil
}

func (d *datetimeTools) formatDT(req datetimeRequest, resp datetimeResponse) (datetimeResponse, error) {
	if req.DateTime == "" {
		return resp, fmt.Errorf("datetime is required for format operation")
	}
	t, err := d.parseTime(req.DateTime)
	if err != nil {
		return resp, fmt.Errorf("failed to parse datetime %q: %w", req.DateTime, err)
	}
	layout := d.resolveFormat(req.Format)
	if layout == "__unix_epoch__" {
		resp.Result = fmt.Sprintf("%d", t.Unix())
	} else {
		resp.Result = t.Format(layout)
	}
	resp.Message = fmt.Sprintf("Formatted: %s", resp.Result)
	return resp, nil
}

func (d *datetimeTools) add(req datetimeRequest, resp datetimeResponse) (datetimeResponse, error) {
	if req.DateTime == "" {
		return resp, fmt.Errorf("datetime is required for add operation")
	}
	if req.Duration == "" {
		return resp, fmt.Errorf("duration is required for add operation")
	}
	t, err := d.parseTime(req.DateTime)
	if err != nil {
		return resp, fmt.Errorf("failed to parse datetime %q: %w", req.DateTime, err)
	}
	dur, err := time.ParseDuration(req.Duration)
	if err != nil {
		return resp, fmt.Errorf("failed to parse duration %q: %w", req.Duration, err)
	}
	result := t.Add(dur)
	resp.Result = result.Format(time.RFC3339)
	resp.Unix = result.Unix()
	resp.Weekday = result.Weekday().String()
	resp.Message = fmt.Sprintf("%s + %s = %s", req.DateTime, req.Duration, resp.Result)
	return resp, nil
}

func (d *datetimeTools) diff(req datetimeRequest, resp datetimeResponse) (datetimeResponse, error) {
	if req.DateTime == "" || req.DateTime2 == "" {
		return resp, fmt.Errorf("both datetime and datetime2 are required for diff operation")
	}
	t1, err := d.parseTime(req.DateTime)
	if err != nil {
		return resp, fmt.Errorf("failed to parse datetime %q: %w", req.DateTime, err)
	}
	t2, err := d.parseTime(req.DateTime2)
	if err != nil {
		return resp, fmt.Errorf("failed to parse datetime2 %q: %w", req.DateTime2, err)
	}
	dur := t2.Sub(t1)
	resp.Result = dur.String()
	resp.Message = fmt.Sprintf("Difference: %s (%.2f hours, %.0f days)", dur, dur.Hours(), dur.Hours()/24)
	return resp, nil
}

func (d *datetimeTools) convertTZ(req datetimeRequest, resp datetimeResponse) (datetimeResponse, error) {
	if req.DateTime == "" {
		return resp, fmt.Errorf("datetime is required for convert_tz operation")
	}
	if req.Timezone == "" {
		return resp, fmt.Errorf("timezone is required for convert_tz operation")
	}
	t, err := d.parseTime(req.DateTime)
	if err != nil {
		return resp, fmt.Errorf("failed to parse datetime %q: %w", req.DateTime, err)
	}
	loc, err := time.LoadLocation(req.Timezone)
	if err != nil {
		return resp, fmt.Errorf("unknown timezone %q: %w", req.Timezone, err)
	}
	converted := t.In(loc)
	resp.Result = converted.Format(time.RFC3339)
	resp.Unix = converted.Unix()
	resp.Timezone = loc.String()
	resp.Weekday = converted.Weekday().String()
	resp.Message = fmt.Sprintf("Converted to %s: %s", loc, converted.Format("2006-01-02 15:04:05"))
	return resp, nil
}

// ────────────────────── Helpers ──────────────────────

// parseTime uses araddon/dateparse to auto-detect and parse virtually any
// date/time format. It handles 100+ formats including RFC3339, ISO 8601,
// US/EU dates, natural language dates, and more — via a state machine that
// is faster than trying formats sequentially.
func (d *datetimeTools) parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	t, err := dateparse.ParseAny(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not parse %q — try a common date format (e.g. 2024-01-15T10:30:00Z or 'Jan 15, 2024')", s)
	}
	return t, nil
}

func (d *datetimeTools) loadLocation(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q: %w", tz, err)
	}
	return loc, nil
}

// resolveFormat maps preset names to Go time layouts.
// The special value "unix" is handled by formatDT directly.
func (d *datetimeTools) resolveFormat(f string) string {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "date":
		return "2006-01-02"
	case "time":
		return "15:04:05"
	case "datetime":
		return "2006-01-02 15:04:05"
	case "rfc3339", "":
		return time.RFC3339
	case "unix":
		return "__unix_epoch__" // sentinel — handled by formatDT
	default:
		return f // treat as Go reference time layout
	}
}
