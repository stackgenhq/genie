package email

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an email Service and satisfies the tools.ToolProviders
// interface so email tools can be passed directly to tools.NewRegistry.
// Without this, email tool construction would be inlined in the registry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised email service.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all email tools (send + read) wired to the underlying service.
func (p *ToolProvider) GetTools() []tool.Tool {
	return AllTools(p.svc)
}
