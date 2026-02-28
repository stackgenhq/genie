// Package hooks defines lifecycle hook interfaces for the ReAcTree execution engine.
//
// Hooks are observer-style callbacks invoked at well-defined points during
// tree execution. They allow external packages (audit, telemetry, debugging)
// to react to execution lifecycle events without modifying the core loop.
//
// Key design decisions:
//   - Event structs use primitive types only — no reactree imports allowed,
//     preventing circular dependencies.
//   - Hooks are fire-and-forget (no return values that influence control flow).
//     Control-flow decisions like reflection-based halting belong in the
//     reflector interface, not in hooks.
//   - All methods must be safe to call concurrently.
//   - Implementations should be cheap; expensive work should be dispatched
//     asynchronously.
package hooks

import "context"

// IterationStartEvent is fired at the beginning of each adaptive loop iteration.
type IterationStartEvent struct {
	Goal          string
	Iteration     int
	MaxIterations int
}

// IterationEndEvent is fired at the end of each adaptive loop iteration.
type IterationEndEvent struct {
	Iteration      int
	Status         string         // "running", "success", "failure"
	ToolCallCounts map[string]int // per-tool call counts for this iteration
	TaskCompleted  bool
	Output         string
}

// ReflectionEvent is fired after a RAR reflection step completes.
type ReflectionEvent struct {
	Iteration     int
	Monologue     string
	ShouldProceed bool
}

// ToolValidationEvent is fired when the critic middleware validates a tool call.
type ToolValidationEvent struct {
	ToolName string
	Allowed  bool
	Reason   string // non-empty when Allowed=false
}

// DryRunEvent is fired when a dry run simulation completes.
type DryRunEvent struct {
	PlannedSteps  int
	ToolsUsed     []string
	EstimatedCost string
}

// PlanExecutionEvent is fired when the orchestrator starts executing a plan.
type PlanExecutionEvent struct {
	Flow      string // "sequence", "parallel", "fallback"
	StepCount int
	StepNames []string
}

// ExecutionHook defines lifecycle callbacks for ReAcTree execution.
// All methods are optional — implementations may embed NoOpHook to get
// default no-op behavior and override only the hooks they care about.
//
//counterfeiter:generate . ExecutionHook
type ExecutionHook interface {
	// OnIterationStart is called before each adaptive loop iteration executes.
	OnIterationStart(ctx context.Context, event IterationStartEvent)

	// OnIterationEnd is called after each adaptive loop iteration completes,
	// with the iteration's result status and tool usage.
	OnIterationEnd(ctx context.Context, event IterationEndEvent)

	// OnReflection is called after a RAR reflection step completes.
	OnReflection(ctx context.Context, event ReflectionEvent)

	// OnToolValidation is called when the critic middleware evaluates a tool call.
	OnToolValidation(ctx context.Context, event ToolValidationEvent)

	// OnDryRun is called when a dry run simulation completes.
	OnDryRun(ctx context.Context, event DryRunEvent)

	// OnPlanExecution is called when the orchestrator starts executing a plan.
	OnPlanExecution(ctx context.Context, event PlanExecutionEvent)
}

// NoOpHook is a default implementation that does nothing.
// Embed this in custom hook implementations to avoid implementing every method.
type NoOpHook struct{}

func (NoOpHook) OnIterationStart(_ context.Context, _ IterationStartEvent) {}
func (NoOpHook) OnIterationEnd(_ context.Context, _ IterationEndEvent)     {}
func (NoOpHook) OnReflection(_ context.Context, _ ReflectionEvent)         {}
func (NoOpHook) OnToolValidation(_ context.Context, _ ToolValidationEvent) {}
func (NoOpHook) OnDryRun(_ context.Context, _ DryRunEvent)                 {}
func (NoOpHook) OnPlanExecution(_ context.Context, _ PlanExecutionEvent)   {}

// Ensure NoOpHook satisfies the interface at compile time.
var _ ExecutionHook = NoOpHook{}
