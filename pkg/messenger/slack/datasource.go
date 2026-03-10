// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package slack provides a DataSource connector that enumerates Slack channel
// messages for vectorization. It uses the Slack Web API (conversations.history)
// and requires a bot token with channels:history and channels:read.
package slack

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/slack-go/slack"
	"github.com/stackgenhq/genie/pkg/datasource"
)

const (
	datasourceNameSlack = "slack"
	historyLimit        = 200
)

// SlackConnector implements datasource.DataSource for Slack channels.
// It fetches conversation history for each channel in scope and returns
// normalized items (one per message) for the sync pipeline to vectorize.
type SlackConnector struct {
	api *slack.Client
}

// NewSlackConnector returns a DataSource that lists messages from Slack
// channels. The caller must provide an authenticated slack.Client (e.g. from
// slack.New(botToken)); the connector uses conversations.history and does not
// connect via Socket Mode.
func NewSlackConnector(api *slack.Client) *SlackConnector {
	return &SlackConnector{api: api}
}

// Name returns the source identifier for Slack.
func (c *SlackConnector) Name() string {
	return datasourceNameSlack
}

// ListItems fetches messages from each channel in scope.SlackChannelIDs and
// returns them as NormalizedItems. Each message becomes one item with ID
// "slack:channelID:timestamp" and content equal to the message text.
func (c *SlackConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	if len(scope.SlackChannelIDs) == 0 {
		return nil, nil
	}
	var out []datasource.NormalizedItem
	for _, chID := range scope.SlackChannelIDs {
		items, err := c.listChannelMessages(ctx, chID)
		if err != nil {
			return nil, fmt.Errorf("slack channel %s: %w", chID, err)
		}
		out = append(out, items...)
	}
	return out, nil
}

func (c *SlackConnector) listChannelMessages(ctx context.Context, channelID string) ([]datasource.NormalizedItem, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     historyLimit, // per-page; pagination via Cursor below
	}
	var out []datasource.NormalizedItem
	for {
		hist, err := c.api.GetConversationHistoryContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("get conversation history: %w", err)
		}
		for _, msg := range hist.Messages {
			if msg.Timestamp == "" {
				continue
			}
			ts, err := parseSlackTimestamp(msg.Timestamp)
			if err != nil {
				continue
			}
			meta := map[string]string{}
			if msg.User != "" {
				meta["author"] = msg.User
			}
			if msg.Username != "" {
				meta["username"] = msg.Username
			}
			out = append(out, datasource.NormalizedItem{
				ID:        "slack:" + channelID + ":" + msg.Timestamp,
				Source:    datasourceNameSlack,
				UpdatedAt: ts,
				Content:   msg.Text,
				Metadata:  meta,
			})
		}
		if !hist.HasMore {
			break
		}
		params.Cursor = hist.ResponseMetaData.NextCursor
		if params.Cursor == "" {
			break
		}
	}
	return out, nil
}

func parseSlackTimestamp(ts string) (time.Time, error) {
	f, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return time.Time{}, err
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec), nil
}

// Ensure SlackConnector implements datasource.DataSource at compile time.
var _ datasource.DataSource = (*SlackConnector)(nil)
