package datetime

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the datetime tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the datetime tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the datetime tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	d := newDatetimeTools()
	return []tool.Tool{d.datetimeTool()}
}
