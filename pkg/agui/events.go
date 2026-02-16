package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/google/uuid"
)

// EventType defines the type of event (e.g., webhook, heartbeat).
type EventType string

const (
	EventTypeWebhook   EventType = "webhook"
	EventTypeHeartbeat EventType = "heartbeat"
)

// EventRequest represents the payload for an event.
type EventRequest struct {
	Type    EventType       `json:"type"`
	Source  string          `json:"source"`            // e.g., "github", "cron"
	Payload json.RawMessage `json:"payload,omitempty"` // Flexible payload
}

// BackgroundWorker handles background agent execution.
type BackgroundWorker struct {
	chatHandler Expert
	mu          sync.Mutex
	active      int
	max         int
	wg          sync.WaitGroup
}

// NewBackgroundWorker creates a worker with a concurrency limit.
func NewBackgroundWorker(handler Expert, maxConcurrent int) *BackgroundWorker {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &BackgroundWorker{
		chatHandler: handler,
		max:         maxConcurrent,
	}
}

// HandleEvent processes an event by spawning a background agent run.
// It returns the runID and an error if the worker pool is full.
func (w *BackgroundWorker) HandleEvent(ctx context.Context, req EventRequest) (string, error) {
	w.mu.Lock()
	if w.active >= w.max {
		w.mu.Unlock()
		return "", fmt.Errorf("background worker pool is full")
	}
	w.active++
	w.wg.Add(1)
	w.mu.Unlock()

	runID := uuid.NewString()

	go func() {
		defer func() {
			w.mu.Lock()
			w.active--
			w.mu.Unlock()
			w.wg.Done()
		}()
		// Use a detached context because the HTTP request context will be cancelled
		w.runAgent(context.Background(), req, runID)
	}()

	return runID, nil
}

// WaitForCompletion blocks until all active background tasks are finished.
func (w *BackgroundWorker) WaitForCompletion() {
	w.wg.Wait()
}

func (w *BackgroundWorker) runAgent(ctx context.Context, req EventRequest, runID string) {
	logr := logger.GetLogger(ctx).With("fn", "BackgroundWorker.runAgent")

	threadID := uuid.NewString()

	message := fmt.Sprintf("System Event [%s from %s]: %s",
		req.Type, req.Source, string(req.Payload))

	logr.Info("Starting background agent run", "threadID", threadID, "type", req.Type)

	// Create a dummy channel that discards events (since there's no UI connected)
	eventChan := make(chan interface{}, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for evt := range eventChan {
			// Drain channel. In the future, we could log key events here.
			// For now, just logging errors is useful.
			if errMsg, ok := evt.(AgentErrorMsg); ok && errMsg.Type == EventRunError {
				logr.Error("Background agent error", "error", errMsg.Error)
			}
		}
	}()

	chatReq := ChatRequest{
		ThreadID:  threadID,
		RunID:     runID,
		Message:   message,
		EventChan: eventChan,
	}

	// This blocks until the agent completes
	w.chatHandler.Handle(ctx, chatReq)

	// Close channel to signal drainer to exit
	close(eventChan)
	wg.Wait()

	logr.Info("Background agent run completed", "threadID", threadID)
}

func (s *Server) handleResumeEndpoint(w http.ResponseWriter, r *http.Request) {
	resume := s.chatHandler.Resume()
	if resume == "" {
		http.Error(w, "Resume not available", http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(resume))
}

// handleEventsEndpoint is the HTTP handler for /api/v1/events
func (s *Server) handleEventsEndpoint(w http.ResponseWriter, r *http.Request) {
	if s.bgWorker == nil {
		http.Error(w, "Background worker not configured", http.StatusServiceUnavailable)
		return
	}

	var req EventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if req.Type == "" {
		http.Error(w, "Event type is required", http.StatusBadRequest)
		return
	}

	runID, err := s.bgWorker.HandleEvent(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Event queued for processing",
		"run_id":  runID,
	}); err != nil {
		logger.GetLogger(r.Context()).Error("failed to write response", "error", err)
	}
}
