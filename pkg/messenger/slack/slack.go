// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

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
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformSlack, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		// Extract allowed_sender_* entries from params.
		var allowedUsers []string
		for i := 0; ; i++ {
			key := fmt.Sprintf("allowed_sender_%d", i)
			val, ok := params[key]
			if !ok {
				break
			}
			allowedUsers = append(allowedUsers, val)
		}
		return New(Config{
			AppToken: params["app_token"],
			BotToken: params["bot_token"],
		}, params["respond_to"], allowedUsers, opts...), nil
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

// respondToAll is the value for respondTo that disables mention filtering.
// When respondTo is empty or any other value, mention-only mode is active (the default).
const respondToAll = "all"

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

	// respondTo controls message filtering: "all" processes everything,
	// "" or "mentions" (default) only processes messages where the bot
	// is @mentioned or messages in threads where the bot was previously mentioned.
	respondTo string
	// allowedUsers is an optional allowlist of Slack user IDs. When non-empty,
	// only messages from these users are processed (even if mentioned).
	allowedUsers []string
	// botUserID is the Slack user ID of the bot, resolved via auth.test at
	// connect time. Used to detect <@BOT_USER_ID> mentions in message text.
	botUserID string
	// mentionedThreads tracks threads where the bot was @mentioned.
	// Key format: "channelID:threadTS". Subsequent messages in these threads
	// are processed without requiring another @mention.
	mentionedThreads sync.Map
	// userInfoCache caches resolved Slack user info (email, display name)
	// keyed by Slack user ID. Avoids repeated users.info API calls.
	userInfoCache sync.Map
}

