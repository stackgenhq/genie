// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package servicenow provides a DataSource connector for ServiceNow that
// reads incidents, change requests, and knowledge articles via MCP resources.
// It delegates to the generic mcpresource.MCPResourceConnector using the
// configured ServiceNow MCP server. Resources are filtered client-side by
// table name when scope has servicenow table names set.
package servicenow

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/mcpresource"
)

const sourceName = "servicenow"

func init() {
	mcpresource.RegisterScopeFilter(sourceName, servicenowScopeFilter)
}

// NewServiceNowConnector returns a DataSource that reads ServiceNow resources via MCP.
// The reader must satisfy the mcpresource.Reader interface (e.g. an *mcp.Client
// or a test fake). When scope has servicenow table names set, only resources whose
// URI contains one of the table names are returned.
func NewServiceNowConnector(reader mcpresource.Reader) *mcpresource.MCPResourceConnector {
	return mcpresource.NewMCPResourceConnector(reader, sourceName,
		mcpresource.WithScopeFilter(servicenowScopeFilter),
	)
}

// servicenowScopeFilter returns true if the resource matches at least one
// table name in scope. When ServiceNowTableNames is empty, all resources are included.
func servicenowScopeFilter(res mcp.Resource, scope datasource.Scope) bool {
	tables := scope.Get("servicenow")
	if len(tables) == 0 {
		return true
	}
	uriLower := strings.ToLower(res.URI)
	for _, table := range tables {
		if strings.Contains(uriLower, strings.ToLower(table)) {
			return true
		}
	}
	return false
}
