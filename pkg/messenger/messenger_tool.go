// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const ToolName = "send_message"

// sendMessageArgs is the JSON schema input for the send_message tool.
type sendMessageArgs struct {
	Type      string `json:"type,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Emoji     string `json:"emoji,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// messageTool wraps a Messenger as a tool.CallableTool so the ReAcTree agent
// can send messages to configured platforms.
// MessageLedger records sent message IDs so that future reactions can be
// correlated back to the agent context that produced them. This interface
// lives here (not in reactree/memory) to avoid an import cycle between
// the messenger and reactree/memory packages.
//
//counterfeiter:generate . MessageLedger
type MessageLedger interface {
	// Record associates a sent message ID with its agent context.
	Record(ctx context.Context, messageID string, goal, output, senderKey string)
}

type messageTool struct {
	messenger Messenger

	// ledger records sent message IDs → agent context for reaction correlation.
	// Optional; nil means no reaction tracking.
	ledger MessageLedger

	// cooldownMu guards lastSent and lastContentSent.
	cooldownMu sync.Mutex
	// lastSent tracks the last send timestamp per channel to prevent
	// sub-agents from flooding the user with progress messages.
	lastSent map[string]time.Time
	// lastContentSent tracks the last content-delivery timestamp per channel.
	// After any content message is sent, ALL messages (informational or content)
	// are suppressed for ContentCooldown. This prevents sub-agents with
	// send_message from generating N copies of a meal plan/recipe.
	lastContentSent map[string]time.Time
}

// MessageCooldown is the minimum interval between successive *informational*
// (short progress/status) send_message calls to the same channel.
const MessageCooldown = 15 * time.Second

// ContentCooldown is the minimum interval between successive *content-rich*
// (meal plan, recipe, long response) deliveries to the same channel.
// After a content message is delivered, ALL subsequent messages (informational
// or content) to that channel are suppressed for this window. This prevents
// sub-agents from sending N copies of the same result across N LLM iterations.
const ContentCooldown = 2 * time.Minute

// NewSendMessageTool creates a tool.Tool that wraps a Messenger for sending messages.
// The tool exposes a "send_message" action that the agent can invoke.
func NewSendMessageTool(m Messenger, opts ...SendMessageToolOption) tool.Tool {
	t := &messageTool{
		messenger:       m,
		lastSent:        make(map[string]time.Time),
		lastContentSent: make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// SendMessageToolOption configures optional behaviour for the send_message tool.
type SendMessageToolOption func(*messageTool)

// WithReactionLedger attaches a MessageLedger so that every successfully
// sent content message is recorded for later reaction-based learning.
func WithReactionLedger(l MessageLedger) SendMessageToolOption {
	return func(t *messageTool) {
		t.ledger = l
	}
}

// Declaration returns the tool metadata.
func (t *messageTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        ToolName,
		Description: fmt.Sprintf("Send a message or react to a message on %s. Use type=\"message\" (default) to send text, or type=\"reaction\" to react with an emoji. If channel_id is omitted, the message is sent to the originating channel.", t.messenger.Platform()),
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"type": {
					Type:        "string",
					Description: "The action type: \"message\" (default) to send text, or \"reaction\" to react with an emoji to an existing message.",
					Enum:        []any{"message", "reaction"},
				},
				"channel_id": {
					Type:        "string",
					Description: "Optional. The channel or conversation ID to send the message to. If omitted, replies to the originating channel.",
				},
				"text": {
					Type:        "string",
					Description: "The message text to send. Required for type=\"message\".",
				},
				"emoji": {
					Type:        "string",
					Description: "The reaction emoji (e.g. \"👍\", \"❤️\"). Required for type=\"reaction\".",
				},
				"message_id": {
					Type:        "string",
					Description: "The ID of the message to react to. Required for type=\"reaction\". If omitted for reactions, reacts to the originating message.",
				},
				"thread_id": {
					Type:        "string",
					Description: "Optional thread ID for threaded replies. Leave empty for top-level messages.",
				},
			},
			Required: []string{},
		},
	}
}

