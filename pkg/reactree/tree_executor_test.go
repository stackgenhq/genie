// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/reactree"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ── Helpers ──────────────────────────────────────────────────────────

// fakeActionReflector implements reactree.ActionReflector.
type fakeActionReflector struct {
	result reactree.ReflectionResult
}

func (f *fakeActionReflector) Reflect(_ context.Context, _ reactree.ReflectionRequest) (reactree.ReflectionResult, error) {
	return f.result, nil
}

// fakeExecutionHook implements hooks.ExecutionHook.
type fakeExecutionHook struct {
	onContextBudget func(context.Context, hooks.ContextBudgetEvent)
}

func (h *fakeExecutionHook) OnIterationStart(_ context.Context, _ hooks.IterationStartEvent) {}
func (h *fakeExecutionHook) OnIterationEnd(_ context.Context, _ hooks.IterationEndEvent)     {}
func (h *fakeExecutionHook) OnReflection(_ context.Context, _ hooks.ReflectionEvent)         {}
func (h *fakeExecutionHook) OnCompactionMiss(_ context.Context, _ hooks.CompactionMissEvent) {}
func (h *fakeExecutionHook) OnToolValidation(_ context.Context, _ hooks.ToolValidationEvent) {}
func (h *fakeExecutionHook) OnDryRun(_ context.Context, _ hooks.DryRunEvent)                 {}
func (h *fakeExecutionHook) OnPlanExecution(_ context.Context, _ hooks.PlanExecutionEvent)   {}
func (h *fakeExecutionHook) OnContextBudget(ctx context.Context, evt hooks.ContextBudgetEvent) {
	if h.onContextBudget != nil {
		h.onContextBudget(ctx, evt)
	}
}

// ── Helpers for building expert responses ────────────────────────────

func textResponse(content string) expert.Response {
	return expert.Response{
		Choices: []model.Choice{
			{Message: model.Message{Role: "assistant", Content: content}},
		},
	}
}

func toolCallResponse(content string, toolNames ...string) expert.Response {
	calls := make([]model.ToolCall, len(toolNames))
	for i, n := range toolNames {
		calls[i] = model.ToolCall{Function: model.FunctionDefinitionParam{Name: n}}
	}
	return expert.Response{
		Choices: []model.Choice{
			{Message: model.Message{
				Role: "assistant", Content: content, ToolCalls: calls,
			}},
		},
	}
}

func textResponseWithUsage(content string, total int) expert.Response {
	r := textResponse(content)
	r.Usage = &model.Usage{TotalTokens: total, PromptTokens: total / 2, CompletionTokens: total / 2}
	return r
}

// ── Tests ────────────────────────────────────────────────────────────

