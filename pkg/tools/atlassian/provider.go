package atlassian

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an Atlassian Service and satisfies the tools.ToolProviders
// interface so Atlassian tools can be passed directly to tools.NewRegistry.
// Without this, Atlassian tool construction would be inlined in the registry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised Atlassian service.
// Callers are responsible for creating and validating the service beforehand.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all Atlassian (Jira + Confluence) tools wired to the underlying service.
func (p *ToolProvider) GetTools() []tool.Tool {
	return AllTools(p.svc)
}
