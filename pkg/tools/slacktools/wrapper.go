package slacktools

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

// slackWrapper implements the Service interface using the slack-go library.
type slackWrapper struct {
	client *slack.Client
}

// newWrapper creates a new slackWrapper from the given Config.
func newWrapper(cfg Config) (*slackWrapper, error) {
	client := slack.New(cfg.Token)
	return &slackWrapper{client: client}, nil
}

// SearchMessages searches for messages in the workspace.
func (w *slackWrapper) SearchMessages(ctx context.Context, query string, count int) ([]MessageResult, error) {
	params := slack.SearchParameters{
		Count: count,
		Sort:  "timestamp",
	}
	result, err := w.client.SearchMessagesContext(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("slack: search failed: %w", err)
	}

	messages := make([]MessageResult, 0, len(result.Matches))
	for _, m := range result.Matches {
		messages = append(messages, MessageResult{
			Channel:   m.Channel.Name,
			User:      m.Username,
			Text:      m.Text,
			Timestamp: m.Timestamp,
			Permalink: m.Permalink,
		})
	}
	return messages, nil
}

// ListChannels returns public channels.
func (w *slackWrapper) ListChannels(ctx context.Context, limit int) ([]ChannelInfo, error) {
	params := &slack.GetConversationsParameters{
		Types:           []string{"public_channel"},
		Limit:           limit,
		ExcludeArchived: true,
	}
	channels, _, err := w.client.GetConversationsContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("slack: failed to list channels: %w", err)
	}

	result := make([]ChannelInfo, 0, len(channels))
	for _, ch := range channels {
		result = append(result, ChannelInfo{
			ID:          ch.ID,
			Name:        ch.Name,
			Topic:       ch.Topic.Value,
			Purpose:     ch.Purpose.Value,
			MemberCount: ch.NumMembers,
		})
	}
	return result, nil
}

// ReadChannelHistory reads recent messages from a channel.
func (w *slackWrapper) ReadChannelHistory(ctx context.Context, channelID string, limit int) ([]Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}
	history, err := w.client.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("slack: failed to read history for %s: %w", channelID, err)
	}

	messages := make([]Message, 0, len(history.Messages))
	for _, m := range history.Messages {
		messages = append(messages, Message{
			User:      m.User,
			Text:      m.Text,
			Timestamp: m.Timestamp,
		})
	}
	return messages, nil
}

// GetChannelInfo returns information about a specific channel.
func (w *slackWrapper) GetChannelInfo(ctx context.Context, channelID string) (*ChannelInfo, error) {
	ch, err := w.client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return nil, fmt.Errorf("slack: failed to get channel %s: %w", channelID, err)
	}

	return &ChannelInfo{
		ID:          ch.ID,
		Name:        ch.Name,
		Topic:       ch.Topic.Value,
		Purpose:     ch.Purpose.Value,
		MemberCount: ch.NumMembers,
	}, nil
}

// ListUsers returns workspace users, excluding bots and deleted users.
func (w *slackWrapper) ListUsers(ctx context.Context) ([]UserInfo, error) {
	users, err := w.client.GetUsersContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack: failed to list users: %w", err)
	}

	result := make([]UserInfo, 0, len(users))
	for _, u := range users {
		if u.Deleted || u.IsBot {
			continue
		}
		result = append(result, UserInfo{
			ID:          u.ID,
			Name:        u.Name,
			DisplayName: u.Profile.DisplayName,
			Email:       u.Profile.Email,
		})
	}
	return result, nil
}

// PostMessage sends a text message to a channel.
func (w *slackWrapper) PostMessage(ctx context.Context, channelID string, text string) error {
	_, _, err := w.client.PostMessageContext(ctx, channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return fmt.Errorf("slack: failed to post message to %s: %w", channelID, err)
	}
	return nil
}

// Validate checks that the token is valid by calling auth.test.
func (w *slackWrapper) Validate(ctx context.Context) error {
	_, err := w.client.AuthTestContext(ctx)
	if err != nil {
		return fmt.Errorf("slack: validate failed: %w", err)
	}
	return nil
}
