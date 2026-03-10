// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package telegram provides a Messenger adapter for the Telegram platform
// using long-polling for bi-directional communication.
//
// The adapter wraps the [github.com/go-telegram/bot] library and converts
// Telegram-native updates into the generic [messenger.IncomingMessage] type.
// Outgoing messages are sent via the Telegram Bot API's sendMessage endpoint.
//
// Transport: Long-polling (no public endpoint required).
//
// # Authentication
//
// A Bot API token from BotFather is required. Pass it via [Config.Token].
//
// # Usage
//
//	m, err := telegram.New(telegram.Config{Token: os.Getenv("TELEGRAM_BOT_TOKEN")})
//	if err != nil { /* handle */ }
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/media"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformTelegram, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		return New(Config{
			Token: params["token"],
		}, opts...)
	})
}

// Config holds Telegram-specific configuration.
type Config struct {
	// Token is the Telegram Bot API token from BotFather.
	Token string
}

// Messenger implements the [messenger.Messenger] interface for Telegram
// using long-polling. It manages the bot lifecycle, incoming message buffer,
// and connection state through an internal mutex.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	bot        *tgbot.Bot
	incoming   chan messenger.IncomingMessage
	connected  bool
	cancel     context.CancelFunc
	connCtx    context.Context
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// New creates a new Telegram Messenger with the given config and options.
// Returns an error if the underlying Telegram bot client cannot be initialised
// (e.g., malformed token).
func New(cfg Config, opts ...messenger.Option) (*Messenger, error) {
	adapterCfg := messenger.ApplyOptions(opts...)
	m := &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
	}

	// Request message_reaction updates so thumbs up/down on approval messages can resolve HITL.
	bot, err := tgbot.New(cfg.Token,
		tgbot.WithDefaultHandler(m.defaultHandler),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{"message", "edited_message", "channel_post", "message_reaction"}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}
	m.bot = bot

	return m, nil
}

// Connect starts the Telegram bot with long-polling.
// Returns a nil http.Handler since Telegram long-polling is outbound.
func (m *Messenger) Connect(ctx context.Context) (http.Handler, error) {
	log := logger.GetLogger(ctx).With("platform", "telegram", "fn", "telegram.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return nil, messenger.ErrAlreadyConnected
	}

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	connCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.connCtx = connCtx

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.bot.Start(connCtx)
	}()

	m.connected = true
	log.Info("connected to Telegram via long-polling")
	return nil, nil
}

// Disconnect gracefully shuts down the Telegram bot.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "telegram", "fn", "telegram.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	m.cancel()
	m.wg.Wait()
	close(m.incoming)
	m.connected = false
	log.Info("disconnected from Telegram")
	return nil
}

// Send delivers a message to a Telegram chat, or adds an emoji reaction when Type is SendTypeReaction.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	if req.Type == messenger.SendTypeReaction && req.ReplyToMessageID != "" && req.Emoji != "" {
		chatID, err := strconv.ParseInt(req.Channel.ID, 10, 64)
		if err != nil {
			return messenger.SendResponse{}, fmt.Errorf("%w: invalid chat ID %q: %s",
				messenger.ErrSendFailed, req.Channel.ID, err)
		}
		msgID, err := strconv.Atoi(req.ReplyToMessageID)
		if err != nil {
			return messenger.SendResponse{}, fmt.Errorf("%w: invalid message ID for reaction %q: %s",
				messenger.ErrSendFailed, req.ReplyToMessageID, err)
		}
		_, err = m.bot.SetMessageReaction(ctx, &tgbot.SetMessageReactionParams{
			ChatID:    chatID,
			MessageID: msgID,
			Reaction: []tgmodels.ReactionType{
				{
					Type: tgmodels.ReactionTypeTypeEmoji,
					ReactionTypeEmoji: &tgmodels.ReactionTypeEmoji{
						Type:  tgmodels.ReactionTypeTypeEmoji,
						Emoji: req.Emoji,
					},
				},
			},
		})
		if err != nil {
			return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
		}
		return messenger.SendResponse{MessageID: req.ReplyToMessageID, Timestamp: time.Now()}, nil
	}

	chatID, err := strconv.ParseInt(req.Channel.ID, 10, 64)
	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: invalid chat ID %q: %s",
			messenger.ErrSendFailed, req.Channel.ID, err)
	}

	params := &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   req.Content.Text,
	}

	if req.ThreadID != "" {
		threadID, err := strconv.Atoi(req.ThreadID)
		if err == nil {
			params.ReplyParameters = &tgmodels.ReplyParameters{
				MessageID: threadID,
			}
		}
	}

	result, err := m.bot.SendMessage(ctx, params)
	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
	}

	return messenger.SendResponse{
		MessageID: strconv.Itoa(result.ID),
		Timestamp: time.Unix(int64(result.Date), 0),
	}, nil
}

