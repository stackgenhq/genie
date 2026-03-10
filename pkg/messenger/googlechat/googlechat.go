// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

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
// Google Chat uses the same logged-in user OAuth token as Gmail/Calendar/Drive.
// The application must pass messenger.WithSecretProvider(sp) when creating the
// messenger so the adapter can resolve the token (TokenFile, keyring, or env).
// No service account credentials file is used.
//
// # Migration from service account
//
// Previous versions used credentials_file and listen_addr with a service account.
// That configuration has been removed. Existing deployments should migrate to
// logged-in user OAuth: run setup and connect Google (same token as Gmail/Calendar).
// See the installation documentation for migration steps.
//
// # Usage
//
//	m := googlechat.New(googlechat.Config{}, messenger.WithSecretProvider(sp))
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package googlechat

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

// Chat API OAuth scope for sending and reading messages as the user.
const chatScope = "https://www.googleapis.com/auth/chat.messages"

func init() {
	messenger.RegisterAdapter(messenger.PlatformGoogleChat, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		return New(Config{}, opts...), nil
	})
}

// Config holds Google Chat-specific configuration.
// Authentication uses the logged-in user token via SecretProvider (pass WithSecretProvider when creating the messenger).
type Config struct{}

// Messenger implements the [messenger.Messenger] interface for Google Chat.
// It manages the Chat API client, returns an HTTP handler for incoming push
// events, and tracks connection state through an internal mutex.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	chatSvc    *chat.Service
	incoming   chan messenger.IncomingMessage
	connected  bool
	cancel     context.CancelFunc
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
	Name         string         `json:"name"`
	Text         string         `json:"text"`
	Thread       *gcThread      `json:"thread"`
	ArgumentText string         `json:"argumentText"`
	CreateTime   string         `json:"createTime"`
	Attachment   []gcAttachment `json:"attachment"`
}

type gcAttachment struct {
	Name              string `json:"name"`
	ContentName       string `json:"contentName"`
	ContentType       string `json:"contentType"`
	DownloadURI       string `json:"downloadUri"`
	ThumbnailURI      string `json:"thumbnailUri"`
	AttachmentDataRef *struct {
		ResourceName string `json:"resourceName"`
	} `json:"attachmentDataRef"`
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

// Connect initializes the Chat API client and returns an http.Handler
// for receiving Google Chat push events. The caller mounts this handler
// on a shared HTTP mux at the desired context path.
//
// The adapter DOES NOT start its own http.Server.
func (m *Messenger) Connect(ctx context.Context) (http.Handler, error) {
	log := logger.GetLogger(ctx).With("platform", "googlechat", "fn", "googlechat.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return nil, messenger.ErrAlreadyConnected
	}

	// Initialize the Chat API service using the logged-in user OAuth token (same as Gmail/Calendar/Drive).
	if m.adapterCfg.SecretProvider == nil {
		return nil, fmt.Errorf("google Chat requires WithSecretProvider so it can use the logged-in user token; no service account credentials file")
	}
	credsEntry, _ := m.adapterCfg.SecretProvider.GetSecret(ctx, security.GetSecretRequest{
		Name:   "CredentialsFile",
		Reason: toolcontext.GetJustification(ctx),
	})
	credsJSON, err := oauth.GetCredentials(credsEntry, "Chat")
	if err != nil {
		return nil, fmt.Errorf("google chat credentials: %w", err)
	}
	tokenJSON, save, err := oauth.GetToken(ctx, m.adapterCfg.SecretProvider)
	if err != nil {
		return nil, fmt.Errorf("google chat token: %w", err)
	}
	client, err := oauth.HTTPClient(ctx, credsJSON, tokenJSON, save, []string{chatScope})
	if err != nil {
		return nil, fmt.Errorf("google chat HTTP client: %w", err)
	}
	svc, err := chat.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create Google Chat service: %w", err)
	}
	m.chatSvc = svc

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	_, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	m.connected = true
	log.Info("Google Chat adapter connected (handler returned for caller to mount)")
	return http.HandlerFunc(m.handleEvent), nil
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
	m.incoming = nil
	m.connected = false
	log.Info("disconnected from Google Chat")
	return nil
}

