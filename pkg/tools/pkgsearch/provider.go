package pkgsearch

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the package search tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the package search tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the package search tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	t := newPkgTools()
	return []tool.Tool{t.searchTool()}
}
