package messenger

import (
	"context"
	"fmt"
)

type contextKey struct {
	name string
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

// String returns the sender context format "platform:senderID:channelID"
// matching IncomingMessage.String() for backward compatibility with
// HITL DB storage and pending approval keys.
func (o *MessageOrigin) String() string {
	if o == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", o.Platform, o.Sender.ID, o.Channel.ID)
}

var messageOriginKey = &contextKey{name: "message_origin"}

// WithMessageOrigin returns a new context carrying the given MessageOrigin.
func WithMessageOrigin(ctx context.Context, origin *MessageOrigin) context.Context {
	return context.WithValue(ctx, messageOriginKey, origin)
}

// MessageOriginFrom returns the MessageOrigin from the context, or nil.
func MessageOriginFrom(ctx context.Context) *MessageOrigin {
	val, _ := ctx.Value(messageOriginKey).(*MessageOrigin)
	return val
}

// SenderContextFrom returns the sender context key string from the context.
// This is a convenience wrapper around MessageOriginFrom(ctx).String() for
// callsites that only need the string key (logging, DB storage, session IDs).
func SenderContextFrom(ctx context.Context) string {
	if origin := MessageOriginFrom(ctx); origin != nil {
		return origin.String()
	}
	return ""
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