var _ = Describe("TreeExecutor", func() {
	var (
		fakeExpert   *expertfakes.FakeExpert
		fakeEpisodic *memoryfakes.FakeEpisodicMemory
	)

	BeforeEach(func() {
		fakeExpert = &expertfakes.FakeExpert{}
		fakeEpisodic = &memoryfakes.FakeEpisodicMemory{}
	})

	run := func(config reactree.TreeConfig, req reactree.TreeRequest) (reactree.TreeResult, error) {
		return reactree.NewTreeExecutor(
			fakeExpert, memory.NewWorkingMemory(), fakeEpisodic, config,
		).Run(context.Background(), req)
	}

	Describe("Reflection", func() {
		It("halts when reflector says stop", func() {
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 5
			config.Stages = nil
			config.Toggles.Reflector = &fakeActionReflector{
				result: reactree.ReflectionResult{ShouldProceed: false, Monologue: "Dangerous. Halt."},
			}
			fakeExpert.DoReturns(toolCallResponse("Running dangerous", ""), nil)

			result, err := run(config, reactree.TreeRequest{Goal: "dangerous"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Failure))
			Expect(result.Output).To(ContainSubstring("halted"))
		})

		It("proceeds when reflector says ok", func() {
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 2
			config.Stages = nil
			config.Toggles.Reflector = &fakeActionReflector{
				result: reactree.ReflectionResult{ShouldProceed: true},
			}
			fakeExpert.DoReturnsOnCall(0, toolCallResponse("Working", ""), nil)
			fakeExpert.DoReturnsOnCall(1, textResponse("Done"), nil)

			result, err := run(config, reactree.TreeRequest{Goal: "safe"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
		})
	})

	Describe("Enterprise Toggles", func() {
		DescribeTable("dry-run toggle",
			func(dryRun bool) {
				config := reactree.DefaultTreeConfig()
				config.MaxIterations = 1
				config.Stages = nil
				config.Toggles.Features.DryRun.Enabled = dryRun
				fakeExpert.DoReturns(textResponse("toggled result"), nil)

				result, err := run(config, reactree.TreeRequest{Goal: "toggle task"})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status).To(Equal(reactree.Success))
			},
			Entry("dry-run enabled", true),
			Entry("dry-run disabled", false),
		)
	})

	Describe("Multi-stage", func() {
		It("runs stages with instructions", func() {
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 0
			config.Stages = []reactree.StageConfig{
				{Name: "Analyze", Instruction: "Read code."},
				{Name: "Implement", Instruction: "Apply changes."},
			}
			fakeExpert.DoReturns(textResponse("Stage result"), nil)

			result, err := run(config, reactree.TreeRequest{Goal: "refactor"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
		})

		It("supports ToolGetter", func() {
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 0
			config.Stages = []reactree.StageConfig{{Name: "S1"}}
			fakeExpert.DoReturns(textResponse("With getter"), nil)

			result, err := run(config, reactree.TreeRequest{
				Goal:       "getter task",
				ToolGetter: func() []tool.Tool { return nil },
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
		})

		DescribeTable("with dry-run toggle",
			func(dryRun bool) {
				config := reactree.DefaultTreeConfig()
				config.MaxIterations = 0
				config.Stages = []reactree.StageConfig{{Name: "Investigate"}}
				config.Toggles.Features.DryRun.Enabled = dryRun
				fakeExpert.DoReturns(textResponse("Stage toggled"), nil)

				result, err := run(config, reactree.TreeRequest{Goal: "multi-stage toggle"})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status).To(Equal(reactree.Success))
			},
			Entry("dry-run enabled", true),
			Entry("dry-run disabled", false),
		)
	})

	Describe("Tool budgets", func() {
		It("enforces budget", func() {
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 3
			config.Stages = nil
			config.ToolBudgets = map[string]int{"web_search": 1}
			fakeExpert.DoReturnsOnCall(0, toolCallResponse("Searched", ""), nil)
			fakeExpert.DoReturnsOnCall(1, textResponse("Done"), nil)

			result, err := run(config, reactree.TreeRequest{Goal: "budgeted"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
		})
	})

	Describe("Context cancellation", func() {
		It("handles gracefully", func() {
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 10
			config.Stages = nil
			callCount := 0
			fakeExpert.DoStub = func(_ context.Context, _ expert.Request) (expert.Response, error) {
				callCount++
				if callCount > 1 {
					return expert.Response{}, fmt.Errorf("context canceled")
				}
				return toolCallResponse("Working...", ""), nil
			}

			result, err := run(config, reactree.TreeRequest{Goal: "cancel"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Failure))
		})
	})

	Describe("Agent node branches", func() {
		It("records context budget from usage", func() {
			fakeExpert.DoReturns(textResponseWithUsage("With usage", 150), nil)
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 1
			config.Stages = nil

			result, err := run(config, reactree.TreeRequest{Goal: "usage"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ContextBudget.TotalTokens).To(Equal(150))
		})

		It("invokes hooks on context budget", func() {
			fakeExpert.DoReturns(textResponseWithUsage("Hooked", 600), nil)
			hookCalled := false
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 1
			config.Stages = nil
			config.Toggles.Hooks = &fakeExecutionHook{
				onContextBudget: func(_ context.Context, _ hooks.ContextBudgetEvent) {
					hookCalled = true
				},
			}

			_, err := run(config, reactree.TreeRequest{Goal: "hook budget"})
			Expect(err).NotTo(HaveOccurred())
			Expect(hookCalled).To(BeTrue())
		})

		It("send_message clears output", func() {
			// Arrange
			fakeExpert.DoReturns(toolCallResponse("mangled", "send_message"), nil)
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 1
			config.Stages = nil

			// Act
			result, err := run(config, reactree.TreeRequest{Goal: "send"})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Output).To(BeEmpty())
		})

		It("ask_clarifying_question keeps output", func() {
			// Arrange
			fakeExpert.DoReturns(toolCallResponse("useful answer", "ask_clarifying_question"), nil)
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 1
			config.Stages = nil

			// Act
			result, err := run(config, reactree.TreeRequest{Goal: "clarify"})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Output).To(Equal("useful answer"))
		})

		It("handles per-request working memory", func() {
			fakeExpert.DoReturns(textResponse("Per-req WM"), nil)
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 1
			config.Stages = nil
			reqWM := memory.NewWorkingMemory()
			reqWM.Store("key", "val")

			result, err := run(config, reactree.TreeRequest{Goal: "per-req WM", WorkingMemory: reqWM})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
		})

		It("handles per-request episodic memory", func() {
			fakeExpert.DoReturns(textResponse("Per-req ep"), nil)
			config := reactree.DefaultTreeConfig()
			config.MaxIterations = 1
			config.Stages = nil

			result, err := run(config, reactree.TreeRequest{
				Goal: "per-req ep", EpisodicMemory: &memoryfakes.FakeEpisodicMemory{},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
		})
	})

	Describe("ExpertReflector", func() {
		It("proceeds when expert says PROCEED", func() {
			fe := &expertfakes.FakeExpert{}
			fe.DoReturns(expert.Response{
				Choices: []model.Choice{{Message: model.Message{Content: "PROCEED"}}},
			}, nil)
			r, err := reactree.NewExpertReflector(fe).Reflect(context.Background(), reactree.ReflectionRequest{Goal: "test"})
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ShouldProceed).To(BeTrue())
		})

		It("halts when expert says HALT", func() {
			// Arrange
			fe := &expertfakes.FakeExpert{}
			fe.DoReturns(expert.Response{
				Choices: []model.Choice{{Message: model.Message{Content: "HALT"}}},
			}, nil)

			// Act
			r, err := reactree.NewExpertReflector(fe).Reflect(context.Background(), reactree.ReflectionRequest{Goal: "danger"})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ShouldProceed).To(BeFalse())
		})

		It("fail-open on error", func() {
			fe := &expertfakes.FakeExpert{}
			fe.DoReturns(expert.Response{}, fmt.Errorf("unavailable"))
			r, err := reactree.NewExpertReflector(fe).Reflect(context.Background(), reactree.ReflectionRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ShouldProceed).To(BeTrue())
		})

		It("proceeds on empty choices", func() {
			// Arrange
			fe := &expertfakes.FakeExpert{}
			fe.DoReturns(expert.Response{}, nil)

			// Act
			r, err := reactree.NewExpertReflector(fe).Reflect(context.Background(), reactree.ReflectionRequest{})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ShouldProceed).To(BeTrue())
		})
	})
})
