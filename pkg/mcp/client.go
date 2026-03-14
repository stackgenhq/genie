// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// defaultCacheTTL is how long cached tools remain valid before re-fetching.
// 30 seconds balances JIT freshness against the cost of reconnecting to
// MCP servers (each connection spawns a subprocess or HTTP session).
const defaultCacheTTL = 30 * time.Second

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
// Tools are fetched just-in-time via GetTools(ctx) and cached for a short
// TTL (defaultCacheTTL). This balances freshness (e.g. new tools after
// user login) with performance (avoiding reconnects on every Registry
// lookup). Close() releases all live connections.
type Client struct {
	config         MCPConfig
	secretProvider security.SecretProvider

	mu            sync.Mutex
	cachedTools   []tool.Tool
	cachedAt      time.Time
	cacheTTL      time.Duration
	liveClients   []*client.Client     // connections backing current cachedTools
	failedServers map[string]time.Time // serverName → last failure time; skip retries within cacheTTL
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
		config:        config,
		cacheTTL:      defaultCacheTTL,
		failedServers: make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(mcpClient)
	}

	return mcpClient, nil
}

// initializeServer connects to a single MCP server, lists available tools,
// and returns ClientTool adapters with a live connection for Call().
// The live *client.Client is also returned so the caller can track and close it.
func (c *Client) initializeServer(ctx context.Context, config MCPServerConfig) ([]tool.Tool, *client.Client, error) {
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
			return nil, nil, fmt.Errorf("failed to create SSE transport: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported transport: %s", config.Transport)
	}

	// Create MCP client
	mcpClient := client.NewClient(trans)

	// Start the transport (spawns the subprocess for stdio, opens the
	// HTTP connection for SSE). Must happen before Initialize.
	if err = mcpClient.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to start MCP transport: %w", err)
	}

	// Initialize the client
	_, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		_ = mcpClient.Close()
		return nil, nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	// List available tools
	availableTools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = mcpClient.Close()
		return nil, nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Convert and filter tools — the mcpClient stays open so Call() works
	tools, filterErr := c.convertAndFilterTools(ctx, mcpClient, availableTools.Tools, config)
	if filterErr != nil {
		_ = mcpClient.Close()
		return nil, nil, filterErr
	}
	return tools, mcpClient, nil
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

// GetTools returns all available tools from all configured MCP servers.
// Results are cached for cacheTTL (default 30s) to avoid reconnecting on
// every Registry lookup (GetTool, ToolNames, AllTools, etc.). After the
// TTL expires, the next call opens fresh connections and closes stale ones.
func (c *Client) GetTools(ctx context.Context) []tool.Tool {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached tools if still fresh.
	if c.cachedTools != nil && time.Since(c.cachedAt) < c.cacheTTL {
		return c.cachedTools
	}

	log := logger.GetLogger(ctx).With("fn", "mcp.GetTools")

	// Close stale connections before opening new ones.
	c.closeClientsLocked(ctx)

	var allTools []tool.Tool
	var newClients []*client.Client

	for i := range c.config.Servers {
		serverConfig := c.config.Servers[i]

		// Skip servers that failed recently to avoid blocking startup
		// with repeated 30s timeouts on unreachable servers.
		if failedAt, ok := c.failedServers[serverConfig.Name]; ok {
			if time.Since(failedAt) < c.cacheTTL {
				log.Debug("skipping recently-failed MCP server",
					"server", serverConfig.Name,
					"failed_ago", time.Since(failedAt).String())
				continue
			}
			// TTL expired — allow a retry.
			delete(c.failedServers, serverConfig.Name)
		}

		tools, mcpClient, err := c.initializeServer(ctx, serverConfig)
		if err != nil {
			log.Warn("failed to fetch tools from MCP server, skipping", "server", serverConfig.Name, "error", err)
			c.failedServers[serverConfig.Name] = time.Now()
			continue
		}

		// Server recovered — clear any previous failure record.
		delete(c.failedServers, serverConfig.Name)
		allTools = append(allTools, tools...)
		newClients = append(newClients, mcpClient)
	}

	c.cachedTools = allTools
	c.cachedAt = time.Now()
	c.liveClients = newClients

	return allTools
}

// Close releases all live MCP connections and clears the tool cache.
// Safe to call multiple times; subsequent calls are no-ops.
func (c *Client) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeClientsLocked(ctx)
	return nil
}

// closeClientsLocked closes all live MCP client connections and clears
// the cache. Must be called with c.mu held.
func (c *Client) closeClientsLocked(ctx context.Context) {
	log := logger.GetLogger(ctx).With("fn", "mcp.closeClientsLocked")
	for _, cl := range c.liveClients {
		if cl != nil {
			if err := cl.Close(); err != nil {
				log.Warn("failed to close MCP client", "error", err)
			}
		}
	}
	c.liveClients = nil
	c.cachedTools = nil
	c.cachedAt = time.Time{}
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
