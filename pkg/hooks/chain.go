package hooks

import "context"

// ChainHook fans out lifecycle events to multiple ExecutionHook implementations.
// This allows composing audit hooks, telemetry hooks, debug hooks, etc. into
// a single injectable dependency.
type ChainHook struct {
	hooks []ExecutionHook
}

// NewChainHook creates a ChainHook from the provided hooks.
// Nil entries are silently skipped.
func NewChainHook(hooks ...ExecutionHook) *ChainHook {
	filtered := make([]ExecutionHook, 0, len(hooks))
	for _, h := range hooks {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	return &ChainHook{hooks: filtered}
}

func (c *ChainHook) OnIterationStart(ctx context.Context, event IterationStartEvent) {
	for _, h := range c.hooks {
		h.OnIterationStart(ctx, event)
	}
}

func (c *ChainHook) OnIterationEnd(ctx context.Context, event IterationEndEvent) {
	for _, h := range c.hooks {
		h.OnIterationEnd(ctx, event)
	}
}

func (c *ChainHook) OnReflection(ctx context.Context, event ReflectionEvent) {
	for _, h := range c.hooks {
		h.OnReflection(ctx, event)
	}
}

func (c *ChainHook) OnToolValidation(ctx context.Context, event ToolValidationEvent) {
	for _, h := range c.hooks {
		h.OnToolValidation(ctx, event)
	}
}

func (c *ChainHook) OnDryRun(ctx context.Context, event DryRunEvent) {
	for _, h := range c.hooks {
		h.OnDryRun(ctx, event)
	}
}

func (c *ChainHook) OnPlanExecution(ctx context.Context, event PlanExecutionEvent) {
	for _, h := range c.hooks {
		h.OnPlanExecution(ctx, event)
	}
}

func (c *ChainHook) OnContextBudget(ctx context.Context, event ContextBudgetEvent) {
	for _, h := range c.hooks {
		h.OnContextBudget(ctx, event)
	}
}

func (c *ChainHook) OnCompactionMiss(ctx context.Context, event CompactionMissEvent) {
	for _, h := range c.hooks {
		h.OnCompactionMiss(ctx, event)
	}
}

// Ensure ChainHook satisfies the interface.
var _ ExecutionHook = (*ChainHook)(nil)
