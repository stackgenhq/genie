// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package confluence provides a DataSource connector for Confluence (Atlassian)
// that reads pages, blog posts, and comments via MCP resources. It delegates to
// the generic mcpresource.MCPResourceConnector using the configured Confluence MCP server.
// Resources are filtered client-side by space key when scope.ConfluenceSpaceKeys
// is set.
package confluence

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	genieMCP "github.com/stackgenhq/genie/pkg/mcp"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/mcpresource"
)

const sourceName = "confluence"

// NewConfluenceConnector returns a DataSource that reads Confluence resources via MCP.
// The reader should be obtained from mcp.Client.GetResourceReader() for the
// Confluence MCP server configured in [mcp]. When scope.ConfluenceSpaceKeys is set,
// only resources whose URI contains one of the space keys are returned.
func NewConfluenceConnector(reader genieMCP.MCPResourceReader) *mcpresource.MCPResourceConnector {
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
