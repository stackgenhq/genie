// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hooks_test

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/hooks"
)

// recordingHook records which events were called for test assertions.
type recordingHook struct {
	hooks.NoOpHook
	iterStartCalls      int
	iterEndCalls        int
	reflectCalls        int
	toolValCalls        int
	dryRunCalls         int
	planExecCalls       int
	contextBudgetCalls  int
	compactionMissCalls int
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
func (r *recordingHook) OnContextBudget(_ context.Context, _ hooks.ContextBudgetEvent) {
	r.contextBudgetCalls++
}
func (r *recordingHook) OnCompactionMiss(_ context.Context, _ hooks.CompactionMissEvent) {
	r.compactionMissCalls++
}

var _ = Describe("Hooks", func() {
	Describe("NoOpHook", func() {
		It("should not panic on any method including OnContextBudget and OnCompactionMiss", func(ctx context.Context) {
			noop := hooks.NoOpHook{}
			noop.OnIterationStart(ctx, hooks.IterationStartEvent{})
			noop.OnIterationEnd(ctx, hooks.IterationEndEvent{})
			noop.OnReflection(ctx, hooks.ReflectionEvent{})
			noop.OnToolValidation(ctx, hooks.ToolValidationEvent{})
			noop.OnDryRun(ctx, hooks.DryRunEvent{})
			noop.OnPlanExecution(ctx, hooks.PlanExecutionEvent{})
			noop.OnContextBudget(ctx, hooks.ContextBudgetEvent{})
			noop.OnCompactionMiss(ctx, hooks.CompactionMissEvent{})
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
				chain.OnContextBudget(ctx, hooks.ContextBudgetEvent{TotalTokens: 5000})
				chain.OnCompactionMiss(ctx, hooks.CompactionMissEvent{ToolName: "read_file"})

				for _, h := range []*recordingHook{h1, h2} {
					Expect(h.iterStartCalls).To(Equal(1))
					Expect(h.iterEndCalls).To(Equal(1))
					Expect(h.reflectCalls).To(Equal(1))
					Expect(h.toolValCalls).To(Equal(1))
					Expect(h.dryRunCalls).To(Equal(1))
					Expect(h.planExecCalls).To(Equal(1))
					Expect(h.contextBudgetCalls).To(Equal(1))
					Expect(h.compactionMissCalls).To(Equal(1))
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
				chain.OnContextBudget(ctx, hooks.ContextBudgetEvent{})
				chain.OnCompactionMiss(ctx, hooks.CompactionMissEvent{})
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

	Describe("EstimatePersonaTokens", func() {
		It("returns roughly len/4 for a non-empty string", func() {
			// Arrange
			text := strings.Repeat("a", 100)

			// Act
			tokens := hooks.EstimatePersonaTokens(text)

			// Assert
			Expect(tokens).To(Equal(25))
		})

		It("returns 0 for an empty string", func() {
			Expect(hooks.EstimatePersonaTokens("")).To(Equal(0))
		})
	})

	Describe("EstimateToolSchemaTokens", func() {
		It("sums name and description lengths then divides by 4", func() {
			// Arrange — two tools with 8-char names and 12-char descriptions
			names := []string{"read_fil", "run_shel"}
			descs := []string{"reads a file", "runs a shell"}

			// Act
			tokens := hooks.EstimateToolSchemaTokens(names, descs)

			// Assert — total chars = 8+12 + 8+12 = 40 → 40/4 = 10
			Expect(tokens).To(Equal(10))
		})

		It("handles more names than descriptions gracefully", func() {
			// Arrange — 2 names but only 1 description
			names := []string{"tool_a", "tool_b"}
			descs := []string{"description_a"}

			// Act
			tokens := hooks.EstimateToolSchemaTokens(names, descs)

			// Assert — 6+13 + 6 = 25 → 25/4 = 6
			Expect(tokens).To(Equal(6))
		})

		It("returns 0 for empty slices", func() {
			Expect(hooks.EstimateToolSchemaTokens(nil, nil)).To(Equal(0))
		})
	})

	Describe("EstimateHistoryTokens", func() {
		It("returns roughly len/4 for a non-empty prompt", func() {
			prompt := strings.Repeat("x", 200)
			Expect(hooks.EstimateHistoryTokens(prompt)).To(Equal(50))
		})

		It("returns 0 for an empty prompt", func() {
			Expect(hooks.EstimateHistoryTokens("")).To(Equal(0))
		})
	})

	Describe("NewContextBudgetEvent", func() {
		It("populates all fields correctly", func() {
			// Arrange
			persona := 100
			tools := 200
			history := 300

			// Act
			event := hooks.NewContextBudgetEvent(persona, tools, history)

			// Assert
			Expect(event.PersonaTokens).To(Equal(100))
			Expect(event.ToolSchemaTokens).To(Equal(200))
			Expect(event.HistoryTokens).To(Equal(300))
			Expect(event.TotalTokens).To(Equal(600))
			Expect(event.MaxTokens).To(Equal(128_000))
			Expect(event.UtilizationPct).To(BeNumerically("~", 600.0/128_000.0, 1e-9))
		})
	})

	Describe("IsOverBudget", func() {
		It("returns true when utilization exceeds 85%", func() {
			event := hooks.ContextBudgetEvent{UtilizationPct: 0.90}
			Expect(event.IsOverBudget()).To(BeTrue())
		})

		It("returns false when utilization is at exactly 85%", func() {
			event := hooks.ContextBudgetEvent{UtilizationPct: 0.85}
			Expect(event.IsOverBudget()).To(BeFalse())
		})

		It("returns false when utilization is below 85%", func() {
			event := hooks.ContextBudgetEvent{UtilizationPct: 0.50}
			Expect(event.IsOverBudget()).To(BeFalse())
		})
	})
})
