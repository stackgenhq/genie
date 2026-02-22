package runbook

import (
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps a vector IStore and satisfies the tools.ToolProviders
// interface so the search_runbook tool can be passed directly to
// tools.NewRegistry.
type ToolProvider struct {
	store vector.IStore
}

// NewToolProvider creates a ToolProvider for the runbook search tool.
func NewToolProvider(store vector.IStore) *ToolProvider {
	return &ToolProvider{store: store}
}

// GetTools returns the search_runbook tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{NewSearchTool(p.store)}
}
