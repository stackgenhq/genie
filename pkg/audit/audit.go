// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package audit provides durable, structured logging of agent and tool events
// (commands, tool calls, LLM requests/responses, errors) for debugging and
// compliance.
//
// It solves the problem of having a single, consistent audit trail: events are
// written as NDJSON to rotating files (~/.genie/{agent}.{date}.ndjson) or a
// configured path. Activity reports and downstream analytics can read these
// files. Without this package, there would be no unified record of what the
// agent did and which tools were invoked.
package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/osutils"
	"github.com/stackgenhq/genie/pkg/pii"
)

//go:generate go tool counterfeiter -generate

// defaultFilePerm is the default permission for creating files (owner read/write only).
const defaultFilePerm = 0600

// maxLinesPerFile caps lines read per audit file to avoid OOM on large logs.
const maxLinesPerFile = 50000

// auditLogDateLayout is the date format used in the default audit log filename
// (~/.genie/{agent_name}.<yyyy_mm_dd>.ndjson). Matches config.GenieConfig AgentName comment.
const auditLogDateLayout = "2006_01_02"

// EventType represents the type of audit event.
type EventType string

const (
	// EventConnection is logged when a client connects.
	EventConnection EventType = "connection"
	// EventDisconnection is logged when a client disconnects.
	EventDisconnection EventType = "disconnection"
	// EventCommand is logged when a command is executed.
	EventCommand EventType = "command"
	// EventError is logged when an error occurs.
	EventError EventType = "error"

	// LLM conversation audit events:

	// EventLLMRequest is logged when an LLM call starts.
	EventLLMRequest EventType = "llm_request"
	// EventLLMResponse is logged when an LLM call completes.
	EventLLMResponse EventType = "llm_response"
	// EventClassification is logged when the front desk classifies a request.
	EventClassification EventType = "classification"
	// EventToolCall is logged when a tool is invoked.
	EventToolCall EventType = "tool_call"
	// EventConversation is logged for a complete Q&A turn.
	EventConversation EventType = "conversation"
	// EventMemoryAccess is logged when memory is read, written, or deleted.
	EventMemoryAccess EventType = "memory_access"
	// EventSecretAccess is logged when a secret is looked up (Manager or keyring).
	// Only the secret name/key is recorded; the value is never logged.
	EventSecretAccess EventType = "secret_access"
)

// LogRequest contains all fields needed to record an audit event.
// This follows the mandatory 2-parameter method pattern (ctx + request struct).
type LogRequest struct {
	EventType EventType
	Actor     string
	Action    string
	Metadata  map[string]any
}

// LookupRequest contains parameters for reading recent audit events.
// Used by Recent to scope which agent and time window to read.
type LookupRequest struct {
	AgentName string
	Since     time.Time
}

