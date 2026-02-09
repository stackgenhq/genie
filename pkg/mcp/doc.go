// Package mcp provides MCP (Model Context Protocol) integration for Genie using trpc-agent-go.
// It enables Genie to connect to MCP servers via multiple transports (stdio, HTTP, SSE) and
// manage tool discovery and execution.
//
// The package leverages trpc-agent-go's out-of-the-box MCP tool integration (mcp.NewMCPToolSet)
// and provides a configuration-driven approach to managing MCP server connections.
//
// # Key Features
//
//   - Multiple transport support: stdio, streamable_http, and sse
//   - Tool filtering with include/exclude patterns
//   - Session reconnection for automatic recovery
//   - Configurable retry behavior with exponential backoff
//   - Multiple concurrent MCP server connections
//
// # Basic Usage
//
//	import (
//	    "context"
//	    "github.com/appcd-dev/genie/pkg/mcp"
//	    "time"
//	)
//
//	// Create configuration
//	config := mcp.MCPConfig{
//	    Servers: []mcp.MCPServerConfig{
//	        {
//	            Name:      "terraform",
//	            Transport: "streamable_http",
//	            ServerURL: "http://localhost:3000/mcp",
//	            Timeout:   10 * time.Second,
//	            IncludeTools: []string{"search_modules", "get_module"},
//	        },
//	    },
//	}
//
//	// Initialize client
//	client, err := mcp.NewClient(context.Background(), config)
//	if err != nil {
//	    // handle error
//	}
//	defer client.Close()
//
//	// Get available tools
//	tools := client.GetTools()
//
// # Configuration Examples
//
// See README.md for comprehensive configuration examples including:
//   - Stdio transport configuration
//   - HTTP/SSE transport configuration
//   - Tool filtering
//   - Session reconnection
//   - Retry configuration
//
// # Integration with trpc-agent-go
//
// This package uses trpc-agent-go's mcp.NewMCPToolSet to create MCP tool sets.
// The tools returned by GetTools() implement the tool.Tool interface and can be
// used directly with trpc-agent-go agents.
//
// For more details on trpc-agent-go MCP integration, see:
// https://github.com/trpc-group/trpc-agent-go/tree/main/tool/mcp
package mcp
