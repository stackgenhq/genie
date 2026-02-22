package reactree_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/reactree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockDryRunTool is a minimal callable tool used for dry run testing.
type mockDryRunTool struct {
	name   string
	called bool
}

func (m *mockDryRunTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: m.name}
}

func (m *mockDryRunTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	m.called = true
	return "real result", nil
}

var _ tool.CallableTool = (*mockDryRunTool)(nil)

var _ = Describe("DryRun Simulation", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("DryRunToolWrapper", func() {
		It("should intercept calls and return simulated response", func() {
			realTool := &mockDryRunTool{name: "read_file"}
			wrapper := reactree.NewDryRunToolWrapper(realTool)

			result, err := wrapper.Call(ctx, []byte(`{"path": "/foo.go"}`))
			Expect(err).NotTo(HaveOccurred())

			// Real tool should NOT have been called
			Expect(realTool.called).To(BeFalse())

			// Result should be a dry-run JSON stub
			resultStr, ok := result.(string)
			Expect(ok).To(BeTrue())
			Expect(resultStr).To(ContainSubstring("dry_run"))
			Expect(resultStr).To(ContainSubstring("read_file"))
		})

		It("should record invocations", func() {
			realTool := &mockDryRunTool{name: "shell"}
			wrapper := reactree.NewDryRunToolWrapper(realTool)

			_, _ = wrapper.Call(ctx, []byte("{}"))
			_, _ = wrapper.Call(ctx, []byte("{}"))

			Expect(wrapper.Invocations()).To(HaveLen(2))
			Expect(wrapper.Invocations()[0]).To(Equal("shell"))
		})
	})

	Context("WrapToolsForDryRun", func() {
		It("should wrap all tools and collect unique invocations", func() {
			tools := []tool.Tool{
				&mockDryRunTool{name: "read_file"},
				&mockDryRunTool{name: "write_file"},
				&mockDryRunTool{name: "shell"},
			}

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
