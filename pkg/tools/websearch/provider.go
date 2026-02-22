package websearch

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps websearch configuration and satisfies the
// tools.ToolProviders interface so it can be passed directly to
// tools.NewRegistry. Without this, websearch tools would need to be
// constructed inline inside the registry.
type ToolProvider struct {
	cfg  Config
	opts []DDGOption
}

// NewToolProvider creates a ToolProvider from the given websearch config.
func NewToolProvider(cfg Config, opts ...DDGOption) *ToolProvider {
	return &ToolProvider{cfg: cfg, opts: opts}
}

// GetTools returns the websearch tool configured from the provider's config.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{
		NewTool(p.cfg, p.opts...),
	}
}
