// Package googlechat provides a Messenger adapter for Google Chat using
// HTTP push for incoming events and the Chat API for outgoing messages.
//
// The adapter wraps [google.golang.org/api/chat/v1] and exposes an HTTP
// endpoint that receives push events from Google Chat. Outgoing messages
// are sent via the Chat API's spaces.messages.create method.
//
// Transport: HTTP push (public endpoint required for incoming events).
//
// # Authentication
//
// A Google service account JSON key file is required for accessing the
// Chat API. The service account must be granted the Chat Bot role.
//
// # Usage
//
//	m := googlechat.New(googlechat.Config{
//		CredentialsFile: "/path/to/service-account.json",
//		ListenAddr:      ":8080",
//	})
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package googlechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
	"google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformGoogleChat, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		return New(Config{
			CredentialsFile: params["credentials_file"],
			ListenAddr:      params["listen_addr"],
		}, opts...), nil
	})
}

// Config holds Google Chat-specific configuration.
type Config struct {
	// CredentialsFile is the path to the Google service account JSON key file.
	CredentialsFile string
	// ListenAddr is the address to listen on for incoming push events (e.g., ":8080").
	ListenAddr string
}

// Messenger implements the [messenger.Messenger] interface for Google Chat.
// It manages the Chat API client, an HTTP push listener for incoming events,
// and connection state through an internal mutex.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	chatSvc    *chat.Service
	incoming   chan messenger.IncomingMessage
	server     *http.Server
	connected  bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// chatEvent represents the JSON payload pushed by Google Chat to the bot's endpoint.
type chatEvent struct {
	Type      string     `json:"type"`
	EventTime string     `json:"eventTime"`
	Message   *gcMessage `json:"message"`
	Space     *gcSpace   `json:"space"`
	User      *gcUser    `json:"user"`
}

type gcMessage struct {
	Name         string    `json:"name"`
	Text         string    `json:"text"`
	Thread       *gcThread `json:"thread"`
	ArgumentText string    `json:"argumentText"`
	CreateTime   string    `json:"createTime"`
}

type gcThread struct {
	Name string `json:"name"`
}

type gcSpace struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}

type gcUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}

// New creates a new Google Chat Messenger with the given config and options.
func New(cfg Config, opts ...messenger.Option) *Messenger {
	adapterCfg := messenger.ApplyOptions(opts...)
	return &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
	}
}

// Connect initializes the Chat API client and starts the HTTP push listener.
func (m *Messenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "googlechat", "fn", "googlechat.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return messenger.ErrAlreadyConnected
	}

	// Initialize the Chat API service.
	var chatOpts []option.ClientOption
	if m.cfg.CredentialsFile != "" {
		// Read credentials file content to avoid deprecated WithCredentialsFile
		// which is flagged as a potential security risk by staticcheck
		creds, err := os.ReadFile(m.cfg.CredentialsFile)
		if err != nil {
			return fmt.Errorf("failed to read credentials file: %w", err)
		}
		chatOpts = append(chatOpts, option.WithAuthCredentialsJSON(option.ServiceAccount, creds))
	}

	svc, err := chat.NewService(ctx, chatOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Google Chat service: %w", err)
	}
	m.chatSvc = svc

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	_, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/", m.handleEvent)

	m.server = &http.Server{
		Addr:              m.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		log.Info("starting Google Chat HTTP push listener", "addr", m.cfg.ListenAddr)
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.GetLogger(ctx).With("platform", "googlechat").Error("Google Chat HTTP server error", "error", err)
		}
	}()

	m.connected = true
	log.Info("connected to Google Chat")
	return nil
}

// Disconnect gracefully shuts down the Google Chat adapter.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "googlechat", "fn", "googlechat.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	m.cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()

	if err := m.server.Shutdown(shutdownCtx); err != nil {
		log.Error("Google Chat server shutdown error", "error", err)
	}

	m.wg.Wait()
	close(m.incoming)
	m.connected = false
	log.Info("disconnected from Google Chat")
	return nil
}

// Send delivers a message to a Google Chat space.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	gcMsg := &chat.Message{
		Text: req.Content.Text,
	}

	if req.ThreadID != "" {
		gcMsg.Thread = &chat.Thread{
			Name: req.ThreadID,
		}
	}

	result, err := m.chatSvc.Spaces.Messages.Create(req.Channel.ID, gcMsg).Context(ctx).Do()
	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
	}

	return messenger.SendResponse{
		MessageID: result.Name,
		Timestamp: time.Now(),
	}, nil
}

// Receive returns a channel of incoming messages from Google Chat.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the Google Chat platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformGoogleChat
}

// handleEvent processes incoming HTTP push events from Google Chat.
func (m *Messenger) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	var event chatEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		logger.GetLogger(ctx).With("platform", "googlechat").Error("failed to decode Google Chat event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if event.Type == "MESSAGE" && event.Message != nil {
		m.convertAndPublish(ctx, event)
	}

	w.WriteHeader(http.StatusOK)
}

func (m *Messenger) convertAndPublish(ctx context.Context, event chatEvent) {
	channelType := messenger.ChannelTypeChannel
	if event.Space != nil && event.Space.Type == "DM" {
		channelType = messenger.ChannelTypeDM
	}

	spaceName := ""
	spaceDisplayName := ""
	if event.Space != nil {
		spaceName = event.Space.Name
		spaceDisplayName = event.Space.DisplayName
	}

	senderID := ""
	senderDisplay := ""
	if event.User != nil {
		senderID = event.User.Name
		senderDisplay = event.User.DisplayName
	}

	threadID := ""
	if event.Message.Thread != nil {
		threadID = event.Message.Thread.Name
	}

	msg := messenger.IncomingMessage{
		ID:       event.Message.Name,
		Platform: messenger.PlatformGoogleChat,
		Channel: messenger.Channel{
			ID:   spaceName,
			Name: spaceDisplayName,
			Type: channelType,
		},
		Sender: messenger.Sender{
			ID:          senderID,
			DisplayName: senderDisplay,
		},
		Content: messenger.MessageContent{
			Text: event.Message.Text,
		},
		ThreadID:  threadID,
		Timestamp: time.Now(),
	}

	select {
	case m.incoming <- msg:
	default:
		logger.GetLogger(ctx).With("platform", "googlechat").Warn("incoming message buffer full, dropping message",
			"space", spaceName, "user", senderID)
	}
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
