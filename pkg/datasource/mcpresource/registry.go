// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcpresource

import "sync"

var (
	scopeFilterMu     sync.RWMutex
	registeredFilters = map[string]ScopeFilter{}
)

// RegisterScopeFilter registers a ScopeFilter for the given source name.
// Call from an init() function in source-specific packages (e.g. jira, confluence)
// so the filter is automatically applied when an MCP server with that name is
// used as a data source. Subsequent calls override any previously registered filter.
func RegisterScopeFilter(sourceName string, fn ScopeFilter) {
	scopeFilterMu.Lock()
	defer scopeFilterMu.Unlock()
	registeredFilters[sourceName] = fn
}

// registeredFilter returns the ScopeFilter for sourceName, or nil if none registered.
func registeredFilter(sourceName string) ScopeFilter {
	scopeFilterMu.RLock()
	defer scopeFilterMu.RUnlock()
	return registeredFilters[sourceName]
}

// NewFromServerName returns a DataSource connector for an MCP server using
// any registered scope filter for the given source name. When no filter is
// registered, all resources from the server are included.
func NewFromServerName(reader Reader, sourceName string) *MCPResourceConnector {
	opts := []Option{}
	if f := registeredFilter(sourceName); f != nil {
		opts = append(opts, WithScopeFilter(f))
	}
	return NewMCPResourceConnector(reader, sourceName, opts...)
}
