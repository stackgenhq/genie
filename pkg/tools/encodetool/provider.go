package encodetool

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the encode tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the encode tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the encode tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	e := newEncodeTools()
	return []tool.Tool{e.encodeTool()}
}
