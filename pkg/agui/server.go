package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/osutils"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type HealthResult struct {
	Healthy      bool   `json:"healthy"`
	LastError    string `json:"last_error,omitempty"`
	FailureCount int    `json:"failure_count"`
	Name         string `json:"name"`
}

//go:generate go tool counterfeiter -generate
//counterfeiter:generate . BGWorker

// BGWorker is an interface for background workers that can be started and stopped.
type BGWorker interface {
	Start(ctx context.Context) error
	HealthCheck(ctx context.Context) []HealthResult
	Stop() error
}

// ChatRequest bundles the inputs for a single AG-UI chat invocation.
type ChatRequest struct {
	ThreadID  string
	RunID     string
	Message   string
	EventChan chan<- interface{}
}

// Expert is the one who knows how to handle a chat request.
//
//counterfeiter:generate . Expert
type Expert interface {
	Handle(ctx context.Context, req ChatRequest)
	Resume(ctx context.Context) string
}

// Message represents a message in the AG-UI RunAgentInput.
type Message struct {
	ID      string `json:"id,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolDefinition represents a tool definition passed by the client.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ContextItem represents a context item passed by the client.
type ContextItem struct {
	Description string `json:"description,omitempty"`
	Value       string `json:"value,omitempty"`
}

// RunAgentInput is the request body for the AG-UI run endpoint.
// See https://docs.ag-ui.com/concepts/architecture#running-agents
type RunAgentInput struct {
	ThreadID       string           `json:"threadId"`
	RunID          string           `json:"runId"`
	Messages       []Message        `json:"messages"`
	Tools          []ToolDefinition `json:"tools,omitempty"`
	Context        []ContextItem    `json:"context,omitempty"`
	ForwardedProps map[string]any   `json:"forwardedProps,omitempty"`
}

func DefaultServerConfig() ServerConfig {
	port := osutils.Getenv("PORT", "8080")
	cfg := ServerConfig{
		CORSOrigins:   []string{"https://appcd-dev.github.io"},
		RateLimit:     0.5, // 30 req/min per IP
		RateBurst:     3,
		MaxConcurrent: 5,
		MaxBodyBytes:  1 << 20, // 1 MB
	}
	portVal, _ := strconv.Atoi(port)
	if portVal == 0 {
		portVal = 8080
	}
	cfg.Port = uint32(portVal)
	return cfg
}

type ServerConfig struct {
	CORSOrigins   []string `yaml:"cors_origins" toml:"cors_origins"`
	Port          uint32   `yaml:"port" toml:"port"`
	RateLimit     float64  `yaml:"rate_limit" toml:"rate_limit"`         // req/sec per IP (0 = disabled)
	RateBurst     int      `yaml:"rate_burst" toml:"rate_burst"`         // burst allowance per IP
	MaxConcurrent int      `yaml:"max_concurrent" toml:"max_concurrent"` // max in-flight requests (0 = unlimited)
	MaxBodyBytes  int64    `yaml:"max_body_bytes" toml:"max_body_bytes"` // max request body in bytes (0 = unlimited)
}

// Server is the AG-UI HTTP server that exposes genie as an SSE endpoint.
type Server struct {
	chatHandler Expert
	port        uint32
	// corsOrigins is the list of allowed CORS origins for browser access.
	// When non-empty, matching origins receive CORS headers in responses.
	corsOrigins []string
	// approvalStore enables human-in-the-loop approval for tool calls.
	// When nil, HITL is disabled and no /approve endpoint is served.
	approvalStore hitl.ApprovalStore
	// DDoS protection fields
	ipLimiter     *ipRateLimiter // nil when rate limiting is disabled
	maxConcurrent int            // 0 = unlimited
	maxBodyBytes  int64          // 0 = unlimited

	// Background worker for events and heartbeats
	bgWorker *BackgroundWorker
	workers  []BGWorker
}

// NewServer creates a new AG-UI HTTP server.
// The bgWorker is created by the caller so it can also be shared with the cron
// scheduler dispatcher, keeping dependency wiring in one place.
func (c ServerConfig) NewServer(
	handler Expert,
	approvalStore hitl.ApprovalStore,
	bgWorker *BackgroundWorker,
	workers ...BGWorker,
) *Server {
	s := &Server{
		chatHandler:   handler,
		port:          c.Port,
		corsOrigins:   c.CORSOrigins,
		approvalStore: approvalStore,
		bgWorker:      bgWorker,
		workers:       workers,
		maxConcurrent: c.MaxConcurrent,
		maxBodyBytes:  c.MaxBodyBytes,
	}

	if c.RateLimit > 0 {
		burst := c.RateBurst
		if burst < 1 {
			burst = 10
		}
		s.ipLimiter = newIPRateLimiter(rate.Limit(c.RateLimit), burst)
	}
	logger.GetLogger(context.Background()).Info("AG-UI server configured",
		"port", c.Port,
		"rate_limit", c.RateLimit,
		"cors_origins", len(c.CORSOrigins),
		"max_concurrent", c.MaxConcurrent,
	)
	return s
}

// Handler returns the chi router with AG-UI endpoints.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	// DDoS protection middleware — applied before CORS and route handlers.
	if s.maxBodyBytes > 0 {
		r.Use(maxBodyMiddleware(s.maxBodyBytes))
	}
	if s.ipLimiter != nil {
		r.Use(rateLimitMiddleware(s.ipLimiter))
	}
	if s.maxConcurrent > 0 {
		r.Use(concurrencyLimitMiddleware(s.maxConcurrent))
	}

	if len(s.corsOrigins) != 0 {
		r.Use(s.corsMiddleware)
	}

	// AG-UI run endpoint
	r.Post("/", s.handleRun)

	// HITL approval endpoint — only registered when approval store is configured.
	if s.approvalStore != nil {
		r.Post("/approve", s.handleApprove)
	}

	// Event Gateway endpoint
	r.Post("/api/v1/events", s.handleEventsEndpoint)
	r.Get("/api/v1/resume", s.handleResumeEndpoint)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	})

	// Serve static documentation from local docs/ directory at /ui
	// Serve documentation via reverse proxy to GitHub Pages
	// This ensures users always see the latest docs without needing local files.
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	r.Handle("/ui/*", http.StripPrefix("/ui", newDocsProxy()))

	return r
}

// newDocsProxy creates a reverse proxy to the Genie GitHub Pages documentation.
// It sanitises the proxied path to prevent traversal attacks: all requests are
// forced under /stackgen-genie/ regardless of what the client sends.
func newDocsProxy() *httputil.ReverseProxy {
	docsURL, _ := url.Parse("https://appcd-dev.github.io/stackgen-genie/")
	proxy := httputil.NewSingleHostReverseProxy(docsURL)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = docsURL.Host
		// Sanitise the path and re-apply the /stackgen-genie/ prefix so
		// cleaned paths like "/" (from "/ui/../") cannot escape.
		cleaned := path.Clean(req.URL.Path)
		req.URL.Path = path.Join("/stackgen-genie", cleaned)
		if !strings.HasPrefix(req.URL.Path, "/stackgen-genie") {
			req.URL.Path = "/stackgen-genie/"
		}
	}
	return proxy
}

// Start starts the HTTP server and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	logger := logger.GetLogger(ctx).With("fn", "agui.Server.Start")
	addr := fmt.Sprintf(":%d", s.port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()

		// Stop workers (e.g. cron scheduler) first.
		for _, w := range s.workers {
			if err := w.Stop(); err != nil {
				logger.Warn("Failed to stop worker", "error", err)
			}
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("AG-UI HTTP server shutdown error", "error", err)
		}
		// Wait for background tasks to finish
		if s.bgWorker != nil {
			s.bgWorker.WaitForCompletion()
		}
	}()

	// Start the cron scheduler if configured.
	for _, w := range s.workers {
		if err := w.Start(ctx); err != nil {
			logger.Warn("Failed to start worker", "error", err)
		}
	}

	// Start Heartbeat ticker (every 10 minutes)
	// This simulates the "Heartbeat" feature from OpenClaw
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.handleHeartbeat(ctx)
			}
		}
	}()

	logger.Info("Starting AG-UI HTTP server", "port", s.port, "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("AG-UI HTTP server error: %w", err)
	}
	return nil
}

// handleHeartbeat dispatches a heartbeat event and checks cron task health.
func (s *Server) handleHeartbeat(ctx context.Context) {
	logger := logger.GetLogger(ctx).With("fn", "server.heartbeat")
	logger.Info("Triggering scheduled heartbeat event")
	if s.bgWorker == nil {
		logger.Warn("bgWorker is nil, skipping heartbeat dispatch")
		return
	}
	errGroup, egCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		req := EventRequest{
			Type:    EventTypeHeartbeat,
			Source:  "system_ticker",
			Payload: json.RawMessage(`{"message": "Scheduled maintenance check"}`),
		}
		if _, err := s.bgWorker.HandleEvent(egCtx, req); err != nil {
			logger.Warn("Failed to trigger heartbeat", "error", err)
		}
		return nil
	})

	errGroup.Go(func() error {
		// Check worker health during heartbeat; dispatch failures through
		// bgWorker so the messaging tool can send notifications.
		for _, w := range s.workers {
			for _, r := range w.HealthCheck(egCtx) {
				if r.Healthy {
					continue
				}
				logger.Warn("Worker unhealthy",
					"name", r.Name,
					"failures", r.FailureCount,
					"last_error", r.LastError,
				)
				payload, _ := json.Marshal(map[string]interface{}{
					"message":       fmt.Sprintf("Worker %q is unhealthy (%d failures): %s", r.Name, r.FailureCount, r.LastError),
					"worker":        r.Name,
					"failure_count": r.FailureCount,
					"last_error":    r.LastError,
				})
				if _, err := s.bgWorker.HandleEvent(egCtx, EventRequest{
					Type:    EventTypeHeartbeat,
					Source:  "health:" + r.Name,
					Payload: payload,
				}); err != nil {
					logger.Warn("Failed to dispatch health alert", "worker", r.Name, "error", err)
				}
			}
		}
		return nil
	})
	if err := errGroup.Wait(); err != nil {
		logger.Warn("Failed to trigger heartbeat", "error", err)
	}
}

// handleRun processes an AG-UI run request.
// It reads RunAgentInput from the POST body, starts the chat handler,
// and streams events back as SSE.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	logger := logger.GetLogger(r.Context()).With("fn", "agui.Server.handleRun")
	// Parse the request body
	var input RunAgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid request body: %s"}`, err), http.StatusBadRequest)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Warn("failed to close request body", "error", err)
		}
	}()

	// Generate IDs if not provided
	if input.ThreadID == "" {
		input.ThreadID = uuid.NewString()
	}
	if input.RunID == "" {
		input.RunID = uuid.NewString()
	}

	// Extract the last user message
	userMessage := ""
	for i := len(input.Messages) - 1; i >= 0; i-- {
		if input.Messages[i].Role == "user" {
			userMessage = input.Messages[i].Content
			break
		}
	}
	if userMessage == "" || strings.TrimSpace(userMessage) == "" {
		http.Error(w, `{"error":"user message is empty or whitespace only"}`, http.StatusBadRequest)
		return
	}

	// Set up SSE writer
	sse, err := NewSSEWriter(w)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	logger.Info("AG-UI run started",
		"threadId", input.ThreadID,
		"runId", input.RunID,
		"messageLength", len(userMessage),
	)

	// Use the request's context so client disconnect cancels the handler
	ctx := r.Context()

	// Create event channel for this request
	eventChan := make(chan interface{}, 100)

	// Start the chat handler in a goroutine
	go func() {
		defer close(eventChan)
		s.chatHandler.Handle(ctx, ChatRequest{
			ThreadID:  input.ThreadID,
			RunID:     input.RunID,
			Message:   userMessage,
			EventChan: eventChan,
		})
	}()

	// Start keep-alive ping goroutine — stopped when streaming ends.
	streamDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := sse.WriteComment("ping"); err != nil {
					return
				}
			case <-streamDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Stream events as SSE. Content dedup is handled by the converter
	// goroutine in NewChatHandlerFromCodeOwner. Here we only suppress
	// duplicate RUN_STARTED events from EmitThinking calls per stage.
	runStarted := false
	for event := range eventChan {
		if ctx.Err() != nil {
			break
		}

		data, eventType, err := MapEvent(event, input.ThreadID, input.RunID)
		if err != nil {
			logger.Debug("skipping unmappable event", "type", fmt.Sprintf("%T", event), "error", err)
			continue
		}

		// Suppress duplicate RUN_STARTED events
		if eventType == EventRunStarted {
			if runStarted {
				continue
			}
			runStarted = true
		}

		if err := sse.WriteEvent(eventType, data); err != nil {
			logger.Debug("SSE write failed (client likely disconnected)", "error", err)
			break
		}
	}

	// Stop the ping goroutine
	close(streamDone)

	logger.Info("AG-UI run completed",
		"threadId", input.ThreadID,
		"runId", input.RunID,
	)
}

