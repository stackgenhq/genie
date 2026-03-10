package cron

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an ICronStore and satisfies the tools.ToolProviders
// interface so the cron tools can be passed directly to tools.NewRegistry.
type ToolProvider struct {
	store      ICronStore
	dispatcher EventDispatcher
}

// NewToolProvider creates a ToolProvider for cron management tools.
func NewToolProvider(store ICronStore) *ToolProvider {
	return &ToolProvider{store: store}
}

// SetDispatcher injects the EventDispatcher after construction. This is
// needed because the dispatcher is created in app.Start() after the tool
// registry is built. When set, the trigger_recurring_task tool becomes available.
func (p *ToolProvider) SetDispatcher(dispatcher EventDispatcher) {
	p.dispatcher = dispatcher
}

// GetTools returns the cron management tool suite: create, list, delete,
// history, toggle (pause/resume), and trigger (run-now).
func (p *ToolProvider) GetTools() []tool.Tool {
	tools := []tool.Tool{
		NewCreateRecurringTaskTool(p.store),
		NewListRecurringTasksTool(p.store),
		NewDeleteRecurringTaskTool(p.store),
		NewHistoryRecurringTaskTool(p.store),
		NewToggleRecurringTaskTool(p.store),
	}
	if p.dispatcher != nil {
		tools = append(tools, NewTriggerRecurringTaskTool(p.store, p.dispatcher))
	}
	return tools
}