// Receive returns a channel of incoming messages from Telegram.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the Telegram platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformTelegram
}

// ConnectionInfo returns connection instructions for the Telegram adapter.
func (m *Messenger) ConnectionInfo() string {
	return "Connected via Telegram long-polling — message me on Telegram"
}

// emitReaction converts a Telegram MessageReactionUpdated to IncomingMessage and sends it to the incoming channel.
// This allows thumbs up/down on the bot's approval message to resolve HITL approval.
func (m *Messenger) emitReaction(ctx context.Context, r *tgmodels.MessageReactionUpdated) {
	// Ignore when user removed all reactions (NewReaction empty).
	if len(r.NewReaction) == 0 {
		return
	}
	emoji := reactionEmojiFromTypes(r.NewReaction)
	if emoji == "" {
		return
	}
	// Sender: user who set the reaction (User for regular users, ActorChat for anonymous in channels).
	sender := m.senderFromReactionActor(r)
	channelType := messenger.ChannelTypeChannel
	switch r.Chat.Type {
	case "private":
		channelType = messenger.ChannelTypeDM
	case "group", "supergroup":
		channelType = messenger.ChannelTypeGroup
	}
	incoming := messenger.IncomingMessage{
		ID:               fmt.Sprintf("reaction-%d-%d", r.MessageID, r.Date),
		Platform:         messenger.PlatformTelegram,
		Type:             messenger.MessageTypeReaction,
		ReactionEmoji:    emoji,
		ReactedMessageID: strconv.Itoa(r.MessageID),
		Channel:          messenger.Channel{ID: strconv.FormatInt(r.Chat.ID, 10), Name: r.Chat.Title, Type: channelType},
		Sender:           sender,
		Content:          messenger.MessageContent{},
		Timestamp:        time.Unix(int64(r.Date), 0),
	}
	select {
	case m.incoming <- incoming:
	default:
		logger.GetLogger(ctx).With("platform", "telegram").Warn("incoming message buffer full, dropping reaction",
			"chat_id", r.Chat.ID, "message_id", r.MessageID)
	}
}

func reactionEmojiFromTypes(types []tgmodels.ReactionType) string {
	for _, rt := range types {
		if rt.ReactionTypeEmoji != nil {
			return rt.ReactionTypeEmoji.Emoji
		}
	}
	return ""
}

func (m *Messenger) senderFromReactionActor(r *tgmodels.MessageReactionUpdated) messenger.Sender {
	if r.User != nil {
		sender := messenger.Sender{ID: strconv.FormatInt(r.User.ID, 10)}
		if r.User.Username != "" {
			sender.Username = r.User.Username
		}
		displayParts := []string{}
		if r.User.FirstName != "" {
			displayParts = append(displayParts, r.User.FirstName)
		}
		if r.User.LastName != "" {
			displayParts = append(displayParts, r.User.LastName)
		}
		if len(displayParts) > 0 {
			sender.DisplayName = displayParts[0]
			if len(displayParts) > 1 {
				sender.DisplayName += " " + displayParts[1]
			}
		}
		return sender
	}
	if r.ActorChat != nil {
		return messenger.Sender{
			ID:          strconv.FormatInt(r.ActorChat.ID, 10),
			DisplayName: r.ActorChat.Title,
		}
	}
	return messenger.Sender{ID: "unknown"}
}

