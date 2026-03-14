// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ClientOption configures an MCP Client (e.g. WithSecretProvider).
type ClientOption func(*Client)

// WithSecretProvider sets the SecretProvider used to resolve env values that
// start with "${" when building the stdio subprocess environment. If not set,
// env values are used as-is (config load may have already expanded them).
func WithSecretProvider(sp security.SecretProvider) ClientOption {
	return func(c *Client) {
		c.secretProvider = sp
	}
}

// Client manages MCP server connections and provides access to MCP tools.
// It uses mark3labs/mcp-go for MCP protocol communication and wraps tools
// to be compatible with trpc-agent-go.
//
// Tools are fetched just-in-time on each GetTools() call rather than cached
// at startup. This ensures the freshest tool list is always returned (e.g.
// after user login grants new OAuth scopes, MCP servers expose new tools).
type Client struct {
	config         MCPConfig
	secretProvider security.SecretProvider
}

// NewClient creates a new MCP client from the provided configuration.
// It validates the configuration but does NOT connect to MCP servers at
// startup. Tools are fetched just-in-time via GetTools(ctx).
//
// Options (e.g. WithSecretProvider) can be used to enable secret lookup
// for MCP server env values containing "${VAR}".
func NewClient(ctx context.Context, config MCPConfig, opts ...ClientOption) (*Client, error) {
	// Apply defaults before validation so that empty sub-configs
	// (e.g. {"retry": {}}) receive sensible values.
	for i := range config.Servers {
		config.Servers[i].SetDefaults()
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid MCP configuration: %w", err)
	}

	mcpClient := &Client{
		config: config,
	}
	for _, opt := range opts {
		opt(mcpClient)
	}

	return mcpClient, nil
}

// initializeServer connects to a single MCP server, lists available tools,
// and returns ClientTool adapters with a live connection for Call().
// The caller is responsible for the lifecycle of the returned tools.
func (c *Client) initializeServer(ctx context.Context, config MCPServerConfig) ([]tool.Tool, error) {
	var trans transport.Interface
	var err error

	switch config.Transport {
	case "stdio":
		env := buildStdioEnvFromConfig(ctx, config, c.secretProvider)
		trans = transport.NewStdio(config.Command, env, config.Args...)
	case "streamable_http", "sse":
		opts := []transport.ClientOption{}
		if len(config.Headers) > 0 {
			headers := make(map[string]string, len(config.Headers))
			for k, v := range config.Headers {
				if c.secretProvider != nil && strings.ContainsAny(v, "$") {
					v = expandEnvWithProvider(ctx, v, c.secretProvider)
				}
				headers[k] = v
			}
			opts = append(opts, transport.WithHeaders(headers))
		}
		trans, err = transport.NewSSE(config.ServerURL, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSE transport: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported transport: %s", config.Transport)
	}

	// Create MCP client
	mcpClient := client.NewClient(trans)

	// Start the transport (spawns the subprocess for stdio, opens the
	// HTTP connection for SSE). Must happen before Initialize.
	if err = mcpClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start MCP transport: %w", err)
	}

	// Initialize the client
	_, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	// List available tools
	availableTools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = mcpClient.Close()
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Convert and filter tools — the mcpClient stays open so Call() works
	return c.convertAndFilterTools(ctx, mcpClient, availableTools.Tools, config)
}

// convertAndFilterTools converts MCP tools to trpc-agent-go tools and applies filtering.
// It wraps each MCP tool using ClientTool and filters based on include/exclude lists.
func (c *Client) convertAndFilterTools(ctx context.Context, mcpClient *client.Client, mcpTools []mcp.Tool, config MCPServerConfig) ([]tool.Tool, error) {
	var tools []tool.Tool
	logger := logger.GetLogger(ctx).With("fn", "mcp.convertAndFilterTools")

	for _, mcpTool := range mcpTools {
		// Apply filtering
		if !c.shouldIncludeTool(mcpTool.Name, config) {
			logger.Debug("Filtering out tool", "tool", mcpTool.Name, "server", config.Name)
			continue
		}

		logger.Debug("Found tool", "tool", mcpTool.Name, "server", config.Name)
		// Wrap tool with ClientTool adapter
		adapter := NewClientTool(mcpClient, mcpTool, config.Name)
		tools = append(tools, adapter)
	}

	return tools, nil
}

// shouldIncludeTool determines if a tool should be included based on filtering configuration.
// It applies include/exclude rules to determine tool availability.
func (c *Client) shouldIncludeTool(toolName string, config MCPServerConfig) bool {
	// If include list is specified, tool must be in it
	if len(config.IncludeTools) > 0 {
		found := false
		for _, name := range config.IncludeTools {
			if name == toolName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// If exclude list is specified, tool must not be in it
	if len(config.ExcludeTools) > 0 {
		for _, name := range config.ExcludeTools {
			if name == toolName {
				return false
			}
		}
	}

	return true
}

// GetTools connects to all configured MCP servers, lists their tools, and
// returns the filtered set. Each call creates fresh connections so that
// changes on the server side (e.g. user logged in, new tools available)
// are reflected immediately.
func (c *Client) GetTools(ctx context.Context) []tool.Tool {
	if c == nil {
		return nil
	}
	log := logger.GetLogger(ctx).With("fn", "mcp.GetTools")
	var allTools []tool.Tool

	for i := range c.config.Servers {
		serverConfig := c.config.Servers[i]

		tools, err := c.initializeServer(ctx, serverConfig)
		if err != nil {
			log.Warn("failed to fetch tools from MCP server, skipping", "server", serverConfig.Name, "error", err)
			continue
		}

		allTools = append(allTools, tools...)
	}

	return allTools
}

// Close is a no-op for the JIT client. Since connections are established
// and closed within each GetTools() call, there are no long-lived
// connections to release at the client level. Retained for backward
// compatibility with callers like doctor.go.
func (c *Client) Close(_ context.Context) error {
	return nil
}

// GetPromptRepositories returns a PromptRepository for each configured MCP
// server so the agent can discover and read MCP Prompts as Genie Skills.
func (c *Client) GetPromptRepositories() []*PromptRepository {
	var repos []*PromptRepository
	for _, server := range c.config.Servers {
		repos = append(repos, NewPromptRepository(server, c.secretProvider))
	}
	return repos
}