// Send delivers a message to a Google Chat space.
// If req.Metadata["cards_v2"] contains a []*chat.CardWithId, the message is sent
// with Cards v2 formatting (the text field is used as the plaintext fallback).
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	if req.Type == messenger.SendTypeReaction {
		return messenger.SendResponse{}, fmt.Errorf("%w: Google Chat adapter does not support adding reactions", messenger.ErrSendFailed)
	}

	gcMsg := &chat.Message{
		Text: req.Content.Text,
	}

	if req.ThreadID != "" {
		gcMsg.Thread = &chat.Thread{
			Name: req.ThreadID,
		}
	}

	if cards := extractCardsV2(req.Metadata); len(cards) > 0 {
		gcMsg.CardsV2 = cards
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

// extractCardsV2 converts metadata["cards_v2"] into typed []*chat.CardWithId.
// It accepts both typed []*chat.CardWithId (pass-through) and generic []any / []map[string]any
// (JSON round-tripped into SDK types). Returns nil if no valid cards are found.
func extractCardsV2(metadata map[string]any) []*chat.CardWithId {
	if metadata == nil {
		return nil
	}

	raw, ok := metadata["cards_v2"]
	if !ok {
		return nil
	}

	switch c := raw.(type) {
	case []*chat.CardWithId:
		return c
	default:
		// JSON round-trip []any / []map[string]any into SDK types.
		data, err := json.Marshal(c)
		if err != nil {
			return nil
		}
		var cards []*chat.CardWithId
		if json.Unmarshal(data, &cards) != nil {
			return nil
		}
		return cards
	}
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

// ConnectionInfo returns connection instructions for the Google Chat adapter.
func (m *Messenger) ConnectionInfo() string {
	return "Message me in Google Chat — push events are received on the shared HTTP server"
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

	// Extract file attachments from Google Chat event.
	for _, a := range event.Message.Attachment {
		att := messenger.Attachment{
			Name:        a.ContentName,
			URL:         a.DownloadURI,
			ContentType: a.ContentType,
		}
		msg.Content.Attachments = append(msg.Content.Attachments, att)
	}

	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()
	if !connected {
		return
	}

	select {
	case m.incoming <- msg:
	default:
		logger.GetLogger(ctx).With("platform", "googlechat").Warn("incoming message buffer full, dropping message",
			"space", spaceName, "user", senderID)
	}
}

// FormatApproval builds a Google Chat Cards v2 message for an approval notification.
// This satisfies the messenger.ApprovalFormatter interface, keeping all
// Google Chat-specific formatting inside the adapter.
func (m *Messenger) FormatApproval(req messenger.SendRequest, info messenger.ApprovalInfo) messenger.SendRequest {
	sections := []any{
		// Args section
		map[string]any{
			"header": "📋 Arguments",
			"widgets": []any{
				map[string]any{
					"textParagraph": map[string]any{
						"text": fmt.Sprintf("<pre>%s</pre>", html.EscapeString(info.Args)),
					},
				},
			},
		},
	}

	// Justification section (if present)
	if info.Feedback != "" {
		sections = append([]any{
			map[string]any{
				"header": "💡 Justification",
				"widgets": []any{
					map[string]any{
						"textParagraph": map[string]any{
							"text": html.EscapeString(info.Feedback),
						},
					},
				},
			},
		}, sections...)
	}

	// Footer section with reply instructions
	sections = append(sections, map[string]any{
		"widgets": []any{
			map[string]any{
				"textParagraph": map[string]any{
					"text": "<i>Reply <b>Yes</b> to approve, <b>No</b> to reject, or type feedback to revisit. React with 👍 to approve or 👎 to reject.</i>",
				},
			},
		},
	})

	card := map[string]any{
		"cardId": "approval_" + info.ID,
		"card": map[string]any{
			"header": map[string]any{
				"title":    fmt.Sprintf("⚠️ Approval Required — %s", info.ToolName),
				"subtitle": "Tool approval request",
			},
			"sections": sections,
		},
	}

	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}
	req.Metadata["cards_v2"] = []any{card}

	return req
}

// FormatClarification builds a Google Chat Cards v2 message for a clarification question.
func (m *Messenger) FormatClarification(req messenger.SendRequest, info messenger.ClarificationInfo) messenger.SendRequest {
	sections := []any{
		// Question section
		map[string]any{
			"header": "Question",
			"widgets": []any{
				map[string]any{
					"textParagraph": map[string]any{
						"text": html.EscapeString(info.Question),
					},
				},
			},
		},
	}

	// Context section (if provided)
	if info.Context != "" {
		sections = append([]any{
			map[string]any{
				"header": "💡 Context",
				"widgets": []any{
					map[string]any{
						"textParagraph": map[string]any{
							"text": html.EscapeString(info.Context),
						},
					},
				},
			},
		}, sections...)
	}

	// Reply instructions
	sections = append(sections, map[string]any{
		"widgets": []any{
			map[string]any{
				"textParagraph": map[string]any{
					"text": "<i>Reply with your answer.</i>",
				},
			},
		},
	})

	card := map[string]any{
		"cardId": "clarify_" + info.RequestID,
		"card": map[string]any{
			"header": map[string]any{
				"title":    "❓ Question from Genie",
				"subtitle": "Clarification needed",
			},
			"sections": sections,
		},
	}

	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}
	req.Metadata["cards_v2"] = []any{card}

	return req
}

// UpdateMessage is a no-op for Google Chat — the adapter does not currently
// support editing previously sent messages. Returns nil to satisfy the
// Messenger interface without error.
func (m *Messenger) UpdateMessage(_ context.Context, _ messenger.UpdateRequest) error {
	return nil
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