// defaultHandler is the bot's default update handler that converts Telegram
// messages and message reactions to IncomingMessage and publishes them to the incoming channel.
func (m *Messenger) defaultHandler(ctx context.Context, _ *tgbot.Bot, update *tgmodels.Update) {
	// Handle message_reaction updates so thumbs up on approval messages can resolve HITL.
	if r := update.MessageReaction; r != nil {
		m.emitReaction(ctx, r)
		return
	}
	if update.Message == nil {
		return
	}

	msg := update.Message

	// Determine channel type from chat type.
	channelType := messenger.ChannelTypeChannel
	switch msg.Chat.Type {
	case "private":
		channelType = messenger.ChannelTypeDM
	case "group", "supergroup":
		channelType = messenger.ChannelTypeGroup
	}

	// Build sender info.
	sender := messenger.Sender{
		ID: strconv.FormatInt(msg.From.ID, 10),
	}
	if msg.From.Username != "" {
		sender.Username = msg.From.Username
	}
	displayParts := []string{}
	if msg.From.FirstName != "" {
		displayParts = append(displayParts, msg.From.FirstName)
	}
	if msg.From.LastName != "" {
		displayParts = append(displayParts, msg.From.LastName)
	}
	if len(displayParts) > 0 {
		sender.DisplayName = displayParts[0]
		if len(displayParts) > 1 {
			sender.DisplayName += " " + displayParts[1]
		}
	}

	// Build thread ID and reply-to correlation from reply.
	threadID := ""
	var metadata map[string]any
	if msg.ReplyToMessage != nil {
		threadID = strconv.Itoa(msg.ReplyToMessage.ID)
		// quoted_message_id lets the app resolve approval replies: when the user
		// replies to our approval message, we match it to the right pending approval
		// instead of assuming FIFO (oldest pending).
		metadata = map[string]any{messenger.QuotedMessageID: strconv.Itoa(msg.ReplyToMessage.ID)}
	}

	incoming := messenger.IncomingMessage{
		ID:       strconv.Itoa(msg.ID),
		Platform: messenger.PlatformTelegram,
		Channel: messenger.Channel{
			ID:   strconv.FormatInt(msg.Chat.ID, 10),
			Name: msg.Chat.Title,
			Type: channelType,
		},
		Sender:    sender,
		Content:   messenger.MessageContent{Text: msg.Text},
		ThreadID:  threadID,
		Timestamp: time.Unix(int64(msg.Date), 0),
		Metadata:  metadata,
	}

	// Extract media attachments from Telegram messages.
	if doc := msg.Document; doc != nil {
		incoming.Content.Attachments = append(incoming.Content.Attachments, messenger.Attachment{
			Name:        doc.FileName,
			ContentType: doc.MimeType,
			Size:        doc.FileSize,
		})
	}
	if len(msg.Photo) > 0 {
		// Use the largest photo size (last in the array).
		photo := msg.Photo[len(msg.Photo)-1]
		incoming.Content.Attachments = append(incoming.Content.Attachments, messenger.Attachment{
			Name:        media.NameFromMIME("image/jpeg", "photo"),
			ContentType: "image/jpeg",
			Size:        int64(photo.FileSize),
		})
	}
	if vid := msg.Video; vid != nil {
		incoming.Content.Attachments = append(incoming.Content.Attachments, messenger.Attachment{
			Name:        vid.FileName,
			ContentType: vid.MimeType,
			Size:        vid.FileSize,
		})
	}
	if audio := msg.Audio; audio != nil {
		name := audio.FileName
		if name == "" {
			name = media.NameFromMIME(audio.MimeType, "audio")
		}
		incoming.Content.Attachments = append(incoming.Content.Attachments, messenger.Attachment{
			Name:        name,
			ContentType: audio.MimeType,
			Size:        audio.FileSize,
		})
	}
	// Use caption as text when no text is present (Telegram sends
	// document/photo captions separately from the text field).
	if incoming.Content.Text == "" && msg.Caption != "" {
		incoming.Content.Text = msg.Caption
	}

	select {
	case m.incoming <- incoming:
	default:
		logger.GetLogger(ctx).With("platform", "telegram").Warn("incoming message buffer full, dropping message",
			"chat_id", msg.Chat.ID, "user_id", msg.From.ID)
	}
}

// FormatApproval returns the request unchanged — Telegram does not use
// rich card formatting for approval notifications.
func (m *Messenger) FormatApproval(req messenger.SendRequest, _ messenger.ApprovalInfo) messenger.SendRequest {
	return req
}

// FormatClarification returns the request unchanged — Telegram does not use
// rich card formatting for clarification notifications.
func (m *Messenger) FormatClarification(req messenger.SendRequest, _ messenger.ClarificationInfo) messenger.SendRequest {
	return req
}

// UpdateMessage is a no-op for Telegram — the adapter does not currently
// support editing previously sent messages. Returns nil to satisfy the
// Messenger interface without error.
func (m *Messenger) UpdateMessage(_ context.Context, _ messenger.UpdateRequest) error {
	return nil
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
