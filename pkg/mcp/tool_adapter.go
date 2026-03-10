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

// ClientTool wraps an MCP Client and Tool to implement the trpc-agent-go tool.Tool interface.
// Tools are namespaced by server name to avoid collisions when multiple MCP
// servers expose tools with the same name (e.g. "search").
type ClientTool struct {
	client     MCPCaller
	tool       mcp.Tool
	serverName string
}

// NewClientTool creates a new ClientTool wrapper.
// The serverName is used to prefix the tool name so that tools from different
// MCP servers are disambiguated (e.g. "github_search" vs "jira_search").
func NewClientTool(caller MCPCaller, mcpTool mcp.Tool, serverName string) *ClientTool {
	return &ClientTool{
		client:     caller,
		tool:       mcpTool,
		serverName: serverName,
	}
}

// Name returns the namespaced tool name (serverName_toolName).
func (t *ClientTool) Name() string {
	return t.serverName + "_" + t.tool.Name
}

// Description returns the tool description.
func (t *ClientTool) Description() string {
	return t.tool.Description
}

// Declaration returns the tool declaration.
func (t *ClientTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: mcputils.ConvertMCPSchema(t.tool.InputSchema),
	}
}

// Call executes the tool using the MCP client.
func (t *ClientTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args map[string]interface{}
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return nil, fmt.Errorf("invalid JSON input for tool %s: %w", t.Name(), err)
		}
	} else {
		args = make(map[string]interface{})
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      t.tool.Name, // Use the original MCP tool name, not the namespaced one
			Arguments: args,
		},
	}

	resp, err := t.client.CallTool(ctx, req)
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
