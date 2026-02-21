package audit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/appcd-dev/genie/pkg/pii"
)

//go:generate go tool counterfeiter -generate

// defaultFilePerm is the default permission for creating files (owner read/write only).
const defaultFilePerm = 0600

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
)

// LogRequest contains all fields needed to record an audit event.
// This follows the mandatory 2-parameter method pattern (ctx + request struct).
type LogRequest struct {
	EventType EventType
	Actor     string
	Action    string
	Metadata  map[string]any
}

// Auditor defines the interface for audit logging.
//
//counterfeiter:generate . Auditor
type Auditor interface {
	Log(ctx context.Context, req LogRequest)
	Close() error
}

// FileAuditor implements Auditor and writes structured JSON logs to a file.
type FileAuditor struct {
	logger  *slog.Logger
	logFile *os.File
}

// NewFileAuditor creates a new auditor that writes JSON logs to the specified file.
func NewFileAuditor(filePath string) (*FileAuditor, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, defaultFilePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log file: %w", err)
	}

	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	return &FileAuditor{logger: logger, logFile: f}, nil
}

// Log records an audit event with structured fields.
// Metadata values are PII-redacted before writing to prevent sensitive data
// (user messages, API keys, tool arguments) from persisting in the audit log.
func (a *FileAuditor) Log(ctx context.Context, req LogRequest) {
	attrs := []any{
		slog.String("event_type", string(req.EventType)),
		slog.String("actor", req.Actor),
		slog.String("action", req.Action),
		slog.Time("timestamp", time.Now().UTC()),
	}

	if len(req.Metadata) > 0 {
		metaAttrs := make([]any, 0, len(req.Metadata))
		for k, v := range req.Metadata {
			// PII-redact string values to prevent leaking secrets/PII to disk.
			if s, ok := v.(string); ok {
				v = pii.Redact(s)
			}
			metaAttrs = append(metaAttrs, slog.Any(k, v))
		}
		attrs = append(attrs, slog.Group("metadata", metaAttrs...))
	}

	a.logger.InfoContext(ctx, "audit_event", attrs...)
}

func (a *FileAuditor) Close() error {
	if a.logFile != nil {
		return a.logFile.Close()
	}
	return nil
}
