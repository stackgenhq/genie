// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

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
	"net/http"
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
	// PlatformAGUI represents the AG-UI SSE server (in-process adapter).
	PlatformAGUI Platform = "agui"
)

// String returns a human-friendly display name for the platform.
func (p Platform) String() string {
	return string(p)
}

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
	Name string `json:"name"`
	// URL is the download/access URL for the attachment.
	URL string `json:"url"`
	// ContentType is the MIME type (e.g., "image/png", "application/pdf").
	ContentType string `json:"content_type"`
	// Size is the file size in bytes (0 if unknown).
	Size int64 `json:"size"`
	// LocalPath is the path to the downloaded file on disk. Populated when
	// the adapter downloads the attachment (e.g., WhatsApp encrypted media).
	// Empty if only metadata is available (e.g., Slack URLs that require auth).
	LocalPath string `json:"local_path"`
}

// MessageContent holds the body of a message.
type MessageContent struct {
	// Text is the plain-text or markdown message body.
	Text string
	// Attachments are optional file/media attachments.
	Attachments []Attachment
}

// MessageType distinguishes incoming message kinds on the Receive channel.
type MessageType string

const (
	// MessageTypeDefault is a normal text/media message.
	MessageTypeDefault MessageType = ""
	// MessageTypeReaction is an emoji reaction to an existing message.
	// When Type == MessageTypeReaction, ReactionEmoji and ReactedMessageID
	// are populated. This allows the system to use reactions as human
	// feedback signals for episodic memory (e.g. 👍 = positive, 👎 = negative).
	MessageTypeReaction MessageType = "reaction"
	// MessageTypeInteraction is a structured action from an interactive UI
	// element (e.g. Slack Block Kit button click, Teams Adaptive Card
	// Action.Submit, Google Chat card action). When Type ==
	// MessageTypeInteraction, the Interaction field is populated with
	// action metadata. This allows the system to resolve approvals and
	// clarifications via button clicks rather than requiring text replies.
	MessageTypeInteraction MessageType = "interaction"
)

// SendType distinguishes message actions routed through Messenger.Send.
type SendType string

const (
	// SendTypeMessage is the default: deliver a text (or rich) message.
	SendTypeMessage SendType = ""
	// SendTypeReaction adds an emoji reaction to an existing message.
	// Requires ReplyToMessageID (the message to react to) and Emoji.
	SendTypeReaction SendType = "reaction"
	// SendTypeUpdate replaces an existing message with new content.
	// Requires ReplyToMessageID (the message ID to update). Used to
	// disarm interactive buttons after resolution (e.g. replacing
	// approval buttons with "✅ Approved by @user").
	SendTypeUpdate SendType = "update"
)

// SendRequest contains all parameters needed to send a message.
type SendRequest struct {
	// Type selects the action. Default ("") sends a normal message.
	// Use SendTypeReaction to react to an existing message with an emoji.
	Type SendType
	// Channel is the target channel/conversation.
	Channel Channel
	// Content is the message body (ignored for reactions).
	Content MessageContent
	// ThreadID is an optional thread/reply-chain identifier for threaded replies.
	// Leave empty to post at top level.
	ThreadID string
	// ReplyToMessageID, if set, quotes/replies to the specified message.
	// For reactions, this is the message ID to react to.
	ReplyToMessageID string
	// Emoji is the reaction emoji (e.g. "👍"). Only used when Type is SendTypeReaction.
	Emoji string
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
	// Type distinguishes regular messages from reactions and interactions.
	// Empty string means a normal text/media message.
	Type MessageType
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

	// ReactionEmoji is the emoji used in a reaction (e.g. "👍", "👎").
	// Only populated when Type == MessageTypeReaction.
	ReactionEmoji string
	// ReactedMessageID is the platform-assigned ID of the message being
	// reacted to. Only populated when Type == MessageTypeReaction.
	ReactedMessageID string

	// Interaction carries structured data when Type == MessageTypeInteraction.
	// Populated by adapters that support interactive UI elements (buttons,
	// menus, card actions). Nil for all other message types.
	Interaction *InteractionData
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
	// Connect establishes a connection to the messaging platform and returns
	// an optional http.Handler for receiving inbound webhook/push events.
	//
	// HTTP-push adapters (Teams, Google Chat, Slack Events API, Telegram
	// webhook) return a non-nil handler that the caller mounts on a shared
	// HTTP mux at the desired context path (e.g., /agents/{name}/{platform}/events).
	// The adapter MUST NOT start its own http.Server.
	//
	// Outbound-only adapters (Slack Socket Mode, Discord WebSocket, Telegram
	// long-polling, WhatsApp) return a nil handler because they initiate
	// connections to the platform rather than receiving inbound HTTP.
	//
	// Calling Connect on an already-connected Messenger returns
	// (nil, ErrAlreadyConnected).
	Connect(ctx context.Context) (http.Handler, error)

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

	ConnectionInfo() string

	// FormatClarification enriches a SendRequest with platform-specific rich
	// formatting for a clarifying question posed by the agent.
	// Adapters that do not support rich formatting should return the request unchanged.
	FormatClarification(req SendRequest, info ClarificationInfo) SendRequest

	// UpdateMessage replaces the content of a previously sent message.
	// Used to disarm interactive buttons after resolution (e.g. replacing
	// approval buttons with "✅ Approved by @user") and to update
	// progress messages. Adapters that do not support message editing
	// should return nil (a no-op).
	// Returns ErrNotConnected if Connect has not been called.
	UpdateMessage(ctx context.Context, req UpdateRequest) error
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

// InteractionData carries structured data from an interactive UI element.
// This is a platform-agnostic representation of a button click, menu
// selection, or card action. Each adapter converts its native interaction
// payload (Slack block_actions, Teams Action.Submit, Google Chat card
// action) into this common type.
//
// Without this type, button clicks from rich approval/clarification
// notifications would be silently discarded because the Receive channel
// only delivers text messages.
type InteractionData struct {
	// ActionID is the platform-specific action identifier.
	// Convention: "{verb}_{resourceID}" (e.g. "approve_abc123",
	// "reject_abc123", "clarify_respond_xyz").
	ActionID string
	// ActionValue is the value associated with the action. For approval
	// buttons, this is the approval ID.
	ActionValue string
	// BlockID is the container block/card identifier
	// (e.g. "approval_abc123" in Slack Block Kit).
	BlockID string
	// ActionType describes the UI element type (e.g. "button", "select").
	ActionType string
	// ResponseURL is an optional, time-limited URL provided by the
	// platform for updating or replacing the message that contained the
	// interactive element. Slack provides this for 30 minutes after a
	// block_actions event. Adapters that don't support it leave this empty.
	ResponseURL string
}

// UpdateRequest contains all parameters needed to update an existing message.
// Used after resolving an approval or clarification to replace interactive
// buttons with a resolved status (e.g. "✅ Approved by @user").
//
// Without this type, interactive buttons would remain active after
// resolution, allowing other users to click them and causing confusion.
type UpdateRequest struct {
	// MessageID is the platform-assigned identifier of the message to update.
	MessageID string
	// Channel is the channel/conversation containing the message.
	Channel Channel
	// Content is the replacement message body.
	Content MessageContent
	// Metadata holds platform-specific key-value pairs for the update
	// (e.g. replacement Slack blocks, updated Adaptive Card).
	Metadata map[string]any
}
