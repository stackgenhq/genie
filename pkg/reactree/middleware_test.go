package reactree_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/reactree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockCallableTool is a dummy tool for testing the middleware wrapper.
type mockCallableTool struct {
	name        string
	calledCount int
}

func (m *mockCallableTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: m.name}
}

func (m *mockCallableTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	m.calledCount++
	return "success", nil
}

var _ tool.CallableTool = (*mockCallableTool)(nil)

var _ = Describe("Critic Middleware", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("DeterministicValidator", func() {
		It("should allow unblocked tools", func() {
			validator := reactree.NewDeterministicValidator([]string{"rm", "drop"})
			err := validator.Validate(ctx, "ls", []byte("{}"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block blocklist tools", func() {
			validator := reactree.NewDeterministicValidator([]string{"drop_database"})
			err := validator.Validate(ctx, "drop_database", []byte("{}"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("prohibited by deterministic"))
		})
	})

	Context("ValidatingToolWrapper", func() {
		It("should forward calls if validator passes", func() {
			validator := reactree.NewDeterministicValidator([]string{"block_me"})
			mockTool := &mockCallableTool{name: "safe_tool"}

			wrapped := reactree.WrapWithValidator(mockTool, validator)

			// Needs to be asserted to CallableTool to call
			callable, ok := wrapped.(tool.CallableTool)
			Expect(ok).To(BeTrue())

			res, err := callable.Call(ctx, []byte("{}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal("success"))
			Expect(mockTool.calledCount).To(Equal(1))
		})

		It("should block calls and return error if validator fails", func() {
			validator := reactree.NewDeterministicValidator([]string{"unsafe_tool"})
			mockTool := &mockCallableTool{name: "unsafe_tool"}

			wrapped := reactree.WrapWithValidator(mockTool, validator)

			callable, ok := wrapped.(tool.CallableTool)
			Expect(ok).To(BeTrue())

			_, err := callable.Call(ctx, []byte("{}"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("action pruned by critic"))
			Expect(mockTool.calledCount).To(Equal(0)) // Tool should not have been called
		})

		It("should return unmodified tool if validator is nil", func() {
			mockTool := &mockCallableTool{name: "safe_tool"}
			wrapped := reactree.WrapWithValidator(mockTool, nil)
			Expect(wrapped).To(Equal(mockTool)) // Exact same pointer
		})
	})
})
