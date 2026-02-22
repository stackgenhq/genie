package snowflake

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps a Snowflake Service and satisfies the tools.ToolProviders
// interface so Snowflake tools can be passed directly to tools.NewRegistry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised Snowflake service.
// Callers are responsible for creating and validating the service beforehand.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all Snowflake tools wired to the underlying service.
func (p *ToolProvider) GetTools() []tool.Tool {
	return AllTools(p.svc)
}
