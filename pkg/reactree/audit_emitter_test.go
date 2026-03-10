// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/audit/auditfakes"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/reactree"
)

var _ = Describe("AuditHook", func() {
	var (
		ctx         context.Context
		fakeAuditor *auditfakes.FakeAuditor
		hook        *reactree.AuditHook
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeAuditor = &auditfakes.FakeAuditor{}
		hook = reactree.NewAuditHook(fakeAuditor)
	})

	Context("NewAuditHook", func() {
		It("should return nil when auditor is nil", func() {
			nilHook := reactree.NewAuditHook(nil)
			Expect(nilHook).To(BeNil())
		})

		It("should return a non-nil hook when auditor is provided", func() {
			Expect(hook).NotTo(BeNil())
		})
	})

	Context("OnIterationStart", func() {
		It("should log an iteration start event", func() {
			hook.OnIterationStart(ctx, hooks.IterationStartEvent{
				Goal:          "test goal",
				Iteration:     1,
				MaxIterations: 3,
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(1))

			_, req := fakeAuditor.LogArgsForCall(0)
			Expect(string(req.EventType)).To(Equal("reactree_iteration_start"))
			Expect(req.Actor).To(Equal("reactree"))
			Expect(req.Metadata["goal"]).To(Equal("test goal"))
			Expect(req.Metadata["iteration"]).To(Equal(1))
		})
	})

	Context("OnIterationEnd", func() {
		It("should log an iteration end event with status and tool counts", func() {
			hook.OnIterationEnd(ctx, hooks.IterationEndEvent{
				Iteration:      2,
				Status:         "success",
				ToolCallCounts: map[string]int{"read_file": 2, "shell": 1},
				TaskCompleted:  true,
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(1))

			_, req := fakeAuditor.LogArgsForCall(0)
			Expect(string(req.EventType)).To(Equal("reactree_iteration_end"))
			Expect(req.Metadata["status"]).To(Equal("success"))
			Expect(req.Metadata["task_completed"]).To(BeTrue())
		})
	})

	Context("OnToolValidation (critic rejection)", func() {
		It("should log a critic rejection event", func() {
			hook.OnToolValidation(ctx, hooks.ToolValidationEvent{
				ToolName: "dangerous_tool",
				Allowed:  false,
				Reason:   "blocked by policy",
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(1))

			_, req := fakeAuditor.LogArgsForCall(0)
			Expect(string(req.EventType)).To(Equal("reactree_critic_rejection"))
			Expect(req.Metadata["tool"]).To(Equal("dangerous_tool"))
			Expect(req.Metadata["reason"]).To(Equal("blocked by policy"))
		})

		It("should not log when tool is allowed", func() {
			hook.OnToolValidation(ctx, hooks.ToolValidationEvent{
				ToolName: "safe_tool",
				Allowed:  true,
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(0))
		})
	})

	Context("OnReflection", func() {
		It("should log a reflection result", func() {
			hook.OnReflection(ctx, hooks.ReflectionEvent{
				Iteration:     1,
				Monologue:     "Output looks safe, proceed.",
				ShouldProceed: true,
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(1))

			_, req := fakeAuditor.LogArgsForCall(0)
			Expect(string(req.EventType)).To(Equal("reactree_reflection"))
			Expect(req.Metadata["should_proceed"]).To(BeTrue())
			Expect(req.Metadata["monologue"]).To(ContainSubstring("safe"))
		})
	})

	Context("OnDryRun", func() {
		It("should log a dry run result", func() {
			hook.OnDryRun(ctx, hooks.DryRunEvent{
				PlannedSteps:  3,
				ToolsUsed:     []string{"read_file", "shell"},
				EstimatedCost: "medium",
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(1))

			_, req := fakeAuditor.LogArgsForCall(0)
			Expect(string(req.EventType)).To(Equal("reactree_dry_run"))
			Expect(req.Metadata["estimated_cost"]).To(Equal("medium"))
		})
	})

	Context("ChainHook integration", func() {
		It("should fan out events to multiple hooks", func() {
			fakeAuditor2 := &auditfakes.FakeAuditor{}
			hook2 := reactree.NewAuditHook(fakeAuditor2)

			chain := hooks.NewChainHook(hook, hook2)
			chain.OnIterationStart(ctx, hooks.IterationStartEvent{
				Goal:      "chain test",
				Iteration: 1,
			})

			Expect(fakeAuditor.LogCallCount()).To(Equal(1))
			Expect(fakeAuditor2.LogCallCount()).To(Equal(1))
		})
	})

	Context("NoOpHook safety", func() {
		It("should not panic when called", func() {
			noop := hooks.NoOpHook{}
			Expect(func() {
				noop.OnIterationStart(ctx, hooks.IterationStartEvent{})
				noop.OnIterationEnd(ctx, hooks.IterationEndEvent{})
				noop.OnReflection(ctx, hooks.ReflectionEvent{})
				noop.OnToolValidation(ctx, hooks.ToolValidationEvent{})
				noop.OnDryRun(ctx, hooks.DryRunEvent{})
				noop.OnPlanExecution(ctx, hooks.PlanExecutionEvent{})
			}).NotTo(Panic())
		})
	})
})
