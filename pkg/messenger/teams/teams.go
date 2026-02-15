// Package teams provides a Messenger adapter for Microsoft Teams using the
// Bot Framework protocol for bi-directional communication.
//
// The adapter wraps [github.com/infracloudio/msbotbuilder-go] and exposes an
// HTTP endpoint that receives Bot Framework activities. Outgoing messages are
// sent as proactive messages through the Bot Framework REST API.
//
// Transport: HTTP webhook (public endpoint required).
//
// # Authentication
//
// A Bot Framework App ID and App Password are required. These are obtained
// from the Azure Bot registration portal.
//
// # Usage
//
//	m, err := teams.New(teams.Config{
//		AppID:       os.Getenv("TEAMS_APP_ID"),
//		AppPassword: os.Getenv("TEAMS_APP_PASSWORD"),
//		ListenAddr:  ":3978",
//	})
//	if err != nil { /* handle */ }
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/infracloudio/msbotbuilder-go/core"
	"github.com/infracloudio/msbotbuilder-go/core/activity"
	"github.com/infracloudio/msbotbuilder-go/schema"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformTeams, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		cfg := Config{
			AppID:       params["app_id"],
			AppPassword: params["app_password"],
			ListenAddr:  params["listen_addr"],
		}
		if cfg.ListenAddr == "" {
			cfg.ListenAddr = ":3978"
		}
		return New(cfg, opts...)
	})
}

// Config holds Teams-specific configuration.
type Config struct {
	// AppID is the Microsoft Bot Framework App ID.
	AppID string
	// AppPassword is the Microsoft Bot Framework App Password.
	AppPassword string
	// ListenAddr is the address to listen on for incoming activities (e.g., ":3978").
	ListenAddr string
}

// Messenger implements the [messenger.Messenger] interface for Microsoft Teams.
// It runs an HTTP server to receive Bot Framework activities and uses the
// adapter to send proactive messages.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	adapter    core.Adapter
	incoming   chan messenger.IncomingMessage
	server     *http.Server
	connected  bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// New creates a new Teams Messenger with the given config and options.
func New(cfg Config, opts ...messenger.Option) (*Messenger, error) {
	adapterCfg := messenger.ApplyOptions(opts...)

	setting := core.AdapterSetting{
		AppID:       cfg.AppID,
		AppPassword: cfg.AppPassword,
	}

	adapter, err := core.NewBotAdapter(setting)
	if err != nil {
		return nil, fmt.Errorf("failed to create Teams bot adapter: %w", err)
	}

	return &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
		adapter:    adapter,
	}, nil
}

// Connect starts the HTTP server to receive Bot Framework activities from Teams.
func (m *Messenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "teams", "fn", "teams.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return messenger.ErrAlreadyConnected
	}

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	_, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/api/messages", m.handleActivity)

	m.server = &http.Server{
		Addr:              m.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		log.Info("starting Teams webhook listener", "addr", m.cfg.ListenAddr)
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.GetLogger(ctx).With("platform", "teams").Error("Teams webhook server error", "error", err)
		}
	}()

	m.connected = true
	log.Info("connected to Teams via Bot Framework")
	return nil
}

// Disconnect gracefully shuts down the Teams webhook server.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "teams", "fn", "teams.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	m.cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()

	if err := m.server.Shutdown(shutdownCtx); err != nil {
		log.Error("Teams server shutdown error", "error", err)
	}

	m.wg.Wait()
	close(m.incoming)
	m.connected = false
	log.Info("disconnected from Teams")
	return nil
}

// Send delivers a message to a Teams conversation.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	// Build a ConversationReference to send a proactive message.
	ref := schema.ConversationReference{
		Conversation: schema.ConversationAccount{
			ID: req.Channel.ID,
		},
	}

	if req.ThreadID != "" {
		ref.ActivityID = req.ThreadID
	}

	err := m.adapter.ProactiveMessage(ctx, ref, activity.HandlerFuncs{
		OnMessageFunc: func(turn *activity.TurnContext) (schema.Activity, error) {
			return turn.SendActivity(activity.MsgOptionText(req.Content.Text))
		},
	})
	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
	}

	return messenger.SendResponse{
		MessageID: ref.ActivityID,
		Timestamp: time.Now(),
	}, nil
}

// Receive returns a channel of incoming messages from Teams.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the Teams platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformTeams
}

// handleActivity processes incoming Bot Framework activities.
func (m *Messenger) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx).With("platform", "teams", "fn", "teams.handleActivity")

	act, err := m.adapter.ParseRequest(ctx, r)
	if err != nil {
		log.Error("failed to parse Teams activity", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Process the activity through the handler to handle auth, etc.
	err = m.adapter.ProcessActivity(ctx, act, activity.HandlerFuncs{
		OnMessageFunc: func(turn *activity.TurnContext) (schema.Activity, error) {
			m.convertAndPublish(ctx, turn.Activity)
			return schema.Activity{}, nil
		},
	})

	if err != nil {
		log.Error("failed to process Teams activity", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Respond with 200 OK as required by Bot Framework.
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *Messenger) convertAndPublish(ctx context.Context, act schema.Activity) {
	if act.Type != schema.Message {
		return
	}

	channelType := messenger.ChannelTypeChannel
	if !act.Conversation.IsGroup {
		channelType = messenger.ChannelTypeDM
	}

	msg := messenger.IncomingMessage{
		ID:       act.ID,
		Platform: messenger.PlatformTeams,
		Channel: messenger.Channel{
			ID:   act.Conversation.ID,
			Name: act.Conversation.Name,
			Type: channelType,
		},
		Sender: messenger.Sender{
			ID:          act.From.ID,
			DisplayName: act.From.Name,
		},
		Content: messenger.MessageContent{
			Text: act.Text,
		},
		ThreadID:  act.ReplyToID,
		Timestamp: time.Now(),
	}

	select {
	case m.incoming <- msg:
	default:
		logger.GetLogger(ctx).With("platform", "teams").Warn("incoming message buffer full, dropping message",
			"conversation", act.Conversation.ID, "from", act.From.ID)
	}
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
