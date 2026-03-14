// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps an IStore and optional Config, and satisfies the
// tools.ToolProviders interface so vector memory tools can be passed
// directly to tools.NewRegistry. When cfg is non-nil and
// AllowedMetadataKeys is set, memory_store and memory_search only
// accept those keys for metadata and filter (product/category buckets).
type ToolProvider struct {
	store  IStore
	cfg    *Config
	scorer MemoryImportanceScorer
}

// NewToolProvider creates a ToolProvider for the vector memory tools
// (memory_store and memory_search). cfg may be nil; when set with
// AllowedMetadataKeys, only those metadata keys are allowed.
// scorer may be nil; when set, memory_store writes are scored for
// importance and the score is stored as "_importance" metadata.
func NewToolProvider(store IStore, cfg *Config, scorer MemoryImportanceScorer) *ToolProvider {
	return &ToolProvider{store: store, cfg: cfg, scorer: scorer}
}

// GetTools returns the memory store and memory search tools.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	return []tool.Tool{
		NewMemoryStoreTool(p.store, p.cfg, p.scorer),
		NewMemorySearchTool(p.store, p.cfg),
		NewMemoryDeleteTool(p.store),
		NewMemoryListTool(p.store, p.cfg),
		NewMemoryMergeTool(p.store, p.cfg),
	}
}
