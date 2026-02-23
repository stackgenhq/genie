// Package telegram provides a Messenger adapter for the Telegram platform.
//
// This file implements the HTTP webhook handler (webhookHTTPHandler),
// enabling Telegram to receive updates via HTTP push instead of long-polling.
// This allows the adapter to be mounted on a shared HTTP mux at a
// context path (e.g., /agents/{name}/telegram/events) rather than
// requiring its own polling loop.
//
// Telegram webhooks are set via the Bot API's setWebhook method.
// Updates arrive as JSON POST requests to the configured URL.
// See: https://core.telegram.org/bots/api#setwebhook
package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	tgmodels "github.com/go-telegram/bot/models"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
)

// webhookHTTPHandler handles incoming Telegram webhook update requests.
// It parses the Telegram Update JSON and converts message events into
// IncomingMessages published to the adapter's incoming channel.
//
// This handler is used when the Telegram adapter runs in webhook mode
// (WebhookURL is set + SharedMux). The handler can be mounted on any
// http.ServeMux or router.
type webhookHTTPHandler struct {
	// botToken is used to verify the webhook secret (optional).
	botToken string
	// incoming is the shared channel with the Telegram messenger.
	incoming chan messenger.IncomingMessage
}

// ServeHTTP processes incoming Telegram webhook update requests.
func (h *webhookHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	log := logger.GetLogger(ctx).With("platform", "telegram", "fn", "webhookHTTPHandler.ServeHTTP")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		log.Error("failed to read request body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var update tgmodels.Update
	if err := json.Unmarshal(body, &update); err != nil {
		log.Error("failed to parse Telegram update", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Acknowledge receipt immediately.
	w.WriteHeader(http.StatusOK)

	// Process message updates.
	if update.Message != nil {
		h.handleMessage(ctx, update.Message)
	}
}

// handleMessage converts a Telegram message to an IncomingMessage.
func (h *webhookHTTPHandler) handleMessage(ctx context.Context, msg *tgmodels.Message) {
	log := logger.GetLogger(ctx).With("platform", "telegram", "fn", "webhookHTTPHandler.handleMessage")

	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	channelType := messenger.ChannelTypeChannel
	switch msg.Chat.Type {
	case "private":
		channelType = messenger.ChannelTypeDM
	case "group", "supergroup":
		channelType = messenger.ChannelTypeGroup
	}

	senderID := ""
	senderUsername := ""
	senderDisplay := ""
	if msg.From != nil {
		senderID = strconv.FormatInt(msg.From.ID, 10)
		senderUsername = msg.From.Username
		senderDisplay = msg.From.FirstName
		if msg.From.LastName != "" {
			senderDisplay += " " + msg.From.LastName
		}
	}

	threadID := ""
	if msg.MessageThreadID != 0 {
		threadID = strconv.Itoa(msg.MessageThreadID)
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	incoming := messenger.IncomingMessage{
		ID:       strconv.Itoa(msg.ID),
		Platform: messenger.PlatformTelegram,
		Channel: messenger.Channel{
			ID:   chatID,
			Name: msg.Chat.Title,
			Type: channelType,
		},
		Sender: messenger.Sender{
			ID:          senderID,
			Username:    senderUsername,
			DisplayName: senderDisplay,
		},
		Content: messenger.MessageContent{
			Text: text,
		},
		ThreadID:  threadID,
		Timestamp: time.Now(),
	}

	// Extract file attachments from the Telegram message.
	if msg.Document != nil {
		att := messenger.Attachment{
			Name:        msg.Document.FileName,
			ContentType: msg.Document.MimeType,
			Size:        int64(msg.Document.FileSize),
		}
		incoming.Content.Attachments = append(incoming.Content.Attachments, att)
	}

	for _, photo := range msg.Photo {
		att := messenger.Attachment{
			Name:        "photo",
			ContentType: "image/jpeg",
			Size:        int64(photo.FileSize),
		}
		incoming.Content.Attachments = append(incoming.Content.Attachments, att)
	}

	select {
	case h.incoming <- incoming:
	default:
		log.Warn("incoming message buffer full, dropping message",
			"chat_id", chatID, "user_id", senderID)
	}
}
