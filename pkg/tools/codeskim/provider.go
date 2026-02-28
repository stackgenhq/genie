package codeskim

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the code skim tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the code skim tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the code skim tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	s := newSkimTools()
	return []tool.Tool{s.skimTool()}
}