// Call sends a message or reaction using the underlying Messenger.
func (t *messageTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args sendMessageArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid send_message arguments: %w", err)
	}

	// Handle reaction type.
	if args.Type == "reaction" {
		if args.Emoji == "" {
			return nil, fmt.Errorf("emoji is required for type=\"reaction\"")
		}
		channelID := args.ChannelID
		messageID := args.MessageID
		if origin := MessageOriginFrom(ctx); !origin.IsZero() {
			if channelID == "" {
				channelID = origin.Channel.ID
			}
			if messageID == "" {
				messageID = origin.MessageID
			}
		}
		if channelID == "" {
			return nil, fmt.Errorf("channel_id is required (no originating channel context available)")
		}
		if messageID == "" {
			return nil, fmt.Errorf("message_id is required for reactions (no originating message context available)")
		}
		resp, err := t.messenger.Send(ctx, SendRequest{
			Type:             SendTypeReaction,
			Channel:          Channel{ID: channelID},
			ReplyToMessageID: messageID,
			Emoji:            args.Emoji,
		})
		if err != nil {
			return nil, err
		}
		return map[string]string{
			"message_id": resp.MessageID,
			"status":     "reacted",
		}, nil
	}

	// Default: send a text message.
	if args.Text == "" {
		return nil, fmt.Errorf("text is required")
	}

	// Resolve channel, thread, and reply-to from MessageOrigin when not explicitly provided.
	channelID := args.ChannelID
	threadID := args.ThreadID
	var replyToMessageID string
	if origin := MessageOriginFrom(ctx); !origin.IsZero() {
		if channelID == "" {
			channelID = origin.Channel.ID
		}
		if threadID == "" {
			threadID = origin.ThreadID
		}
		// Always reply-to the original user message for context.
		replyToMessageID = origin.MessageID
	}

	if channelID == "" {
		return nil, fmt.Errorf("channel_id is required (no originating channel context available)")
	}

	// Check content-level cooldown FIRST (overrides all other logic).
	// After any content-rich message is delivered, suppress ALL subsequent
	// messages for ContentCooldown — even short progress updates — to prevent
	// chatty sub-agents from peppering the user.
	t.cooldownMu.Lock()
	if lastContent, ok := t.lastContentSent[channelID]; ok && time.Since(lastContent) < ContentCooldown {
		t.cooldownMu.Unlock()
		logger.GetLogger(ctx).Info("send_message suppressed (content cooldown active)",
			"channelID", channelID,
			"text", truncate(args.Text, 60),
			"cooldownRemaining", ContentCooldown-time.Since(lastContent),
		)
		return map[string]string{
			"message_id": "done",
			"status":     "done",
		}, nil
	}
	t.cooldownMu.Unlock()

	// Classify message: only rate-limit short informational/progress messages.
	// Urgent or content-rich messages always go through.
	if isInformational(args.Text) {
		t.cooldownMu.Lock()
		if last, ok := t.lastSent[channelID]; ok && time.Since(last) < MessageCooldown {
			t.cooldownMu.Unlock()
			logger.GetLogger(ctx).Info("send_message suppressed (informational cooldown)",
				"channelID", channelID,
				"text", truncate(args.Text, 60),
				"cooldownRemaining", MessageCooldown-time.Since(last),
			)
			return map[string]string{
				"message_id": "done",
				"status":     "done",
			}, nil
		}
		t.lastSent[channelID] = time.Now()
		t.cooldownMu.Unlock()
	}

	resp, err := t.messenger.Send(ctx, SendRequest{
		Channel: Channel{
			ID: channelID,
		},
		Content: MessageContent{
			Text: args.Text,
		},
		ThreadID:         threadID,
		ReplyToMessageID: replyToMessageID,
	})
	if err != nil {
		return nil, err
	}

	// If this was a content-rich message, set the content cooldown so the next
	// N minutes of messages are suppressed.
	if !isInformational(args.Text) {
		t.cooldownMu.Lock()
		t.lastContentSent[channelID] = time.Now()
		t.cooldownMu.Unlock()
		logger.GetLogger(ctx).Info("send_message content delivered, content cooldown started",
			"channelID", channelID,
			"contentCooldown", ContentCooldown,
		)
	}

	// Record the sent message in the reaction ledger so that future
	// emoji reactions (👍/👎) can be correlated to this goal/output.
	if t.ledger != nil && !isInformational(args.Text) {
		goalCtx := GoalFromContext(ctx)
		senderKey := ""
		if origin := MessageOriginFrom(ctx); !origin.IsZero() {
			senderKey = origin.String()
		}
		t.ledger.Record(ctx, resp.MessageID, goalCtx, args.Text, senderKey)
	}

	return map[string]string{
		"message_id": resp.MessageID,
		"status":     "sent",
	}, nil
}

// truncate returns at most n runes of s, appending "…" if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// isInformational returns true if the message is a short progress/status
// update that can be rate-limited. Content-rich messages (results, recipes,
// errors, lists) are classified as urgent and always delivered.
func isInformational(text string) bool {
	// Long messages are content delivery — always urgent.
	if len([]rune(text)) > 280 {
		return false
	}
	// Messages with structure (newlines, bullets, numbered lists) are content.
	if strings.Contains(text, "\n") {
		return false
	}
	// Messages with markdown formatting are likely results.
	if strings.Contains(text, "**") || strings.Contains(text, "##") {
		return false
	}
	// Error/warning indicators → urgent.
	if strings.Contains(text, "⚠️") || strings.Contains(text, "❌") || strings.Contains(text, "Error") {
		return false
	}
	// Short, single-line messages are likely progress updates.
	return true
}

// Compile-time interface compliance check.
var _ tool.CallableTool = (*messageTool)(nil)
