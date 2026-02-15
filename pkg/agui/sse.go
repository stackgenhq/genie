package agui

import (
	"fmt"
	"net/http"
)

// SSEWriter wraps an http.ResponseWriter to write Server-Sent Events.
// Each event is formatted as:
//
//	event: <type>\ndata: <json>\n\n
//
// The writer flushes after every event to ensure real-time streaming.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSEWriter and sets the required SSE headers.
// Returns an error if the ResponseWriter does not support http.Flusher.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support streaming")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteEvent writes a single SSE event with the given type and JSON data.
// Format: event: <eventType>\ndata: <data>\n\n
func (s *SSEWriter) WriteEvent(eventType string, data []byte) error {
	if eventType != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", eventType); err != nil {
			return fmt.Errorf("failed to write event type: %w", err)
		}
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("failed to write event data: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// WriteComment writes an SSE comment (used for keep-alive pings).
// Format: : <comment>\n\n
func (s *SSEWriter) WriteComment(comment string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", comment); err != nil {
		return fmt.Errorf("failed to write comment: %w", err)
	}
	s.flusher.Flush()
	return nil
}
