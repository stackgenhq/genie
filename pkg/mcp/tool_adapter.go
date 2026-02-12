package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/appcd-dev/genie/pkg/mcputils"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ClientTool wraps an MCP Client and Tool to implement the trpc-agent-go tool.Tool interface.
type ClientTool struct {
	client *client.Client
	tool   mcp.Tool
}

// NewClientTool creates a new ClientTool wrapper.
func NewClientTool(client *client.Client, mcpTool mcp.Tool) *ClientTool {
	return &ClientTool{
		client: client,
		tool:   mcpTool,
	}
}

// Name returns the tool name.
func (t *ClientTool) Name() string {
	return t.tool.Name
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
			Name:      t.Name(),
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
