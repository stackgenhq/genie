package regextool

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the regex tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the regex tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the regex tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	r := newRegexTools()
	return []tool.Tool{r.regexTool()}
}
