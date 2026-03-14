// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps an IStore and satisfies the tools.ToolProviders
// interface so graph tools can be passed to tools.NewRegistry. Pass a non-nil
// store only when the graph is enabled; when store is nil, GetTools returns
// no tools (callers should not register the provider when graph is disabled).
type ToolProvider struct {
	store IStore
}

// NewToolProvider creates a ToolProvider for the graph tools. store must be
// non-nil when the graph is enabled; otherwise do not register this provider.
func NewToolProvider(store IStore) *ToolProvider {
	return &ToolProvider{store: store}
}

// GetTools returns graph_store and graph_query when store is non-nil. Returns nil
// when store is nil so that disabled graph does not add tools.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	if p.store == nil {
		return nil
	}
	return []tool.Tool{
		newGraphStoreTool(p.store),
		newGraphQueryTool(p.store),
	}
}
