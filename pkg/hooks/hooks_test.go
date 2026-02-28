package hooks_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/hooks"
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
func (r *recordingHook) OnReflection(_ context.Context, _ hooks.ReflectionEvent) {
	r.reflectCalls++
}
func (r *recordingHook) OnToolValidation(_ context.Context, _ hooks.ToolValidationEvent) {
	r.toolValCalls++
}
func (r *recordingHook) OnDryRun(_ context.Context, _ hooks.DryRunEvent) {
	r.dryRunCalls++
}
func (r *recordingHook) OnPlanExecution(_ context.Context, _ hooks.PlanExecutionEvent) {
	r.planExecCalls++
}

var _ = Describe("Hooks", func() {
	Describe("NoOpHook", func() {
		It("should not panic on any method", func(ctx context.Context) {
			noop := hooks.NoOpHook{}
			noop.OnIterationStart(ctx, hooks.IterationStartEvent{})
			noop.OnIterationEnd(ctx, hooks.IterationEndEvent{})
			noop.OnReflection(ctx, hooks.ReflectionEvent{})
			noop.OnToolValidation(ctx, hooks.ToolValidationEvent{})
			noop.OnDryRun(ctx, hooks.DryRunEvent{})
			noop.OnPlanExecution(ctx, hooks.PlanExecutionEvent{})
		})
	})

	Describe("ChainHook", func() {
		Context("when multiple hooks are chained", func() {
			It("should fan out each event to all hooks", func(ctx context.Context) {
				h1 := &recordingHook{}
				h2 := &recordingHook{}
				chain := hooks.NewChainHook(h1, h2)

				chain.OnIterationStart(ctx, hooks.IterationStartEvent{Goal: "test", Iteration: 1})
				chain.OnIterationEnd(ctx, hooks.IterationEndEvent{Iteration: 1, Status: "success"})
				chain.OnReflection(ctx, hooks.ReflectionEvent{Iteration: 1})
				chain.OnToolValidation(ctx, hooks.ToolValidationEvent{ToolName: "read_file"})
				chain.OnDryRun(ctx, hooks.DryRunEvent{PlannedSteps: 3})
				chain.OnPlanExecution(ctx, hooks.PlanExecutionEvent{Flow: "sequence"})

				for _, h := range []*recordingHook{h1, h2} {
					Expect(h.iterStartCalls).To(Equal(1))
					Expect(h.iterEndCalls).To(Equal(1))
					Expect(h.reflectCalls).To(Equal(1))
					Expect(h.toolValCalls).To(Equal(1))
					Expect(h.dryRunCalls).To(Equal(1))
					Expect(h.planExecCalls).To(Equal(1))
				}
			})
		})

		Context("when the chain contains nil entries", func() {
			It("should skip nil and still invoke non-nil hooks", func(ctx context.Context) {
				h1 := &recordingHook{}
				chain := hooks.NewChainHook(nil, h1, nil)

				chain.OnIterationStart(ctx, hooks.IterationStartEvent{})
				Expect(h1.iterStartCalls).To(Equal(1))
			})
		})

		Context("when the chain is empty", func() {
			It("should not panic when invoking any method", func(ctx context.Context) {
				chain := hooks.NewChainHook()
				chain.OnIterationStart(ctx, hooks.IterationStartEvent{})
				chain.OnIterationEnd(ctx, hooks.IterationEndEvent{})
			})
		})
	})

	Describe("Event structs", func() {
		It("should carry IterationStartEvent data correctly", func() {
			event := hooks.IterationStartEvent{
				Goal:          "deploy",
				Iteration:     2,
				MaxIterations: 5,
			}
			Expect(event.Goal).To(Equal("deploy"))
			Expect(event.Iteration).To(Equal(2))
			Expect(event.MaxIterations).To(Equal(5))
		})

		It("should carry IterationEndEvent data correctly", func() {
			endEvent := hooks.IterationEndEvent{
				Iteration:      1,
				Status:         "failure",
				ToolCallCounts: map[string]int{"shell": 3},
				TaskCompleted:  false,
				Output:         "error occurred",
			}
			Expect(endEvent.Status).To(Equal("failure"))
			Expect(endEvent.ToolCallCounts["shell"]).To(Equal(3))
		})
	})
})
