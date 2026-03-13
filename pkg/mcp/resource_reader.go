// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

//go:generate go tool counterfeiter -generate

// MCPResourceReader is the subset of the MCP client interface used for reading
// resources. Extracting this allows unit testing with counterfeiter fakes and
// enables the datasource layer to read MCP resources without a full Client.
//
//counterfeiter:generate . MCPResourceReader
type MCPResourceReader interface {
	ListResources(ctx context.Context, req mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error)
	ReadResource(ctx context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
}

// GetResourceReader returns the MCPResourceReader for the given MCP server name.
// Returns nil and false if no server with that name was initialized.
func (c *Client) GetResourceReader(serverName string) (MCPResourceReader, bool) {
	if c == nil {
		return nil, false
	}
	reader, ok := c.resourceReaders[serverName]
	return reader, ok
}
