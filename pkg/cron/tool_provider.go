package cron

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an ICronStore and satisfies the tools.ToolProviders
// interface so the cron tool can be passed directly to tools.NewRegistry.
type ToolProvider struct {
	store ICronStore
}

// NewToolProvider creates a ToolProvider for the create_recurring_task tool.
func NewToolProvider(store ICronStore) *ToolProvider {
	return &ToolProvider{store: store}
}

// GetTools returns the cron recurring task tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{NewCreateRecurringTaskTool(p.store)}
}
