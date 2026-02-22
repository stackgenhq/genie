package clarify

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps a clarify Store and EventEmitter and satisfies the
// tools.ToolProviders interface so the clarify tool can be passed directly
// to tools.NewRegistry.
type ToolProvider struct {
	store   Store
	emitter EventEmitter
	opts    []ToolOption
}

// NewToolProvider creates a ToolProvider for the ask_clarifying_question tool.
func NewToolProvider(store Store, emitter EventEmitter, opts ...ToolOption) *ToolProvider {
	return &ToolProvider{store: store, emitter: emitter, opts: opts}
}

// GetTools returns the clarify tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{NewTool(p.store, p.emitter, p.opts...)}
}
