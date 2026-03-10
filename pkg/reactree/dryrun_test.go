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
	"github.com/stackgenhq/genie/pkg/reactree"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("DryRun Simulation", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("DryRunToolWrapper", func() {
		It("should intercept calls and return simulated response", func() {
			realTool := &toolsfakes.FakeCallableTool{}
			realTool.DeclarationReturns(&tool.Declaration{Name: "read_file"})
			wrapper := reactree.NewDryRunToolWrapper(realTool)

			result, err := wrapper.Call(ctx, []byte(`{"path": "/foo.go"}`))
			Expect(err).NotTo(HaveOccurred())

			// Real tool should NOT have been called
			Expect(realTool.CallCallCount()).To(Equal(0))

			// Result should be a dry-run JSON stub
			resultStr, ok := result.(string)
			Expect(ok).To(BeTrue())
			Expect(resultStr).To(ContainSubstring("dry_run"))
			Expect(resultStr).To(ContainSubstring("read_file"))
		})

		It("should record invocations", func() {
			realTool := &toolsfakes.FakeCallableTool{}
			realTool.DeclarationReturns(&tool.Declaration{Name: "shell"})
			wrapper := reactree.NewDryRunToolWrapper(realTool)

			_, _ = wrapper.Call(ctx, []byte("{}"))
			_, _ = wrapper.Call(ctx, []byte("{}"))

			Expect(wrapper.Invocations()).To(HaveLen(2))
			Expect(wrapper.Invocations()[0]).To(Equal("shell"))
		})
	})

	Context("WrapToolsForDryRun", func() {
		It("should wrap all tools and collect unique invocations", func() {
			fake1 := &toolsfakes.FakeCallableTool{}
			fake1.DeclarationReturns(&tool.Declaration{Name: "read_file"})
			fake2 := &toolsfakes.FakeCallableTool{}
			fake2.DeclarationReturns(&tool.Declaration{Name: "write_file"})
			fake3 := &toolsfakes.FakeCallableTool{}
			fake3.DeclarationReturns(&tool.Declaration{Name: "shell"})
			tools := []tool.Tool{fake1, fake2, fake3}

			wrapped, collector := reactree.WrapToolsForDryRun(tools)
			Expect(wrapped).To(HaveLen(3))

			// Simulate calling two tools
			callable1, ok := wrapped[0].(tool.CallableTool)
			Expect(ok).To(BeTrue())
			_, _ = callable1.Call(ctx, []byte("{}"))

			callable2, ok := wrapped[2].(tool.CallableTool)
			Expect(ok).To(BeTrue())
			_, _ = callable2.Call(ctx, []byte("{}"))

			// Collector should return unique tool names
			invocations := collector()
			Expect(invocations).To(ConsistOf("read_file", "shell"))
		})
	})

	Context("BuildDryRunSummary", func() {
		It("should generate a low cost summary for few tools", func() {
			result := reactree.BuildDryRunSummary([]string{"read_file"}, 1)
			Expect(result.EstimatedCost).To(Equal("low"))
			Expect(result.PlannedSteps).To(Equal(1))
			Expect(result.Summary).To(ContainSubstring("Dry Run Simulation Report"))
		})

		It("should generate a medium cost summary for moderate tools", func() {
			tools := []string{"a", "b", "c", "d", "e", "f"}
			result := reactree.BuildDryRunSummary(tools, 2)
			Expect(result.EstimatedCost).To(Equal("medium"))
		})

		It("should generate a high cost summary for many tools/iterations", func() {
			tools := make([]string, 12)
			for i := range tools {
				tools[i] = "tool"
			}
			result := reactree.BuildDryRunSummary(tools, 6)
			Expect(result.EstimatedCost).To(Equal("high"))
		})
	})
})
