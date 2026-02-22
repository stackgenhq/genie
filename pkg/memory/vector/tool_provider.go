package vector

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an IStore and satisfies the tools.ToolProviders
// interface so vector memory tools can be passed directly to
// tools.NewRegistry.
type ToolProvider struct {
	store IStore
}

// NewToolProvider creates a ToolProvider for the vector memory tools
// (memory_store and memory_search).
func NewToolProvider(store IStore) *ToolProvider {
	return &ToolProvider{store: store}
}

// GetTools returns the memory store and memory search tools.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{
		NewMemoryStoreTool(p.store),
		NewMemorySearchTool(p.store),
	}
}
