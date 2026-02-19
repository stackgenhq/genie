// Package messenger provides a generic, platform-agnostic interface for
// bi-directional messaging across chat platforms (Slack, Telegram, Teams,
// Google Chat, Discord, etc.).
//
// This package is designed to be open-sourceable and has zero dependencies on
// genie, LLM, or agent-specific code. All types represent pure communication
// constructs.
//
// Platform adapters implement the Messenger interface and live in sub-packages
// (e.g., messenger/slack, messenger/telegram, messenger/teams).
package messenger

import (
	"context"
	"fmt"
	"time"
)

//go:generate go tool counterfeiter -generate

// Platform identifies a messaging platform.
type Platform string

const (
	// PlatformSlack represents the Slack messaging platform.
	PlatformSlack Platform = "slack"
	// PlatformDiscord represents the Discord messaging platform.
	PlatformDiscord Platform = "discord"
	// PlatformTelegram represents the Telegram messaging platform.
	PlatformTelegram Platform = "telegram"
	// PlatformTeams represents the Microsoft Teams messaging platform.
	PlatformTeams Platform = "teams"
	// PlatformGoogleChat represents the Google Chat messaging platform.
	PlatformGoogleChat Platform = "googlechat"
	// PlatformWhatsApp represents the WhatsApp Business messaging platform.
	PlatformWhatsApp Platform = "whatsapp"
)

// ChannelType classifies a conversation channel.
type ChannelType string

const (
	// ChannelTypeDM represents a direct message / private conversation.
	ChannelTypeDM ChannelType = "dm"
	// ChannelTypeGroup represents a group conversation (e.g., Slack group DM, Discord group).
	ChannelTypeGroup ChannelType = "group"
	// ChannelTypeChannel represents a public or private channel/guild.
	ChannelTypeChannel ChannelType = "channel"
)

// Channel identifies a conversation in a messaging platform.
type Channel struct {
	// ID is the platform-specific channel identifier.
	ID string
	// Name is the human-readable channel name (may be empty for DMs).
	Name string
	// Type classifies the channel (DM, group, or channel).
	Type ChannelType
}

// Sender represents the author of an incoming message.
type Sender struct {
	// ID is the platform-specific user identifier.
	ID string
	// Username is the unique handle (e.g., Slack member ID, Discord username).
	Username string
	// DisplayName is the user's friendly name (e.g. "John Doe").
	// Adapters should populate both if possible; otherwise DisplayName generally falls back to Username.
	DisplayName string
}

// Attachment represents a file or media attachment on a message.
type Attachment struct {
	// Name is the filename or label.
	Name string
	// URL is the download/access URL for the attachment.
	URL string
	// ContentType is the MIME type (e.g., "image/png", "application/pdf").
	ContentType string
	// Size is the file size in bytes (0 if unknown).
	Size int64
}

// MessageContent holds the body of a message.
type MessageContent struct {
	// Text is the plain-text or markdown message body.
	Text string
	// Attachments are optional file/media attachments.
	Attachments []Attachment
}

// SendRequest contains all parameters needed to send a message.
type SendRequest struct {
	// Channel is the target channel/conversation.
	Channel Channel
	// Content is the message body.
	Content MessageContent
	// ThreadID is an optional thread/reply-chain identifier for threaded replies.
	// Leave empty to post at top level.
	ThreadID string
	// Metadata holds platform-specific key-value pairs that adapters can use
	// for features not covered by the common interface (e.g., Slack blocks,
	// Discord embeds).
	Metadata map[string]any
}

// SendResponse is returned after a message is successfully sent.
type SendResponse struct {
	// MessageID is the platform-assigned identifier for the sent message.
	MessageID string
	// Timestamp is when the platform recorded the message.
	Timestamp time.Time
}

// IncomingMessage represents a message received from a platform.
type IncomingMessage struct {
	// ID is the platform-assigned message identifier.
	ID string
	// Platform identifies which platform the message came from.
	Platform Platform
	// Channel is the conversation where the message was posted.
	Channel Channel
	// Sender is the author of the message.
	Sender Sender
	// Content is the message body and any attachments.
	Content MessageContent
	// ThreadID is the thread/reply-chain ID (empty if top-level).
	ThreadID string
	// Timestamp is when the message was sent.
	Timestamp time.Time
	// Metadata holds platform-specific data not captured by common fields.
	Metadata map[string]any
}

func (msg IncomingMessage) String() string {
	return fmt.Sprintf("%s:%s:%s", msg.Platform, msg.Sender.ID, msg.Channel.ID)

}

// Messenger is the core interface for bi-directional communication with a
// messaging platform. Platform adapters (Slack, Telegram, Teams, Google Chat)
// implement this interface.
//
//counterfeiter:generate . Messenger
type Messenger interface {
	// Connect establishes a connection to the messaging platform.
	// It must be called before Send or Receive. Calling Connect on an
	// already-connected Messenger returns ErrAlreadyConnected.
	Connect(ctx context.Context) error

	// Disconnect gracefully shuts down the platform connection.
	// After Disconnect, the Receive channel will be closed and further
	// Send calls will return ErrNotConnected.
	Disconnect(ctx context.Context) error

	// Send delivers a message to a specific channel/conversation.
	// Returns ErrNotConnected if Connect has not been called.
	Send(ctx context.Context, req SendRequest) (SendResponse, error)

	// Receive returns a read-only channel that delivers incoming messages.
	// The channel is closed when the context is cancelled or Disconnect is called.
	// Returns ErrNotConnected if Connect has not been called.
	Receive(ctx context.Context) (<-chan IncomingMessage, error)

	// Platform returns the platform identifier for this adapter.
	Platform() Platform

	// FormatApproval enriches a SendRequest with platform-specific rich
	// formatting (e.g. Slack Block Kit, Google Chat Cards v2, Teams Adaptive
	// Cards) for the given approval. Adapters that do not support rich
	// formatting should return the request unchanged.
	FormatApproval(req SendRequest, info ApprovalInfo) SendRequest

	// FormatClarification enriches a SendRequest with platform-specific rich
	// formatting for a clarifying question posed by the agent.
	// Adapters that do not support rich formatting should return the request unchanged.
	FormatClarification(req SendRequest, info ClarificationInfo) SendRequest
}

// ApprovalInfo carries the data needed by adapters to render a rich approval
// notification. Defined here (not in the hitl package) to avoid a circular
// dependency between messenger and hitl.
type ApprovalInfo struct {
	// ID is the unique approval identifier.
	ID string
	// ToolName is the tool that requires approval.
	ToolName string
	// Args is the pretty-printed JSON arguments.
	Args string
	// Feedback is the optional justification / reason for the call.
	Feedback string
}

// ClarificationInfo carries data for rendering a clarifying-question
// notification on a chat platform.
type ClarificationInfo struct {
	// RequestID is the unique clarification request identifier.
	RequestID string
	// Question is the question posed by the agent.
	Question string
	// Context is optional context explaining why the agent needs this info.
	Context string
}
