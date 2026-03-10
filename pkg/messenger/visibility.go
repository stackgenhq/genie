// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger

// DeriveVisibility determines the memory visibility scope from a MessageOrigin.
// The returned string is stored as metadata on vector store entries and used
// as a filter key during retrieval to enforce memory isolation.
//
// Visibility values:
//   - "private:{senderID}"  — only the sender can retrieve this memory
//   - "group:{channelID}"   — anyone in the channel/group can retrieve this memory
//   - "global"              — no restriction (e.g. runbooks, system content)
func (origin MessageOrigin) DeriveVisibility() string {
	if origin.IsZero() {
		return "global"
	}

	switch {
	case origin.IsPrivateContext():
		return "private:" + origin.Sender.ID
	case origin.IsGroupContext():
		return "group:" + origin.Channel.ID
	default:
		return "global"
	}
}

// DeriveConversationKey returns a memory key for conversation history isolation.
// Unlike DeriveVisibility (which scopes by sender for privacy/access-control),
// this method includes the Channel.ID so each chat thread gets isolated
// conversation memory. Without this, all AG-UI threads from the same sender
// would share conversation history, causing the agent to confuse questions
// across separate chat sessions.
//
// Conversation key values:
//   - "private:{senderID}:{channelID}" — per-thread history (AG-UI, DMs with channel)
//   - "private:{senderID}"             — fallback when no channel is set (TUI)
//   - "group:{channelID}"              — shared history within a group/channel
//   - "global"                         — no restriction (system content)
func (origin MessageOrigin) DeriveConversationKey() string {
	if origin.IsZero() {
		return "global"
	}

	switch {
	case origin.IsGroupContext():
		return "group:" + origin.Channel.ID
	case origin.Channel.ID != "":
		return "private:" + origin.Sender.ID + ":" + origin.Channel.ID
	default:
		return "private:" + origin.Sender.ID
	}
}

// IsPrivateContext returns true when the message is from a 1:1 / DM context
// where memory should not be shared with other users.
//
// Uses Channel.Type which is set by each platform adapter:
//   - whatsapp: DM for 1:1, Group for group chats
//   - slack/teams/discord/googlechat: DM or Channel/Group
//   - agui/tui: defaults to private (no channel type set)
func (origin MessageOrigin) IsPrivateContext() bool {
	// If the channel type is explicitly DM, it's private.
	// If channel type is empty (agui, tui, unknown adapters), default to private.
	return origin.Channel.Type == ChannelTypeDM || origin.Channel.Type == ""
}

// IsGroupContext returns true when the message is from a shared context
// (channel, group) where memories should be accessible to other members.
// This covers WhatsApp group chats, Slack channels, Teams channels, etc.
func (origin MessageOrigin) IsGroupContext() bool {
	return origin.Channel.Type == ChannelTypeGroup || origin.Channel.Type == ChannelTypeChannel
}
