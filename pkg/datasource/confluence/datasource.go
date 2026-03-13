// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package confluence provides a DataSource connector for Confluence (Atlassian)
// that reads pages, blog posts, and comments via MCP resources. It delegates to
// the generic mcpresource.MCPResourceConnector using the configured Confluence MCP server.
// Resources are filtered client-side by space key when scope has confluence space
// keys set.
package confluence

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/mcpresource"
)

const sourceName = "confluence"

func init() {
	mcpresource.RegisterScopeFilter(sourceName, confluenceScopeFilter)
}

// NewConfluenceConnector returns a DataSource that reads Confluence resources via MCP.
// The reader must satisfy the mcpresource.Reader interface (e.g. an *mcp.Client
// or a test fake). When scope.Get("confluence") returns space keys, only resources
// whose URI or Name contains one of the space keys are returned.
func NewConfluenceConnector(reader mcpresource.Reader) *mcpresource.MCPResourceConnector {
	return mcpresource.NewMCPResourceConnector(reader, sourceName,
		mcpresource.WithScopeFilter(confluenceScopeFilter),
	)
}

// confluenceScopeFilter returns true if the resource matches at least one
// space key in scope. When ConfluenceSpaceKeys is empty, all resources are included.
func confluenceScopeFilter(res mcp.Resource, scope datasource.Scope) bool {
	keys := scope.Get("confluence")
	if len(keys) == 0 {
		return true
	}
	uriUpper := strings.ToUpper(res.URI)
	nameUpper := strings.ToUpper(res.Name)
	for _, key := range keys {
		keyUpper := strings.ToUpper(key)
		if strings.Contains(uriUpper, keyUpper) || strings.Contains(nameUpper, keyUpper) {
			return true
		}
	}
	return false
}
