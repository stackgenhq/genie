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

// defaultCacheTTL is how long cached tool metadata remains valid before
// re-fetching. 30 seconds balances JIT freshness against the cost of
// reconnecting to MCP servers (each connection spawns a subprocess or HTTP
// session) just to list tools.
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
// Tool metadata (names, descriptions, schemas) is fetched just-in-time via
// GetTools(ctx) and cached for a short TTL (defaultCacheTTL). Each tool's
// Call() method dials its own ephemeral MCP connection, so cache refreshes
// never close connections that are still in use by in-flight tool calls.
type Client struct {
	config         MCPConfig
	secretProvider security.SecretProvider

	mu            sync.Mutex
	cachedTools   []tool.Tool
	cachedAt      time.Time
	cacheTTL      time.Duration
	failedServers map[string]time.Time // serverName → last failure time; skip retries within cacheTTL
}

// NewClient creates a new MCP client from the provided configuration.
// It validates the configuration but does NOT connect to MCP servers at
// startup. Tool metadata is fetched just-in-time via GetTools(ctx).
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

// listServerTools connects to a single MCP server, lists available tools,
// and returns ClientTool adapters with per-call dialers. The listing
// connection is closed before returning — each tool Call() dials fresh.
func (c *Client) listServerTools(ctx context.Context, config MCPServerConfig) ([]tool.Tool, error) {
	mcpClient, err := c.dialServer(ctx, config)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mcpClient.Close() }()

	// List available tools
	availableTools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Convert and filter tools — each tool gets its own dialer closure.
	return c.convertAndFilterTools(ctx, availableTools.Tools, config)
}

// dialServer creates and initializes an MCP client connection. The caller is
// responsible for closing it.
func (c *Client) dialServer(ctx context.Context, config MCPServerConfig) (*client.Client, error) {
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

	mcpClient := client.NewClient(trans)

	if err = mcpClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start MCP transport: %w", err)
	}

	if _, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		_ = mcpClient.Close()
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return mcpClient, nil
}

// convertAndFilterTools converts MCP tools to trpc-agent-go tools and applies
// filtering. Each tool gets a per-call dialer that opens an ephemeral MCP
// connection for every Call().
func (c *Client) convertAndFilterTools(ctx context.Context, mcpTools []mcp.Tool, config MCPServerConfig) ([]tool.Tool, error) {
	var tools []tool.Tool
	log := logger.GetLogger(ctx).With("fn", "mcp.convertAndFilterTools")

	// Build a dialer closure that captures server config + secret provider.
	// Each tool Call() will use this to dial a fresh connection.
	serverConfig := config
	sp := c.secretProvider
	dialer := func(ctx context.Context) (MCPCaller, func(), error) {
		conn, err := dialMCP(ctx, serverConfig, sp)
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { _ = conn.Close() }, nil
	}

	for _, mcpTool := range mcpTools {
		if !c.shouldIncludeTool(mcpTool.Name, config) {
			log.Debug("Filtering out tool", "tool", mcpTool.Name, "server", config.Name)
			continue
		}

		log.Debug("Found tool", "tool", mcpTool.Name, "server", config.Name)
		adapter := newClientTool(mcpTool, config.Name, dialer)
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
// Tool metadata (names, descriptions, schemas) is cached for cacheTTL
// (default 30s) to avoid reconnecting on every Registry lookup (GetTool,
// ToolNames, AllTools, etc.). Each returned tool dials its own ephemeral
// connection per Call(), so cache refreshes never interfere with in-flight
// tool calls.
func (c *Client) GetTools(ctx context.Context) []tool.Tool {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached tool metadata if still fresh.
	if c.cachedTools != nil && time.Since(c.cachedAt) < c.cacheTTL {
		return c.cachedTools
	}

	log := logger.GetLogger(ctx).With("fn", "mcp.GetTools")

	var allTools []tool.Tool

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

		tools, err := c.listServerTools(ctx, serverConfig)
		if err != nil {
			log.Warn("failed to fetch tools from MCP server, skipping", "server", serverConfig.Name, "error", err)
			c.failedServers[serverConfig.Name] = time.Now()
			continue
		}

		// Server recovered — clear any previous failure record.
		delete(c.failedServers, serverConfig.Name)
		allTools = append(allTools, tools...)
	}

	c.cachedTools = allTools
	c.cachedAt = time.Now()

	return allTools
}

// Close clears the tool metadata cache. No live connections are held, so
// this is a lightweight operation. Safe to call multiple times.
func (c *Client) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedTools = nil
	c.cachedAt = time.Time{}
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
