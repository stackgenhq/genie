// Package agui provides a Messenger adapter for the AG-UI SSE server.
//
// Unlike other adapters that connect to external platforms, this adapter
// runs in-process and IS the AG-UI HTTP/SSE server. Connect() starts the
// HTTP server that listens for browser SSE connections, and Disconnect()
// shuts it down.
//
// # Thread Registration Model
//
// The adapter uses a thread-registration model rather than full message
// routing. When handleRun receives a user message, it calls InjectMessage
// to register the thread's event channel in the adapter. This allows other
// subsystems (HITL, send_message tool) to route responses back to the SSE
// client via the adapter's Send method. The chat handler still runs
// directly in handleRun — messages are NOT routed through the messenger
// receive loop.
//
// The adapter exposes three methods beyond the Messenger interface:
//   - InjectMessage: registers a thread and its event channel
//   - CompleteThread: cleans up thread state when a run ends
//   - ThreadDone: returns a channel that's closed when the thread completes
package agui

import (
	"context"
	"fmt"
	"sync"
	"time"

	aguitypes "github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/clarify"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformAGUI, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		return New(Config{}, opts...), nil
	})
}

// Config holds AGUI adapter settings.
type Config struct {
	// AppName is used as the session.Key AppName for thread tracking.
	// Defaults to "genie" if empty.
	AppName string `yaml:"app_name" toml:"app_name"`
}

// DefaultAppName is used when Config.AppName is empty.
const DefaultAppName = "genie"

// SenderInfo carries optional identity information for the web user.
// When provided to InjectMessage, it replaces the default "agui-user"
// sender identity, enabling sender allowlists and accurate attribution.
type SenderInfo struct {
	ID          string
	Username    string
	DisplayName string
}

// threadState tracks the event channel and done signal for an active AG-UI thread.
type threadState struct {
	eventChan chan<- interface{}
	done      chan struct{}
	userID    string // stored for session cleanup in CompleteThread
}

// Messenger implements the [messenger.Messenger] interface for the AG-UI SSE
// server. Unlike other messenger adapters that connect to external platforms,
// this IS the server — Connect() starts the AG-UI HTTP/SSE server that
// browsers connect to.
//
// The adapter uses a thread-registration model: InjectMessage registers a
// thread's event channel so that Send() can route responses back to the
// SSE stream. The incoming Receive channel is unused in normal operation
// because chat handling runs directly in handleRun, not through the
// messenger receive loop.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	incoming   chan messenger.IncomingMessage
	connected  bool
	closeDone  chan struct{} // closed on Disconnect to signal shutdown
	mu         sync.RWMutex

	// threads maps active threadID → event channel + done signal for sending
	// SSE responses back to the AG-UI client.
	threads   map[string]*threadState
	threadsMu sync.RWMutex

	// sessionSvc provides structured thread persistence using the framework's
	// session.Service. Sessions are keyed by {AppName, UserID, SessionID=threadID}.
	sessionSvc session.Service
	appName    string // resolved from Config.AppName or DefaultAppName

	// --- AG-UI HTTP server (owned by this adapter) ---
	server    *Server            // the AG-UI HTTP/SSE server; nil until ConfigureServer
	serverCfn context.CancelFunc // cancel function for server shutdown
	wg        sync.WaitGroup     // tracks background goroutines
}

// New creates a new AGUI Messenger with the given config and options.
// An optional session.Service can be provided for structured thread tracking.
// If nil, an in-memory session service is created automatically.
func New(cfg Config, opts ...messenger.Option) *Messenger {
	adapterCfg := messenger.ApplyOptions(opts...)
	appName := cfg.AppName
	if appName == "" {
		appName = DefaultAppName
	}
	return &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
		threads:    make(map[string]*threadState),
		appName:    appName,
	}
}

// ServerConfig holds the dependencies needed to run the AG-UI HTTP server.
// These are injected via ConfigureServer before Connect is called.
type ServerConfig struct {
	AGUIConfig    messenger.AGUIConfig
	ChatHandler   Expert
	ApprovalStore hitl.ApprovalStore
	ClarifyStore  clarify.Store
	BGWorker      *BackgroundWorker
	Workers       []aguitypes.BGWorker

	// ChatFunc is the raw chat function used to create a framework Runner.
	// If non-nil, a Runner is wired automatically.
	ChatFunc ChatFunc
}

