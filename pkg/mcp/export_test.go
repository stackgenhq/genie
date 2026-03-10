// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ShouldIncludeToolForTest exports shouldIncludeTool for tests only. Not part of the public API.
func (c *Client) ShouldIncludeToolForTest(toolName string, config MCPServerConfig) bool {
	return c.shouldIncludeTool(toolName, config)
}

// BuildStdioEnvForTest exports buildStdioEnv for tests only. Not part of the public API.
func (c *Client) BuildStdioEnvForTest(ctx context.Context, config MCPServerConfig) []string {
	return c.buildStdioEnv(ctx, config)
}

// ExpandEnvValueForTest exports expandEnvValue for tests only. Not part of the public API.
func (c *Client) ExpandEnvValueForTest(ctx context.Context, value string) string {
	return c.expandEnvValue(ctx, value)
}

// NewClientForTest creates a Client with the given options for testing.
// It skips config validation and server initialization.
func NewClientForTest(opts ...ClientOption) *Client {
	c := &Client{
		tools: make([]tool.Tool, 0),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
