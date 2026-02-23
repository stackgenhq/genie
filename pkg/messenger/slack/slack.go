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
	"encoding/json"
	"fmt"
	"net/http"
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
	// Not needed when using HTTP Events API mode (SigningSecret + SharedMux).
	AppToken string
	// BotToken is the Slack bot user OAuth token (xoxb-...).
	BotToken string
	// SigningSecret is used to verify incoming HTTP Events API requests.
	// When set together with WithSharedMux(), the adapter operates in HTTP
	// push mode instead of Socket Mode: incoming events arrive as signed
	// HTTP POST requests rather than over a WebSocket connection.
	// See: https://api.slack.com/authentication/verifying-requests-from-slack
	SigningSecret string
}

// Messenger implements the [messenger.Messenger] interface for Slack.
// It supports two transport modes:
//   - Socket Mode (default): outbound WebSocket, no public endpoint required.
//   - HTTP Events API: inbound HTTP push, requires SigningSecret + SharedMux.
//
// It manages the event routing, incoming message buffer, and connection
// state through an internal mutex.
type Messenger struct {
	cfg           Config
	adapterCfg    messenger.AdapterConfig
	api           *slack.Client
	socket        *socketmode.Client
	incoming      chan messenger.IncomingMessage
	eventsHandler *eventsHTTPHandler // non-nil when using HTTP Events API mode
	connected     bool
	cancel        context.CancelFunc
	connCtx       context.Context
	wg            sync.WaitGroup
	mu            sync.RWMutex
}

// New creates a new Slack Messenger with the given config and options.
func New(cfg Config, opts ...messenger.Option) *Messenger {
	adapterCfg := messenger.ApplyOptions(opts...)
	return &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
	}
}

// useHTTPEventsAPI returns true when the adapter should use HTTP Events API
// mode instead of Socket Mode. This requires a signing secret to be configured.
func (m *Messenger) useHTTPEventsAPI() bool {
	return m.cfg.SigningSecret != ""
}

// Connect establishes a connection to Slack and returns an optional
// http.Handler.
//
// In HTTP Events API mode (SigningSecret set): returns a non-nil handler
// that the caller mounts at a context path. Events arrive as signed HTTP
// POST requests.
//
// In Socket Mode (default): returns nil handler and opens an outbound
// WebSocket connection. No inbound HTTP required.
func (m *Messenger) Connect(ctx context.Context) (http.Handler, error) {
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "slack.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return nil, messenger.ErrAlreadyConnected
	}

	m.api = slack.New(
		m.cfg.BotToken,
		slack.OptionAppLevelToken(m.cfg.AppToken),
	)

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	connCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.connCtx = connCtx

	if m.useHTTPEventsAPI() {
		// HTTP Events API mode: return handler for caller to mount.
		m.eventsHandler = &eventsHTTPHandler{
			signingSecret: m.cfg.SigningSecret,
			incoming:      m.incoming,
		}
		m.connected = true
		log.Info("connected to Slack via HTTP Events API")
		return m.eventsHandler, nil
	}

	// Socket Mode: outbound WebSocket connection (nil handler).
	m.socket = socketmode.New(m.api)

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
	return nil, nil
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
	if m.useHTTPEventsAPI() {
		// HTTP Events API mode: don't close the channel, in-flight
		// requests from the shared HTTP server may still write to it.
		m.incoming = nil
	} else {
		// Socket Mode: goroutines are drained by wg.Wait() first.
		m.wg.Wait()
		close(m.incoming)
	}
	m.connected = false
	log.Info("disconnected from Slack")
	return nil
}

// Send delivers a message to a Slack channel or thread.
// If req.Metadata["blocks"] contains a []slack.Block, the message is sent
// with Block Kit formatting (the text field is used as the plaintext fallback).
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

	if blocks := extractBlocks(req.Metadata); len(blocks) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(blocks...))
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

