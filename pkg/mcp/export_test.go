// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"time"
)

// ShouldIncludeToolForTest exports shouldIncludeTool for tests only. Not part of the public API.
func (c *Client) ShouldIncludeToolForTest(toolName string, config MCPServerConfig) bool {
	return c.shouldIncludeTool(toolName, config)
}

// BuildStdioEnvForTest exports buildStdioEnvFromConfig for tests only. Not part of the public API.
func (c *Client) BuildStdioEnvForTest(ctx context.Context, config MCPServerConfig) []string {
	return buildStdioEnvFromConfig(ctx, config, c.secretProvider)
}

// ExpandEnvValueForTest exports expandEnvWithProvider for tests only. Not part of the public API.
func (c *Client) ExpandEnvValueForTest(ctx context.Context, value string) string {
	return expandEnvWithProvider(ctx, value, c.secretProvider)
}

// NewClientForTest creates a Client with the given options for testing.
// It skips config validation and server initialization.
func NewClientForTest(opts ...ClientOption) *Client {
	c := &Client{
		cacheTTL:      defaultCacheTTL,
		failedServers: make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
