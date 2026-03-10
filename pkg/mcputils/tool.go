// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcputils

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// MCPTool wraps an MCP ServerTool to implement the trpc-agent-go tool.Tool interface
type MCPTool struct {
	mcpTool server.ServerTool
}

// NewMCPTool creates a new MCPTool wrapper around an MCP ServerTool
func NewMCPTool(mcpTool server.ServerTool) *MCPTool {
	return &MCPTool{
		mcpTool: mcpTool,
	}
}

// Declaration returns the tool declaration with converted schema
func (t *MCPTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        t.mcpTool.Tool.Name,
		Description: t.mcpTool.Tool.Description,
		InputSchema: ConvertMCPSchema(t.mcpTool.Tool.InputSchema),
	}
}

// Call executes the MCP tool with the provided arguments
func (t *MCPTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	// Create MCP request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      t.mcpTool.Tool.Name,
			Arguments: args,
		},
	}

	// Call MCP handler
	result, err := t.mcpTool.Handler(ctx, request)
	if err != nil {
		return nil, err
	}

	// Extract text content
	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			return textContent.Text, nil
		}
	}

	// Fallback: return the whole result
	return result, nil
}