// ConfigureServer injects the dependencies needed to run the AG-UI HTTP
// server. This must be called before Connect(). The server is owned by
// the AGUI messenger — Connect() starts it, Disconnect() stops it.
func (m *Messenger) ConfigureServer(cfg ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.server = NewServer(
		cfg.AGUIConfig,
		cfg.ChatHandler,
		cfg.ApprovalStore,
		cfg.ClarifyStore,
		cfg.BGWorker,
		cfg.Workers...,
	)
	m.server.SetMessengerBridge(m)

	if cfg.ChatFunc != nil {
		m.server.SetRunner(NewRunner(cfg.ChatFunc))
	}
}

// Connect starts the AG-UI HTTP/SSE server and begins listening for
// browser connections. Must be called after ConfigureServer.
func (m *Messenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "agui", "fn", "agui.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return messenger.ErrAlreadyConnected
	}

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)
	m.closeDone = make(chan struct{})
	m.sessionSvc = inmemory.NewSessionService()

	// Start the AG-UI HTTP server in a background goroutine.
	if m.server != nil {
		var serverCtx context.Context
		serverCtx, m.serverCfn = context.WithCancel(ctx)

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := m.server.Start(serverCtx); err != nil {
				logger.GetLogger(serverCtx).Error("AG-UI HTTP server error", "error", err)
			}
		}()

		log.Info("AG-UI HTTP server started",
			"port", m.server.port,
			"appName", m.appName)
	}

	m.connected = true
	log.Info("AGUI messenger adapter connected", "appName", m.appName)
	return nil
}

// Disconnect gracefully shuts down the AG-UI HTTP server, cleans up all
// active threads, and releases resources.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "agui", "fn", "agui.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	close(m.closeDone)

	// Stop the AG-UI HTTP server by cancelling its context.
	if m.serverCfn != nil {
		m.serverCfn()
	}
	// Wait for the server goroutine to finish.
	m.wg.Wait()

	// Clean up all active threads.
	m.threadsMu.Lock()
	for id, ts := range m.threads {
		close(ts.done)
		delete(m.threads, id)
	}
	m.threadsMu.Unlock()

	// Close session service to release resources.
	if m.sessionSvc != nil {
		if err := m.sessionSvc.Close(); err != nil {
			log.Warn("failed to close session service", "error", err)
		}
	}

	close(m.incoming)
	m.connected = false

	log.Info("AGUI messenger adapter disconnected")
	return nil
}

// Send delivers a message to the AG-UI SSE stream for the given thread.
// It looks up the active thread's event channel and writes the text content.
// The event channel reader (handleRun) is responsible for converting this
// into proper SSE events.
//
// Send is safe to call from any goroutine. If the thread has already
// completed (client disconnect, run finished), Send returns an error
// without blocking.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	// The ThreadID maps to the AG-UI threadID from InjectMessage.
	threadID := req.ThreadID
	if threadID == "" {
		threadID = req.Channel.ID
	}

	m.threadsMu.RLock()
	ts, ok := m.threads[threadID]
	m.threadsMu.RUnlock()

	if !ok {
		return messenger.SendResponse{}, fmt.Errorf(
			"%w: no active AG-UI thread %q", messenger.ErrChannelNotFound, threadID)
	}

	// AG-UI SSE doesn't support emoji reactions — silently ignore.
	if req.Type == messenger.SendTypeReaction {
		return messenger.SendResponse{
			MessageID: uuid.NewString(),
			Timestamp: time.Now(),
		}, nil
	}

	// Use a three-way select to prevent blocking if the thread has
	// already completed (e.g. client disconnect) or the context is
	// cancelled. This fixes the thread-leak race condition (blind spot #4).
	select {
	case ts.eventChan <- req.Content.Text:
	case <-ts.done:
		return messenger.SendResponse{}, fmt.Errorf(
			"%w: AG-UI thread %q already completed", messenger.ErrChannelNotFound, threadID)
	case <-ctx.Done():
		return messenger.SendResponse{}, ctx.Err()
	}

	return messenger.SendResponse{
		MessageID: uuid.NewString(),
		Timestamp: time.Now(),
	}, nil
}

// Receive returns a read-only channel that delivers incoming messages.
//
// Note: In the current thread-registration model, this channel is not
// actively consumed. It exists to satisfy the Messenger interface and
// for future use if full message routing through the messenger pipeline
// is desired.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the AGUI platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformAGUI
}

// ConnectionInfo returns connection instructions for the AG-UI adapter.
func (m *Messenger) ConnectionInfo() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.server != nil {
		return fmt.Sprintf("AG-UI HTTP server on :%d (%s)", m.server.port, m.appName)
	}
	return fmt.Sprintf("In-process AG-UI adapter (%s)", m.appName)
}