// Event represents a single parsed audit log entry (read path). It mirrors the
// structure written by Log (event_type, actor, action, timestamp, metadata).
type Event struct {
	EventType string                 `json:"event_type"`
	Actor     string                 `json:"actor"`
	Action    string                 `json:"action"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Auditor defines the interface for audit logging and reading recent events.
//
//counterfeiter:generate . Auditor
type Auditor interface {
	Log(ctx context.Context, req LogRequest)
	Recent(ctx context.Context, req LookupRequest) ([]Event, error)
	Close() error
}

// DefaultAuditPath returns the default audit log path for the named agent for
// the current date. See DefaultAuditPathForDate for the format and semantics.
func DefaultAuditPath(agentName string) string {
	return DefaultAuditPathForDate(agentName, time.Now().UTC())
}

// DefaultAuditPathForDate returns the default audit log path for the named agent
// for the given date in the format ~/.genie/{agent_name}.<yyyy_mm_dd>.ndjson.
// Agent name is sanitized for filenames. Creates ~/.genie if missing. If home
// cannot be determined, uses workingDir. Empty agentName falls back to "genie".
// This is the single source of truth for the path format documented on config.AgentName.
func DefaultAuditPathForDate(agentName string, t time.Time) string {
	safe := osutils.SanitizeForFilename(agentName)
	if safe == "" {
		safe = "genie"
	}
	date := t.Format(auditLogDateLayout)
	baseName := safe + "." + date + ".ndjson"
	return filepath.Join(osutils.GenieDir(), baseName)
}

// FileAuditor implements Auditor and writes structured JSON logs to a file.
// It supports fixed path (single file) or date-rotating path (~/.genie/{agent}.<date>.ndjson).
type FileAuditor struct {
	// fixed mode: fixedPath set; ensureFile uses it and does not rotate
	// rotating mode: agentName set; ensureFile uses DefaultAuditPathForDate(agentName, today)
	logger  *slog.Logger
	logFile *os.File
	mu      sync.Mutex

	// fixed mode: exact path to write to
	fixedPath string
	// rotating mode only
	agentName   string
	currentPath string
}

// NewRotatingFileAuditor creates an auditor that writes to the default path for
// the current date (~/.genie/{agent_name}.<yyyy_mm_dd>.ndjson). On each Log call
// the path is resolved for "today" (UTC); when the date changes (e.g. after
// 24h uptime), the next log goes to the new day's file. Logs are always written
// to the correct date's file and serialized so lines are not interleaved.
func NewRotatingFileAuditor(agentName string) (*FileAuditor, error) {
	if agentName == "" {
		agentName = "genie"
	}
	safe := osutils.SanitizeForFilename(agentName)
	if safe != "" {
		agentName = safe
	} else {
		agentName = "genie"
	}
	return &FileAuditor{
		agentName: agentName,
	}, nil
}

// NewFixedPathAuditor creates an auditor that writes to the given path (single file).
// Use for tests or when a custom audit path is configured. The file is created on first Log.
func NewFixedPathAuditor(path string) (*FileAuditor, error) {
	if path == "" {
		return nil, fmt.Errorf("audit path is required")
	}
	return &FileAuditor{fixedPath: path}, nil
}

// ensureFile opens the rotating-mode file for today (UTC) or the fixed-path file.
// Must be called with a.mu held.
func (a *FileAuditor) ensureFile() error {
	var path string
	if a.fixedPath != "" {
		path = a.fixedPath
	} else {
		if a.agentName == "" {
			a.agentName = "genie"
		}
		path = DefaultAuditPathForDate(a.agentName, time.Now().UTC())
	}
	if path == a.currentPath {
		return nil
	}
	if a.logFile != nil {
		path := a.currentPath
		if err := a.logFile.Close(); err != nil {
			slog.Error("failed to close previous audit log file", "path", path, "error", err)
		}
		a.logFile = nil
		a.logger = nil
		a.currentPath = ""
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, defaultFilePerm)
	if err != nil {
		return fmt.Errorf("failed to open audit log file: %w", err)
	}
	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	a.logFile = f
	a.logger = slog.New(handler)
	a.currentPath = path
	return nil
}

// Log records an audit event with structured fields. In rotating mode, resolves
// the path for the current date (UTC) so logs always go to the correct day's file.
// Metadata values are PII-redacted before writing.
func (a *FileAuditor) Log(ctx context.Context, req LogRequest) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.ensureFile(); err != nil {
		// No logger yet; slog default will emit to stderr if we have nothing
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "audit ensureFile failed", "error", err)
		}
		return
	}

	messengerContext := messenger.MessageOriginFrom(ctx)
	attrs := []any{
		slog.String("event_type", string(req.EventType)),
		slog.String("actor", req.Actor),
		slog.String("action", req.Action),
		slog.Time("timestamp", time.Now().UTC()),
		slog.String("messenger_context", messengerContext.String()),
	}

	if len(req.Metadata) > 0 {
		metaAttrs := make([]any, 0, len(req.Metadata))
		for k, v := range req.Metadata {
			if s, ok := v.(string); ok {
				v = pii.Redact(ctx, s)
			}
			metaAttrs = append(metaAttrs, slog.Any(k, v))
		}
		attrs = append(attrs, slog.Group("metadata", metaAttrs...))
	}

	a.logger.InfoContext(ctx, "audit_event", attrs...)
}

// Recent reads audit log files for the agent from req.Since to now (UTC),
// parsing each NDJSON line with msg "audit_event" and returning events in
// chronological order. Uses req.AgentName when set; otherwise the
// FileAuditor's agent name. Without this method, activity report and other
// features could not obtain recent activities from the file audit trail.
func (a *FileAuditor) Recent(ctx context.Context, req LookupRequest) ([]Event, error) {
	since := req.Since.UTC()
	now := time.Now().UTC()
	if since.After(now) {
		return nil, nil
	}
	if a.fixedPath != "" {
		events, err := readAuditFile(a.fixedPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		var all []Event
		for _, e := range events {
			if e.Timestamp.IsZero() || !e.Timestamp.Before(since) {
				all = append(all, e)
			}
		}
		return all, nil
	}
	agentName := req.AgentName
	if agentName == "" {
		agentName = a.agentName
	}
	if agentName == "" {
		agentName = "genie"
	}

	var all []Event
	for t := since; !t.After(now); t = t.AddDate(0, 0, 1) {
		path := DefaultAuditPathForDate(agentName, t)
		events, err := readAuditFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read audit file %s: %w", path, err)
		}
		for _, e := range events {
			if e.Timestamp.IsZero() || !e.Timestamp.Before(since) {
				all = append(all, e)
			}
		}
	}
	return all, nil
}

// Close closes the current audit log file. Safe to call multiple times.
func (a *FileAuditor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.logFile != nil {
		err := a.logFile.Close()
		a.logFile = nil
		a.logger = nil
		a.currentPath = ""
		return err
	}
	return nil
}

// readAuditFile reads one NDJSON audit file and returns parsed events.
// Only lines with msg "audit_event" are included.
func readAuditFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []Event
	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > maxLinesPerFile {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if msg, _ := raw["msg"].(string); msg != "audit_event" {
			continue
		}
		e := Event{}
		if v, ok := raw["event_type"].(string); ok {
			e.EventType = v
		}
		if v, ok := raw["actor"].(string); ok {
			e.Actor = v
		}
		if v, ok := raw["action"].(string); ok {
			e.Action = v
		}
		if v, ok := raw["timestamp"].(string); ok {
			e.Timestamp, _ = time.Parse(time.RFC3339, v)
			if e.Timestamp.IsZero() {
				e.Timestamp, _ = time.Parse(time.RFC3339Nano, v)
			}
		}
		if m, ok := raw["metadata"].(map[string]interface{}); ok {
			e.Metadata = m
		}
		out = append(out, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filepath.Base(path), err)
	}
	return out, nil
}
