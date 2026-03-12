// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package slack provides a Messenger adapter for the Slack platform.
//
// This file implements the HTTP Events API handler (eventsHTTPHandler),
// enabling Slack to receive events via HTTP push instead of Socket Mode.
// This allows the adapter to be mounted on a shared HTTP mux at a
// context path (e.g., /agents/{name}/slack/events) rather than
// requiring its own WebSocket connection or dedicated port.
//
// The Events API is Slack's recommended approach for production apps
// that have a publicly accessible endpoint. It delivers events as
// signed HTTP POST requests. See:
// https://api.slack.com/events-api
package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack/slackevents"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
)

// eventsHTTPHandler handles incoming Slack Events API HTTP requests.
// It supports:
//   - URL verification challenge (Slack sends this when configuring the endpoint)
//   - Event callback processing (message events → IncomingMessage channel)
//   - Signing secret verification for request authenticity
//
// This handler is used when the Slack adapter runs in HTTP Events API
// mode (SigningSecret is set). The handler can be mounted on any
// http.ServeMux or router.
type eventsHTTPHandler struct {
	signingSecret string
	incoming      chan messenger.IncomingMessage

	// Filtering fields — shared with the parent Messenger.
	botUserID        string
	respondTo        string
	allowedUsers     []string
	mentionedThreads *sync.Map
}

// urlVerificationBody is the JSON payload Slack sends for URL verification.
type urlVerificationBody struct {
	Type      string `json:"type"`
	Token     string `json:"token"`
	Challenge string `json:"challenge"`
}

// ServeHTTP processes incoming Slack Events API requests and interactive
// payloads (block_actions from button clicks).
//
// Slack sends two types of HTTP requests to this endpoint:
//   - Events API: application/json with event callbacks and URL verification
//   - Interactive: application/x-www-form-urlencoded with a JSON "payload" field
//
// Flow:
//  1. Read and limit the request body (1 MB max).
//  2. Verify the signing secret if configured.
//  3. Route based on content type: form-urlencoded → interactive, JSON → events.
//  4. Handle URL verification challenges (Slack sends these during setup).
//  5. Parse the event and convert to IncomingMessage.
func (h *eventsHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "eventsHTTPHandler.ServeHTTP")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		log.Error("failed to read request body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Verify signing secret if configured.
	if h.signingSecret != "" {
		if !h.verifySignature(r, body) {
			log.Warn("Slack signature verification failed")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Slack sends interactive payloads (button clicks) as form-urlencoded
	// with a JSON "payload" field. Detect and route accordingly.
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		w.WriteHeader(http.StatusOK)
		h.handleInteractiveHTTP(ctx, body)
		return
	}

	// Check if this is a URL verification challenge.
	var challenge urlVerificationBody
	if json.Unmarshal(body, &challenge) == nil && challenge.Type == "url_verification" {
		log.Info("Responding to Slack URL verification challenge")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, challenge.Challenge)
		return
	}

	// Parse the Events API event.
	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		log.Error("failed to parse Slack event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Acknowledge receipt immediately (Slack expects 200 within 3 seconds).
	w.WriteHeader(http.StatusOK)

	// Process the event asynchronously.
	if event.Type == slackevents.CallbackEvent {
		h.handleCallback(ctx, event)
	}
}

// shouldProcess determines whether a message should be forwarded based on
// mention-only mode and allowed-user filtering. Uses the same algorithm
// as Messenger.shouldProcess.
func (h *eventsHTTPHandler) shouldProcess(channelID, userID, text, threadTS string) bool {
	// Allowed-user check.
	if len(h.allowedUsers) > 0 {
		allowed := false
		for _, u := range h.allowedUsers {
			if u == userID {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	if h.respondTo == respondToAll {
		return true
	}

	// DMs always pass.
	if strings.HasPrefix(channelID, "D") {
		return true
	}

	// Explicit @mention.
	if h.botUserID != "" && strings.Contains(text, "<@"+h.botUserID+">") {
		if threadTS != "" && h.mentionedThreads != nil {
			h.mentionedThreads.Store(channelID+":"+threadTS, true)
		}
		return true
	}

	// Thread where bot was previously mentioned.
	if threadTS != "" && h.mentionedThreads != nil {
		if _, ok := h.mentionedThreads.Load(channelID + ":" + threadTS); ok {
			return true
		}
	}

	return false
}

// stripBotMention removes the bot's <@BOT_USER_ID> mention from message text.
func (h *eventsHTTPHandler) stripBotMention(text string) string {
	if h.botUserID == "" {
		return text
	}
	cleaned := strings.ReplaceAll(text, "<@"+h.botUserID+">", "")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimLeft(cleaned, ":")
	return strings.TrimSpace(cleaned)
}

// handleCallback processes a callback event from the Slack Events API.
func (h *eventsHTTPHandler) handleCallback(ctx context.Context, event slackevents.EventsAPIEvent) {
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "eventsHTTPHandler.handleCallback")

	innerEvent := event.InnerEvent
	switch ev := innerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		// Skip bot messages to avoid echo loops.
		if ev.BotID != "" {
			return
		}

		// Apply mention-only and allowed-user filtering.
		if !h.shouldProcess(ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp) {
			return
		}

		// Strip the bot mention from text so the LLM gets clean input.
		cleanText := h.stripBotMention(ev.Text)

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
				Text: cleanText,
			},
			ThreadID:  ev.ThreadTimeStamp,
			Timestamp: time.Now(),
		}
		// When the user replies in a thread, thread_ts is the parent message's ts.
		// Set quoted_message_id so HITL can resolve the specific approval they replied to.
		if ev.ThreadTimeStamp != "" {
			msg.Metadata = map[string]any{messenger.QuotedMessageID: ev.Channel + ":" + ev.ThreadTimeStamp}
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
		case h.incoming <- msg:
		default:
			log.Warn("incoming message buffer full, dropping message",
				"channel", ev.Channel, "user", ev.User)
		}
	}
}