// FormatApproval returns the request unchanged — AG-UI SSE does not support
// rich formatting (the web UI handles approval rendering client-side).
func (m *Messenger) FormatApproval(req messenger.SendRequest, _ messenger.ApprovalInfo) messenger.SendRequest {
	return req
}

// FormatClarification returns the request unchanged — AG-UI SSE handles
// clarification rendering client-side.
func (m *Messenger) FormatClarification(req messenger.SendRequest, _ messenger.ClarificationInfo) messenger.SendRequest {
	return req
}

// InjectMessage registers a thread's event channel with the adapter so that
// Send() can route responses back to the SSE stream. This is the bridge
// between the AG-UI server's handleRun and the messenger subsystem.
//
// The method does NOT push messages into the Receive channel — the chat
// handler runs directly in handleRun. Thread registration is purely for
// enabling subsystems (HITL, send_message tool) to reach the SSE client.
//
// If the threadID already has an active thread registered, the old thread
// is cleaned up first (its done channel is closed) before the new one is
// registered. This prevents resource leaks from rapid same-thread reuse.
//
// Parameters:
//   - threadID: the AG-UI thread identifier
//   - runID: the AG-UI run identifier (used for correlation)
//   - text: the user's message content (logged for debugging)
//   - eventChan: the channel for writing SSE events back to the client
//   - sender: optional *SenderInfo (passed as interface{} to satisfy MessengerBridge); nil defaults to "agui-user"
func (m *Messenger) InjectMessage(threadID, runID, text string, eventChan chan<- interface{}, sender interface{}) error {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.ErrNotConnected
	}

	// Resolve sender identity for session UserID.
	userID := "agui-user"
	if si, ok := sender.(*SenderInfo); ok && si != nil && si.ID != "" {
		userID = si.ID
	}

	done := make(chan struct{})

	m.threadsMu.Lock()
	// Clean up any existing thread with the same ID (blind spot #7).
	if existing, ok := m.threads[threadID]; ok {
		close(existing.done)
	}
	m.threads[threadID] = &threadState{
		eventChan: eventChan,
		done:      done,
		userID:    userID,
	}
	m.threadsMu.Unlock()

	// Create or update the session in the session service.
	// Sessions are keyed by {AppName, UserID, SessionID=threadID}.
	if m.sessionSvc != nil {
		key := session.Key{
			AppName:   m.appName,
			UserID:    userID,
			SessionID: threadID,
		}
		ctx := context.Background()
		if _, err := m.sessionSvc.CreateSession(ctx, key, nil); err != nil {
			// Session may already exist from a previous run — not fatal.
			logger.GetLogger(ctx).Debug("session create (may already exist)",
				"threadID", threadID, "error", err)
		}
	}

	return nil
}

// CompleteThread removes the thread from the active threads map.
// Called by the AG-UI server when a chat run finishes to clean up state.
// Safe to call multiple times for the same threadID.
func (m *Messenger) CompleteThread(threadID string) {
	var userID string
	m.threadsMu.Lock()
	if ts, ok := m.threads[threadID]; ok {
		userID = ts.userID
		close(ts.done)
		delete(m.threads, threadID)
	}
	m.threadsMu.Unlock()

	// Delete the session from the session service (blind spot #4 fix).
	if m.sessionSvc != nil && userID != "" {
		key := session.Key{
			AppName:   m.appName,
			UserID:    userID,
			SessionID: threadID,
		}
		_ = m.sessionSvc.DeleteSession(context.Background(), key)
	}
}

// ThreadDone returns a channel that is closed when the given thread completes.
// This allows callers to detect when a thread's SSE connection has ended.
func (m *Messenger) ThreadDone(threadID string) <-chan struct{} {
	m.threadsMu.RLock()
	defer m.threadsMu.RUnlock()

	if ts, ok := m.threads[threadID]; ok {
		return ts.done
	}
	// Return an already-closed channel if the thread doesn't exist.
	ch := make(chan struct{})
	close(ch)
	return ch
}

// ActiveThreadCount returns the number of currently registered threads.
// Useful for monitoring and testing.
func (m *Messenger) ActiveThreadCount() int {
	m.threadsMu.RLock()
	defer m.threadsMu.RUnlock()
	return len(m.threads)
}

// SessionService returns the underlying session.Service for direct access
// (e.g. session history queries, state management). May be nil if not connected.
func (m *Messenger) SessionService() session.Service {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionSvc
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
