package tui

import (
	"time"
)

// LogEntry represents a single log entry with timestamp.
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
	Source    string
}
