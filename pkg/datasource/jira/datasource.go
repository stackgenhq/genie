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

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/mcpresource"
)

const sourceName = "jira"

func init() {
	mcpresource.RegisterScopeFilter(sourceName, jiraScopeFilter)
}

// NewJiraConnector returns a DataSource that reads Jira resources via MCP.
// The reader must satisfy the mcpresource.Reader interface (e.g. an *mcp.Client
// or a test fake). When scope has jira project keys set, only resources whose
// URI or Name contains one of the project keys are returned.
func NewJiraConnector(reader mcpresource.Reader) *mcpresource.MCPResourceConnector {
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
