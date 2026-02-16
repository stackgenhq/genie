// Package slack provides a Messenger adapter for the Slack platform using
// Socket Mode for bi-directional communication without a public endpoint.
//
// The adapter wraps [github.com/slack-go/slack] and its Socket Mode client.
// Incoming events are received over a WebSocket connection, and outgoing
// messages are sent via the Slack Web API.
//
// Transport: Socket Mode (WebSocket — no public endpoint required).
//
// # Authentication
//
// Two tokens are required:
//   - An app-level token (xapp-…) with the connections:write scope for Socket Mode.
//   - A bot user OAuth token (xoxb-…) with chat:write and other desired scopes.
//
// # Usage
//
//	m := slack.New(slack.Config{
//		AppToken: os.Getenv("SLACK_APP_TOKEN"),
//		BotToken: os.Getenv("SLACK_BOT_TOKEN"),
//	})
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package slack

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformSlack, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		return New(Config{
			AppToken: params["app_token"],
			BotToken: params["bot_token"],
		}, opts...), nil
	})
}

// Config holds Slack-specific configuration.
type Config struct {
	// AppToken is the Slack app-level token (xapp-...) required for Socket Mode.
	AppToken string
	// BotToken is the Slack bot user OAuth token (xoxb-...).
	BotToken string
}

// Messenger implements the [messenger.Messenger] interface for Slack using
// Socket Mode. It manages the WebSocket lifecycle, event routing, incoming
// message buffer, and connection state through an internal mutex.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	api        *slack.Client
	socket     *socketmode.Client
	incoming   chan messenger.IncomingMessage
	connected  bool
	cancel     context.CancelFunc
	connCtx    context.Context
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// New creates a new Slack Messenger with the given config and options.
func New(cfg Config, opts ...messenger.Option) *Messenger {
	adapterCfg := messenger.ApplyOptions(opts...)
	return &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
	}
}

// Connect establishes a Socket Mode connection to Slack.
func (m *Messenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "slack.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return messenger.ErrAlreadyConnected
	}

	m.api = slack.New(
		m.cfg.BotToken,
		slack.OptionAppLevelToken(m.cfg.AppToken),
	)

	m.socket = socketmode.New(m.api)
	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	connCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.connCtx = connCtx

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.handleEvents(connCtx)
	}()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := m.socket.RunContext(connCtx); err != nil {
			logger.GetLogger(connCtx).With("platform", "slack").Error("socket mode connection error", "error", err)
		}
	}()

	m.connected = true
	log.Info("connected to Slack via Socket Mode")
	return nil
}

// Disconnect gracefully shuts down the Slack connection.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "slack.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	m.cancel()
	m.wg.Wait()
	close(m.incoming)
	m.connected = false
	log.Info("disconnected from Slack")
	return nil
}

// Send delivers a message to a Slack channel or thread.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(req.Content.Text, false),
	}

	if req.ThreadID != "" {
		opts = append(opts, slack.MsgOptionTS(req.ThreadID))
	}

	channelID, ts, _, err := m.api.SendMessageContext(ctx, req.Channel.ID, opts...)
	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
	}

	return messenger.SendResponse{
		MessageID: channelID + ":" + ts,
		Timestamp: time.Now(),
	}, nil
}

// Receive returns a channel of incoming messages from Slack.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the Slack platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformSlack
}

// handleEvents processes Socket Mode events and converts them to IncomingMessages.
func (m *Messenger) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-m.socket.Events:
			if !ok {
				return
			}
			m.processEvent(ctx, evt)
		}
	}
}

func (m *Messenger) processEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		m.socket.Ack(*evt.Request)
		m.handleEventsAPI(ctx, eventsAPIEvent)
	}
}

func (m *Messenger) handleEventsAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			// Skip bot messages to avoid echo loops.
			if ev.BotID != "" {
				return
			}
			msg := messenger.IncomingMessage{
				ID:       ev.TimeStamp,
				Platform: messenger.PlatformSlack,
				Channel: messenger.Channel{
					ID:   ev.Channel,
					Type: messenger.ChannelTypeChannel,
				},
				Sender: messenger.Sender{
					ID:       ev.User,
					Username: ev.User,
				},
				Content: messenger.MessageContent{
					Text: ev.Text,
				},
				ThreadID:  ev.ThreadTimeStamp,
				Timestamp: time.Now(),
			}

			select {
			case m.incoming <- msg:
			default:
				logger.GetLogger(ctx).With("platform", "slack").Warn("incoming message buffer full, dropping message",
					"channel", ev.Channel, "user", ev.User)
			}
		}
	}
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
