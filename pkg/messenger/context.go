// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/logger"
)

type contextKey struct {
	name string
}

func SystemMessageOrigin() MessageOrigin {
	return MessageOrigin{
		Platform: Platform("system"),
		Channel: Channel{
			ID: "system",
		},
		Sender: Sender{
			ID: "system",
		},
	}
}

// MessageOrigin captures where an incoming message originated from.
// It replaces the string-based senderContext ("platform:senderID:channelID")
// with structured data, providing reply routing without string parsing.
type MessageOrigin struct {
	// Platform identifies which messaging platform (whatsapp, slack, etc).
	Platform Platform
	// Channel is the conversation where the message was posted.
	Channel Channel
	// Sender is the author of the message.
	Sender Sender
	// ThreadID is the thread/reply-chain ID (empty if top-level).
	ThreadID string
	// MessageID is the platform-assigned ID of the original incoming message.
	// Used to quote/reply-to the user's message in outgoing responses.
	MessageID string
}

// IsZero reports whether the origin is the zero value (no origin set).
func (o MessageOrigin) IsZero() bool {
	return o.Platform == "" && o.Sender.ID == "" && o.Channel.ID == ""
}

// IsSystem reports whether the origin is the system message origin.
func (o MessageOrigin) IsSystem() bool {
	return o.Platform == "system" && o.Sender.ID == "system" && o.Channel.ID == "system"
}

// String returns the sender context format "platform:senderID:channelID"
// matching IncomingMessage.String() for backward compatibility with
// HITL DB storage and pending approval keys.
func (o MessageOrigin) String() string {
	if o.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", o.Platform, o.Sender.ID, o.Channel.ID)
}

var messageOriginKey = &contextKey{name: "message_origin"}

// WithMessageOrigin returns a new context carrying the given MessageOrigin.
func WithMessageOrigin(ctx context.Context, origin MessageOrigin) context.Context {
	// Don't overwrite a real user origin — only allow overwrite when the
	// existing origin is absent (zero) or is the system placeholder.
	if existing, ok := ctx.Value(messageOriginKey).(MessageOrigin); ok && !existing.IsZero() && !existing.IsSystem() {
		return ctx
	}
	return context.WithValue(ctx, messageOriginKey, origin)
}

// MessageOriginFrom returns the MessageOrigin from the context.
// Every inbound request (AG-UI, messenger, webhook) should have an origin.
// A zero-value return indicates a bug in the request pipeline and is logged as a warning.
func MessageOriginFrom(ctx context.Context) MessageOrigin {
	val, ok := ctx.Value(messageOriginKey).(MessageOrigin)
	if !ok {
		logger.GetLogger(ctx).Debug("MessageOriginFrom: no MessageOrigin in context")
		return MessageOrigin{}
	}
	return val
}

var goalKey = &contextKey{name: "agent_goal"}

// WithGoal returns a new context carrying the agent's current goal string.
// The goal flows through to tools like send_message so that the reaction
// ledger can associate sent message IDs with the goal that produced them.
func WithGoal(ctx context.Context, goal string) context.Context {
	return context.WithValue(ctx, goalKey, goal)
}

// GoalFromContext returns the agent's current goal from the context, or
// an empty string if not set.
func GoalFromContext(ctx context.Context) string {
	val, _ := ctx.Value(goalKey).(string)
	return val
}
