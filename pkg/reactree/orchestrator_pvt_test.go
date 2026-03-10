// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package reactree

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("Orchestrator", func() {
	Describe("joinStepGoals", func() {
		It("formats steps into a numbered plan", func() {
			got := joinStepGoals([]PlanStep{
				{Name: "fetch", Goal: "get data"},
				{Name: "parse", Goal: "parse data"},
			})
			Expect(got).To(HavePrefix("Execute the following plan:"))
			Expect(got).To(ContainSubstring("1. fetch: get data"))
			Expect(got).To(ContainSubstring("2. parse: parse data"))
		})

		It("handles empty step list", func() {
			Expect(joinStepGoals(nil)).To(HavePrefix("Execute the following plan:"))
		})
	})

	Describe("buildSubAgentInstruction", func() {
		It("includes tool names", func() {
			result := buildSubAgentInstruction([]string{"run_shell", "read_file"})
			Expect(result).To(ContainSubstring("run_shell"))
			Expect(result).To(ContainSubstring("read_file"))
		})

		It("handles nil tool list", func() {
			Expect(buildSubAgentInstruction(nil)).NotTo(BeEmpty())
		})
	})

	Describe("ExecutePlan", func() {
		var (
			fakeExpert *expertfakes.FakeExpert
			fakeEp     *memoryfakes.FakeEpisodicMemory
			wm         *memory.WorkingMemory
			registry   *tools.Registry
		)

		BeforeEach(func() {
			fakeExpert = &expertfakes.FakeExpert{}
			fakeEp = &memoryfakes.FakeEpisodicMemory{}
			wm = memory.NewWorkingMemory()
			registry = tools.NewRegistry(context.Background())
		})

		successResponse := func() expert.Response {
			return expert.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: "assistant", Content: "result"}},
				},
			}
		}

		cfg := func(overrides ...func(*OrchestratorConfig)) OrchestratorConfig {
			c := OrchestratorConfig{MaxDecisions: 5}
			for _, fn := range overrides {
				fn(&c)
			}
			return c
		}

		It("returns error for empty plan", func() {
			// Arrange
			config := cfg(func(c *OrchestratorConfig) {
				c.Expert = fakeExpert
				c.ToolRegistry = registry
			})

			// Act
			_, err := ExecutePlan(context.Background(), Plan{}, config)

			// Assert
			Expect(err).To(MatchError("plan has no steps"))
		})

		DescribeTable("control flow types",
			func(flow ControlFlowType, stepCount int) {
				fakeExpert.DoReturns(successResponse(), nil)
				steps := make([]PlanStep, stepCount)
				for i := range steps {
					steps[i] = PlanStep{Name: fmt.Sprintf("s%d", i), Goal: "task"}
				}
				result, err := ExecutePlan(context.Background(), Plan{Flow: flow, Steps: steps}, OrchestratorConfig{
					Expert: fakeExpert, WorkingMemory: wm, Episodic: fakeEp,
					MaxDecisions: 5, ToolRegistry: registry,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Outputs).NotTo(BeEmpty())
			},
			Entry("single step (sequence)", ControlFlowSequence, 1),
			Entry("multi-step sequence", ControlFlowSequence, 2),
			Entry("parallel", ControlFlowParallel, 2),
			Entry("fallback", ControlFlowFallback, 2),
			Entry("unknown → sequence", ControlFlowType("unknown"), 1),
		)

		It("handles expert error", func() {
			fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("model error"))
			result, err := ExecutePlan(context.Background(),
				Plan{Flow: ControlFlowSequence, Steps: []PlanStep{{Name: "s", Goal: "fail"}}},
				OrchestratorConfig{Expert: fakeExpert, WorkingMemory: wm, Episodic: fakeEp,
					MaxDecisions: 5, ToolRegistry: registry})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(Failure))
		})

		It("stores results in working memory", func() {
			fakeExpert.DoReturns(successResponse(), nil)
			_, err := ExecutePlan(context.Background(),
				Plan{Flow: ControlFlowSequence, Steps: []PlanStep{{Name: "wm-step", Goal: "produce"}}},
				OrchestratorConfig{Expert: fakeExpert, WorkingMemory: wm, Episodic: fakeEp,
					MaxDecisions: 5, ToolRegistry: registry})
			Expect(err).NotTo(HaveOccurred())
			val, found := wm.Recall("plan_step:wm-step:result")
			Expect(found).To(BeTrue())
			Expect(val).NotTo(BeNil())
		})

		It("stores episode with episodic memory", func() {
			fakeExpert.DoReturns(successResponse(), nil)
			_, err := ExecutePlan(context.Background(),
				Plan{Flow: ControlFlowSequence, Steps: []PlanStep{{Name: "ep", Goal: "data"}}},
				OrchestratorConfig{Expert: fakeExpert, WorkingMemory: wm, Episodic: fakeEp,
					MaxDecisions: 5, ToolRegistry: registry})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeEp.StoreCallCount()).To(BeNumerically(">=", 1))
		})

		It("truncates long trajectory in episodic storage", func() {
			longOutput := ""
			for i := 0; i < 100; i++ {
				longOutput += fmt.Sprintf("Line %d analysis.\n", i)
			}
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{{Message: model.Message{Role: "assistant", Content: longOutput}}},
			}, nil)
			_, err := ExecutePlan(context.Background(),
				Plan{Flow: ControlFlowSequence, Steps: []PlanStep{{Name: "long", Goal: "lots"}}},
				OrchestratorConfig{Expert: fakeExpert, WorkingMemory: wm, Episodic: fakeEp,
					MaxDecisions: 5, ToolRegistry: registry})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeEp.StoreCallCount()).To(BeNumerically(">=", 1))
		})

		It("wraps tools with critic middleware", func() {
			fakeExpert.DoReturns(successResponse(), nil)
			fakeTool := &toolsfakes.FakeCallableTool{}
			fakeTool.DeclarationReturns(&tool.Declaration{Name: "read_file"})
			reg := tools.NewRegistry(context.Background(), &testToolProvider{t: []tool.Tool{fakeTool}})

			result, err := ExecutePlan(context.Background(),
				Plan{Flow: ControlFlowSequence, Steps: []PlanStep{{Name: "s", Goal: "critic"}}},
				OrchestratorConfig{Expert: fakeExpert, ToolRegistry: reg, MaxDecisions: 5,
					Toggles: Toggles{EnableCriticMiddleware: true}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(Success))
		})

		It("handles step with context field", func() {
			fakeExpert.DoReturns(successResponse(), nil)
			result, err := ExecutePlan(context.Background(),
				Plan{Flow: ControlFlowSequence, Steps: []PlanStep{
					{Name: "ctx", Goal: "analyze", Context: "Prior: 3 issues"}}},
				OrchestratorConfig{Expert: fakeExpert, WorkingMemory: wm,
					ToolRegistry: registry, MaxDecisions: 5})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(Success))
		})
	})
})
