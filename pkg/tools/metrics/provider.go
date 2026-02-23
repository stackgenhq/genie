package metrics

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the metrics tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the metrics tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the metrics tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	m := newMetricsTools()
	return []tool.Tool{m.metricsTool()}
}
