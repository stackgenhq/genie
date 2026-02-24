# MCP Integration for Genie

This package provides MCP (Model Context Protocol) integration for Genie, enabling connection to MCP servers via multiple transports (stdio, HTTP, SSE) and managing tool discovery and execution.

## Overview

The MCP package uses [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) for MCP protocol communication and wraps tools to be compatible with [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go).

## Features

- **Multiple Transport Support**: stdio, streamable_http, and sse
- **Tool Filtering**: Include/exclude specific tools using allowlist/blocklist patterns
- **Configuration-Driven**: TOML/YAML configuration for easy setup
- **trpc-agent-go Compatible**: Tools work seamlessly with trpc-agent-go agents
- **Multiple Servers**: Connect to multiple MCP servers simultaneously

## Configuration

### TOML Configuration Example

```toml
# Stdio transport (local MCP server)
[[mcp.servers]]
name = "local-tools"
transport = "stdio"
command = "go"
args = ["run", "./mcp-server/main.go"]
timeout = "10s"
include_tools = ["echo", "add"]  # Optional: allowlist specific tools

# HTTP streaming transport with headers
[[mcp.servers]]
name = "terraform-registry"
transport = "streamable_http"
server_url = "http://localhost:3000/mcp"
timeout = "10s"

[mcp.servers.headers]
User-Agent = "genie/1.0"
Authorization = "Bearer your-token-here"

include_tools = ["search_modules", "get_module"]

# SSE transport
[[mcp.servers]]
name = "sse-server"
transport = "sse"
server_url = "http://localhost:8080/sse"
timeout = "10s"
```

### YAML Configuration Example

```yaml
mcp:
  servers:
    - name: local-tools
      transport: stdio
      command: go
      args:
        - run
        - ./mcp-server/main.go
      timeout: 10s
      include_tools:
        - echo
        - add
    
    - name: terraform-registry
      transport: streamable_http
      server_url: http://localhost:3000/mcp
      timeout: 10s
      headers:
        User-Agent: genie/1.0
        Authorization: Bearer your-token-here
      include_tools:
        - search_modules
        - get_module
```

## Usage

### Basic Usage

```go
import (
    "context"
    "github.com/stackgenhq/genie/pkg/mcp"
    "time"
)

// Create configuration
config := mcp.MCPConfig{
    Servers: []mcp.MCPServerConfig{
        {
            Name:      "terraform",
            Transport: "streamable_http",
            ServerURL: "http://localhost:3000/mcp",
            Timeout:   10 * time.Second,
            IncludeTools: []string{"search_modules", "get_module"},
        },
    },
}

// Initialize client
client, err := mcp.NewClient(context.Background(), config)
if err != nil {
    // handle error
}
defer client.Close()

// Get available tools
tools := client.GetTools()

// Use tools with trpc-agent-go agents
// tools can be passed to llmagent.WithTools()
```

### Tool Filtering

```go
// Include only specific tools (allowlist)
config := mcp.MCPServerConfig{
    Name:      "server",
    Transport: "stdio",
    Command:   "mcp-server",
    IncludeTools: []string{"tool1", "tool2"},
}

// Exclude specific tools (blocklist)
config := mcp.MCPServerConfig{
    Name:      "server",
    Transport: "stdio",
    Command:   "mcp-server",
    ExcludeTools: []string{"dangerous_tool"},
}
```

### Multiple Servers

```go
config := mcp.MCPConfig{
    Servers: []mcp.MCPServerConfig{
        {
            Name:      "local-tools",
            Transport: "stdio",
            Command:   "go",
            Args:      []string{"run", "./server.go"},
        },
        {
            Name:      "remote-tools",
            Transport: "streamable_http",
            ServerURL: "http://localhost:3000/mcp",
        },
    },
}

client, err := mcp.NewClient(ctx, config)
// All tools from both servers are available via client.GetTools()
```

## Configuration Options

### MCPServerConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the server |
| `transport` | string | Yes | Transport type: "stdio", "streamable_http", or "sse" |
| `server_url` | string | For HTTP/SSE | Server URL for HTTP/SSE transports |
| `command` | string | For stdio | Executable command for stdio transport |
| `args` | []string | No | Command arguments for stdio transport |
| `timeout` | duration | No | Connection timeout (default: 10s) |
| `headers` | map[string]string | No | Custom HTTP headers for HTTP/SSE transports |
| `include_tools` | []string | No | allowlist of tool names to include |
| `exclude_tools` | []string | No | blocklist of tool names to exclude |

## Troubleshooting

### Connection Issues

**Problem**: Failed to connect to stdio server

**Solution**: 
- Verify the command and args are correct
- Ensure the MCP server executable is in PATH or use absolute path
- Check server logs for initialization errors

**Problem**: HTTP/SSE connection timeout

**Solution**:
- Verify the server_url is correct and accessible
- Increase timeout value if server is slow to respond
- Check network connectivity and firewall rules

### Tool Discovery

**Problem**: No tools available after initialization

**Solution**:
- Check if tool filtering is too restrictive
- Verify the MCP server is properly implementing the ListTools endpoint
- Check server logs for errors during tool listing

### Integration with trpc-agent-go

**Problem**: Tools not working with agents

**Solution**:
- Ensure tools are passed to agent via `llmagent.WithTools(client.GetTools()...)`
- Verify tool schemas are valid
- Check agent logs for tool execution errors

## Testing

Run tests:

```bash
go test ./pkg/mcp/... -v
```

Run tests with coverage:

```bash
go test ./pkg/mcp/... -cover
```

## References

- [Model Context Protocol Specification](https://modelcontextprotocol.io)
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)
