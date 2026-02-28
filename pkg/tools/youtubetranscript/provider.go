package youtubetranscript

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider satisfies the tools.ToolProviders interface for the
// YouTube transcript tool.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the youtube_transcript tool.
func NewToolProvider() *ToolProvider {
	return &ToolProvider{}
}

// GetTools returns the YouTube transcript tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{NewTool()}
}
