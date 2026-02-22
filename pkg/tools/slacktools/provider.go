package slacktools

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps a Slack Service and satisfies the tools.ToolProviders
// interface so Slack tools can be passed directly to tools.NewRegistry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised Slack service.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all Slack tools wired to the underlying service.
func (p *ToolProvider) GetTools() []tool.Tool {
	return AllTools(p.svc)
}