// corsMiddleware adds CORS headers for browser access.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check for origin in the allowed origins
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, allowedOrigin := range s.corsOrigins {
				if origin == allowedOrigin || allowedOrigin == "*" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
					w.Header().Set("Access-Control-Max-Age", "86400")
					break
				}
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// approveRequest is the JSON body expected by the /approve endpoint.
type approveRequest struct {
	ApprovalID string `json:"approvalId"`
	Decision   string `json:"decision"` // "approved" or "rejected"
	Feedback   string `json:"feedback"` // optional user feedback
}

// handleApprove processes a human approval or rejection for a pending tool call.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	logr := logger.GetLogger(r.Context()).With("fn", "agui.Server.handleApprove")

	var req approveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid request body: %s"}`, err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close() //nolint:errcheck

	if req.ApprovalID == "" || req.Decision == "" {
		http.Error(w, `{"error":"approvalId and decision are required"}`, http.StatusBadRequest)
		return
	}

	decision := hitl.ApprovalStatus(req.Decision)
	if decision != hitl.StatusApproved && decision != hitl.StatusRejected {
		http.Error(w, `{"error":"decision must be 'approved' or 'rejected'"}`, http.StatusBadRequest)
		return
	}

	if err := s.approvalStore.Resolve(r.Context(), hitl.ResolveRequest{
		ApprovalID: req.ApprovalID,
		Decision:   decision,
		ResolvedBy: "chat-ui",
		Feedback:   req.Feedback,
	}); err != nil {
		logr.Error("failed to resolve approval", "error", err, "approvalId", req.ApprovalID)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	logr.Info("approval resolved", "approvalId", req.ApprovalID, "decision", req.Decision)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
