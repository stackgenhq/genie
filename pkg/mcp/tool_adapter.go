// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/mcputils"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// MCPCaller is the subset of the MCP client interface used by ClientTool.
// Extracting this allows unit testing Call() with counterfeiter fakes.
//
//go:generate go tool counterfeiter -generate
//counterfeiter:generate . MCPCaller
type MCPCaller interface {
	CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

// toolDialer creates an ephemeral MCP connection for a single tool call and
// returns a caller, a cleanup function, and any dial error. Production code
// uses dialMCP; tests inject a fake via NewClientToolForTest.
type toolDialer func(ctx context.Context) (MCPCaller, func(), error)

// ClientTool wraps an MCP Tool definition to implement the trpc-agent-go
// tool.Tool interface. Each Call() dials a fresh ephemeral MCP connection
// via the dialer, executes the tool, and tears the connection down. This
// eliminates the race where a shared cached connection could be closed by
// a concurrent cache refresh while an in-flight tool call is still using it.
//
// Tools are namespaced by server name to avoid collisions when multiple MCP
// servers expose tools with the same name (e.g. "search").
type ClientTool struct {
	mcpTool    mcp.Tool
	serverName string
	dialer     toolDialer
}

// newClientTool creates a ClientTool that dials the given MCP server for
// every Call(). The serverName is used to prefix the tool name so that
// tools from different MCP servers are disambiguated (e.g. "github_search"
// vs "jira_search").
func newClientTool(mcpTool mcp.Tool, serverName string, dialer toolDialer) *ClientTool {
	return &ClientTool{
		mcpTool:    mcpTool,
		serverName: serverName,
		dialer:     dialer,
	}
}

// NewClientTool creates a ClientTool that uses a static MCPCaller for every
// Call(). This constructor is kept for backward compatibility with code that
// already holds a live MCPCaller (e.g. tests using counterfeiter fakes).
func NewClientTool(caller MCPCaller, mcpTool mcp.Tool, serverName string) *ClientTool {
	return &ClientTool{
		mcpTool:    mcpTool,
		serverName: serverName,
		dialer: func(_ context.Context) (MCPCaller, func(), error) {
			return caller, func() {}, nil
		},
	}
}

// Name returns the namespaced tool name (serverName_toolName).
func (t *ClientTool) Name() string {
	return t.serverName + "_" + t.mcpTool.Name
}

// Description returns the tool description.
func (t *ClientTool) Description() string {
	return t.mcpTool.Description
}

// Declaration returns the tool declaration.
func (t *ClientTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: mcputils.ConvertMCPSchema(t.mcpTool.InputSchema),
	}
}

// Call dials a fresh MCP connection, executes the tool, and closes the
// connection. Each invocation is fully independent — no shared state.
func (t *ClientTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	caller, cleanup, err := t.dialer(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial MCP %s: %w", t.serverName, err)
	}
	defer cleanup()

	var args map[string]interface{}
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return nil, fmt.Errorf("invalid JSON input for tool %s: %w", t.Name(), err)
		}
	} else {
		args = make(map[string]interface{})
	}

	// Strip _justification — LLMs inject this field based on sub-agent
	// instructions but MCP servers don't expect it and may reject it
	// as an unknown field (e.g. "error converting arguments: input is invalid").
	delete(args, "_justification")

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      t.mcpTool.Name, // Use the original MCP tool name, not the namespaced one
			Arguments: args,
		},
	}

	resp, err := caller.CallTool(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert content to string
	// MCP response content is a list of Content objects (Text or Image)
	// We concatenate text content.
	var resultStr string
	for _, content := range resp.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			resultStr += textContent.Text + "\n"
		} else if imgContent, ok := content.(mcp.ImageContent); ok {
			resultStr += fmt.Sprintf("[Image: %s]\n", imgContent.Data)
		}
	}

	return resultStr, nil
}