// New creates a new Slack Messenger with the given config and options.
func New(cfg Config, respondTo string, allowedUsers []string, opts ...messenger.Option) *Messenger {
	adapterCfg := messenger.ApplyOptions(opts...)
	return &Messenger{
		cfg:          cfg,
		adapterCfg:   adapterCfg,
		respondTo:    respondTo,
		allowedUsers: allowedUsers,
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

	// Resolve the bot's own Slack user ID so we can detect @mentions.
	// auth.test returns the bot user associated with the token.
	authResp, err := m.api.AuthTestContext(ctx)
	if err != nil {
		log.Warn("failed to resolve bot user ID via auth.test — mention filtering may not work", "error", err)
	} else {
		m.botUserID = authResp.UserID
		log.Info("resolved bot user ID", "botUserID", m.botUserID)
	}

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	connCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.connCtx = connCtx

	if m.useHTTPEventsAPI() {
		// HTTP Events API mode: return handler for caller to mount.
		m.eventsHandler = &eventsHTTPHandler{
			signingSecret:    m.cfg.SigningSecret,
			incoming:         m.incoming,
			botUserID:        m.botUserID,
			respondTo:        m.respondTo,
			allowedUsers:     m.allowedUsers,
			mentionedThreads: &m.mentionedThreads,
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
// If req.Type is SendTypeReaction, adds an emoji reaction to the message identified by ReplyToMessageID (format "channelID:ts").
// If req.Metadata["blocks"] contains a []slack.Block, the message is sent with Block Kit formatting.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	if req.Type == messenger.SendTypeReaction && req.ReplyToMessageID != "" && req.Emoji != "" {
		channelID, ts, ok := parseSlackMessageID(req.ReplyToMessageID)
		if !ok {
			return messenger.SendResponse{}, fmt.Errorf("%w: invalid message ID for reaction (expected channelID:ts)", messenger.ErrSendFailed)
		}
		name := slackEmojiName(req.Emoji)
		if name == "" {
			return messenger.SendResponse{}, fmt.Errorf("%w: unsupported reaction emoji %q", messenger.ErrSendFailed, req.Emoji)
		}
		item := slack.NewRefToMessage(channelID, ts)
		if err := m.api.AddReactionContext(ctx, name, item); err != nil {
			return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
		}
		return messenger.SendResponse{MessageID: req.ReplyToMessageID, Timestamp: time.Now()}, nil
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

// parseSlackMessageID splits a Slack message ID ("channelID:ts") into channel and timestamp.
// Returns (channelID, ts, true) on success, or ("", "", false) if the format is invalid.
func parseSlackMessageID(messageID string) (channelID, ts string, ok bool) {
	idx := strings.Index(messageID, ":")
	if idx <= 0 || idx >= len(messageID)-1 {
		return "", "", false
	}
	return messageID[:idx], messageID[idx+1:], true
}

// slackEmojiName maps Unicode emoji to Slack reaction names (e.g. "white_check_mark").
func slackEmojiName(emoji string) string {
	switch emoji {
	case "✅":
		return "white_check_mark"
	case "👎":
		return "thumbsdown"
	case "👍":
		return "thumbsup"
	default:
		return ""
	}
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
	case socketmode.EventTypeInteractive:
		m.socket.Ack(*evt.Request)
		m.handleInteractive(ctx, evt)
	}
}

// handleInteractive processes Socket Mode interactive payloads (block_actions)
// and converts them into IncomingMessage with Type == MessageTypeInteraction.
// This allows the app layer to route button clicks (e.g. approval Approve/Reject)
// through the same Receive channel as text messages.
//
// Without this handler, interactive buttons rendered by FormatApproval are
// purely decorative — clicks are silently discarded by Slack because no
// handler acknowledges them.
func (m *Messenger) handleInteractive(ctx context.Context, evt socketmode.Event) {
	log := logger.GetLogger(ctx).With("platform", "slack", "fn", "handleInteractive")

	// The interactive payload is stored as a json.RawMessage in the event data.
	raw, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		log.Warn("unexpected interactive payload type", "type", fmt.Sprintf("%T", evt.Data))
		return
	}

	if raw.Type != slack.InteractionTypeBlockActions {
		log.Debug("ignoring non-block_actions interaction", "type", raw.Type)
		return
	}

	for _, action := range raw.ActionCallback.BlockActions {
		msg := messenger.IncomingMessage{
			ID:       raw.MessageTs,
			Platform: messenger.PlatformSlack,
			Type:     messenger.MessageTypeInteraction,
			Channel: messenger.Channel{
				ID:   raw.Channel.ID,
				Name: raw.Channel.Name,
				Type: messenger.ChannelTypeChannel,
			},
			Sender: messenger.Sender{
				ID:          raw.User.ID,
				Username:    raw.User.Name,
				DisplayName: raw.User.Name,
			},
			Timestamp: time.Now(),
			Interaction: &messenger.InteractionData{
				ActionID:    action.ActionID,
				ActionValue: action.Value,
				BlockID:     action.BlockID,
				ActionType:  string(action.Type),
				ResponseURL: raw.ResponseURL,
			},
		}

		select {
		case m.incoming <- msg:
		default:
			log.Warn("incoming message buffer full, dropping interaction",
				"channel", raw.Channel.ID, "action", action.ActionID)
		}
	}
}

// shouldProcess determines whether an incoming message should be forwarded to
// the app layer. It implements mention-only filtering and allowed-user checks.
//
// Rules:
//   - respondTo == "" or "all": always process (backwards compatible)
//   - respondTo == "mentions":
//     1. DMs (channel type "D" prefix) are always processed
//     2. Messages containing <@botUserID> are processed; the thread is tracked
//     3. Messages in a previously-mentioned thread are processed
//     4. All other messages are silently dropped
//   - If allowedUsers is non-empty, sender must be in the list
func (m *Messenger) shouldProcess(channelID, userID, text, threadTS string) bool {
	// Allowed-user check (independent of mention mode).
	if len(m.allowedUsers) > 0 {
		allowed := false
		for _, u := range m.allowedUsers {
			if u == userID {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// respondTo == "all" disables mention filtering.
	if m.respondTo == respondToAll {
		return true
	}

	// DMs always pass (channel IDs starting with "D" are direct messages).
	if strings.HasPrefix(channelID, "D") {
		return true
	}

	// Check for explicit @mention.
	if m.containsBotMention(text) {
		// Track this thread so subsequent messages are processed too.
		if threadTS != "" {
			m.mentionedThreads.Store(channelID+":"+threadTS, true)
		}
		return true
	}

	// Check if this message is in a thread where the bot was previously mentioned.
	if threadTS != "" {
		if _, ok := m.mentionedThreads.Load(channelID + ":" + threadTS); ok {
			return true
		}
	}

	return false
}

// containsBotMention returns true if the message text contains a Slack
// user mention for this bot (e.g., <@U12345>).
func (m *Messenger) containsBotMention(text string) bool {
	if m.botUserID == "" {
		return false
	}
	return strings.Contains(text, "<@"+m.botUserID+">")
}

// stripBotMention removes the bot's <@BOT_USER_ID> mention from message text
// so the LLM receives clean input without raw Slack mention syntax.
func (m *Messenger) stripBotMention(text string) string {
	if m.botUserID == "" {
		return text
	}
	cleaned := strings.ReplaceAll(text, "<@"+m.botUserID+">", "")
	// Trim leading/trailing whitespace and any leftover colon+space.
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimLeft(cleaned, ":")
	return strings.TrimSpace(cleaned)
}

// cachedUserInfo holds resolved Slack user metadata.
type cachedUserInfo struct {
	email       string
	displayName string
}

// resolveUserInfo looks up a Slack user's email and display name via the
// users.info API, caching the result so each user is only looked up once.
// Returns (email, displayName) — email falls back to userID, displayName
// falls back to userID.
func (m *Messenger) resolveUserInfo(ctx context.Context, userID string) (string, string) {
	// Check cache first.
	if cached, ok := m.userInfoCache.Load(userID); ok {
		info := cached.(cachedUserInfo)
		return info.email, info.displayName
	}

	if m.api == nil {
		return userID, userID
	}

	user, err := m.api.GetUserInfoContext(ctx, userID)
	if err != nil {
		logger.GetLogger(ctx).Debug("failed to resolve Slack user info", "userID", userID, "error", err)
		return userID, userID
	}

	email := user.Profile.Email
	if email == "" {
		email = userID // fallback if email is not available
	}

	displayName := user.Profile.DisplayName
	if displayName == "" {
		displayName = user.RealName
	}
	if displayName == "" {
		displayName = userID
	}

	// Cache the result.
	m.userInfoCache.Store(userID, cachedUserInfo{email: email, displayName: displayName})

	logger.GetLogger(ctx).Info("resolved Slack user info",
		"slackUID", userID, "email", email, "displayName", displayName)

	return email, displayName
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

			// Apply mention-only and allowed-user filtering.
			if !m.shouldProcess(ev.Channel, ev.User, ev.Text, ev.ThreadTimeStamp) {
				return
			}

			// Strip the bot mention from text so the LLM gets clean input.
			cleanText := m.stripBotMention(ev.Text)

			// Resolve sender email and display name (cached per user).
			email, displayName := m.resolveUserInfo(ctx, ev.User)

			msg := messenger.IncomingMessage{
				ID:       ev.TimeStamp,
				Platform: messenger.PlatformSlack,
				Channel: messenger.Channel{
					ID:   ev.Channel,
					Type: messenger.ChannelTypeChannel,
				},
				Sender: messenger.Sender{
					ID:          email,
					Username:    ev.User,
					DisplayName: displayName,
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
				"text": "You can also reply *Yes* to approve, *No* to reject, or type feedback to revisit. React with 👍 to approve or 👎 to reject.",
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

// UpdateMessage replaces the content of a previously sent Slack message
// using the chat.update API. Used to disarm approval/clarification buttons
// after resolution (e.g. replacing buttons with "✅ Approved by @user").
//
// The MessageID must be in the "channelID:timestamp" format returned by Send.
// If Metadata["blocks"] is set, the blocks are used for the update.
func (m *Messenger) UpdateMessage(ctx context.Context, req messenger.UpdateRequest) error {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.ErrNotConnected
	}

	// Parse the composite message ID (channelID:timestamp from Send).
	// Fall back to using req.Channel.ID + req.MessageID as separate parts.
	channelID := req.Channel.ID
	ts := req.MessageID
	if parts := splitMessageID(ts); len(parts) == 2 {
		channelID = parts[0]
		ts = parts[1]
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(req.Content.Text, false),
	}

	if blocks := extractBlocks(req.Metadata); len(blocks) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(blocks...))
	}

	_, _, _, err := m.api.UpdateMessageContext(ctx, channelID, ts, opts...)
	if err != nil {
		return fmt.Errorf("slack update message: %w", err)
	}
	return nil
}

// splitMessageID splits a composite "channelID:timestamp" message ID.
// Returns nil if the ID doesn't contain a colon separator.
func splitMessageID(id string) []string {
	for i := range id {
		if id[i] == ':' {
			return []string{id[:i], id[i+1:]}
		}
	}
	return nil
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
