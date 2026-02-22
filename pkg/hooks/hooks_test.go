package hooks_test

import (
	"context"
	"testing"

	"github.com/appcd-dev/genie/pkg/hooks"
)

// recordingHook records which events were called for test assertions.
type recordingHook struct {
	hooks.NoOpHook
	iterStartCalls int
	iterEndCalls   int
	reflectCalls   int
	toolValCalls   int
	dryRunCalls    int
	planExecCalls  int
}

func (r *recordingHook) OnIterationStart(_ context.Context, _ hooks.IterationStartEvent) {
	r.iterStartCalls++
}
func (r *recordingHook) OnIterationEnd(_ context.Context, _ hooks.IterationEndEvent) {
	r.iterEndCalls++
}
func (r *recordingHook) OnReflection(_ context.Context, _ hooks.ReflectionEvent) { r.reflectCalls++ }
func (r *recordingHook) OnToolValidation(_ context.Context, _ hooks.ToolValidationEvent) {
	r.toolValCalls++
}
func (r *recordingHook) OnDryRun(_ context.Context, _ hooks.DryRunEvent) { r.dryRunCalls++ }
func (r *recordingHook) OnPlanExecution(_ context.Context, _ hooks.PlanExecutionEvent) {
	r.planExecCalls++
}

func TestNoOpHook(t *testing.T) {
	// NoOpHook should not panic on any method.
	noop := hooks.NoOpHook{}
	ctx := context.Background()
	noop.OnIterationStart(ctx, hooks.IterationStartEvent{})
	noop.OnIterationEnd(ctx, hooks.IterationEndEvent{})
	noop.OnReflection(ctx, hooks.ReflectionEvent{})
	noop.OnToolValidation(ctx, hooks.ToolValidationEvent{})
	noop.OnDryRun(ctx, hooks.DryRunEvent{})
	noop.OnPlanExecution(ctx, hooks.PlanExecutionEvent{})
}

func TestChainHook_FanOut(t *testing.T) {
	h1 := &recordingHook{}
	h2 := &recordingHook{}
	chain := hooks.NewChainHook(h1, h2)
	ctx := context.Background()

	chain.OnIterationStart(ctx, hooks.IterationStartEvent{Goal: "test", Iteration: 1})
	chain.OnIterationEnd(ctx, hooks.IterationEndEvent{Iteration: 1, Status: "success"})
	chain.OnReflection(ctx, hooks.ReflectionEvent{Iteration: 1})
	chain.OnToolValidation(ctx, hooks.ToolValidationEvent{ToolName: "read_file"})
	chain.OnDryRun(ctx, hooks.DryRunEvent{PlannedSteps: 3})
	chain.OnPlanExecution(ctx, hooks.PlanExecutionEvent{Flow: "sequence"})

	for _, h := range []*recordingHook{h1, h2} {
		if h.iterStartCalls != 1 {
			t.Errorf("expected 1 OnIterationStart call, got %d", h.iterStartCalls)
		}
		if h.iterEndCalls != 1 {
			t.Errorf("expected 1 OnIterationEnd call, got %d", h.iterEndCalls)
		}
		if h.reflectCalls != 1 {
			t.Errorf("expected 1 OnReflection call, got %d", h.reflectCalls)
		}
		if h.toolValCalls != 1 {
			t.Errorf("expected 1 OnToolValidation call, got %d", h.toolValCalls)
		}
		if h.dryRunCalls != 1 {
			t.Errorf("expected 1 OnDryRun call, got %d", h.dryRunCalls)
		}
		if h.planExecCalls != 1 {
			t.Errorf("expected 1 OnPlanExecution call, got %d", h.planExecCalls)
		}
	}
}

func TestChainHook_SkipsNil(t *testing.T) {
	h1 := &recordingHook{}
	chain := hooks.NewChainHook(nil, h1, nil)
	ctx := context.Background()

	chain.OnIterationStart(ctx, hooks.IterationStartEvent{})
	if h1.iterStartCalls != 1 {
		t.Errorf("expected 1 call despite nil entries, got %d", h1.iterStartCalls)
	}
}

func TestChainHook_Empty(t *testing.T) {
	chain := hooks.NewChainHook()
	ctx := context.Background()
	// Should not panic with zero hooks.
	chain.OnIterationStart(ctx, hooks.IterationStartEvent{})
	chain.OnIterationEnd(ctx, hooks.IterationEndEvent{})
}

func TestEventFields(t *testing.T) {
	// Verify event structs carry data correctly.
	event := hooks.IterationStartEvent{
		Goal:          "deploy",
		Iteration:     2,
		MaxIterations: 5,
	}
	if event.Goal != "deploy" {
		t.Errorf("expected Goal=deploy, got %s", event.Goal)
	}
	if event.Iteration != 2 || event.MaxIterations != 5 {
		t.Errorf("unexpected iteration values: %d/%d", event.Iteration, event.MaxIterations)
	}

	endEvent := hooks.IterationEndEvent{
		Iteration:      1,
		Status:         "failure",
		ToolCallCounts: map[string]int{"shell": 3},
		TaskCompleted:  false,
		Output:         "error occurred",
	}
	if endEvent.Status != "failure" {
		t.Errorf("expected Status=failure, got %s", endEvent.Status)
	}
	if endEvent.ToolCallCounts["shell"] != 3 {
		t.Errorf("expected shell=3, got %d", endEvent.ToolCallCounts["shell"])
	}
}
