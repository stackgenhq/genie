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
	"time"

	"github.com/slack-go/slack/slackevents"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
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
}

// urlVerificationBody is the JSON payload Slack sends for URL verification.
type urlVerificationBody struct {
	Type      string `json:"type"`
	Token     string `json:"token"`
	Challenge string `json:"challenge"`
}

// ServeHTTP processes incoming Slack Events API requests.
//
// Flow:
//  1. Read and limit the request body (1 MB max).
//  2. Verify the signing secret if configured.
//  3. Handle URL verification challenges (Slack sends these during setup).
//  4. Parse the event and convert message events to IncomingMessage.
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
	defer r.Body.Close()

	// Verify signing secret if configured.
	if h.signingSecret != "" {
		if !h.verifySignature(r, body) {
			log.Warn("Slack signature verification failed")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Check if this is a URL verification challenge.
	var challenge urlVerificationBody
	if json.Unmarshal(body, &challenge) == nil && challenge.Type == "url_verification" {
		log.Info("Responding to Slack URL verification challenge")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, challenge.Challenge)
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
		case h.incoming <- msg:
		default:
			log.Warn("incoming message buffer full, dropping message",
				"channel", ev.Channel, "user", ev.User)
		}
	}
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
