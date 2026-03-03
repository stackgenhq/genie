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

// ContextBudgetEvent is fired before making a model request to report token usage.
// Use NewContextBudgetEvent to construct one from raw inputs; the helper
// functions EstimatePersonaTokens / EstimateToolSchemaTokens /
// EstimateHistoryTokens perform the heuristic word-to-token conversion.
type ContextBudgetEvent struct {
	PersonaTokens    int
	ToolSchemaTokens int
	HistoryTokens    int
	TotalTokens      int
	MaxTokens        int
	UtilizationPct   float64
}

// defaultMaxTokens is the generic fallback context window size used when the
// model's actual limit is not exposed by trpc-agent-go.
const defaultMaxTokens = 128_000

// budgetWarningThreshold is the utilization fraction above which a warning
// is emitted. 0.85 = 85%.
const budgetWarningThreshold = 0.85

// EstimatePersonaTokens returns a rough token count for the persona text.
// Uses the common heuristic of ~4 chars per token.
func EstimatePersonaTokens(personaText string) int {
	return len(personaText) / 4
}

// EstimateToolSchemaTokens returns a rough token count for a set of tool
// declarations by summing name + description lengths.
func EstimateToolSchemaTokens(names []string, descriptions []string) int {
	total := 0
	for i := range names {
		total += len(names[i])
		if i < len(descriptions) {
			total += len(descriptions[i])
		}
	}
	return total / 4
}

// EstimateHistoryTokens returns a rough token count for the prompt / message
// history string.
func EstimateHistoryTokens(prompt string) int {
	return len(prompt) / 4
}

// NewContextBudgetEvent constructs a fully populated ContextBudgetEvent from
// the three token components.
func NewContextBudgetEvent(personaTokens, toolSchemaTokens, historyTokens int) ContextBudgetEvent {
	total := personaTokens + toolSchemaTokens + historyTokens
	return ContextBudgetEvent{
		PersonaTokens:    personaTokens,
		ToolSchemaTokens: toolSchemaTokens,
		HistoryTokens:    historyTokens,
		TotalTokens:      total,
		MaxTokens:        defaultMaxTokens,
		UtilizationPct:   float64(total) / float64(defaultMaxTokens),
	}
}

// IsOverBudget reports whether context utilization exceeds the warning
// threshold (85%).
func (e ContextBudgetEvent) IsOverBudget() bool {
	return e.UtilizationPct > budgetWarningThreshold
}

// CompactionMissEvent is fired when a tool is re-invoked identically after its prior output was compressed.
type CompactionMissEvent struct {
	ToolName       string
	OriginalSize   int
	CompressedSize int
}

type compactionTrackerKey struct{}

// CompactionTrackerKey is the context key for passing a CompactionTracker.
var CompactionTrackerKey = compactionTrackerKey{}

// CompactionTracker allows middlewares to signal that compaction occurred
// and to query adaptive chunk-boost hints set by the loop on compaction misses.
type CompactionTracker interface {
	MarkCompressed(toolName string, originalSize, compressedSize int)
	// GetChunkBoost returns a multiplier (>1) for max_chunks on a tool that
	// previously caused a compaction miss, or 0 if no boost is needed.
	GetChunkBoost(toolName string) int
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

	// OnContextBudget is called before each model execution to report token utilization.
	OnContextBudget(ctx context.Context, event ContextBudgetEvent)

	// OnCompactionMiss is called when the runner detects a likely infinite loop caused by output compaction.
	OnCompactionMiss(ctx context.Context, event CompactionMissEvent)
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
func (NoOpHook) OnContextBudget(_ context.Context, _ ContextBudgetEvent)   {}
func (NoOpHook) OnCompactionMiss(_ context.Context, _ CompactionMissEvent) {}

// Ensure NoOpHook satisfies the interface at compile time.
var _ ExecutionHook = NoOpHook{}
