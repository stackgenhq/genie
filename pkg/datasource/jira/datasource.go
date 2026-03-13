// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package jira provides a DataSource connector for Jira (Atlassian) that
// reads issues, comments, and sprint data via MCP resources. It delegates to
// the generic mcpresource.MCPResourceConnector using the configured Jira MCP server.
// Resources are filtered client-side by project key when scope.JiraProjectKeys
// is set.
package jira

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	genieMCP "github.com/stackgenhq/genie/pkg/mcp"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/mcpresource"
)

const sourceName = "jira"

// NewJiraConnector returns a DataSource that reads Jira resources via MCP.
// The reader should be obtained from mcp.Client.GetResourceReader() for the
// Jira MCP server configured in [mcp]. When scope.JiraProjectKeys is set,
// only resources whose URI contains one of the project keys are returned.
func NewJiraConnector(reader genieMCP.MCPResourceReader) *mcpresource.MCPResourceConnector {
	return mcpresource.NewMCPResourceConnector(reader, sourceName,
		mcpresource.WithScopeFilter(jiraScopeFilter),
	)
}

// jiraScopeFilter returns true if the resource matches at least one project
// key in scope. When JiraProjectKeys is empty, all resources are included.
func jiraScopeFilter(res mcp.Resource, scope datasource.Scope) bool {
	keys := scope.Get("jira")
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
