// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an IStore and optional Config, and satisfies the
// tools.ToolProviders interface so vector memory tools can be passed
// directly to tools.NewRegistry. When cfg is non-nil and
// AllowedMetadataKeys is set, memory_store and memory_search only
// accept those keys for metadata and filter (product/category buckets).
type ToolProvider struct {
	store IStore
	cfg   *Config
}

// NewToolProvider creates a ToolProvider for the vector memory tools
// (memory_store and memory_search). cfg may be nil; when set with
// AllowedMetadataKeys, only those metadata keys are allowed.
func NewToolProvider(store IStore, cfg *Config) *ToolProvider {
	return &ToolProvider{store: store, cfg: cfg}
}

// GetTools returns the memory store and memory search tools.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{
		NewMemoryStoreTool(p.store, p.cfg),
		NewMemorySearchTool(p.store, p.cfg),
	}
}
