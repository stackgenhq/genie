package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

var _ slog.Handler = (*TUIHandler)(nil)

// TUIHandler is a custom slog.Handler that pipes log messages to the TUI event channel.
// This handler enables automatic real-time log streaming to the TUI without manual EmitLog calls.
//
// Usage:
//
//	eventChan := make(chan interface{}, 100)
//	tuiHandler := tui.NewTUIHandler(eventChan, slog.LevelInfo)
//	logger.SetLogHandler(tuiHandler)
//
// Now all slog calls will automatically appear in the TUI log viewer.
type TUIHandler struct {
	eventChan chan<- interface{}
	level     slog.Level
	attrs     []slog.Attr
	groups    []string
}

// NewTUIHandler creates a new TUI-aware slog handler.
// All log messages at or above the specified level will be sent to the event channel as LogMsg events.
func NewTUIHandler(eventChan chan<- interface{}, level slog.Level) *TUIHandler {
	return &TUIHandler{
		eventChan: eventChan,
		level:     level,
		attrs:     make([]slog.Attr, 0),
		groups:    make([]string, 0),
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *TUIHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle processes a log record and sends it to the TUI event channel.
func (h *TUIHandler) Handle(_ context.Context, r slog.Record) error {
	// Filter out noisy debug messages that create clutter
	if shouldSkipMessage(r.Message) {
		return nil
	}

	// Convert slog level to TUI LogLevel
	var tuiLevel LogLevel
	switch {
	case r.Level >= slog.LevelError:
		tuiLevel = LogError
	case r.Level >= slog.LevelWarn:
		tuiLevel = LogWarn
	case r.Level >= slog.LevelInfo:
		tuiLevel = LogInfo
	default:
		tuiLevel = LogDebug
	}

	// Extract source and build message with attributes
	source := "system"
	var attrParts []string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "fn" || a.Key == "function" || a.Key == "component" {
			source = a.Value.String()
		} else {
			// Include other attributes in the message
			attrParts = append(attrParts, fmt.Sprintf("%s=%v", a.Key, a.Value))
		}
		return true // Continue iteration
	})

	// Build complete message with attributes
	message := r.Message
	if len(attrParts) > 0 {
		message = fmt.Sprintf("%s | %s", r.Message, strings.Join(attrParts, ", "))
	}

	// Send log message to TUI
	select {
	case h.eventChan <- LogMsg{
		Level:   tuiLevel,
		Message: message,
		Source:  source,
	}:
	default:
		// Channel full, skip this log message
		// This prevents blocking the application if TUI can't keep up
	}

	return nil
}

// shouldSkipMessage returns true if the message should be filtered out.
// This helps reduce noise from repetitive system messages.
func shouldSkipMessage(msg string) bool {
	noisyPatterns := []string{
		"method call",
		"could not map",
		"We do not support this method name yet",
		"expert is working on the request",
	}

	for _, pattern := range noisyPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// WithAttrs returns a new handler with the given attributes added.
func (h *TUIHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)

	return &TUIHandler{
		eventChan: h.eventChan,
		level:     h.level,
		attrs:     newAttrs,
		groups:    h.groups,
	}
}

// WithGroup returns a new handler with the given group added.
func (h *TUIHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name

	return &TUIHandler{
		eventChan: h.eventChan,
		level:     h.level,
		attrs:     h.attrs,
		groups:    newGroups,
	}
}

// formatLogEntry formats a log entry with timestamp for display.
func formatLogEntry(entry LogEntry) string {
	timestamp := entry.Timestamp.Format("15:04:05")
	return fmt.Sprintf("[%s] [%s] [%s] %s",
		timestamp,
		entry.Level.String(),
		entry.Source,
		entry.Message,
	)
}