// handleInteractiveHTTP processes an interactive payload delivered via HTTP
// (e.g. a button click from a FormatApproval message when using the Events
// API instead of Socket Mode).
//
// Slack sends interactive payloads as application/x-www-form-urlencoded with
// a single "payload" field containing JSON. The JSON is a slack.InteractionCallback.
func (h *eventsHTTPHandler) handleInteractiveHTTP(ctx context.Context, body []byte) {
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "eventsHTTPHandler.handleInteractiveHTTP")

	// Extract the "payload" field from the form-urlencoded body.
	// The body is "payload=<url-encoded-json>".
	bodyStr := string(body)
	const prefix = "payload="
	if !strings.HasPrefix(bodyStr, prefix) {
		log.Warn("interactive payload missing 'payload=' prefix")
		return
	}

	// The payload is URL-encoded JSON. We need to parse it.
	// Note: net/url.QueryUnescape handles the URL decoding.
	payloadJSON := bodyStr[len(prefix):]
	// URL-decode the payload.
	decoded, err := urlDecode(payloadJSON)
	if err != nil {
		log.Warn("failed to URL-decode interactive payload", "error", err)
		return
	}

	var callback interactionPayload
	if err := json.Unmarshal([]byte(decoded), &callback); err != nil {
		log.Warn("failed to parse interactive payload JSON", "error", err)
		return
	}

	if callback.Type != "block_actions" {
		log.Debug("ignoring non-block_actions interactive payload", "type", callback.Type)
		return
	}

	for _, action := range callback.Actions {
		msg := messenger.IncomingMessage{
			ID:       callback.Message.Ts,
			Platform: messenger.PlatformSlack,
			Type:     messenger.MessageTypeInteraction,
			Channel: messenger.Channel{
				ID:   callback.Channel.ID,
				Name: callback.Channel.Name,
				Type: messenger.ChannelTypeChannel,
			},
			Sender: messenger.Sender{
				ID:          callback.User.ID,
				Username:    callback.User.Name,
				DisplayName: callback.User.Name,
			},
			Timestamp: time.Now(),
			Interaction: &messenger.InteractionData{
				ActionID:    action.ActionID,
				ActionValue: action.Value,
				BlockID:     action.BlockID,
				ActionType:  action.Type,
				ResponseURL: callback.ResponseURL,
			},
		}

		select {
		case h.incoming <- msg:
		default:
			log.Warn("incoming message buffer full, dropping interaction",
				"channel", callback.Channel.ID, "action", action.ActionID)
		}
	}
}

// interactionPayload is a minimal representation of Slack's InteractionCallback
// for parsing block_actions via the HTTP Events API path. We use a custom type
// instead of slack.InteractionCallback to avoid pulling in the full slack
// package as a dependency of the events HTTP handler.
type interactionPayload struct {
	Type        string `json:"type"`
	ResponseURL string `json:"response_url"`
	User        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	Channel struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
	Message struct {
		Ts string `json:"ts"`
	} `json:"message"`
	Actions []struct {
		ActionID string `json:"action_id"`
		BlockID  string `json:"block_id"`
		Value    string `json:"value"`
		Type     string `json:"type"`
	} `json:"actions"`
}

// urlDecode performs URL percent-decoding. Handles + as space.
func urlDecode(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '+':
			b.WriteByte(' ')
		case '%':
			if i+2 >= len(s) {
				return "", fmt.Errorf("incomplete percent-encoding at position %d", i)
			}
			high, lok := unhex(s[i+1])
			low, rok := unhex(s[i+2])
			if !lok || !rok {
				return "", fmt.Errorf("invalid percent-encoding at position %d", i)
			}
			b.WriteByte(high<<4 | low)
			i += 2
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

// unhex converts a hex character to its numeric value.
func unhex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// verifySignature validates the Slack signing secret against the request.
// See: https://api.slack.com/authentication/verifying-requests-from-slack
func (h *eventsHTTPHandler) verifySignature(r *http.Request, body []byte) bool {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	signature := r.Header.Get("X-Slack-Signature")

	if timestamp == "" || signature == "" {
		return false
	}

	// Build the base string: v0:timestamp:body
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	return strings.EqualFold(expected, signature)
}
