// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
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
// Note: This is a simplified implementation that provides configuration management.
// Full MCP integration will be available when trpc-agent-go releases its MCP package.
type Client struct {
	config         MCPConfig
	clients        []*client.Client
	tools          []tool.Tool
	secretProvider security.SecretProvider
}

// NewClient creates a new MCP client from the provided configuration.
// It initializes connections to all configured MCP servers and returns a client
// that provides access to all available tools. Options (e.g. WithSecretProvider)
// can be used to enable secret lookup for MCP server env values containing "${VAR}".
//
// The client must be closed when no longer needed to release resources.
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
		config:  config,
		clients: make([]*client.Client, 0),
		tools:   make([]tool.Tool, 0),
	}
	for _, opt := range opts {
		opt(mcpClient)
	}

	for i := range config.Servers {
		serverConfig := config.Servers[i]

		tools, err := mcpClient.initializeServer(ctx, serverConfig)
		if err != nil {
			// Cleanup already initialized clients
			mcpClient.Close(ctx)
			return nil, fmt.Errorf("failed to initialize server %s: %w", serverConfig.Name, err)
		}

		mcpClient.tools = append(mcpClient.tools, tools...)
	}

	return mcpClient, nil
}

// initializeServer initializes a single MCP server connection and returns its tools.
// It handles different transport types and applies tool filtering as configured.
func (c *Client) initializeServer(ctx context.Context, config MCPServerConfig) ([]tool.Tool, error) {
	var trans transport.Interface
	var err error

	switch config.Transport {
	case "stdio":
		env := c.buildStdioEnv(ctx, config)
		trans = transport.NewStdio(config.Command, env, config.Args...)
	case "streamable_http", "sse":
		trans, err = transport.NewSSE(config.ServerURL)
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
	toolsResponse, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Store client for cleanup
	c.clients = append(c.clients, mcpClient)

	// Convert and filter tools
	return c.convertAndFilterTools(ctx, mcpClient, toolsResponse.Tools, config)
}

// buildStdioEnv returns the environment for the stdio subprocess: parent env
// plus config.Env overrides. When c.secretProvider is set, any value that
// starts with "${" is resolved via the provider (e.g. ${GH_TOKEN} -> GetSecret(ctx, "GH_TOKEN")).
func (c *Client) buildStdioEnv(ctx context.Context, config MCPServerConfig) []string {
	base := os.Environ()
	if len(config.Env) == 0 {
		return base
	}
	overlay := make(map[string]string)
	for _, s := range base {
		if i := strings.IndexByte(s, '='); i > 0 {
			overlay[s[:i]] = s[i+1:]
		}
	}
	for k, v := range config.Env {
		if c.secretProvider != nil && strings.Contains(v, "${") {
			v = c.expandEnvValue(ctx, v)
		}
		overlay[k] = v
	}
	out := make([]string, 0, len(overlay))
	for k, v := range overlay {
		out = append(out, k+"="+v)
	}
	return out
}

// expandEnvValue expands ${NAME} and $NAME in value using the client's SecretProvider.
func (c *Client) expandEnvValue(ctx context.Context, value string) string {
	return os.Expand(value, func(name string) string {
		val, err := c.secretProvider.GetSecret(ctx, security.GetSecretRequest{
			Name:   name,
			Reason: toolcontext.GetJustification(ctx),
		})
		if err != nil {
			logger.GetLogger(ctx).With("fn", "mcp.expandEnvValue").Debug("secret lookup failed", "name", name, "error", err)
			return ""
		}
		return val
	})
}

// convertAndFilterTools converts MCP tools to trpc-agent-go tools and applies filtering.
// It wraps each MCP tool using MCPTool and filters based on include/exclude lists.
//
// Note: This is a simplified implementation. Full MCP tool integration will be available
// when trpc-agent-go releases its MCP package or when we fully integrate with mark3labs/mcp-go.
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

// GetTools returns all available tools from all configured MCP servers.
// The tools implement the tool.Tool interface and can be used with trpc-agent-go agents.
func (c *Client) GetTools() []tool.Tool {
	return c.tools
}

// Close closes all MCP server connections and releases resources.
// This should be called when the client is no longer needed.
func (c *Client) Close(ctx context.Context) {
	for _, client := range c.clients {
		if client != nil {
			if err := client.Close(); err != nil {
				logger.GetLogger(ctx).Warn("failed to close MCP client", "err", err)
			}
		}
	}
}
