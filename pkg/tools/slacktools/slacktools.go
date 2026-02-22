package slacktools

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the Slack connector. This gives the
// AI agent the ability to *read and search* Slack workspaces — distinct from
// the messenger package which handles *sending* platform notifications.
//
//counterfeiter:generate . Service
type Service interface {
	// SearchMessages searches for messages matching a query string.
	SearchMessages(ctx context.Context, query string, count int) ([]MessageResult, error)

	// ListChannels returns public channels in the workspace.
	ListChannels(ctx context.Context, limit int) ([]ChannelInfo, error)

	// ReadChannelHistory reads recent messages from a channel.
	ReadChannelHistory(ctx context.Context, channelID string, limit int) ([]Message, error)

	// GetChannelInfo returns information about a specific channel.
	GetChannelInfo(ctx context.Context, channelID string) (*ChannelInfo, error)

	// ListUsers returns workspace users.
	ListUsers(ctx context.Context) ([]UserInfo, error)

	// PostMessage sends a message to a channel.
	PostMessage(ctx context.Context, channelID string, text string) error

	// Validate performs a lightweight health check (auth.test).
	Validate(ctx context.Context) error
}

// Config holds configuration for the Slack connector.
type Config struct {
	Token string `yaml:"token" toml:"token"` // Slack Bot Token (xoxb-...)
}

// ── Domain Types ────────────────────────────────────────────────────────

// MessageResult represents a search result message.
type MessageResult struct {
	Channel   string `json:"channel"`
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Permalink string `json:"permalink"`
}

// Message represents a channel message.
type Message struct {
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

// ChannelInfo describes a Slack channel.
type ChannelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Topic       string `json:"topic,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	MemberCount int    `json:"member_count"`
}

// UserInfo describes a Slack user.
type UserInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
}

// ── Factory ─────────────────────────────────────────────────────────────

// New creates a new Slack Service from the given configuration.
// The token must be a Bot User OAuth Token (xoxb-...) with appropriate scopes.
func New(cfg Config) (Service, error) {
	log := logger.GetLogger(context.Background())
	log.Info("Initializing Slack tools service", "has_token", cfg.Token != "")

	if cfg.Token == "" {
		return nil, fmt.Errorf("slack: token is required")
	}

	return newWrapper(cfg)
}

// ── Request Types ───────────────────────────────────────────────────────

type searchRequest struct {
	Query string `json:"query" jsonschema:"description=Search query for Slack messages,required"`
	Count int    `json:"count" jsonschema:"description=Maximum number of results (default 20)"`
}

type listChannelsRequest struct {
	Limit int `json:"limit" jsonschema:"description=Maximum number of channels to return (default 100)"`
}

type readHistoryRequest struct {
	ChannelID string `json:"channel_id" jsonschema:"description=Slack channel ID (e.g. C01ABCDEF),required"`
	Limit     int    `json:"limit" jsonschema:"description=Number of messages to retrieve (default 20)"`
}

type channelInfoRequest struct {
	ChannelID string `json:"channel_id" jsonschema:"description=Slack channel ID (e.g. C01ABCDEF),required"`
}

type postMessageRequest struct {
	ChannelID string `json:"channel_id" jsonschema:"description=Slack channel ID to post to,required"`
	Text      string `json:"text" jsonschema:"description=Message text to send,required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewSearchMessagesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.searchMessages,
		function.WithName("slack_search_messages"),
		function.WithDescription("Search for messages in the Slack workspace. Returns matching messages with channel, user, text, and permalink."),
	)
}

func NewListChannelsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listChannels,
		function.WithName("slack_list_channels"),
		function.WithDescription("List public channels in the Slack workspace."),
	)
}

func NewReadChannelHistoryTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.readChannelHistory,
		function.WithName("slack_read_channel_history"),
		function.WithDescription("Read recent messages from a Slack channel. Use slack_list_channels first to find channel IDs."),
	)
}

func NewGetChannelInfoTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getChannelInfo,
		function.WithName("slack_get_channel_info"),
		function.WithDescription("Get information about a specific Slack channel including topic and purpose."),
	)
}

func NewListUsersTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listUsers,
		function.WithName("slack_list_users"),
		function.WithDescription("List users in the Slack workspace."),
	)
}

func NewPostMessageTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.postMessage,
		function.WithName("slack_post_message"),
		function.WithDescription("Post a message to a Slack channel."),
	)
}

// AllTools returns all Slack tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewSearchMessagesTool(s),
		NewListChannelsTool(s),
		NewReadChannelHistoryTool(s),
		NewGetChannelInfoTool(s),
		NewListUsersTool(s),
		NewPostMessageTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) searchMessages(ctx context.Context, req searchRequest) ([]MessageResult, error) {
	count := req.Count
	if count <= 0 {
		count = 20
	}
	return ts.s.SearchMessages(ctx, req.Query, count)
}

func (ts *toolSet) listChannels(ctx context.Context, req listChannelsRequest) ([]ChannelInfo, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	return ts.s.ListChannels(ctx, limit)
}

func (ts *toolSet) readChannelHistory(ctx context.Context, req readHistoryRequest) ([]Message, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	return ts.s.ReadChannelHistory(ctx, req.ChannelID, limit)
}

func (ts *toolSet) getChannelInfo(ctx context.Context, req channelInfoRequest) (*ChannelInfo, error) {
	return ts.s.GetChannelInfo(ctx, req.ChannelID)
}

func (ts *toolSet) listUsers(ctx context.Context, _ struct{}) ([]UserInfo, error) {
	return ts.s.ListUsers(ctx)
}

func (ts *toolSet) postMessage(ctx context.Context, req postMessageRequest) (string, error) {
	err := ts.s.PostMessage(ctx, req.ChannelID, req.Text)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Message posted to %s", req.ChannelID), nil
}
