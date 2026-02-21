package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Client manages MCP server connections and provides access to MCP tools.
// It uses mark3labs/mcp-go for MCP protocol communication and wraps tools
// to be compatible with trpc-agent-go.
//
// Note: This is a simplified implementation that provides configuration management.
// Full MCP integration will be available when trpc-agent-go releases its MCP package.
type Client struct {
	config  MCPConfig
	clients []*client.Client
	logger  *logrus.Logger
	tools   []tool.Tool
}

// NewClient creates a new MCP client from the provided configuration.
// It initializes connections to all configured MCP servers and returns a client
// that provides access to all available tools.
//
// The client must be closed when no longer needed to release resources.
func NewClient(ctx context.Context, config MCPConfig) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid MCP configuration: %w", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	mcpClient := &Client{
		config:  config,
		clients: make([]*client.Client, 0),
		logger:  logger,
		tools:   make([]tool.Tool, 0),
	}

	for i := range config.Servers {
		config.Servers[i].SetDefaults()
		serverConfig := config.Servers[i]

		tools, err := mcpClient.initializeServer(ctx, serverConfig)
		if err != nil {
			// Cleanup already initialized clients
			mcpClient.Close()
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
		trans = transport.NewStdio(config.Command, nil, config.Args...)
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
	return c.convertAndFilterTools(mcpClient, toolsResponse.Tools, config)
}

// convertAndFilterTools converts MCP tools to trpc-agent-go tools and applies filtering.
// It wraps each MCP tool using MCPTool and filters based on include/exclude lists.
//
// Note: This is a simplified implementation. Full MCP tool integration will be available
// when trpc-agent-go releases its MCP package or when we fully integrate with mark3labs/mcp-go.
func (c *Client) convertAndFilterTools(mcpClient *client.Client, mcpTools []mcp.Tool, config MCPServerConfig) ([]tool.Tool, error) {
	var tools []tool.Tool

	for _, mcpTool := range mcpTools {
		// Apply filtering
		if !c.shouldIncludeTool(mcpTool.Name, config) {
			c.logger.Debugf("Filtering out tool %s from server %s", mcpTool.Name, config.Name)
			continue
		}

		c.logger.Infof("Found tool %s from server %s", mcpTool.Name, config.Name)
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
func (c *Client) Close() {
	for _, client := range c.clients {
		if client != nil {
			if err := client.Close(); err != nil {
				c.logger.Warnf("failed to close MCP client: %v", err)
			}
		}
	}
}