// extractBlocks converts metadata["blocks"] into typed []slack.Block.
// It accepts both typed []slack.Block (pass-through) and generic []any / []map[string]any
// (JSON round-tripped into SDK types). Returns nil if no valid blocks are found.
func extractBlocks(metadata map[string]any) []slack.Block {
	if metadata == nil {
		return nil
	}

	raw, ok := metadata["blocks"]
	if !ok {
		return nil
	}

	switch b := raw.(type) {
	case []slack.Block:
		return b
	default:
		// JSON round-trip []any / []map[string]any into SDK types.
		data, err := json.Marshal(b)
		if err != nil {
			return nil
		}
		var blocks slack.Blocks
		if json.Unmarshal(data, &blocks) != nil {
			return nil
		}
		return blocks.BlockSet
	}
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

// ConnectionInfo returns connection instructions for the Slack adapter.
func (m *Messenger) ConnectionInfo() string {
	if m.useHTTPEventsAPI() {
		return "Connected via Slack HTTP Events API — configure your Request URL to point to the GUILD ingress"
	}
	return "Connected via Slack Socket Mode — message me in your Slack workspace"
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

			// Extract file attachments from the Slack message.
			if ev.Message != nil {
				for _, f := range ev.Message.Files {
					att := messenger.Attachment{
						Name:        f.Name,
						URL:         f.URLPrivateDownload,
						ContentType: f.Mimetype,
						Size:        int64(f.Size),
					}
					if att.URL == "" {
						att.URL = f.Permalink
					}
					msg.Content.Attachments = append(msg.Content.Attachments, att)
				}
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

// FormatApproval builds a Slack Block Kit message for an approval notification.
// This satisfies the messenger.ApprovalFormatter interface, keeping all
// Slack-specific formatting inside the Slack adapter.
func (m *Messenger) FormatApproval(req messenger.SendRequest, info messenger.ApprovalInfo) messenger.SendRequest {
	// Plaintext fallback (used in push notifications and unfurls).
	req.Content = messenger.MessageContent{
		Text: fmt.Sprintf("⚠️ Approval Required — %s\nArgs:\n%s", info.ToolName, info.Args),
	}

	blocks := []any{
		// Header
		map[string]any{
			"type": "header",
			"text": map[string]any{
				"type": "plain_text",
				"text": fmt.Sprintf("⚠️ Approval Required — %s", info.ToolName),
			},
		},
	}

	// Justification section
	if info.Feedback != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf("💡 *Why*: %s", info.Feedback),
			},
		})
	}

	// Args as a code block section
	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": fmt.Sprintf("📋 *Arguments*:\n```%s```", info.Args),
		},
	})

	// Divider before actions
	blocks = append(blocks, map[string]any{
		"type": "divider",
	})

	// Action buttons
	blocks = append(blocks, map[string]any{
		"type":     "actions",
		"block_id": "approval_" + info.ID,
		"elements": []any{
			map[string]any{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "✅ Approve"},
				"style":     "primary",
				"action_id": "approve_" + info.ID,
				"value":     info.ID,
			},
			map[string]any{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "🔄 Revisit"},
				"action_id": "revisit_" + info.ID,
				"value":     info.ID,
			},
			map[string]any{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "❌ Reject"},
				"style":     "danger",
				"action_id": "reject_" + info.ID,
				"value":     info.ID,
			},
		},
	})

	// Context footer
	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []any{
			map[string]any{
				"type": "mrkdwn",
				"text": "You can also reply *Yes* to approve, *No* to reject, or type feedback to revisit.",
			},
		},
	})

	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}
	req.Metadata["blocks"] = blocks

	return req
}

// FormatClarification builds a Slack Block Kit message for a clarification question.
func (m *Messenger) FormatClarification(req messenger.SendRequest, info messenger.ClarificationInfo) messenger.SendRequest {
	req.Content = messenger.MessageContent{
		Text: fmt.Sprintf("❓ Question from Genie:\n%s", info.Question),
	}

	blocks := []any{
		// Header
		map[string]any{
			"type": "header",
			"text": map[string]any{
				"type": "plain_text",
				"text": "❓ Question from Genie",
			},
		},
	}

	// Context section (if provided)
	if info.Context != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf("💡 *Context*: %s", info.Context),
			},
		})
	}

	// Question section
	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": info.Question,
		},
	})

	// Divider
	blocks = append(blocks, map[string]any{
		"type": "divider",
	})

	// Footer
	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []any{
			map[string]any{
				"type": "mrkdwn",
				"text": "_Reply with your answer._",
			},
		},
	})

	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}
	req.Metadata["blocks"] = blocks

	return req
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
